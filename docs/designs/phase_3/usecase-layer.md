> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# 設計文件：Use-case 層（`internal/usecase/`）

## 狀態

done

## Phase

- **Phase：** Phase 3
- **Phase Plan：** `docs/designs/phase-3-plan.md`

---

## 目的

目前 domain + store + adapter 各自定義了介面，但沒有一個統一的協調層。
CLI、MCP Server、未來的 Telegram Bot 若各自組裝這些元件，會造成業務邏輯重複、測試困難。

Use-case 層（`internal/usecase/`）負責：
1. 聚合 `domain`、`store`、`adapter` 三個層次
2. 實作專案生命週期操作的完整業務流程（狀態機轉移 + 持久化 + 執行）
3. 作為 CLI、MCP Server、Telegram Bot 的**唯一入口**

---

## 範圍

### 包含

- `ProjectService` 介面定義（7 個方法）
- `projectService` struct 實作（wire 所有依賴）
- 每個方法的完整業務流程（含狀態轉移、持久化、adapter 呼叫）
- `reset` 語意定義（Stop → Destroy → Create → Start）
- Use-case 層的 `UsecaseError` 統一錯誤型別
- `ProjectView` 輸出 DTO（避免將 domain struct 直接暴露給 CLI/MCP）
- 每個方法的完整單元測試（mock store + mock adapter）

### 不包含

- HTTP handler（Phase 3 無 HTTP Server）
- CLI 命令解析（`cmd/sbctl/`，屬功能 2）
- MCP tool 定義（屬功能 3）
- Telegram Bot 整合（Phase 4）
- 認證 / 授權（超出 Phase 3 範圍）

---

## 資料模型

### `ProjectView` — 輸出 DTO

```go
// ProjectView is the read model returned by ProjectService operations.
// It is a safe, serialisable representation of a project — sensitive config
// values are masked (see ProjectConfig.Get vs GetSensitive).
type ProjectView struct {
    Slug           string            `json:"slug"`
    DisplayName    string            `json:"display_name"`
    Status         string            `json:"status"`
    PreviousStatus string            `json:"previous_status,omitempty"`
    LastError      string            `json:"last_error,omitempty"`
    CreatedAt      time.Time         `json:"created_at"`
    UpdatedAt      time.Time         `json:"updated_at"`
    Health         *HealthView       `json:"health,omitempty"`
    // Config contains masked config values (sensitive fields show "***").
    // Only populated in Get; omitted in List for performance.
    Config         map[string]string `json:"config,omitempty"`
    // URLs provides convenient access to project endpoints.
    URLs           *ProjectURLs      `json:"urls,omitempty"`
}

// HealthView is the serialisable representation of ProjectHealth.
type HealthView struct {
    Healthy    bool                     `json:"healthy"`
    Services   map[string]ServiceView   `json:"services"`
    CheckedAt  time.Time                `json:"checked_at"`
}

// ServiceView is the serialisable representation of ServiceHealth.
type ServiceView struct {
    Status    string    `json:"status"`    // one of: healthy, unhealthy, starting, stopped, unknown
    Message   string    `json:"message,omitempty"`
    CheckedAt time.Time `json:"checked_at"`
}

// ProjectURLs contains the public-facing URLs for a running project.
type ProjectURLs struct {
    API      string `json:"api"`       // Kong API gateway URL
    Studio   string `json:"studio"`    // Supabase Studio URL
    Inbucket string `json:"inbucket"`  // Email testing URL (dev only)
}
```

### `UsecaseError` — 統一錯誤型別

```go
// UsecaseError is returned by all ProjectService methods.
// It wraps lower-level errors (store, adapter, domain) with operation context.
type UsecaseError struct {
    // Code is a stable machine-readable error code.
    Code    ErrorCode
    // Message is a human-readable description safe to surface to end users.
    Message string
    // Err is the underlying cause (may be nil for pure domain errors).
    Err     error
}

// Error implements the error interface.
func (e *UsecaseError) Error() string {
    if e.Err != nil {
        return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Err)
    }
    return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap allows errors.Is / errors.As to inspect the underlying error.
func (e *UsecaseError) Unwrap() error { return e.Err }

// ErrorCode classifies UsecaseError for programmatic handling.
type ErrorCode string

const (
    ErrCodeNotFound       ErrorCode = "not_found"
    ErrCodeConflict       ErrorCode = "conflict"
    ErrCodeInvalidInput   ErrorCode = "invalid_input"
    ErrCodeInvalidState   ErrorCode = "invalid_state"
    ErrCodeInternal       ErrorCode = "internal"
)
```

