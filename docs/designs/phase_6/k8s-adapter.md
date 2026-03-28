> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# 設計文件：K8s Adapter 核心

## 狀態

done

## Phase

- **Phase：** Phase 6
- **Phase Plan：** `docs/designs/phase-6-plan.md`

---

## 目的

K8s Adapter 是 Phase 6 的最終整合功能。它實作 `domain.RuntimeAdapter` 介面的全部 7 個方法，
透過 shell out 呼叫 `helm` 與 `kubectl` CLI，在 Kubernetes 叢集上管理 Supabase 專案的完整生命週期
（建立、啟動、停止、銷毀、狀態查詢、設定渲染與套用）。

此 adapter 整合了 Phase 6 前序功能的所有子元件：
- **Feature 3** `K8sValuesRenderer`（`domain.ConfigRenderer`）— 將 `ProjectConfig` 渲染為 `values.yaml`
- **Feature 4** `parseK8sPods`（status parser）— 將 `kubectl get pods -o json` 解析為 `ProjectHealth`
- **Feature 5** `K8sPortAllocator`（`domain.PortAllocator`）— 在 NodePort 範圍分配無衝突的 port set

設計目標為與 `ComposeAdapter` 保持語義對稱：`Create` 僅準備資源不啟動服務，`Start` 才實際部署，
`Stop` 保留持久化資料，`Destroy` 清除一切。

---

## 範圍

### 包含

- `K8sAdapter` struct：實作 `domain.RuntimeAdapter` 全部 7 個方法
- `NewK8sAdapter()` 生產環境建構函式
- `newK8sAdapterWithRunner()` 測試用建構函式（注入 mock `cmdRunner`）
- `cmdRunner` 介面與 `osCmdRunner` 實作（複製 compose 套件的模式，避免跨套件依賴）
- namespace / release name / values path 等 helper 方法
- 完整的單元測試覆蓋（mock `cmdRunner`）

### 不包含

- `K8sValuesRenderer` 的實作（Feature 3，已完成）
- `parseK8sPods` 的實作（Feature 4，已完成）
- `K8sPortAllocator` 的實作（Feature 5，已完成）
- Helm chart 本身的安裝與 repo 設定（一次性 setup，不屬於 adapter 邏輯）
- `AdapterRegistry` 的註冊接線（屬於 Feature 1 multi-runtime-infra）
- 整合測試（需要真實 K8s 叢集，另案處理）

---

## 資料模型

### K8sAdapter struct

```go
package k8s

type K8sAdapter struct {
    chartRef     string              // "supabase-community/supabase" 或本地路徑
    chartVersion string              // "0.5.2"
    dataDir      string              // values.yaml 檔案的根目錄
    renderer     domain.ConfigRenderer
    runner       cmdRunner           // 與 compose 套件相同的介面
}

// 靜態介面斷言 — 編譯期確保實作 RuntimeAdapter
var _ domain.RuntimeAdapter = (*K8sAdapter)(nil)
```

### cmdRunner 介面

複製 `compose/cmd_runner.go` 的相同模式，建立 `k8s/cmd_runner.go`。避免跨套件引用，
同時保持兩個 adapter 的測試注入方式一致。

```go
package k8s

// cmdRunner 抽象化 exec.Command，允許測試時注入 mock 而不產生真實 process。
type cmdRunner interface {
    // Run 在 dir 目錄下以 name 執行 args，合併 stdout 和 stderr。
    // 非零退出碼透過 *exec.ExitError 包含在回傳的 error 中。
    Run(ctx context.Context, dir, name string, args ...string) ([]byte, error)
}

// osCmdRunner 為生產環境的 cmdRunner 實作，底層使用 exec.CommandContext。
type osCmdRunner struct{}

func (r *osCmdRunner) Run(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
    cmd := exec.CommandContext(ctx, name, args...)
    cmd.Dir = dir
    return cmd.CombinedOutput()
}
```

### Namespace 與 Release Name 慣例

每個專案對應一個獨立的 K8s namespace，命名規則如下：

| 元素 | 格式 | 範例（slug = `my-app`） |
|------|------|------------------------|
| Namespace | `supabase-{slug}` | `supabase-my-app` |
| Helm release name | `supabase-{slug}` | `supabase-my-app` |
| Values 目錄 | `{dataDir}/{slug}/` | `/data/k8s/my-app/` |
| Values 檔案 | `{dataDir}/{slug}/values.yaml` | `/data/k8s/my-app/values.yaml` |

### Helper 方法

