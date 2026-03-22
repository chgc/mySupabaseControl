> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# 設計文件：Runtime Adapter 介面定義（Runtime Adapter）

## 狀態

approved（兩輪審查通過）

## Phase

- **Phase：** Phase 1
- **Phase Plan：** `docs/designs/phase_1/phase-1-plan.md`

---

## 目的

定義 **Runtime Adapter 介面**，將 Control Plane 的控制邏輯與底層執行 runtime（Docker Compose、K8s）完全解耦。這是 Control Plane 架構中最關鍵的抽象層 — 沒有它，K8s 遷移將需要重寫整個系統。

---

## 範圍

### 包含

- `RuntimeAdapter` Go interface 定義
- 6 個核心方法的合約（create, start, stop, destroy, status, renderConfig）
- 錯誤型別與 sentinel errors
- `ProjectStatus` 查詢結果型別
- Adapter 選擇與工廠模式
- 全域服務管理介面（可選）

### 不包含

- Docker Compose Adapter 實作（Phase 2）
- K8s Adapter 實作（Phase 5）
- HTTP API 層（Phase 3）

---

## 資料模型

### RuntimeAdapter 介面

```go
package domain

import "context"

// RuntimeAdapter 定義 Control Plane 與底層執行 runtime 的抽象介面。
// Docker Compose Adapter（Phase 2）與 K8s Adapter（Phase 5）各自實作此介面。
type RuntimeAdapter interface {
    // Create 為專案建立隔離邊界與持久儲存。
    //
    // Docker Compose：建立專案目錄、渲染 .env 檔案。
    // K8s：建立 namespace、PVC。
    //
    // 前置條件：project.Status == creating
    // 後置條件：成功時 project.Status → stopped
    //           失敗時 project.Status → error
    Create(ctx context.Context, project *ProjectModel, config *ProjectConfig) error

    // Start 部署並啟動專案的所有服務。
    //
    // Docker Compose：docker compose up -d
    // K8s：kubectl apply / helm install
    //
    // 前置條件：project.Status == stopped || starting
    // 後置條件：成功時 project.Status → running
    //           失敗時 project.Status → error
    Start(ctx context.Context, project *ProjectModel) error

    // Stop 停止專案的所有服務，保留資料。
    //
    // Docker Compose：docker compose down（不加 -v）
    // K8s：scale replicas to 0
    //
    // 前置條件：project.Status == running || stopping
    // 後置條件：成功時 project.Status → stopped
    //           失敗時 project.Status → error
    Stop(ctx context.Context, project *ProjectModel) error

    // Destroy 移除專案的所有資源，包含持久資料。
    //
    // Docker Compose：docker compose down -v + 刪除專案目錄
    // K8s：刪除 namespace（級聯刪除所有資源）
    //
    // 前置條件：project.Status == stopped || destroying || error
    // 後置條件：成功時 project.Status → destroyed
    //           失敗時 project.Status → error
    Destroy(ctx context.Context, project *ProjectModel) error

    // Status 查詢專案中所有服務的健康狀態。
    //
    // Docker Compose：docker compose ps + healthcheck 解析
    // K8s：kubectl get pods + readiness probe
    //
    // 此方法不改變 project.Status，僅回傳當前快照。
    Status(ctx context.Context, project *ProjectModel) (*ProjectHealth, error)

    // RenderConfig 將專案設定渲染為 runtime 特定的 artifacts，並回傳供檢查。
    // 注意：此方法僅負責渲染（純計算），不寫入 runtime。
    //
    // Docker Compose：渲染為 .env 格式 Artifact
    // K8s：渲染為 ConfigMap + Secret YAML Artifact
    //
    // 若需將設定套用至 runtime（如更新設定），應呼叫 ApplyConfig。
    RenderConfig(ctx context.Context, project *ProjectModel, config *ProjectConfig) ([]Artifact, error)

    // ApplyConfig 將專案設定渲染並寫入 runtime。
    // 等同於 RenderConfig + 將 artifacts 寫入 runtime 目標（檔案系統或 kubectl apply）。
    //
    // Docker Compose：覆寫 .env 檔案
    // K8s：kubectl apply ConfigMap/Secret
    //
    // 此方法可在 Create 後獨立呼叫，用於更新設定（不需重新建立整個 runtime 環境）。
    // 冪等：重複呼叫安全，結果相同。
    ApplyConfig(ctx context.Context, project *ProjectModel, config *ProjectConfig) error
}
```

