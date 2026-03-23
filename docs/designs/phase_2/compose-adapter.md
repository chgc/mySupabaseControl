> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# 設計文件：Compose Runtime Adapter

## 狀態

design_in_progress

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
2. `os.MkdirAll(projectDir, 0700)` — 若已存在則為 idempotent
3. 呼叫 `renderer.Render(config)` → `[]Artifact`
4. 對每個 Artifact，`os.WriteFile(filepath.Join(projectDir, artifact.Path), artifact.Content, artifact.Mode)`
5. `os.WriteFile(filepath.Join(projectDir, "docker-compose.yml"), composeTemplate, 0644)`
6. 不執行 `docker compose pull`（避免 Create 耗時過長；pull 可在 Start 前由 use-case 層選擇性觸發）
7. 成功回傳 `nil`；任何步驟失敗回傳 `&domain.AdapterError{Operation: "create", Slug: project.Slug, Cause: err}`

> **注意**：不在 `Create` 內呼叫 `docker compose`。`Create` 純粹為磁碟操作，不需要 Docker daemon 可用。

### Start(ctx, project)

```
前置條件：project.Status == starting（由 use-case 設定後呼叫）
後置條件：成功 → 所有服務健康；失敗 → StartError（含健康快照）
```

1. `projectDir = filepath.Join(projectsDir, project.Slug)`
2. 執行 `runner.Run(ctx, projectDir, "docker", "compose", "up", "-d")`
3. 若 `runner.Run` 失敗 → `return &domain.StartError{Slug: project.Slug, Cause: err, Health: nil}`
4. 輪詢健康：每 5 秒呼叫一次 `Status(ctx, project)`，共輪詢至多 24 次（120 秒上限）
   - 每次輪詢前檢查 `ctx.Done()`；若 canceled → 回傳 `ctx.Err()`
   - 若 `health.IsHealthy()` → 回傳 `nil`（成功）
5. 120 秒內仍未健康 → 回傳 `&domain.StartError{Slug: project.Slug, Cause: domain.ErrServiceNotHealthy, Health: health}`

> **健康輪詢實作**：使用 `time.NewTicker(5 * time.Second)` + `select` on `ticker.C` / `ctx.Done()`。不使用 `time.Sleep`（阻塞 context 取消偵測）。

### Stop(ctx, project)

```
前置條件：project.Status == stopping
後置條件：成功 → 所有容器停止（檔案保留）
```

1. `projectDir = filepath.Join(projectsDir, project.Slug)`
2. `runner.Run(ctx, projectDir, "docker", "compose", "stop")`
3. 失敗 → `&domain.AdapterError{Operation: "stop", Slug: project.Slug, Cause: err}`

> `docker compose stop` 預設 10 秒 graceful timeout（Docker 預設），不需 adapter 層額外控制。

### Destroy(ctx, project)

```
前置條件：project.Status == destroying
後置條件：成功 → 容器 + volume + 目錄全部移除
```

1. `projectDir = filepath.Join(projectsDir, project.Slug)`
2. `runner.Run(ctx, projectDir, "docker", "compose", "down", "-v", "--remove-orphans")`
3. 若 runner 失敗 → `&domain.AdapterError{Operation: "destroy", Slug: project.Slug, Cause: err}`
4. `os.RemoveAll(projectDir)` — 移除整個專案目錄
5. 若 `os.RemoveAll` 失敗 → `&domain.AdapterError{Operation: "destroy:cleanup", Slug: project.Slug, Cause: err}`

> Step 4 無論 Step 2 是否成功都執行（attempt best-effort cleanup），但若 Step 2 失敗仍回傳其錯誤。

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
4. 失敗 → `&domain.AdapterError{Operation: "apply-config", Slug: project.Slug, Cause: err}`

> `docker-compose.yml` 在 ApplyConfig 時**不重寫**（模板永遠不變；若需更新 compose 檔案，需重新 Destroy + Create）。

---

## 錯誤處理

| 情境 | 回傳 |
|------|------|
| renderer.Render 失敗 | 直接回傳 renderer error（Create/ApplyConfig）；RenderConfig pass-through |
| os.MkdirAll / os.WriteFile 失敗 | `domain.AdapterError{Operation: "create"/"apply-config", ...}` |
| runner.Run 失敗（非零 exit） | `domain.AdapterError{...}` 或 `domain.StartError{...}` |
| Start 輪詢逾時 | `domain.StartError{Cause: domain.ErrServiceNotHealthy, Health: <snapshot>}` |
| Context canceled 在輪詢中 | `ctx.Err()` |
| parseComposePS 解析失敗（單行）| warn log + skip（不回傳 error） |
| projectDir 不存在（ApplyConfig）| `os.WriteFile` 自然失敗 → AdapterError |

> 所有 `AdapterError` 均以 `fmt.Errorf("compose adapter %s %q: %w", op, slug, cause)` 形式 wrap。

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
| `TestCreate_RendererError` | renderer 失敗 → 回傳 AdapterError，不建立目錄 |
| `TestCreate_MkdirAllError` | 若寫入失敗（eg. 權限不足）→ AdapterError |
| `TestStart_Success` | runner 呼叫順序：`up -d` → 輪詢 Status → IsHealthy → nil |
| `TestStart_UpFailure` | `up -d` 失敗 → StartError{Cause: cmd error} |
| `TestStart_HealthTimeout` | 輪詢 25 次（逾時） → StartError{Cause: ErrServiceNotHealthy, Health: snapshot} |
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

（待審）

### Reviewer B（實作）

（待審）