```go
func (a *K8sAdapter) namespace(slug string) string {
    return "supabase-" + slug
}

func (a *K8sAdapter) releaseName(slug string) string {
    return "supabase-" + slug
}

func (a *K8sAdapter) valuesDir(slug string) string {
    return filepath.Join(a.dataDir, slug)
}

func (a *K8sAdapter) valuesPath(slug string) string {
    return filepath.Join(a.valuesDir(slug), "values.yaml")
}
```

> **長度限制：** Slug 長度由上游 domain 層驗證（最大 32 字元），加上 `supabase-` 前綴最長 41 字元，遠低於 Helm 53 字元限制。因此 adapter 層不需額外的長度驗證。

---

## 介面合約

### 建構函式

```go
// NewK8sAdapter 回傳一個使用 OS command runner 的 K8sAdapter。
func NewK8sAdapter(
    chartRef, chartVersion, dataDir string,
    renderer domain.ConfigRenderer,
) *K8sAdapter {
    return newK8sAdapterWithRunner(chartRef, chartVersion, dataDir, renderer, &osCmdRunner{})
}

// newK8sAdapterWithRunner 為測試用建構函式，可注入 mock cmdRunner。
func newK8sAdapterWithRunner(
    chartRef, chartVersion, dataDir string,
    renderer domain.ConfigRenderer,
    runner cmdRunner,
) *K8sAdapter {
    return &K8sAdapter{
        chartRef:     chartRef,
        chartVersion: chartVersion,
        dataDir:      dataDir,
        renderer:     renderer,
        runner:       runner,
    }
}
```

### RuntimeAdapter 介面（來自 `domain/runtime_adapter.go`）

`K8sAdapter` 實作以下全部 7 個方法：

```go
type RuntimeAdapter interface {
    Create(ctx context.Context, project *ProjectModel, config *ProjectConfig) error
    Start(ctx context.Context, project *ProjectModel) error
    Stop(ctx context.Context, project *ProjectModel) error
    Destroy(ctx context.Context, project *ProjectModel) error
    Status(ctx context.Context, project *ProjectModel) (*ProjectHealth, error)
    RenderConfig(ctx context.Context, project *ProjectModel, config *ProjectConfig) ([]Artifact, error)
    ApplyConfig(ctx context.Context, project *ProjectModel, config *ProjectConfig) error
}
```

---

## 執行流程

### Create(ctx, project, config)

建立專案的隔離邊界（namespace）與持久化儲存（values.yaml 目錄）。
不啟動服務 — 與 `ComposeAdapter.Create` 語義一致。

1. **渲染設定**（fail-fast，任何 mutation 之前）
   - 呼叫 `a.renderer.Render(config)` → 取得 artifacts
   - 若渲染失敗，回傳 `*AdapterError{Operation: "create"}`，不進行任何檔案系統操作

2. **建立 values 目錄**
   - `os.MkdirAll(a.valuesDir(slug), 0700)`
   - 若失敗，回傳 `*AdapterError{Operation: "create"}`

3. **寫入 values.yaml**
   - 將 artifact 內容寫入 `a.valuesPath(slug)`
   - 使用 artifact 指定的 `Mode`（`0600`）保護敏感資料
   - 若失敗，回傳 `*AdapterError{Operation: "create"}`

4. **建立 K8s namespace**（idempotent）
   - 執行：`kubectl create namespace {namespace}`
   - 若 namespace 已存在（`AlreadyExists` 錯誤），視為成功（忽略錯誤）
   - 若其他錯誤，回傳 `*AdapterError{Operation: "create"}`

5. **回傳 `nil`** 表示成功

```
┌─────────────────────────┐
│ Create(ctx, proj, cfg)  │
└────────┬────────────────┘
         │
         ▼
┌─────────────────────────────┐
│ renderer.Render(config)     │──── error → AdapterError{"create"}
└────────┬────────────────────┘
         │ ok
         ▼
┌─────────────────────────────┐
│ os.MkdirAll(valuesDir)      │──── error → AdapterError{"create"}
└────────┬────────────────────┘
         │ ok
         ▼
┌─────────────────────────────┐
│ os.WriteFile(values.yaml)   │──── error → AdapterError{"create"}
└────────┬────────────────────┘
         │ ok
         ▼
┌─────────────────────────────┐
│ kubectl create namespace    │──── error (非 AlreadyExists)
│ (ignore AlreadyExists)      │     → AdapterError{"create"}
└────────┬────────────────────┘
         │ ok
         ▼
      return nil
```

