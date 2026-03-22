> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# 設計文件：專案模型定義（Project Model）

## 狀態

design_complete

## Phase

- **Phase：** Phase 1
- **Phase Plan：** `docs/designs/phase_1/phase-1-plan.md`

---

## 目的

定義 Control Plane 的核心領域型別 — **ProjectModel**。這是所有其他功能（config schema、runtime adapter、state store）的共用基礎。ProjectModel 描述一個 Supabase 專案的 identity、狀態與 metadata，且 **不包含任何 runtime 特定資訊**。

---

## 範圍

### 包含

- `ProjectModel` struct 定義
- 專案狀態機（`ProjectStatus` enum + 合法轉換）
- Slug 驗證規則與正規化
- `ProjectHealth` 型別（服務層級的健康資訊）
- 專案相關的 sentinel errors

### 不包含

- 設定 schema（見 `config-schema.md`）
- DB 持久化邏輯（見 `state-store.md`）
- Runtime 操作邏輯（見 `runtime-adapter.md`）
- HTTP API 層（Phase 3）

---

## 資料模型

### ProjectModel

```go
package domain

import "time"

// ProjectModel 表示一個 Supabase 專案的核心 identity 與 metadata。
// 不包含 runtime 特定資訊（如 .env 內容或 K8s namespace）。
type ProjectModel struct {
    // Slug 是專案的唯一識別名稱，用於目錄名稱、compose project name 等。
    // 格式：小寫英數字與連字號，3–40 字元，不可以連字號開頭或結尾。
    Slug string

    // DisplayName 是專案的人類可讀名稱。
    DisplayName string

    // Status 是專案的當前狀態。
    Status ProjectStatus

    // CreatedAt 是專案建立時間。
    CreatedAt time.Time

    // UpdatedAt 是專案最後更新時間。
    UpdatedAt time.Time

    // Health 是專案的服務健康資訊。
    // 僅在 Status 為 Running 時有意義。
    Health *ProjectHealth
}
```

### ProjectStatus

```go
type ProjectStatus string

const (
    StatusCreating  ProjectStatus = "creating"
    StatusStopped   ProjectStatus = "stopped"
    StatusStarting  ProjectStatus = "starting"
    StatusRunning   ProjectStatus = "running"
    StatusStopping  ProjectStatus = "stopping"
    StatusDestroyed ProjectStatus = "destroyed"
    StatusError     ProjectStatus = "error"
)
```

**狀態機轉換圖：**

```
                  ┌──────────┐
          ┌──────►│  error   │◄────────────────────┐
          │       └──────────┘                     │
          │            │                           │
          │            │ RetryCreate / RetryStart   │
          │            ▼                           │
     ┌────┴─────┐  ┌──────────┐  ┌──────────┐  ┌──┴───────┐
─────►│ creating │─►│ stopped  │─►│ starting │─►│ running  │
     └──────────┘  └────┬─────┘  └──────────┘  └──┬───────┘
                        │                          │
                        │                          │ Stop
                        │                     ┌────▼─────┐
                        │                     │ stopping │
                        │                     └────┬─────┘
                        │                          │
                        │◄─────────────────────────┘
                        │
                        │ Destroy
                   ┌────▼─────┐
                   │destroyed │
                   └──────────┘
```

**合法轉換規則：**

| 起始狀態 | 目標狀態 | 觸發動作 |
|---------|---------|---------|
| `creating` | `stopped` | Create 完成（設定已渲染、目錄已建立） |
| `creating` | `error` | Create 失敗 |
| `stopped` | `starting` | Start 請求 |
| `stopped` | `destroyed` | Destroy 請求 |
| `starting` | `running` | 所有服務 healthy |
| `starting` | `error` | 啟動逾時或服務失敗 |
| `running` | `stopping` | Stop 請求 |
| `running` | `error` | 服務異常崩潰 |
| `stopping` | `stopped` | 所有服務已停止 |
| `stopping` | `error` | 停止逾時 |
| `error` | `creating` | RetryCreate（從 creating 的 error） |
| `error` | `starting` | RetryStart（從 running/starting 的 error） |
| `error` | `destroyed` | 強制 Destroy |