---

## 介面合約

### `ProjectService` 介面

```go
package usecase

import "context"

// ProjectService defines all use-case operations for managing Supabase projects.
// It is the single entry point for CLI, MCP Server, and Telegram Bot.
type ProjectService interface {
    // Create provisions a new project: allocates ports, generates secrets,
    // persists the project record, creates runtime resources, and transitions
    // the project to stopped on success. **Does not start the project.**
    // Callers must call Start separately to bring up services.
    // Returns ErrCodeConflict if the slug already exists.
    // Returns ErrCodeInvalidInput if the slug or displayName fails validation.
    Create(ctx context.Context, slug, displayName string) (*ProjectView, error)

    // List returns all active projects (excluding destroyed ones).
    // Health fields are not populated in list results.
    List(ctx context.Context) ([]*ProjectView, error)

    // Get returns a single project by slug with masked config and live health status.
    // Returns ErrCodeNotFound if the project does not exist or is destroyed.
    Get(ctx context.Context, slug string) (*ProjectView, error)

    // Start brings up all Docker Compose services for the project.
    // Returns ErrCodeInvalidState if the project is not in a startable state.
    Start(ctx context.Context, slug string) (*ProjectView, error)

    // Stop tears down all Docker Compose services while preserving data.
    // Returns ErrCodeInvalidState if the project is not running.
    Stop(ctx context.Context, slug string) (*ProjectView, error)

    // Reset performs a full data wipe and re-provision:
    //   Stop (if running) → Destroy runtime resources → Create runtime resources → Start
    // After Reset the project is in running state with a clean database.
    // Returns ErrCodeNotFound if the project does not exist.
    Reset(ctx context.Context, slug string) (*ProjectView, error)

    // Delete destroys all runtime resources and soft-deletes the project record.
    // The project record is retained for audit purposes (status = destroyed).
    // Returns ErrCodeInvalidState if the project is not in a destroyable state.
    Delete(ctx context.Context, slug string) (*ProjectView, error)
}
```

### 建構函式

```go
// Config holds the external dependencies required to construct a ProjectService.
type Config struct {
    ProjectRepo     store.ProjectRepository
    ConfigRepo      store.ConfigRepository
    Adapter         domain.RuntimeAdapter
    PortAllocator   domain.PortAllocator
    SecretGenerator domain.SecretGenerator
    // Logger is optional; falls back to slog.Default() when nil.
    Logger          *slog.Logger
}

// NewProjectService constructs a ProjectService with all dependencies.
// Returns an error if any required field in cfg is nil.
func NewProjectService(cfg Config) (ProjectService, error)
```

---

## 執行流程

### `Create(ctx, slug, displayName)`

```
1. domain.ValidateSlug(slug) — 回傳 ErrCodeInvalidInput on failure
2. domain.NewProject(slug, displayName) — 建立 ProjectModel(status=creating)
   > domain.NewProject 的初始 status 即為 creating；此為狀態機起始點，非 UpdateStatus 設定。
3. store.ProjectRepo.Create(ctx, project) — 回傳 ErrCodeConflict on ErrProjectAlreadyExists
4. domain.GenerateProjectSecrets(secretGenerator) — 產生 per-project secrets map
5. domain.PortAllocator.AllocatePorts(ctx) — 分配 PortSet
6. domain.ResolveConfig(project, secrets, portSet, nil) — 產生 ProjectConfig
7. store.ConfigRepo.SaveConfig(ctx, slug, config) — 持久化設定
8. adapter.Create(ctx, project, config) — 建立 runtime 資源（目錄、.env）
9. store.ProjectRepo.UpdateStatus(ctx, slug, stopped, creating, "") — 終態 stopped
   > previousStatus=creating：記錄本次成功轉換的起始狀態，用於 error recovery 判斷路徑。
10. 回傳 toProjectView(project, config, nil)
```

**設計說明：** `Create` 只負責佈建資源，**不自動啟動服務**。
終態為 `stopped`。`sbctl project create` CLI 命令在 use-case 層只呼叫 `Create`；
若需建立後立即啟動，CLI 可接著呼叫 `Start`（或作為 `--start` 旗標的實作方式）。

**補償策略（best-effort）：**
- 步驟 3-7 失敗（store/config 操作）：直接回傳錯誤，project record 視步驟而定是否已寫入。
  若步驟 3 已成功但後續失敗，呼叫 `SetError + UpdateStatus(error)` 並記錄 lastError。