### Start(ctx, project)

部署全套 Supabase 服務。透過 `helm upgrade --install` 執行部署，隨後以 polling 迴圈
監控服務健康狀態。採用與 `ComposeAdapter.Start` 相同的 polling 模式（每 5 秒檢查一次，
上限 120 秒），確保錯誤回報與 health snapshot 的一致性。

1. **執行 Helm 部署**
   - 執行：`helm upgrade --install {releaseName} {chartRef} -n {namespace} --version {chartVersion} -f {valuesPath}`
   - 不使用 `--wait` 旗標 — 由 adapter 自行 polling，確保一致的錯誤回報
   - 若 Helm 指令失敗，回傳 `*StartError{Slug: slug, Err: err}`

2. **Polling 迴圈**（與 `ComposeAdapter` 一致）
   - 每 5 秒呼叫 `a.Status(ctx, project)`
   - 若 `health.IsHealthy()` 回傳 `true`，立即回傳 `nil`
   - 若 `ctx.Done()` 觸發，回傳 `ctx.Err()`
   - 最多 polling 24 次（24 × 5s = 120s）

3. **Timeout**
   - 回傳 `*StartError{Slug: slug, Err: domain.ErrServiceNotHealthy, Health: lastHealth}`
   - 附帶最後一次取得的 health snapshot

```
┌──────────────────────────┐
│ Start(ctx, project)      │
└────────┬─────────────────┘
         │
         ▼
┌──────────────────────────────────────┐
│ helm upgrade --install               │──── error → StartError
│ {release} {chartRef} -n {ns}         │
│ --version {ver} -f {values}          │
└────────┬─────────────────────────────┘
         │ ok
         ▼
┌──────────────────────────────────────┐
│ polling loop (5s × 24 = 120s)        │
│   Status(ctx, project)               │
│   health.IsHealthy()? → return nil   │
│   ctx.Done()? → return ctx.Err()     │
│   ticks >= 24? → StartError          │
└──────────────────────────────────────┘
```

### Stop(ctx, project)

停止所有服務但保留資料。透過 `helm uninstall` 移除所有 pod 與 service，但保留
namespace 與 PVC — 資料不會遺失。

1. **執行 Helm 解除安裝**
   - 執行：`helm uninstall {releaseName} -n {namespace}`
   - 若 release 不存在（`"not found"` / `"release: not found"` 錯誤），視為成功（忽略錯誤），與 Destroy 中相同的冪等模式
   - 若成功或 release 不存在，回傳 `nil`
   - 若其他錯誤，回傳 `*AdapterError{Operation: "stop"}`

> **注意：** 此語義與 `ComposeAdapter.Stop`（`docker compose stop`）對稱。
> Namespace 與 PVC 被刻意保留，使得後續 `Start` 可以在相同 namespace 中重新部署。

> **設計決策（Stop 使用 `helm uninstall` 而非 scale to 0）：**
> Helm 缺少 'scale all to 0' 的原生操作。使用 `helm uninstall` 能完全釋放資源（CPU/memory），且 Start 使用 `helm upgrade --install` 可重新安裝（冪等）。此行為已記錄於 `runtime_adapter.go` 的 K8s 註解中。
>
> **實作備註：** `runtime_adapter.go` 中 `Stop` 的介面文件註解（目前描述為 "K8s: scale replicas to 0"）應在實作時更新為反映 `helm uninstall` 語義。

### Destroy(ctx, project)

移除專案的所有資源，包含持久化資料。採用 best-effort cleanup 策略：嘗試所有 3 個步驟，
回報第一個失敗。與 `ComposeAdapter.Destroy` 相同的模式。

1. **Helm 解除安裝**（best-effort）
   - 執行：`helm uninstall {releaseName} -n {namespace}`
   - 忽略「not found」錯誤（release 可能已被移除）
   - 記錄錯誤但繼續下一步

2. **刪除 namespace**（best-effort）
   - 執行：`kubectl delete namespace {namespace}`
   - 忽略「not found」錯誤（namespace 可能已被移除，確保 retry 冪等性）
   - Namespace 刪除會 cascade 到所有資源，包含 PVC
   - 記錄錯誤但繼續下一步

3. **移除本地 values 目錄**
   - 執行：`os.RemoveAll(a.valuesDir(slug))`
   - 記錄錯誤

