> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# 設計文件：Compose Runtime Adapter

## 狀態

approved

## Phase

- **Phase：** Phase 2
- **Phase Plan：** `docs/designs/phase_2/phase-2-plan.md`
- **依賴：**
  - `compose-port-allocator`（approved）
  - `compose-env-renderer`（approved）

---

## 目的

`ComposeAdapter` 實作 `domain.RuntimeAdapter` 介面，以 Docker Compose v2 作為 Supabase 專案的 runtime 後端。

核心職責：
1. **Create**：建立專案目錄，寫入 `docker-compose.yml`（embedded 靜態模板）與 `.env`（由 `ComposeEnvRenderer` 產生）
2. **Start**：啟動容器，輪詢健康狀態直至健康或逾時，回傳 `StartError`（含健康快照）
3. **Stop**：優雅停止所有容器
4. **Destroy**：移除所有容器與 volume，刪除專案目錄
5. **Status**：執行 `docker compose ps --format json`，解析回傳 `*ProjectHealth`
6. **RenderConfig**：純計算，委派給 `ConfigRenderer`，不寫入磁碟
7. **ApplyConfig**：重新寫入 `.env`，若容器運行中則執行 `docker compose up -d` 重新協調

---

## 範疇

| 在範疇內 | 在範疇外 |
|----------|----------|
| `ComposeAdapter` struct 定義與建構子 | `docker-compose.yml` 模板具體內容（另立 task） |
| 所有 7 個 RuntimeAdapter 方法的執行流程 | PortAllocator 呼叫（use-case 層負責） |
| `cmdRunner` 介面（exec.Command 抽象） | ConfigRepository 互動（use-case 層負責） |
| docker compose ps 輸出解析 → `ProjectHealth` | K8s Adapter（Phase 6） |
| 測試策略（fake cmdRunner、tempDir）| 多節點/叢集場景 |

---

## 目錄結構

```
control-plane/internal/adapter/compose/
├── adapter.go             # ComposeAdapter struct + 所有方法
├── cmd_runner.go          # cmdRunner 介面 + osCmdRunner 實作
├── status_parser.go       # docker compose ps JSON 解析 → ProjectHealth
├── templates/
│   └── docker-compose.yml # 靜態 embedded 模板（所有動態值由 .env 提供）
├── adapter_test.go        # 單元測試（fake cmdRunner + tempDir）
├── status_parser_test.go  # 解析邏輯單元測試
└── mock_test.go           # 測試用 mock（package compose）
```

### 專案運行目錄

```
<projectsDir>/<slug>/
├── docker-compose.yml     ← 來自 embedded 模板（Create 時寫入，不再更新）
└── .env                   ← 來自 ComposeEnvRenderer（Create/ApplyConfig 時寫入）
```

預設 `projectsDir` 由 `NewComposeAdapter` 接收，生產環境傳入 `~/.supabase-cp/projects`。

---

## 資料模型與介面

### ComposeAdapter struct

```go
// ComposeAdapter implements domain.RuntimeAdapter using Docker Compose v2.
// projectsDir is the base directory; each project has a subdirectory named by its slug.
// The embedded docker-compose.yml is written once at Create time and never modified.
type ComposeAdapter struct {
    projectsDir string
    renderer    domain.ConfigRenderer
    runner      cmdRunner  // white-box: testable exec.Command abstraction
}

// Static interface assertion — fails to compile if ComposeAdapter no longer implements RuntimeAdapter.
var _ domain.RuntimeAdapter = (*ComposeAdapter)(nil)
```

### 建構子

```go
// NewComposeAdapter returns a ComposeAdapter backed by osCmdRunner.
// projectsDir is the base directory for all project subdirectories.
// renderer is typically *ComposeEnvRenderer.
func NewComposeAdapter(projectsDir string, renderer domain.ConfigRenderer) *ComposeAdapter

// newComposeAdapterWithRunner is a white-box constructor used in tests to inject a fake runner.
func newComposeAdapterWithRunner(projectsDir string, renderer domain.ConfigRenderer, runner cmdRunner) *ComposeAdapter
```

### cmdRunner 介面