- 步驟 8（adapter.Create）失敗：`SetError + UpdateStatus(error)`。
  不自動清除 store 中的 project/config rows（保留以利 retry 與 audit）。
- 若 `UpdateStatus` 本身失敗：以 `slog.Error` 記錄孤立狀態，不阻斷 error 回傳。

### `Start(ctx, slug)`

```
1. store.ProjectRepo.GetBySlug(ctx, slug) — 回傳 ErrCodeNotFound
2. project.CanStart() — 回傳 ErrCodeInvalidState if false
3. store.ProjectRepo.UpdateStatus(ctx, slug, starting, prev, "")
4. adapter.Start(ctx, project) — docker compose up -d
5. store.ProjectRepo.UpdateStatus(ctx, slug, running, starting, "")
6. adapter.Status(ctx, project) — 取得健康狀態（best-effort，失敗不影響狀態）
7. 回傳 toProjectView(project, nil, health)
```

**補償策略：** 步驟 4 失敗 → `SetError + UpdateStatus(error, starting, errMsg)`。

### `Stop(ctx, slug)`

```
1. store.ProjectRepo.GetBySlug(ctx, slug)
2. project.CanStop() — 回傳 ErrCodeInvalidState if false
3. store.ProjectRepo.UpdateStatus(ctx, slug, stopping, running, "")
4. adapter.Stop(ctx, project) — docker compose down
5. store.ProjectRepo.UpdateStatus(ctx, slug, stopped, stopping, "")
6. 回傳 toProjectView(project, nil, nil)
```

### `Reset(ctx, slug)`

```
1. store.ProjectRepo.GetBySlug(ctx, slug) — 回傳 ErrCodeNotFound
2. 若 status == running: 執行 Stop 流程（步驟 2-4 of Stop）
3. 若 status ∉ {stopped, error} 且非 running: 回傳 ErrCodeInvalidState
   （允許的起始狀態：running, stopped, error）
4. project.CanDestroy() 驗證（error 狀態在 CanDestroy 中已允許）
5. store.ProjectRepo.UpdateStatus(ctx, slug, destroying, prev, "")
6. adapter.Destroy(ctx, project) — docker compose down -v + 刪目錄
   失敗 → SetError + UpdateStatus(error, destroying, errMsg)；回傳 ErrCodeInternal
7. store.ConfigRepo.GetConfig(ctx, slug) — 取得原設定（含原有 ports）
8. domain.GenerateProjectSecrets(secretGenerator) — 重新產生 secrets（清除舊資料）
9. domain.PortAllocator.AllocatePorts(ctx) — **重新分配 ports**（不沿用）
   > 設計決策：PortAllocator 介面只有 AllocatePorts(ctx)，不支援指定 preferred ports。
   > Reset 一律重新分配，避免 port 衝突。新舊 ports 差異記錄於 config 變更歷程。
10. domain.ResolveConfig(project, secrets, newPortSet, nil) + store.ConfigRepo.SaveConfig(...)
11. store.ProjectRepo.UpdateStatus(ctx, slug, creating, destroying, "")
12. adapter.Create(ctx, project, newConfig)
    失敗 → SetError + UpdateStatus(error, creating, errMsg)；回傳 ErrCodeInternal
    （孤立的 config rows 保留在 store，不清除，利於 operator 重試）
13. adapter.Start(ctx, project)
    失敗 → adapter.Destroy（best-effort 清除已建立的資源）→ SetError + UpdateStatus(error, creating, errMsg)；回傳 ErrCodeInternal
    若 best-effort Destroy 也失敗：slog.Error 記錄孤立資源（"orphaned_runtime_resource"）
14. store.ProjectRepo.UpdateStatus(ctx, slug, running, creating, "")
15. adapter.Status(ctx, project) — 取得健康狀態（best-effort）
16. 回傳 toProjectView(project, nil, health)
```

**補償策略：**
- 步驟 6（Destroy）失敗：SetError，終止 Reset，不繼續後續步驟
- 步驟 12（Create）失敗：SetError，不需清除（资源尚未建立）
- 步驟 13（Start）失敗：best-effort 呼叫 Destroy 清除已建立資源，再 SetError

### `Delete(ctx, slug)`

```
1. store.ProjectRepo.GetBySlug(ctx, slug) — 回傳 ErrCodeNotFound
2. project.CanDestroy() — 回傳 ErrCodeInvalidState if false
3. store.ProjectRepo.UpdateStatus(ctx, slug, destroying, ...)
4. adapter.Destroy(ctx, project) — 清除所有 runtime 資源
5. store.ProjectRepo.Delete(ctx, slug) — 軟刪除（status=destroyed）
6. 回傳 toProjectView(project, nil, nil)
```