4. **回傳第一個錯誤**
   - 若步驟 1 失敗：`*AdapterError{Operation: "destroy"}`
   - 若步驟 1 成功但步驟 2 失敗：`*AdapterError{Operation: "destroy:cleanup"}`
   - 若步驟 1、2 成功但步驟 3 失敗：`*AdapterError{Operation: "destroy:cleanup"}`
   - 全部成功：`nil`

```
┌──────────────────────────────┐
│ Destroy(ctx, project)        │
└────────┬─────────────────────┘
         │
         ▼
┌──────────────────────────────┐
│ helm uninstall {release}     │──── 記錄 helmErr（忽略 not found）
│ -n {namespace}               │
└────────┬─────────────────────┘
         │ 繼續
         ▼
┌──────────────────────────────┐
│ kubectl delete namespace     │──── 記錄 nsErr（忽略 not found）
│ {namespace}                  │
└────────┬─────────────────────┘
         │ 繼續
         ▼
┌──────────────────────────────┐
│ os.RemoveAll(valuesDir)      │──── 記錄 cleanupErr
└────────┬─────────────────────┘
         │
         ▼
     回傳第一個非 nil 錯誤
```

### Status(ctx, project)

查詢專案所有服務的即時健康狀態。為唯讀操作，不改變任何 project 狀態。

1. **查詢 pod 狀態**
   - 執行：`kubectl get pods -n {namespace} -o json`
   - 若 kubectl 失敗，回傳 `*AdapterError{Operation: "status"}`

2. **解析為 ProjectHealth**
   - 呼叫 `parseK8sPods(output)`（Feature 4）
   - 回傳 `*ProjectHealth`

### RenderConfig(ctx, project, config)

純粹的計算操作。委派給 renderer，不產生任何副作用。

1. **委派渲染**
   - 呼叫 `a.renderer.Render(config)`
   - 直接回傳結果

### ApplyConfig(ctx, project, config)

渲染設定並寫入執行環境。若專案正在執行中，則進一步透過 `helm upgrade` 將變更套用至叢集。

1. **渲染設定**
   - 呼叫 `a.renderer.Render(config)` → artifacts
   - 若失敗，回傳 `*AdapterError{Operation: "apply-config"}`

2. **寫入 values.yaml**
   - 寫入 artifact 至 `a.valuesPath(slug)`
   - 若失敗，回傳 `*AdapterError{Operation: "apply-config"}`

3. **條件式 reconcile**（僅在專案正在運行時）
   - 若 `project.Status == domain.StatusRunning`：
     - 執行：`helm upgrade {releaseName} {chartRef} -n {namespace} --version {chartVersion} -f {valuesPath}`
     - 若失敗，回傳 `*AdapterError{Operation: "apply-config:reconcile"}`
   - 若非 running 狀態，跳過 reconcile

4. **回傳 `nil`** 表示成功

```
┌──────────────────────────────────┐
│ ApplyConfig(ctx, proj, config)   │
└────────┬─────────────────────────┘
         │
         ▼
┌──────────────────────────────────┐
│ renderer.Render(config)          │──── error → AdapterError{"apply-config"}
└────────┬─────────────────────────┘
         │ ok
         ▼
┌──────────────────────────────────┐
│ os.WriteFile(values.yaml)        │──── error → AdapterError{"apply-config"}
└────────┬─────────────────────────┘
         │ ok
         ▼
┌──────────────────────────────────┐
│ project.Status == running ?      │
│   是 → helm upgrade {release}    │──── error → AdapterError{"apply-config:reconcile"}
│   否 → skip                      │
└────────┬─────────────────────────┘
         │
         ▼
      return nil
```

---

## 錯誤處理

所有錯誤皆包裝為 `*AdapterError` 或 `*StartError`，攜帶 operation 上下文與 project slug，
與 `ComposeAdapter` 的錯誤處理策略完全一致。