```go
// cmdRunner abstracts exec.Command; allows test injection without spawning real processes.
type cmdRunner interface {
    // Run executes name with args in dir, combining stdout+stderr.
    // Returns combined output and error (non-zero exit wraps as *exec.ExitError).
    Run(ctx context.Context, dir, name string, args ...string) ([]byte, error)
}

// osCmdRunner is the production implementation using exec.CommandContext.
type osCmdRunner struct{}
```

`osCmdRunner.Run` 設定 `Cmd.Dir = dir`，以 `exec.CommandContext(ctx, name, args...)` 執行，並以 `cmd.CombinedOutput()` 捕捉輸出。

### docker-compose.yml 模板（embedded）

```go
//go:embed templates/docker-compose.yml
var composeTemplate []byte
```

模板為靜態檔案（非 Go template），所有動態值由 `.env` 透過 Docker Compose v2 原生 env-var 替換提供（`${VAR}` 語法）。模板引用的 env var 完全來自 `config_schema.go` 的 key。詳細模板內容另立 task 定義。

---

## 執行流程

### Create(ctx, project, config)

```
前置條件：project.Status == creating
後置條件：成功 → 磁碟上有完整的 project dir；失敗 → 返回 AdapterError（目錄可能殘留）
```

1. 計算 `projectDir = filepath.Join(projectsDir, project.Slug)`
2. 呼叫 `renderer.Render(config)` → `[]Artifact`（先計算，在任何磁碟操作前完成驗證）
3. `os.MkdirAll(projectDir, 0700)` — 若已存在則為 idempotent
4. 對每個 Artifact，`os.WriteFile(filepath.Join(projectDir, artifact.Path), artifact.Content, artifact.Mode)`
5. `os.WriteFile(filepath.Join(projectDir, "docker-compose.yml"), composeTemplate, 0644)`
6. 不執行 `docker compose pull`（避免 Create 耗時過長；pull 可在 Start 前由 use-case 層選擇性觸發）
7. 成功回傳 `nil`；任何步驟失敗回傳 `&domain.AdapterError{Operation: "create", Slug: project.Slug, Err: err}`

> **注意**：renderer.Render 在 MkdirAll 之前執行（Fail-fast before side-effects）。若 renderer 失敗，目錄不會被建立。不在 `Create` 內呼叫 `docker compose`，不需要 Docker daemon 可用。

### Start(ctx, project)

```
前置條件：project.Status == starting（由 use-case 設定後呼叫）
後置條件：成功 → 所有服務健康；失敗 → StartError（含健康快照）
```

1. `projectDir = filepath.Join(projectsDir, project.Slug)`
2. 執行 `runner.Run(ctx, projectDir, "docker", "compose", "up", "-d")`
3. 若 `runner.Run` 失敗 → `return &domain.StartError{Slug: project.Slug, Err: err, Health: nil}`
4. 輪詢健康：每 5 秒呼叫一次 `Status(ctx, project)`，共至多 24 次 tick（即 24 × 5s = 120 秒上限）
   - 每次輪詢前檢查 `ctx.Done()`；若 canceled → 回傳 `ctx.Err()`
   - 若 `health.IsHealthy()` → 回傳 `nil`（成功）
5. 120 秒內仍未健康 → 回傳 `&domain.StartError{Slug: project.Slug, Err: domain.ErrServiceNotHealthy, Health: health}`

> **健康輪詢實作**：使用 `time.NewTicker(5 * time.Second)` + `select` on `ticker.C` / `ctx.Done()`。不使用 `time.Sleep`（阻塞 context 取消偵測）。

### Stop(ctx, project)

```
前置條件：project.Status == stopping
後置條件：成功 → 所有容器停止（檔案保留）
```

1. `projectDir = filepath.Join(projectsDir, project.Slug)`
2. `runner.Run(ctx, projectDir, "docker", "compose", "stop")`
3. 失敗 → `&domain.AdapterError{Operation: "stop", Slug: project.Slug, Err: err}`

> `docker compose stop` 預設 10 秒 graceful timeout（Docker 預設），不需 adapter 層額外控制。

### Destroy(ctx, project)

```
前置條件：project.Status == destroying
後置條件：成功 → 容器 + volume + 目錄全部移除
```