### 方法合約摘要

| 方法 | 前置條件 | 成功後狀態 | 失敗後狀態 | 冪等性 |
|------|---------|-----------|-----------|--------|
| `Create` | `creating` | `stopped` | `error` | ✅ 可重試 |
| `Start` | `stopped` / `starting` | `running` | `error` | ✅ 可重試 |
| `Stop` | `running` / `stopping` | `stopped` | `error` | ✅ 可重試 |
| `Destroy` | `stopped` / `destroying` / `error` | `destroyed` | `error` | ✅ 可重試 |
| `Status` | 任何非 `destroyed` | 不變 | 不變 | ✅ 唯讀 |
| `RenderConfig` | 任何 | 不變 | 不變 | ✅ 純計算 |
| `ApplyConfig` | 任何 | 不變 | 不變 | ✅ 可重試 |

> **冪等性說明：** 前置條件由呼叫端（ProjectService）負責驗證。Adapter 的冪等性意指：**runtime 操作層面**的冪等（如目錄已存在不報錯、服務已在運行不報錯），適用於 error recovery 重試場景。呼叫端不應在狀態不符前置條件時呼叫 Adapter 方法。

### Adapter 錯誤型別

```go
// AdapterError 表示 Runtime Adapter 操作失敗。
// 包含操作名稱、專案 slug 與底層錯誤。
type AdapterError struct {
    Operation string        // "create", "start", "stop", "destroy", "status", "apply_config"
    Slug      string        // 專案 slug
    Err       error         // 底層錯誤
}

func (e *AdapterError) Error() string {
    return fmt.Sprintf("adapter %s failed for project %q: %v", e.Operation, e.Slug, e.Err)
}

func (e *AdapterError) Unwrap() error {
    return e.Err
}

// StartError 在 Start 方法因服務健康檢查失敗時回傳。
// 攜帶失敗當下的服務健康快照，呼叫端可透過 errors.As 取得詳情，
// 無需額外呼叫 Status。
type StartError struct {
    Slug   string        // 專案 slug
    Health *ProjectHealth // 失敗當下的服務健康快照
    Err    error          // 底層原因（通常為 ErrServiceNotHealthy 或 ErrAdapterTimeout）
}

func (e *StartError) Error() string {
    return fmt.Sprintf("start failed for project %q: %v", e.Slug, e.Err)
}

func (e *StartError) Unwrap() error {
    return e.Err
}

// Sentinel errors
var (
    ErrAdapterTimeout    = errors.New("adapter operation timed out")
    ErrServiceNotHealthy = errors.New("one or more services failed health check")
    ErrRuntimeNotFound   = errors.New("runtime not available")
)
```

### Adapter 工廠

```go
// RuntimeType 標識支援的 runtime 類型。
type RuntimeType string

const (
    RuntimeDockerCompose RuntimeType = "docker-compose"
    RuntimeKubernetes    RuntimeType = "kubernetes"
)

// NewRuntimeAdapter 根據 RuntimeType 建立對應的 adapter 實例。
// Phase 1-4 僅支援 docker-compose；Phase 5 加入 kubernetes。
func NewRuntimeAdapter(rt RuntimeType, opts ...AdapterOption) (RuntimeAdapter, error)

// AdapterOption 是 adapter 建構選項。
type AdapterOption func(*adapterOptions)

// WithComposeFilePath 設定 docker-compose.yml 的路徑。
func WithComposeFilePath(path string) AdapterOption

// WithProjectsDir 設定專案目錄的根路徑。
func WithProjectsDir(dir string) AdapterOption

// WithTimeout 設定操作預設逾時時間。
func WithTimeout(timeout time.Duration) AdapterOption
```