| 情境 | 錯誤類型 | Operation | 處理方式 |
|------|---------|-----------|---------|
| `Create` 中渲染失敗 | `*AdapterError` | `"create"` | Fail-fast，不建立目錄 |
| `Create` 中 `os.MkdirAll` 失敗 | `*AdapterError` | `"create"` | 回傳錯誤 |
| `Create` 中 `kubectl create namespace` 失敗（非 `AlreadyExists`） | `*AdapterError` | `"create"` | 回傳錯誤 |
| `Create` 中 namespace 已存在（`AlreadyExists`） | — | — | 忽略錯誤，視為成功 |
| `Create` 中寫入 `values.yaml` 失敗 | `*AdapterError` | `"create"` | 回傳錯誤 |
| `Start` 中 `helm upgrade --install` 失敗 | `*StartError` | Start | 附帶底層 error |
| `Start` 中 health check timeout | `*StartError` | Start | 附帶最後的 `Health` snapshot |
| `Start` 中 context cancelled | `context.Canceled` / `context.DeadlineExceeded` | Start | 直接回傳 `ctx.Err()` |
| `Stop` 中 `helm uninstall` 失敗（非 `not found`） | `*AdapterError` | `"stop"` | 回傳錯誤 |
| `Stop` 中 release 不存在（`not found`） | — | — | 忽略錯誤，視為成功（冪等） |
| `Destroy` 中 `helm uninstall` 失敗 | `*AdapterError` | `"destroy"` | 記錄但繼續後續步驟 |
| `Destroy` 中 namespace 刪除失敗（非 `not found`） | `*AdapterError` | `"destroy:cleanup"` | 記錄但繼續後續步驟 |
| `Destroy` 中 namespace 已不存在（`not found`） | — | — | 忽略錯誤，繼續後續步驟（retry 冪等） |
| `Destroy` 中 `os.RemoveAll` 失敗 | `*AdapterError` | `"destroy:cleanup"` | 回報第一個錯誤 |
| `Status` 中 `kubectl get pods` 失敗 | `*AdapterError` | `"status"` | 回傳錯誤 |
| `ApplyConfig` 中渲染失敗 | `*AdapterError` | `"apply-config"` | 回傳錯誤 |
| `ApplyConfig` 中寫入檔案失敗 | `*AdapterError` | `"apply-config"` | 回傳錯誤 |
| `ApplyConfig` 中 `helm upgrade` 失敗 | `*AdapterError` | `"apply-config:reconcile"` | 回傳錯誤 |

---

## 測試策略

### 需要測試的行為

- `Create` 成功路徑：建立目錄、寫入 `values.yaml`、執行 `kubectl create namespace`
- `Create` 渲染錯誤：錯誤正確傳播，不建立任何目錄
- `Create` namespace 建立錯誤：回傳 `*AdapterError`
- `Start` 成功路徑：`helm upgrade --install` + polling 回傳 healthy
- `Start` Helm 錯誤：回傳 `*StartError`
- `Start` timeout：回傳 `*StartError` 並附帶 health snapshot
- `Stop` 成功路徑：`helm uninstall`
- `Stop` 錯誤：回傳 `*AdapterError`
- `Destroy` 成功路徑：`helm uninstall` + `kubectl delete namespace` + `os.RemoveAll`
- `Destroy` best-effort：第一步失敗仍嘗試後續步驟
- `Status` 成功路徑：`kubectl get pods` + `parseK8sPods` 解析
- `Status` 錯誤：回傳 `*AdapterError`
- `RenderConfig` 委派：直接傳遞至 renderer
- `ApplyConfig` running 狀態：寫入檔案 + `helm upgrade` reconcile
- `ApplyConfig` stopped 狀態：寫入檔案，不執行 `helm upgrade`
- `ApplyConfig` 渲染錯誤：回傳 `*AdapterError`
- `ApplyConfig` 寫入錯誤：回傳 `*AdapterError`
- `Start` context cancelled：回傳 `ctx.Err()`
- `Stop` release not found：忽略錯誤（冪等）
- `Create` namespace 已存在：忽略 `AlreadyExists` 錯誤（冪等）
- `Destroy` 僅步驟 2 失敗：helm 成功但 kubectl delete ns 失敗
- `Destroy` namespace 已刪除：namespace 不存在時忽略 not found（retry 冪等）
- `Destroy` 僅步驟 3 失敗：helm+kubectl 成功但 os.RemoveAll 失敗

### 測試類型分配