1. `projectDir = filepath.Join(projectsDir, project.Slug)`
2. `downErr := runner.Run(ctx, projectDir, "docker", "compose", "down", "-v", "--remove-orphans")`（不論結果，繼續執行清理）
3. `cleanupErr := os.RemoveAll(projectDir)` — 移除整個專案目錄（best-effort，無論 Step 2 是否失敗都執行）
4. 若 `downErr != nil` → 回傳 `&domain.AdapterError{Operation: "destroy", Slug: project.Slug, Err: downErr}`
5. 若 `cleanupErr != nil` → 回傳 `&domain.AdapterError{Operation: "destroy:cleanup", Slug: project.Slug, Err: cleanupErr}`

> **Best-effort cleanup**：Step 2 與 Step 3 之間無 early return，確保即使 `docker compose down` 失敗，目錄仍被嘗試移除。`downErr` 優先回傳，因為它指向 Docker daemon 層的根本原因。

### Status(ctx, project)

```
前置條件：任意狀態
後置條件：回傳當前時間點的 ProjectHealth 快照
```

1. `projectDir = filepath.Join(projectsDir, project.Slug)`
2. `out, err = runner.Run(ctx, projectDir, "docker", "compose", "ps", "--format", "json")`
3. 若 runner 失敗 → `nil, &domain.AdapterError{Operation: "status", ...}`
4. 呼叫 `parseComposePS(out)` → `*domain.ProjectHealth`
5. 回傳 health, nil

#### parseComposePS 規格

`docker compose ps --format json`（Compose v2.17+）輸出為 **NDJSON**（每行一個 JSON 物件）：

```json
{"ID":"abc123","Name":"myproject-db-1","Service":"db","State":"running","Health":"healthy"}
{"ID":"def456","Name":"myproject-kong-1","Service":"kong","State":"running","Health":""}
```

解析規則：
- `State == "running"` + `Health == "healthy"` → `ServiceStatusHealthy`
- `State == "running"` + `Health == "starting"` → `ServiceStatusStarting`
- `State == "running"` + `Health == ""` → `ServiceStatusHealthy`（無 healthcheck 的服務視為健康）
- `State == "running"` + `Health == "unhealthy"` → `ServiceStatusUnhealthy`
- `State == "exited"` → `ServiceStatusStopped`
- 其他 → `ServiceStatusUnknown`

若輸出為空（所有容器皆未建立）→ 回傳空 `Services` map（不報錯）。
若某行 JSON 解析失敗 → 記錄 warn log，跳過該行，繼續處理其他行。

### RenderConfig(ctx, project, config)

```
前置條件：任意狀態
後置條件：純計算，不修改磁碟或任何外部狀態
```

1. 直接委派：`return renderer.Render(config)`
2. 任何錯誤直接 pass-through（不包裝 AdapterError，因為這是純渲染層的責任）

### ApplyConfig(ctx, project, config)

```
前置條件：project dir 必須已存在（即 Create 已執行過）
後置條件：磁碟上 .env 已更新；若容器運行中則已協調
```

1. 呼叫 `renderer.Render(config)` → `[]Artifact`
2. 對每個 Artifact，`os.WriteFile(filepath.Join(projectDir, artifact.Path), artifact.Content, artifact.Mode)`
3. 若容器運行中（`project.Status == domain.ProjectStatusRunning`）：
   - `runner.Run(ctx, projectDir, "docker", "compose", "up", "-d")` — Compose 偵測到 .env 變更並重建受影響的容器
4. 失敗 → `&domain.AdapterError{Operation: "apply-config", Slug: project.Slug, Err: err}`

> `docker-compose.yml` 在 ApplyConfig 時**不重寫**（模板永遠不變；若需更新 compose 檔案，需重新 Destroy + Create）。

---

## 錯誤處理

| 情境 | 回傳 |
|------|------|
| renderer.Render 失敗 | `domain.AdapterError{Operation: "create"/"apply-config", ...}`；`RenderConfig` 則 pass-through |
| os.MkdirAll / os.WriteFile 失敗 | `domain.AdapterError{Operation: "create"/"apply-config", ...}` |
| runner.Run 失敗（非零 exit） | `domain.AdapterError{...}` 或 `domain.StartError{...}` |
| Start 輪詢逾時 | `domain.StartError{Err: domain.ErrServiceNotHealthy, Health: <snapshot>}` |
| Context canceled 在輪詢中 | `ctx.Err()` |
| parseComposePS 解析失敗（單行）| warn log + skip（不回傳 error） |
| projectDir 不存在（ApplyConfig）| `os.WriteFile` 自然失敗 → AdapterError |