```go
// ValidTransition 檢查從 from 到 to 的狀態轉換是否合法。
func ValidTransition(from, to ProjectStatus) bool

// TransitionError 在嘗試不合法的狀態轉換時回傳。
type TransitionError struct {
    From    ProjectStatus
    To      ProjectStatus
    Message string
}
```

### Slug 驗證

```go
// ValidateSlug 驗證專案 slug 是否符合命名規則。
// 規則：
//   - 長度 3–40 字元
//   - 僅允許小寫英文字母（a-z）、數字（0-9）、連字號（-）
//   - 不可以連字號開頭或結尾
//   - 不可包含連續連字號
//   - 不可使用保留名稱（如 "supabase"、"control-plane"、"default"）
func ValidateSlug(slug string) error

// NormalizeSlug 將輸入正規化為合法 slug。
// 轉小寫、空格轉連字號、移除非法字元、截斷至 40 字元。
func NormalizeSlug(input string) (string, error)

// 保留的 slug 名稱清單
var ReservedSlugs = []string{
    "supabase", "control-plane", "default", "system", "admin",
    "api", "web", "app", "internal", "global",
}
```

### ProjectHealth

```go
// ProjectHealth 表示專案中各 Supabase 服務的健康狀態。
type ProjectHealth struct {
    // Services 是各服務的健康狀態 map，key 為服務名稱。
    Services map[ServiceName]ServiceHealth

    // OverallHealthy 為 true 表示所有服務都健康。
    OverallHealthy bool

    // CheckedAt 是最後一次健康檢查的時間。
    CheckedAt time.Time
}

// ServiceName 是 Supabase 服務的識別名稱。
type ServiceName string

const (
    ServiceDB        ServiceName = "db"
    ServiceAuth      ServiceName = "auth"
    ServiceREST      ServiceName = "rest"
    ServiceRealtime  ServiceName = "realtime"
    ServiceStorage   ServiceName = "storage"
    ServiceImgproxy  ServiceName = "imgproxy"
    ServiceMeta      ServiceName = "meta"
    ServiceFunctions ServiceName = "functions"
    ServiceKong      ServiceName = "kong"
    ServiceStudio    ServiceName = "studio"
    ServiceAnalytics ServiceName = "analytics"
    ServiceVector    ServiceName = "vector"
    ServiceSupavisor ServiceName = "supavisor"
)

// AllServices 回傳所有 Supabase 服務名稱。
func AllServices() []ServiceName

// ServiceHealth 表示單一服務的健康狀態。
type ServiceHealth struct {
    Status    ServiceStatus
    Message   string     // 可選的狀態訊息（如錯誤描述）
    CheckedAt time.Time
}

type ServiceStatus string

const (
    ServiceStatusHealthy   ServiceStatus = "healthy"
    ServiceStatusUnhealthy ServiceStatus = "unhealthy"
    ServiceStatusStarting  ServiceStatus = "starting"
    ServiceStatusStopped   ServiceStatus = "stopped"
    ServiceStatusUnknown   ServiceStatus = "unknown"
)
```

### Sentinel Errors

```go
var (
    ErrProjectNotFound      = errors.New("project not found")
    ErrProjectAlreadyExists = errors.New("project with this slug already exists")
    ErrInvalidSlug          = errors.New("invalid project slug")
    ErrReservedSlug         = errors.New("slug is reserved")
    ErrInvalidTransition    = errors.New("invalid status transition")
)
```

---

## 介面合約

### 專案模型方法

```go
// NewProject 建立一個新的 ProjectModel，狀態為 creating。
// 驗證 slug 並設定時間戳記。
func NewProject(slug, displayName string) (*ProjectModel, error)

// TransitionTo 嘗試將專案狀態轉換至 target。
// 若轉換不合法，回傳 TransitionError。
func (p *ProjectModel) TransitionTo(target ProjectStatus) error

// IsTerminal 回傳此專案是否處於終端狀態（destroyed）。
func (p *ProjectModel) IsTerminal() bool

// CanStart 回傳此專案是否可以啟動。
func (p *ProjectModel) CanStart() bool

// CanStop 回傳此專案是否可以停止。
func (p *ProjectModel) CanStop() bool

// CanDestroy 回傳此專案是否可以銷毀。
func (p *ProjectModel) CanDestroy() bool
```