### 全域服務管理（可選）

```go
// GlobalServiceManager 管理全域共用服務（如 vector）。
// 此介面為可選設計，若不需要全域服務管理，可不實作。
type GlobalServiceManager interface {
    // EnsureGlobalServices 確保全域共用服務正在運行。
    // 若已運行則不做任何事（冪等）。
    EnsureGlobalServices(ctx context.Context) error

    // GlobalStatus 查詢全域服務的健康狀態。
    GlobalStatus(ctx context.Context) (*GlobalHealth, error)
}

type GlobalHealth struct {
    Services map[ServiceName]ServiceHealth
    CheckedAt time.Time
}
```

---

## 介面合約

### 生命週期編排流程

Control Plane 使用 RuntimeAdapter 的典型流程：

```go
// 建立專案
func (s *ProjectService) CreateProject(ctx context.Context, slug, displayName string, overrides map[string]string) (*ProjectModel, error) {
    // 1. 驗證 slug
    if err := ValidateSlug(slug); err != nil {
        return nil, err
    }

    // 2. 建立 ProjectModel
    project, err := NewProject(slug, displayName)
    if err != nil {
        return nil, err
    }

    // 3. 產生設定（完整簽名：需先產生 secrets 與分配 ports）
    secrets, err := GenerateProjectSecrets(generator)
    if err != nil {
        return nil, err
    }
    portSet, err := portAllocator.AllocatePorts(ctx)
    if err != nil {
        return nil, err
    }
    config, err := ResolveConfig(project, secrets, portSet, overrides)
    if err != nil {
        return nil, err
    }

    // 4. 持久化
    if err := s.store.Create(ctx, project); err != nil {
        return nil, err
    }
    if err := s.store.SaveConfig(ctx, slug, config); err != nil {
        return nil, err
    }

    // 5. 呼叫 adapter 建立 runtime 資源
    if err := s.adapter.Create(ctx, project, config); err != nil {
        project.TransitionTo(StatusError)
        s.store.UpdateStatus(ctx, slug, StatusError, err.Error())
        return nil, err
    }

    // 6. 更新狀態
    project.TransitionTo(StatusStopped)
    s.store.UpdateStatus(ctx, slug, StatusStopped, "")

    return project, nil
}
```

### 方法執行時序

```
Control Plane                RuntimeAdapter              Docker/K8s
     │                           │                          │
     │  Create(project, config)  │                          │
     │──────────────────────────►│                          │
     │                           │  mkdir projects/<slug>   │
     │                           │─────────────────────────►│
     │                           │  write .env              │
     │                           │─────────────────────────►│
     │                           │  ◄── ok ─────────────────│
     │  ◄── ok ─────────────────│                          │
     │                           │                          │
     │  Start(project)           │                          │
     │──────────────────────────►│                          │
     │                           │  docker compose up -d    │
     │                           │─────────────────────────►│
     │                           │  wait for healthy        │
     │                           │─────────────────────────►│
     │                           │  ◄── all healthy ────────│
     │  ◄── ok ─────────────────│                          │
     │                           │                          │
     │  Status(project)          │                          │
     │──────────────────────────►│                          │
     │                           │  docker compose ps       │
     │                           │─────────────────────────►│
     │                           │  ◄── status ─────────────│
     │  ◄── ProjectHealth ──────│                          │
```

---

## 執行流程

### Create

1. 驗證 `project.Status == creating`
2. 呼叫 `ApplyConfig(ctx, project, config)` — 渲染並寫入 artifacts（.env 或 ConfigMap/Secret）
3. 建立專案目錄（Docker Compose）或 namespace（K8s）
4. 建立持久儲存空間（bind mount 目錄或 PVC）
5. 回傳 nil（成功）