| 測試類型 | 測試目標 | 層次 |
|---|---|---|
| 單元測試 | `TestCreate_Success` — 驗證目錄建立、檔案寫入、kubectl 指令參數 | adapter |
| 單元測試 | `TestCreate_RenderError` — 渲染失敗時不進行任何 mutation | adapter |
| 單元測試 | `TestCreate_NamespaceError` — kubectl 失敗回傳 `*AdapterError` | adapter |
| 單元測試 | `TestCreate_AlreadyExists` — namespace 已存在時忽略 AlreadyExists 錯誤（冪等） | adapter |
| 單元測試 | `TestStart_Success` — helm 執行 + polling 回傳 healthy | adapter |
| 單元測試 | `TestStart_HelmError` — helm 執行失敗回傳 `*StartError` | adapter |
| 單元測試 | `TestStart_Timeout` — polling timeout 回傳 `*StartError` 附帶 health | adapter |
| 單元測試 | `TestStop_Success` — helm uninstall 成功 | adapter |
| 單元測試 | `TestStop_Error` — helm uninstall 失敗回傳 `*AdapterError` | adapter |
| 單元測試 | `TestDestroy_Success` — 三步驟全部成功 | adapter |
| 單元測試 | `TestDestroy_BestEffort` — 第一步失敗仍嘗試後續 | adapter |
| 單元測試 | `TestStatus_Success` — kubectl 輸出正確解析為 `ProjectHealth` | adapter |
| 單元測試 | `TestStatus_Error` — kubectl 失敗回傳 `*AdapterError` | adapter |
| 單元測試 | `TestRenderConfig_Delegates` — 直接傳遞至 renderer | adapter |
| 單元測試 | `TestApplyConfig_Running` — 寫入檔案 + helm upgrade | adapter |
| 單元測試 | `TestApplyConfig_Stopped` — 寫入檔案，不執行 helm upgrade | adapter |
| 單元測試 | `TestApplyConfig_RenderError` — 渲染失敗回傳 `*AdapterError` | adapter |
| 單元測試 | `TestApplyConfig_WriteError` — 寫入失敗回傳 `*AdapterError` | adapter |
| 單元測試 | `TestStart_ContextCancelled` — context 取消回傳 `ctx.Err()` | adapter |
| 單元測試 | `TestStop_NotFound` — release 不存在時忽略錯誤 | adapter |
| 單元測試 | `TestDestroy_Step2OnlyFails` — helm 成功但 kubectl delete ns 失敗回傳 `*AdapterError` | adapter |
| 單元測試 | `TestDestroy_NamespaceAlreadyDeleted` — namespace 已不存在時忽略 not found 錯誤（retry 冪等） | adapter |
| 單元測試 | `TestDestroy_Step3OnlyFails` — helm+kubectl 成功但 os.RemoveAll 失敗回傳 `*AdapterError` | adapter |

### Mock 策略

- **`cmdRunner`**：使用與 `ComposeAdapter` 測試相同的 mock 模式。
  建立 `mockCmdRunner` struct，記錄每次 `Run` 呼叫的指令與參數，並依序回傳預設的
  `([]byte, error)` 回應。可驗證：
  - 呼叫次數
  - 傳入的指令名稱與參數
  - 工作目錄
- **`domain.ConfigRenderer`**：使用 mock renderer，可控制 `Render` 方法的回傳值
  （成功時回傳預設 artifacts，失敗時回傳指定 error）
- **檔案系統操作**：使用 `t.TempDir()` 提供隔離的臨時目錄作為 `dataDir`，
  測試結束後自動清除

### CI 執行方式

- 所有測試為純粹的單元測試，不需要真實的 K8s 叢集、Helm CLI 或 kubectl
- 在一般 CI pipeline 中透過 `go test ./internal/adapter/k8s/...` 執行
- 整合測試（需要 OrbStack K8s 環境）使用 `//go:build integration` build tag 隔離，
  透過 `go test -tags=integration` 執行

---

## Production Ready 考量

### 錯誤處理

- 所有操作錯誤統一包裝為 `*AdapterError` 或 `*StartError`，攜帶 `Operation` 與 `Slug` 上下文
- 使用 `Unwrap()` 方法支援 `errors.Is` / `errors.As` 解包檢查
- `Destroy` 採用 best-effort 策略：所有步驟皆嘗試，回報第一個錯誤
- `Start` 的 timeout 錯誤附帶最後一次的 health snapshot，呼叫端無需額外呼叫 `Status`

### 日誌與可觀測性

- 不適用：目前 Control Plane 尚未引入結構化日誌框架
- 錯誤資訊透過 `AdapterError.Error()` 方法提供完整的操作上下文
- 未來引入日誌框架時，可在各方法入口與出口加入日誌

### 輸入驗證

- `nil` config 由 `renderer.Render` 內部檢查（`"k8s renderer: config is nil"`）
- `renderer.Render` 在任何 mutation 之前呼叫（fail-fast 模式），確保無效輸入不會留下部分建立的資源
- `slug` 來自已驗證的 `ProjectModel`，不在 adapter 層重複驗證
- **Helm release name 長度限制：** Helm 限制 release name 最長 53 字元。`supabase-` 前綴為 9 字元，留給 slug 44 字元。Slug 長度由上游 domain 層驗證（最大 32 字元），加上前綴最長 41 字元，遠低於 53 字元限制，因此不需要額外的 runtime 驗證