> `AdapterError` 本身實作了 `error` 介面（`Error()` 已格式化訊息），直接以 `&domain.AdapterError{Operation: op, Slug: slug, Err: err}` 初始化即可，**不額外以 `fmt.Errorf` 包裝**（否則造成 double-wrap）。

---

## 測試策略

### 單元測試（無外部依賴）

使用：
- `t.TempDir()` 作為 `projectsDir`（真實檔案系統，不需 afero）
- `fakeCmdRunner`（white-box，注入 `newComposeAdapterWithRunner`）
- `mockConfigRenderer`（white-box，注入 renderer）

```go
// mock_test.go（package compose）

type fakeCmdRunner struct {
    RunFn func(ctx context.Context, dir, name string, args ...string) ([]byte, error)
    Calls []fakeCall
}

type fakeCall struct {
    Dir  string
    Name string
    Args []string
}

func (f *fakeCmdRunner) Run(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
    f.Calls = append(f.Calls, fakeCall{Dir: dir, Name: name, Args: args})
    if f.RunFn != nil {
        return f.RunFn(ctx, dir, name, args...)
    }
    return nil, nil
}

type mockConfigRenderer struct {
    RenderFn func(config *domain.ProjectConfig) ([]domain.Artifact, error)
}

func (m *mockConfigRenderer) Render(config *domain.ProjectConfig) ([]domain.Artifact, error) {
    if m.RenderFn != nil {
        return m.RenderFn(config)
    }
    return []domain.Artifact{{Path: ".env", Content: []byte("KEY=val\n"), Mode: 0600}}, nil
}
```

### 測試案例

#### adapter_test.go

| 測試名稱 | 驗證項目 |
|----------|----------|
| `TestCreate_WritesFilesToDisk` | projectDir 建立、.env 內容正確、docker-compose.yml 為 embedded 內容 |
| `TestCreate_RendererError` | renderer 失敗 → 回傳 AdapterError，projectDir **不建立**（因 Render 在 MkdirAll 之前）|
| `TestCreate_MkdirAllError` | MkdirAll 失敗（eg. 唯讀 FS）→ AdapterError，不寫入任何檔案 |
| `TestStart_Success` | runner 呼叫順序：`up -d` → 輪詢 Status → IsHealthy → nil |
| `TestStart_UpFailure` | `up -d` 失敗 → StartError{Err: cmd error} |
| `TestStart_HealthTimeout` | 輪詢 24 次 tick 皆未健康（逾時）→ StartError{Err: ErrServiceNotHealthy, Health: snapshot} |
| `TestStart_ContextCanceled` | ctx 取消 → ctx.Err() |
| `TestStop_RunsComposeStop` | runner 呼叫為 `docker compose stop`，projectDir 正確 |
| `TestStop_RunnerError` | 回傳 AdapterError |
| `TestDestroy_RemovesContainersAndDir` | runner 呼叫 `down -v --remove-orphans`，dir 被刪除 |
| `TestDestroy_DownFailureStillCleansDir` | down 失敗但 dir 被嘗試刪除；回傳 down 的錯誤 |
| `TestStatus_ParsesHealthy` | NDJSON 輸出 → 所有服務 healthy |
| `TestStatus_EmptyOutput` | 空輸出 → 空 Services map，無 error |
| `TestStatus_RunnerError` | AdapterError |
| `TestRenderConfig_Delegates` | 直接 pass-through renderer 結果，不寫磁碟 |
| `TestApplyConfig_WritesAndReconciles` | 寫 .env，若 running 則呼叫 `up -d` |
| `TestApplyConfig_WritesOnly_WhenStopped` | stopped 時只寫檔案，不呼叫 runner |

#### status_parser_test.go