### Start

1. 驗證 `project.Status == stopped || starting`
2. 執行 runtime 啟動指令（`docker compose up -d` 或 `kubectl apply`）
3. 等待所有服務 healthy（polling 或 watch）
4. 若逾時，回傳 `StartError{Err: ErrAdapterTimeout, Health: <當前快照>}`
5. 若服務未健康，回傳 `StartError{Err: ErrServiceNotHealthy, Health: <當前快照>}`

### Stop

1. 驗證 `project.Status == running || stopping`
2. 執行 runtime 停止指令（`docker compose down` 或 scale to 0）
3. 等待所有服務停止

### Destroy

1. 驗證 `project.Status == stopped || destroying || error`
2. 執行 runtime 銷毀指令（`docker compose down -v` 或刪除 namespace）
3. 清理專案目錄與持久資料

### Status

1. 查詢 runtime 中所有服務的狀態
2. 將 runtime 特定的狀態（如 Docker `running`/`exited`）映射為 `ServiceStatus`
3. 計算 `OverallHealthy`
4. 回傳 `ProjectHealth`

---

## 錯誤處理

| 情境 | 處理方式 | 回應格式 |
|------|---------|---------|
| 前置條件不滿足 | 回傳 `TransitionError` | `{ "error": "invalid_transition" }` |
| Runtime 指令執行失敗 | 包裝為 `AdapterError` | `{ "error": "adapter_failed", "operation": "..." }` |
| 健康檢查逾時 | 回傳 `StartError{Err: ErrAdapterTimeout, Health: <snapshot>}` | `{ "error": "timeout" }` |
| Runtime 不可用（如 Docker 未啟動） | 回傳 `ErrRuntimeNotFound` | `{ "error": "runtime_not_found" }` |
| 部分服務失敗 | 回傳 `StartError{Err: ErrServiceNotHealthy, Health: <snapshot>}`；呼叫端可用 `errors.As` 取得 `*StartError` 以存取健康詳情 | `{ "error": "unhealthy" }` |

---

## 測試策略

### 需要測試的行為

- 所有方法的前置條件驗證（不合法狀態應報錯）
- 冪等性：重複 Create/Start/Stop 不應報錯
- 逾時處理：context 取消或 deadline 時正確傳播
- 錯誤包裝：AdapterError 可被 errors.Is/errors.As 解包
- 工廠方法：不支援的 RuntimeType 回傳錯誤

### 測試類型分配

| 測試類型 | 測試目標 | 層次 |
|---------|---------|------|
| 單元測試 | 介面合約、錯誤型別、工廠方法 | domain |
| 單元測試 | 前置條件驗證 | domain |
| 整合測試 | Docker Compose Adapter 實作 | adapter/compose（Phase 2） |
| 整合測試 | K8s Adapter 實作 | adapter/k8s（Phase 5） |

### Mock 策略

- Domain 層測試使用 mock RuntimeAdapter（實作所有方法，記錄呼叫參數）
- `MockRuntimeAdapter` 可設定每個方法的回傳值，支援驗證呼叫順序

```go
// MockRuntimeAdapter 用於測試的 mock 實作。
type MockRuntimeAdapter struct {
    CreateFunc       func(ctx context.Context, project *ProjectModel, config *ProjectConfig) error
    StartFunc        func(ctx context.Context, project *ProjectModel) error
    StopFunc         func(ctx context.Context, project *ProjectModel) error
    DestroyFunc      func(ctx context.Context, project *ProjectModel) error
    StatusFunc       func(ctx context.Context, project *ProjectModel) (*ProjectHealth, error)
    RenderConfigFunc func(ctx context.Context, project *ProjectModel, config *ProjectConfig) ([]Artifact, error)
    ApplyConfigFunc  func(ctx context.Context, project *ProjectModel, config *ProjectConfig) error
}
```