### 安全性

- `values.yaml` 以 `0600` 權限寫入（owner-read/write），保護內含的 secret 值
- 每個專案使用獨立的 K8s namespace（`supabase-{slug}`），提供資源隔離
- Values 目錄以 `0700` 權限建立，限制目錄存取

### 優雅降級

- `Destroy` 在任一步驟失敗時仍繼續嘗試後續清理步驟
- `Start` 的 polling 迴圈尊重 `ctx.Done()`，支援外部 timeout 與 cancellation
- `Stop` 保留 namespace 與 PVC，避免意外資料遺失

### 設定管理

| 設定項 | 注入方式 | 說明 |
|--------|---------|------|
| `chartRef` | 建構函式參數 | Helm chart 參照（如 `"supabase-community/supabase"`） |
| `chartVersion` | 建構函式參數 | Helm chart 版本（如 `"0.5.2"`） |
| `dataDir` | 建構函式參數 | values.yaml 檔案的根目錄 |
| `renderer` | 建構函式參數 | `domain.ConfigRenderer` 實例（`K8sValuesRenderer`） |

所有設定項皆透過建構函式注入，無隱含的環境變數依賴。

---

## 影響檔案

| 檔案路徑 | 操作 |
|---|---|
| `internal/adapter/k8s/cmd_runner.go` | 新增 |
| `internal/adapter/k8s/adapter.go` | 新增 |
| `internal/adapter/k8s/adapter_test.go` | 新增 |

---

## 待決問題

- Helm chart repo 必須在首次使用前手動新增：
  `helm repo add supabase-community https://supabase-community.github.io/supabase-kubernetes`。
  這是一次性的環境設定步驟，不屬於 adapter 邏輯。應記錄於 README 或 setup 文件中。

- **Namespace 刪除的非同步特性：** Namespace 刪除為非同步操作（進入 Terminating 狀態），快速連續的 Destroy→Create 可能衝突。目前視為已知限制，因為 control plane 的狀態機會等待 Destroy 完成後才允許 Create。若未來出現問題，可在 Destroy 中加入等待 namespace 完全刪除的邏輯。

---

## 審查

### Reviewer A（架構）

- **狀態：** APPROVED（Round 3）
- **意見：**

**Round 1 回饋：**

**Issue 1（與 Reviewer B 共同提出）：Create 中的 pipe 指令無法透過 `cmdRunner.Run` 執行**

`kubectl create namespace --dry-run=client -o yaml | kubectl apply -f -` 使用 pipe，但 `cmdRunner.Run` 僅產生單一 process。

> **已解決：** 採用方案 (b) — 改為 `kubectl create namespace {namespace}` 並忽略 `AlreadyExists` 錯誤。已更新 Create 流程、流程圖與錯誤處理表。

**Issue 2：Stop 語義偏離介面合約**

介面註解描述 "K8s: scale replicas to 0" 但設計使用 `helm uninstall`。

> **已解決：** 在 Stop 段落新增設計決策說明（Helm 缺少 scale to 0 原生操作，uninstall 能完全釋放資源），並加入實作備註提醒更新 `runtime_adapter.go` 的介面文件註解。

**Issue 3：Stop 冪等性缺口**

`helm uninstall` 在 release 不存在時會失敗，但 Stop 前置條件包含 `stopping` 狀態（先前嘗試可能已移除 release）。

> **已解決：** Stop 現在忽略 "not found" / "release: not found" 錯誤，與 Destroy 中相同的冪等模式。已更新 Stop 流程與錯誤處理表。

**Issue 4：Namespace 刪除的非同步競態**

`kubectl delete namespace` 為非同步操作，快速連續的 Destroy→Create 可能衝突。

> **已解決：** 新增至待決問題段落，記錄為已知限制。Control plane 狀態機會等待 Destroy 完成後才允許 Create。

**Issue 5：Helm release name 長度限制**

Helm 限制 release name 最長 53 字元，`supabase-` 前綴為 9 字元。

> **已解決：** 在輸入驗證段落與 helper 方法段落新增說明：slug 最大 32 字元 + 前綴 = 41 字元，遠低於 53 字元限制。

### Reviewer B（實作）

- **狀態：** APPROVED（Round 3）
- **意見：**

**Round 1 回饋：**

**Issue 1（與 Reviewer A 共同提出，阻塞）：Create 中的 pipe 指令無法透過 `cmdRunner.Run` 執行**