| 測試名稱 | 驗證項目 |
|----------|----------|
| `TestParseComposePS_AllHealthy` | healthy state → ServiceStatusHealthy |
| `TestParseComposePS_NoHealthcheck` | Health=="" && State=="running" → ServiceStatusHealthy |
| `TestParseComposePS_Unhealthy` | Health=="unhealthy" → ServiceStatusUnhealthy |
| `TestParseComposePS_Starting` | Health=="starting" → ServiceStatusStarting |
| `TestParseComposePS_Exited` | State=="exited" → ServiceStatusStopped |
| `TestParseComposePS_EmptyOutput` | 空輸出 → 空 Services map |
| `TestParseComposePS_MalformedLine` | 單行 JSON 錯誤 → 該行跳過，其他行正常解析 |

### CI 執行方式

- 單元測試：`go test -race ./internal/adapter/compose/...`（無外部依賴，所有測試 CI 中執行）
- 整合測試：`go test -race -tags integration ./internal/adapter/compose/...`（需要 Docker daemon）

---

## 開放議題

| # | 議題 | 優先級 | 備註 |
|---|------|--------|------|
| 1 | `docker-compose.yml` 模板具體內容 | 高 | 另立 task；本設計僅規定為 embedded 靜態檔案 |
| 2 | `Start` 輪詢間隔 / 超時可否設定 | 低 | Phase 2 hard-code 5s/120s；Phase 5 可加 option |
| 3 | `Create` 是否需要 `docker compose pull` | 低 | 建議 Phase 2 跳過，以加速初始設定；pull 另加 `Prepare` 方法或由 use-case 層處理 |
| 4 | Compose v2 版本下限 | 中 | `--format json` 在 v2.17+ 改為 NDJSON；需在 README 記錄最低版本要求 |
| 5 | `Destroy` 是否 `os.RemoveAll` 在 docker down 失敗時執行 | 中 | 目前設計：down 失敗仍嘗試 RemoveAll，回傳 down 的錯誤 |

---

## 審查結果

### Reviewer A（架構）

**[Round 2] 狀態：APPROVED**
**意見：** 五項必修問題全部解決。

1. ✅ 所有 `Cause:` → `Err:` 已正確替換（流程描述 + 錯誤表共 7 處，均使用 `Err:`）
2. ✅ Create 流程順序已調整：`renderer.Render` 在 `os.MkdirAll` 之前執行；`TestCreate_RendererError` 斷言也同步更新為「projectDir 不建立」，邏輯自洽
3. ✅ Destroy best-effort 模式清晰：`downErr` 與 `cleanupErr` 分別賦值，兩步驟之間無 early return，`downErr` 優先回傳並有明確理由（Docker daemon 根本原因）
4. ✅ 錯誤處理注意事項已更新為「不額外以 `fmt.Errorf` 包裝，否則造成 double-wrap」
5. ✅ 輪詢次數統一為「24 次 tick」（規格第 4 步與 `TestStart_HealthTimeout` 一致）

遺留 🟡 項目（Round 1 #4–#8）未在本次修正範圍內，均為非阻擋性建議，可在後續實作 task 或 PR 中處理。實作可基於此文件進行。

---

**[Round 1] 狀態：REVISE**

**意見：**

整體設計架構清晰，方法職責分離正確，embedded static template 策略合理，health polling 選用 `time.NewTicker` + `ctx.Done()` 而非 `time.Sleep` 是正確做法。但以下問題必須修正後才可 APPROVE。

---

**🔴 必修（阻擋 APPROVE）**

**1. `AdapterError` 與 `StartError` 欄位名稱錯誤**

設計文件全篇使用 `Cause` 欄位（例如 `&domain.AdapterError{Operation: "create", Slug: project.Slug, Err: err}`），但 `runtime_adapter.go` 中兩個型別的欄位實際上都是 `Err`：

```go
// 實際定義（runtime_adapter.go）
type AdapterError struct { Operation string; Slug string; Err error }
type StartError   struct { Slug string; Health *ProjectHealth; Err error }
```

文件中所有 `Cause: err` 均需改為 `Err: err`。此錯誤若直接進入實作會造成編譯失敗。

**2. Destroy 流程描述自相矛盾**

流程步驟 3 說「若 runner 失敗 → 回傳 `AdapterError`」（隱含 early return），步驟 4 又說「Step 4 無論 Step 2 是否成功都執行」。兩者矛盾——若 step 3 直接 return，step 4 永遠不會執行。