### CI 執行方式

- 單元測試：一般 CI
- 整合測試：需要 Docker（Phase 2）或 K8s（Phase 5）環境

---

## Production Ready 考量

### 錯誤處理
- 所有 adapter 錯誤包含 operation name 與 project slug
- 底層 runtime 的 stderr/stdout 納入 error message

### 日誌與可觀測性
- 每個 adapter 操作記錄：`operation`、`project_slug`、`runtime_type`、`duration_ms`、`success`
- Start 操作額外記錄各服務的 healthcheck 結果

### 輸入驗證
- 所有方法驗證 project 不為 nil
- 所有方法驗證 context 未取消

### 安全性
- Adapter 不直接處理 secrets（由 ConfigRenderer 處理）
- Docker Compose 指令的 stdout/stderr 可能包含敏感資訊，需在日誌中遮罩

### 優雅降級
- `Status` 方法在 runtime 不可用時回傳所有服務為 `unknown`，而非報錯
- `Stop` 方法設定合理的 grace period（預設 30 秒）

### 設定管理
- 操作逾時可透過 `WithTimeout` 設定
- compose file 路徑可透過 `WithComposeFilePath` 設定
- 專案目錄根路徑可透過 `WithProjectsDir` 設定

---

## 待決問題

- Start 方法等待 healthy 的逾時應設多長？建議 120 秒（Supabase 完整啟動約 90 秒）。
- Status 方法的輪詢間隔？建議 2 秒。
- 是否需要 `Restart(project)` 方法？建議不需要，用 Stop + Start 替代。
- `RenderConfig` 更新設定後，是否需要 `Reload(project)` 方法讓服務重新載入？Phase 1 先不需要。

---

## 審查

### Reviewer A（架構）

- **狀態：** 🔁 REVISE（第一輪）→ ✅ APPROVE（第二輪）
- **第一輪意見（摘要）：**
  1. 🔴 **[已修正]** `destroying` 狀態孤立：`Destroy` 前置條件遺漏 `destroying` → 加入 `stopped || destroying || error`
  2. 🔴 **[已修正]** `ErrServiceNotHealthy + ProjectHealth` 無法共回傳：定義 `StartError` 結構攜帶 `Health *ProjectHealth` 與 `Err error`

### Reviewer B（實作）

- **狀態：** 🔁 REVISE（第一輪）→ ✅ APPROVE（第二輪）
- **第一輪意見（摘要）：**
  1. 🔴 **[已修正]** 冪等性與前置條件矛盾：明確說明前置條件由呼叫端負責，Adapter 冪等性指 runtime 操作層面
  2. 🔴 **[已修正]** 設定更新路徑不可實作：`RenderConfig` 只渲染不寫入，增加 `ApplyConfig` 方法處理渲染+寫入

---

## 任務

> 審查通過後展開。所有任務均在 `control-plane/internal/domain/` 下實作。

| 任務 ID | 檔案 | 說明 | 狀態 |
|---------|------|------|------|
| `domain-runtime-adapter` | `runtime_adapter.go` | `RuntimeAdapter` interface（7 方法：Create, Start, Stop, Destroy, Status, RenderConfig, ApplyConfig）、`AdapterError` struct、`StartError` struct（含 `Health *ProjectHealth`）、sentinel errors、`RuntimeType`、`AdapterOption`、`NewRuntimeAdapter()` factory（Phase 1 回傳 stub）、`GlobalServiceManager` interface（可選）、`GlobalHealth` struct | [ ] pending |
| `domain-mock-adapter` | `mock_adapter.go` | `MockRuntimeAdapter` struct，7 個 `func` 欄位（每方法一個），供 domain 層單元測試使用，不依賴任何外部系統 | [ ] pending |

---

## 程式碼審查

- **審查結果：**
- **發現問題：**
- **修正記錄：**