設計指定 `kubectl create namespace {namespace} --dry-run=client -o yaml | kubectl apply -f -`，但 `cmdRunner.Run` 底層使用 `exec.CommandContext`，僅能產生單一 process，不支援 Unix pipe（`|`）。此指令無法在現有介面下實作。

> **已解決：** 採用方案 (b) — 改為 `kubectl create namespace {namespace}` 並忽略 `AlreadyExists` 錯誤。

**Issue 2（阻塞）：測試案例不完整 — 錯誤處理表列出的路徑在測試表中缺少對應案例**

錯誤處理表定義了多條失敗路徑，但測試類型分配表中缺少以下案例：
- `TestApplyConfig_RenderError` — 渲染失敗回傳 `*AdapterError{"apply-config"}`
- `TestApplyConfig_WriteError` — 檔案寫入失敗回傳 `*AdapterError{"apply-config"}`
- `TestStart_ContextCancelled` — context 取消時回傳 `ctx.Err()`
- `TestDestroy_SecondStepFails` — 僅 namespace 刪除失敗（step 2），回傳 `"destroy:cleanup"`
- `TestDestroy_ThirdStepFails` — 僅 `os.RemoveAll` 失敗（step 3），回傳 `"destroy:cleanup"`

> **已解決：** 已新增全部 5 個測試案例至測試類型分配表（命名為 `TestDestroy_Step2OnlyFails` / `TestDestroy_Step3OnlyFails`）。同時補充「需要測試的行為」清單中缺少的 `ApplyConfig` 錯誤路徑。

**Issue 3（非阻塞，建議修正）：`--namespace` 與 `-n` 旗標不一致**

Stop 使用 `helm uninstall {releaseName} --namespace {namespace}`，而 Start、Destroy、ApplyConfig 等使用 `-n`。

> **已解決：** 已統一所有 helm/kubectl 指令使用 `-n`（短格式）。

---

## 任務

### Task 1：K8sAdapter 核心結構與 cmdRunner

**檔案：** `control-plane/internal/adapter/k8s/adapter.go`

- 定義 `K8sAdapter` struct（`projectsDir`, `renderer`, `cmdRunner`）
- 定義 `cmdRunner` 介面（與 compose adapter 相同模式）
- 定義 `osCmdRunner` 生產用實作
- Helper 方法：`namespace(slug)`, `releaseName(slug)`, `valuesDir(slug)`, `valuesPath(slug)`
- Static interface assertion：`var _ domain.RuntimeAdapter = (*K8sAdapter)(nil)`
- `NewK8sAdapter` constructor

### Task 2：RuntimeAdapter 7 方法實作

**檔案：** `control-plane/internal/adapter/k8s/adapter.go`（延續 Task 1）

實作全部 7 個 `RuntimeAdapter` 方法：
- `Create` — MkdirAll + render values.yaml + kubectl create namespace（忽略 AlreadyExists）+ 寫入檔案
- `Start` — helm upgrade --install + polling Status 每 5s / 120s timeout
- `Stop` — helm uninstall（忽略 not found）
- `Destroy` — best-effort: helm uninstall + kubectl delete namespace（忽略 not found）+ os.RemoveAll
- `Status` — kubectl get pods -n -o json + parseK8sPods
- `RenderConfig` — 委派至 renderer
- `ApplyConfig` — render + 寫入 + 若 running 則 helm upgrade

### Task 3：單元測試（23 個測試案例）

**檔案：** `control-plane/internal/adapter/k8s/adapter_test.go`

全部使用 mock cmdRunner + mock renderer：
- TestCreate_Success, TestCreate_RenderError, TestCreate_NamespaceError, TestCreate_AlreadyExists
- TestStart_Success, TestStart_HelmError, TestStart_Timeout, TestStart_ContextCancelled
- TestStop_Success, TestStop_Error, TestStop_NotFound
- TestDestroy_Success, TestDestroy_BestEffort, TestDestroy_Step2OnlyFails, TestDestroy_NamespaceAlreadyDeleted, TestDestroy_Step3OnlyFails
- TestStatus_Success, TestStatus_Error
- TestRenderConfig_Delegates
- TestApplyConfig_Running, TestApplyConfig_Stopped, TestApplyConfig_RenderError, TestApplyConfig_WriteError

---

## 程式碼審查

<!-- 所有任務完成後，由 code-review subagent 審查 feature branch 對 main 的完整 diff -->

- **審查結果：** PASS
- **發現問題：** 無
- **修正記錄：** N/A