---

## 執行流程

### 建立專案

1. 使用者提供 slug（或顯示名稱，由 NormalizeSlug 轉換）
2. `ValidateSlug(slug)` 驗證格式
3. 檢查是否為保留名稱
4. 檢查是否已存在同名專案（由 state-store 負責）
5. `NewProject(slug, displayName)` 建立 ProjectModel，狀態為 `creating`
6. 持久化至 state store
7. 後續流程由 runtime adapter 接手

### 狀態轉換

1. 呼叫 `project.TransitionTo(target)`
2. 查詢狀態機轉換表，若不合法回傳 `TransitionError`
3. 若合法，更新 `Status` 與 `UpdatedAt`
4. 持久化至 state store

---

## 錯誤處理

| 情境 | 處理方式 | 回應格式 |
|------|---------|---------|
| Slug 格式不合法 | 回傳 `ErrInvalidSlug` + 具體原因 | `{ "error": "invalid_slug", "message": "slug must be 3-40 chars..." }` |
| Slug 為保留名稱 | 回傳 `ErrReservedSlug` | `{ "error": "reserved_slug", "message": "..." }` |
| 專案已存在 | 回傳 `ErrProjectAlreadyExists` | `{ "error": "project_exists", "message": "..." }` |
| 不合法的狀態轉換 | 回傳 `TransitionError` | `{ "error": "invalid_transition", "from": "...", "to": "..." }` |

---

## 測試策略

### 需要測試的行為

- Slug 驗證：合法 slug、太短、太長、非法字元、連字號開頭/結尾、連續連字號、保留名稱
- Slug 正規化：空格轉換、大寫轉小寫、非法字元移除、截斷
- 狀態機轉換：所有合法轉換、所有不合法轉換
- NewProject：正常建立、空 slug、空 displayName
- CanStart/CanStop/CanDestroy：各狀態下的回傳值

### 測試類型分配

| 測試類型 | 測試目標 | 層次 |
|---------|---------|------|
| 單元測試 | Slug 驗證與正規化 | domain |
| 單元測試 | 狀態機轉換 | domain |
| 單元測試 | NewProject 建構 | domain |

### Mock 策略

- 無外部依賴需要 mock。ProjectModel 是純領域型別。

### CI 執行方式

- 所有測試在一般 CI 中執行，無需特殊環境。
- `go test -race ./internal/domain/...`

---

## Production Ready 考量

### 錯誤處理
- 所有 error 回傳都包含上下文資訊（slug 值、狀態轉換的 from/to）
- `TransitionError` 實作 `error` interface 且可被 `errors.Is` 匹配

### 日誌與可觀測性
- 狀態轉換事件應記錄日誌：`project_slug`、`from_status`、`to_status`
- 專案建立事件應記錄日誌：`project_slug`、`display_name`

### 輸入驗證
- Slug：regex `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`，長度 3–40
- DisplayName：不可為空，最長 100 字元

### 安全性
- Slug 驗證防止路徑穿越攻擊（禁止 `.`、`/`、`\` 等字元）
- 保留名稱清單防止與系統目錄衝突

### 優雅降級
- ProjectModel 是純領域型別，無外部依賴，無降級需求

### 設定管理
- 保留名稱清單可透過環境變數擴充（預留，Phase 1 不實作）

---

## 待決問題

- Slug 最大長度 40 是否足夠？需與 Docker Compose project name 的限制對齊。
- 是否需要 soft delete（`destroyed` 狀態保留記錄）或 hard delete？建議 Phase 1 先做 soft delete。
- Error 狀態是否需要記錄 error source（creating error vs running error）？建議加入 `LastError string` 欄位。

---

## 審查

### Reviewer A（架構）

- **狀態：**
- **意見：**

### Reviewer B（實作）

- **狀態：**
- **意見：**

---

## 任務

<!-- 待審查通過後展開 -->

---

## 程式碼審查

- **審查結果：**
- **發現問題：**
- **修正記錄：**