### `Get(ctx, slug)`

```
1. store.ProjectRepo.GetBySlug(ctx, slug) — 回傳 ErrCodeNotFound (含 destroyed)
2. store.ConfigRepo.GetConfig(ctx, slug)
3. 若 status == running: adapter.Status(ctx, project)
4. 組裝 ProjectView，config 欄位使用 ProjectConfig.Get（masked）
5. 計算 ProjectURLs（從 config 中取 Kong/Studio port）
6. 回傳
```

---

## 錯誤處理

| 情境 | 來源錯誤 | UsecaseError.Code | 處理方式 |
|------|---------|-------------------|---------|
| 專案不存在 | `store.ErrProjectNotFound` 或 `domain.ErrProjectNotFound` | `not_found` | 兩個 sentinel 均映射，使用 `errors.Is` 依序檢查 |
| Slug 重複 | `store.ErrProjectAlreadyExists` | `conflict` | 直接包裝回傳 |
| 非法 slug / displayName | `domain.ErrInvalidSlug`、`domain.ErrInvalidDisplayName` 等 | `invalid_input` | 直接包裝回傳 |
| 狀態不允許操作 | `domain.ErrInvalidTransition` | `invalid_state` | 包裝並說明當前狀態 |
| Adapter 操作失敗 | `*domain.AdapterError`、`*domain.StartError` | `internal` | SetError + 持久化 lastError；`slog.Error` 記錄 |
| Port 耗盡 | `domain.ErrNoAvailablePort` | `internal` | 視為 internal，訊息說明原因 |
| DB 不可用 | `store.ErrStoreUnavailable` | `internal` | 直接回傳，不重試 |

**多步驟操作補償原則：**
- 以 **best-effort + SetError** 為補償策略：失敗時盡力更新 project 狀態為 error 並記錄 lastError
- 不實作 saga / distributed transaction；不自動清除已寫入的 store rows
- 若 `UpdateStatus` 本身失敗，以 `slog.Error` 記錄孤立狀態（系統最終一致性需靠 operator 處理）
- Sensitive 錯誤訊息（如 DB 連線字串）不得出現在 `UsecaseError.Message` 中

---

## 測試策略

### 需要測試的行為

**`Create`：**
- 成功路徑：正確呼叫 store + adapter，回傳 ProjectView
- Slug 重複：回傳 ErrCodeConflict
- Adapter Create 失敗：project 狀態轉為 error，持久化 lastError
- Adapter Start 失敗：同上

**`Start`：**
- 成功路徑：stopped → running
- 非法狀態（已在 running）：回傳 ErrCodeInvalidState
- Adapter Start 失敗：狀態轉為 error

**`Stop`：**
- 成功路徑：running → stopped
- 非法狀態（已在 stopped）：回傳 ErrCodeInvalidState

**`Reset`：**
- 成功路徑：running → stopped → destroying → creating → running
- 從 stopped 開始：跳過 Stop 步驟
- Adapter Destroy 失敗：狀態轉為 error

**`Delete`：**
- 成功路徑：stopped → destroying → destroyed（軟刪除）
- 非法狀態（running）：回傳 ErrCodeInvalidState

**`Get`：**
- 已存在：回傳含 config（masked）+ health（若 running）的 ProjectView
- 不存在：回傳 ErrCodeNotFound
- Config sensitive 欄位已 masked（"***"）

**`List`：**
- 回傳所有非 destroyed 專案，不含 config

### 測試類型分配

| 測試類型 | 測試目標 | 層次 |
|---------|---------|------|
| 單元測試 | 所有 ProjectService 方法的 happy path + error path | usecase |
| 單元測試 | ProjectView 轉換（masked config、URLs 組裝） | usecase |
| 單元測試 | Reset 的 port 沿用邏輯 | usecase |
| 整合測試 | 不適用（use-case 層不直接依賴 I/O） | — |

### Mock 策略

- `store.ProjectRepository` → 手工 mock struct（實作介面，記錄呼叫，與 Phase 1/2 慣例一致）
- `store.ConfigRepository` → 同上
- `domain.RuntimeAdapter` → 使用 `internal/domain/mock_adapter.go`（Phase 1 已存在）
- `domain.PortAllocator` → 手工 mock，回傳固定 PortSet
- `domain.SecretGenerator` → 手工 mock，回傳固定 secrets（方便測試失敗路徑）

所有 mock 定義於 `internal/usecase/mock_test.go`（僅供測試使用）。**不引入 mockery 或 gomock**，維持現有手工 mock 風格。