應明確選擇一種策略並清楚描述控制流程，建議：

```
2. runner.Run("down -v --remove-orphans")  → 記錄 downErr（不 early return）
3. os.RemoveAll(projectDir)               → 記錄 cleanErr（不 early return）
4. 若 downErr != nil → 回傳 downErr（AdapterError{Operation:"destroy"}）
5. 若 cleanErr != nil → 回傳 cleanErr（AdapterError{Operation:"destroy:cleanup"}）
6. 回傳 nil
```

**3. `TestCreate_RendererError` 測試斷言描述錯誤**

測試說「renderer 失敗 → 回傳 AdapterError，**不建立目錄**」，但依 Create 流程，`os.MkdirAll` 在 step 2 就執行，renderer.Render 在 step 3 才呼叫——renderer 失敗時目錄**已存在**。後置條件說明（「目錄可能殘留」）正確，但測試描述矛盾，實作者會寫出錯誤斷言。需修正測試案例說明為「renderer 失敗 → 回傳 AdapterError，目錄可能已建立」。

---

**🟡 建議修正（不阻擋但應說明）**

**4. Stop 指令與介面合約不一致**

`runtime_adapter.go` 介面註解說 Stop 執行 `docker compose down (no -v)`，但設計文件實作為 `docker compose stop`。兩者行為不同：`stop` 保留容器（較快重啟），`down` 移除容器。設計選用 `stop` 語意上更正確，但應在文件中明確說明此偏離（或請求更新介面定義的 comment）。

**5. `RenderConfig` 忽略 `ctx` 與 `project` 參數**

`RenderConfig(ctx, project, config)` 的委派為 `return renderer.Render(config)`，`ctx` 與 `project` 未使用。若 renderer 是純計算這沒問題，但應加一行說明（`_ = ctx` 的處理或 renderer 介面不接受 ctx 的理由），避免實作者困惑。

**6. Start 逾時 120s 與 Phase Plan 建議 5 分鐘有落差**

Phase 2 Plan 的風險表明確記載「Supabase 服務冷啟動較慢，建議預設 timeout 5 分鐘」，但設計 hard-code 120s。開放議題 #2 已標記為低優先，建議至少在文件中補充冷啟動（first-time image pull）場景下 120s 不足的說明，並說明 Phase 2 接受此限制的理由（例如「假設 pull 已在 Create 前完成」或「Phase 5 再參數化」）。

**7. `parseComposePS` 未定義 `State == "restarting"` 的映射**

`restarting` 在語意上最接近 `ServiceStatusStarting`，不應落入 `ServiceStatusUnknown`。建議明確補充此映射。

**8. NDJSON 格式版本邊界**

開放議題 #4 已記載「v2.17+ 才是 NDJSON」，但沒有說明舊版輸出為 JSON Array 時 parser 的行為（靜默失敗 vs 明確錯誤）。建議補充：若第一行無法作為 JSON object 解析，可考慮 fallback 偵測並回傳明確的版本不支援錯誤。

---

**✅ 確認正確的設計決策**

- `ComposeAdapter` 不擁有 PortAllocator，僅接收 renderer；PortAllocator 由 use-case 層呼叫 ✅
- `RenderConfig` 為純計算（不寫磁碟）與 `ApplyConfig` 的職責分離正確 ✅
- `Create` 不執行 `docker compose`，純磁碟操作，不依賴 Docker daemon ✅
- Embedded static template + `.env` env-var 替換，避免 Go template 在 YAML 中的轉義問題 ✅
- `fakeCmdRunner` 以 white-box constructor 注入，`t.TempDir()` 作為真實 FS 測試 ✅
- `IsHealthy()` 在 Services 為空時回傳 false，Start 輪詢可正確處理容器尚未建立的狀態 ✅
- 靜態介面斷言 `var _ domain.RuntimeAdapter = (*ComposeAdapter)(nil)` ✅

### Reviewer B（實作）

**[Round 2] 狀態：APPROVED**
**意見：**

五個前輪問題全數確認修正。Mock nil guard 正確。另發現一個新 🟡 小問題，不阻擋 APPROVE。

---

**✅ 前輪 5 項問題確認修正**

- **[B1] ✅** 所有 `AdapterError` / `StartError` 初始化均使用 `Err:`，doc 主體中無 `Cause:` 殘留。
- **[B2] ✅** Create 流程已調整為「renderer.Render → MkdirAll」；`TestCreate_RendererError` 說明同步更新為「不建立目錄（因 Render 在 MkdirAll 之前）」，邏輯自洽。
- **[B3] ✅** Destroy 流程明確使用 `downErr` / `cleanupErr` 分開賦值，兩步驟之間無 early return，`downErr` 優先回傳並附帶理由（Docker daemon 根本原因）。
- **[B4] ✅** 錯誤處理注意事項已更正：「直接以 `&domain.AdapterError{...}` 初始化即可，不額外以 `fmt.Errorf` 包裝（否則造成 double-wrap）」。
- **[B5] ✅** 流程（步驟 4）與 `TestStart_HealthTimeout` 均一致使用「24 次 tick」。

---

**✅ Mock nil guard 確認**

`fakeCmdRunner.Run` 於 `f.RunFn != nil` 後才呼叫，default 回傳 `nil, nil`；`mockConfigRenderer.Render` 同樣於 `m.RenderFn != nil` 後才呼叫，default 回傳標準 `.env` artifact。兩者 nil guard 正確，與 `MockRuntimeAdapter` 風格一致。

---

**🟡 [B6] 錯誤處理表「renderer.Render 失敗」條目與流程/測試不一致（建議修正）**

- **錯誤處理表（§錯誤處理，第 1 行）**：`renderer.Render 失敗 → 直接回傳 renderer error（Create/ApplyConfig）`
- **Create 流程 step 7**：`任何步驟失敗回傳 &domain.AdapterError{Operation: "create", ...}`
- **`TestCreate_RendererError` 測試描述**：「renderer 失敗 → 回傳 AdapterError」

表格說「直接回傳 renderer error（不包裝）」，但流程說明與測試案例都說回傳 `AdapterError`。兩者矛盾，實作者會困惑。建議將錯誤表該行修正為：

```
renderer.Render 失敗 | &domain.AdapterError{Operation: "create"/"apply-config", Err: rendererErr}
（RenderConfig 為 pass-through，不包裝）
```

此問題不阻擋 APPROVE（流程與測試本身一致，錯誤在表格），但需在實作前修正以免產生歧義。

---

**[Round 1] 狀態：REVISE**
**意見：**

整體結構清晰，struct 設計可建置，`cmdRunner` 介面完整可測，NDJSON 解析策略正確，健康輪詢 `time.NewTicker + select` 模式正確，靜態介面斷言存在，`t.TempDir()` 策略充分，`fakeCmdRunner` / `mockConfigRenderer` 的 nil guard 與 `MockRuntimeAdapter` 風格一致。但以下三個 **Blocker** 與兩個 **Major** 問題必須修正後才能進入實作。

---

**🔴 [B1] `AdapterError` / `StartError` struct 欄位名稱錯誤（Compile-Time Blocker）**

設計中所有錯誤構造範例均使用 `Cause:` 欄位（例如 `&domain.AdapterError{Operation: "create", Slug: project.Slug, Err: err}`），但 `runtime_adapter.go` 中 `AdapterError` 與 `StartError` 的底層錯誤欄位實際名稱為 `Err`，而非 `Cause`：

```go
// runtime_adapter.go
type AdapterError struct {
    Operation string
    Slug      string
    Err       error   // ← 欄位名為 Err，非 Cause
}

type StartError struct {
    Slug   string
    Health *ProjectHealth
    Err    error   // ← 欄位名為 Err，非 Cause
}
```

設計文件中 **全部** 涉及 `AdapterError` 與 `StartError` 初始化的程式碼片段（流程描述與錯誤表共 8 處）須將 `Cause:` 改為 `Err:`。此問題會導致每個方法的實作在編譯時直接失敗。

---

**🔴 [B2] `TestCreate_RendererError` 斷言「不建立目錄」與流程矛盾**

Create 流程為：

1. 計算 `projectDir`
2. **`os.MkdirAll(projectDir, 0700)`** ← 目錄在此建立
3. 呼叫 `renderer.Render(config)` ← renderer 在此失敗