### CI 執行方式

- 所有 use-case 單元測試在一般 CI 中執行（`go test -race ./internal/usecase/...`）
- 不需要 Docker 或真實 DB

---

## Production Ready 考量

### 錯誤處理
- 所有 adapter 失敗都持久化 lastError 至 store，確保系統狀態可觀測
- `UsecaseError.Message` 只包含安全的使用者可見訊息，不暴露內部細節

### 日誌與可觀測性
- 每個操作開始與結束時記錄 structured log（`log/slog`，Go 1.21+ 標準庫），包含 `slug`、`operation`、`duration_ms`
- 錯誤時記錄 `error` 欄位（不含 sensitive 值）
- 補償失敗（如 UpdateStatus 失敗）記錄 `slog.Error` + `"orphaned_state"` key
- `Config.Logger` 為可選；nil 時使用 `slog.Default()`

### 輸入驗證
- `slug`：由 `domain.ValidateSlug()` 驗證（已有完整實作）
- `displayName`：由 `domain.NewProject()` 驗證（trim、non-empty、max 100 runes）

### 安全性
- `ProjectConfig.Get()` 自動 mask sensitive 欄位（"***"）
- `ProjectURLs` 只包含公開端點，不含 credentials

### 優雅降級
- Adapter 操作有 timeout（從 adapter 層傳入，非 use-case 層責任）
- Store 不可用時，直接回傳 `ErrCodeInternal`，不重試

### 設定管理
- 所有依賴透過 `Config` struct 注入，無全域狀態
- `Config.Logger`：可選，nil 時 fallback to `slog.Default()`
- 無其他 use-case 層專屬設定；timeout 由 adapter 管理

---

## 待決問題

- ~~**Reset 的 port 策略**~~：已決定 — Reset 一律重新分配新 ports，`PortAllocator` 介面不擴充。
- ~~**日誌框架**~~：已決定 — 使用標準庫 `log/slog`，`Config.Logger` 可選注入。
- **`Update` 方法（DisplayName 修改）**：有意省略於 Phase 3，後續版本補充。

---

## 審查

### Reviewer A（架構）

- **狀態：** APPROVED（第三輪）
- **確認項目：** HealthView/ServiceView 完整定義 ✅、UpdateStatus CAS 語意說明 ✅、Reset 補償粒度細化 ✅

### Reviewer B（實作）

- **狀態：** APPROVED（第二輪）
- **確認項目：** UsecaseError.Error()/Unwrap() ✅、PortAllocator 介面一致性 ✅、SecretGenerator 注入 ✅、Create 終態 stopped ✅、雙重 ErrProjectNotFound 映射 ✅

---

## 任務

### 任務 1：定義 usecase 層型別與介面
- **影響檔案：** `internal/usecase/project_service.go`（新增）
- **內容：** `ProjectService` 介面、`ProjectView`、`HealthView`、`ServiceView`、`ProjectURLs`、`UsecaseError`、`ErrorCode` 常數、`Config` struct、`NewProjectService` 建構函式簽名
- **驗收標準：** `go build ./internal/usecase/` 通過

### 任務 2：實作 projectService — Create + List + Get
- **影響檔案：** `internal/usecase/project_service_impl.go`（新增）
- **內容：** `projectService` struct + `Create`、`List`、`Get` 方法
- **驗收標準：** 對應單元測試通過

### 任務 3：實作 projectService — Start + Stop + Delete
- **影響檔案：** `internal/usecase/project_service_impl.go`（修改）
- **內容：** `Start`、`Stop`、`Delete` 方法
- **驗收標準：** 對應單元測試通過

### 任務 4：實作 projectService — Reset
- **影響檔案：** `internal/usecase/project_service_impl.go`（修改）
- **內容：** `Reset` 方法（含分步補償邏輯）
- **驗收標準：** 對應單元測試通過（happy path + 各步驟失敗路徑）

### 任務 5：Mock + 單元測試
- **影響檔案：** `internal/usecase/mock_test.go`、`internal/usecase/project_service_test.go`（新增）
- **內容：** 手工 mock（ProjectRepository、ConfigRepository、PortAllocator、SecretGenerator）+ 所有方法的 table-driven 測試
- **驗收標準：** `go test -race ./internal/usecase/...` 全部通過

---

## 程式碼審查

- **審查結果：** PASS
- **發現問題：** 無重大問題。錯誤處理完整、狀態機轉換正確、config masking 正確、Reset 補償路徑完整、測試覆蓋充分。
- **修正記錄：** 無需修正