當 renderer 失敗（步驟 3）時，目錄**已在步驟 2 建立**。測試案例 `TestCreate_RendererError` 描述「renderer 失敗 → 回傳 AdapterError，不建立目錄」中的「不建立目錄」斷言是錯的，實際跑測試時會失敗。

修正方案（擇一）：
- **方案 A（建議）**：將流程改為先 `renderer.Render` 再 `os.MkdirAll`，使失敗時確實不建立目錄，並更新測試描述。
- **方案 B**：保留現有流程（MkdirAll 先），將測試描述改為「回傳 AdapterError；目錄可能已建立（不做 cleanup）」。

---

**🔴 [B3] Destroy 流程自我矛盾（early return vs. best-effort cleanup）**

Destroy 流程的步驟 3 寫道：「若 runner 失敗 → `return &domain.AdapterError{...}`」，這是 early return。  
但流程末尾的注意事項寫道：「Step 4 無論 Step 2 是否成功都執行（attempt best-effort cleanup）」。

兩者互相矛盾：若步驟 3 直接 return，步驟 4 的 `os.RemoveAll` 永遠不會執行。設計必須明確描述實作模式，例如：

```go
// 建議的 pseudo-code
downErr := runner.Run(ctx, projectDir, "docker", "compose", "down", "-v", "--remove-orphans")
if rmErr := os.RemoveAll(projectDir); rmErr != nil {
    return &domain.AdapterError{Operation: "destroy:cleanup", Slug: project.Slug, Err: rmErr}
}
if downErr != nil {
    return &domain.AdapterError{Operation: "destroy", Slug: project.Slug, Err: downErr}
}
return nil
```

同時需要說明：若 down 失敗且 RemoveAll 也失敗，回傳哪個錯誤（建議回傳 RemoveAll 的 cleanup 錯誤，因為它更持久且需要人工介入）。

---

**🟡 [B4] `fmt.Errorf` 包裝說明有誤（會造成 double-wrap）**

錯誤處理表末尾的注意事項：
> 所有 `AdapterError` 均以 `fmt.Errorf("compose adapter %s %q: %w", op, slug, cause)` 形式 wrap。

這與 `AdapterError.Error()` 的實作衝突：`AdapterError` 本身已透過 `Error()` 方法格式化輸出（`"adapter %s failed for project %q: %v"`）。若實作者先 `fmt.Errorf(...)` 包裝再放入 `Err` 欄位，將造成訊息被雙重包裝（`"adapter destroy failed for project "x": compose adapter destroy "x": original error"`）。

應將此注意事項刪除，或改為：「`AdapterError.Err` 欄位存放原始 cause；格式化輸出由 `AdapterError.Error()` 負責，無需額外 `fmt.Errorf` 包裝。」

---

**🟡 [B5] `TestStart_HealthTimeout` 輪詢次數與規格不一致**

規格說「至多 24 次」（24 × 5s = 120s），但 `TestStart_HealthTimeout` 描述「輪詢 25 次（逾時）」。  
- 若 fakeCmdRunner 設計為回傳 unhealthy 25 次，而實際最多只輪詢 24 次，第 25 次永遠不會被呼叫，`Calls` 只有 24 筆，斷言 25 次會失敗。  
- 請統一：規格與測試都使用 24（或改規格為 "不超過 24 次 tick 後逾時"，測試 runner 只需被呼叫 ≥ 1 次並確認回傳 `StartError` 即可）。

---

**✅ 實作亮點**

- `var _ domain.RuntimeAdapter = (*ComposeAdapter)(nil)` 靜態介面斷言存在 ✅  
- `fakeCmdRunner` / `mockConfigRenderer` nil guard 與 `MockRuntimeAdapter` 風格一致 ✅  
- `time.NewTicker + select` 健康輪詢模式正確，可正確偵測 context 取消 ✅  
- NDJSON 解析策略（逐行 decode + warn-and-skip 機制）技術上正確 ✅  
- `t.TempDir()` 無外部 fs 依賴，`newComposeAdapterWithRunner` white-box constructor 設計正確 ✅  
- `RenderConfig` 不包裝 `AdapterError` 的設計理由清晰 ✅  
- `os.WriteFile` 用於 embedded template，`composeTemplate` `//go:embed` 設計完整 ✅
