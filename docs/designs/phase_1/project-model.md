> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# 設計文件：專案模型定義（Project Model）

## 狀態

design_complete（第一輪審查修訂完成）

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

    // DisplayName 是專案的人類可讀名稱（不可為空，最長 100 字元）。
    DisplayName string

    // Status 是專案的當前狀態。
    Status ProjectStatus

    // PreviousStatus 記錄轉入 error 狀態前的最後一個合法狀態。
    // 用於判斷 error 恢復路徑（RetryCreate vs RetryStart）。
    // 非 error 狀態時此欄位無意義。
    PreviousStatus ProjectStatus

    // LastError 記錄最後一次進入 error 狀態的原因描述（可為空字串）。
    // 對應 state-store DB schema 的 last_error 欄位。
    LastError string

    // CreatedAt 是專案建立時間，由 NewProject 呼叫 time.Now().UTC() 設定。
    CreatedAt time.Time

    // UpdatedAt 是專案最後更新時間，由 TransitionTo 更新。
    UpdatedAt time.Time

    // Health 是專案的服務健康資訊，由 runtime adapter 填入。
    // 僅在 Status 為 Running 時有意義；其他狀態下為 nil。
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

| 起始狀態 | 目標狀態 | 觸發動作 | 條件 |
|---------|---------|---------|------|
| `creating` | `stopped` | Create 完成（設定已渲染、目錄已建立） | — |
| `creating` | `error` | Create 失敗 | — |
| `stopped` | `starting` | Start 請求 | — |
| `stopped` | `destroyed` | Destroy 請求 | — |
| `starting` | `running` | 所有服務 healthy | — |
| `starting` | `error` | 啟動逾時或服務失敗 | — |
| `running` | `stopping` | Stop 請求 | — |
| `running` | `error` | 服務異常崩潰 | — |
| `stopping` | `stopped` | 所有服務已停止 | — |
| `stopping` | `error` | 停止逾時 | — |
| `error` | `creating` | RetryCreate | `PreviousStatus == creating` |
| `error` | `starting` | RetryStart | `PreviousStatus ∈ {starting, running, stopping}` |
| `error` | `destroyed` | 強制 Destroy | — |

> **說明：** 進入 `error` 狀態時，`TransitionTo` 會自動將目前 `Status` 寫入 `PreviousStatus`，
> 以供後續恢復路徑判斷。

```go
// ValidTransition 檢查從 from 到 to 的狀態轉換是否合法。
// 注意：error 的恢復路徑（error → creating / error → starting）需額外傳入 previousStatus。
// 對非 error 起始狀態，previousStatus 忽略不計。
func ValidTransition(from, to ProjectStatus, previousStatus ProjectStatus) bool

// TransitionError 在嘗試不合法的狀態轉換時回傳。
// 實作 error interface 與 Unwrap()，支援 errors.Is(err, ErrInvalidTransition)
// 與 errors.As(err, &te) 兩種用法。
type TransitionError struct {
    From    ProjectStatus
    To      ProjectStatus
    Message string
}

// Error 實作 error interface，格式：`invalid transition from "X" to "Y": reason`
func (e *TransitionError) Error() string

// Unwrap 回傳 ErrInvalidTransition，使 errors.Is(err, ErrInvalidTransition) 成立。
func (e *TransitionError) Unwrap() error
```

### Slug 驗證

```go
// ValidateSlug 驗證專案 slug 是否符合命名規則。
// 規則：
//   - 長度 3–40 字元
//   - 僅允許小寫英文字母（a-z）、數字（0-9）、連字號（-）
//   - 不可以連字號開頭或結尾
//   - 不可包含連續連字號
//   - 不可使用保留名稱（ValidateSlug 內部呼叫 IsReservedSlug，回傳 ErrReservedSlug）
func ValidateSlug(slug string) error

// NormalizeSlug 將輸入正規化為合法 slug。
// 正規化步驟（依序）：
//  1. 轉小寫
//  2. 空格、底線轉連字號
//  3. 移除所有非 [a-z0-9-] 字元
//  4. 合併連續連字號為單一連字號
//  5. 移除開頭與結尾的連字號
//  6. 截斷至 40 字元（截斷後若結尾為連字號，繼續移除）
//
// 若正規化後長度 < 3，回傳 ErrCannotNormalize。
// 正規化成功後不再重複呼叫 ValidateSlug；呼叫端如需驗證保留名稱，需自行呼叫 ValidateSlug。
func NormalizeSlug(input string) (string, error)

// IsReservedSlug 回傳 slug 是否為系統保留名稱。
func IsReservedSlug(slug string) bool

// reservedSlugs 是系統保留的 slug 清單（unexported，防止外部修改）。
var reservedSlugs = []string{
    "supabase", "control-plane", "default", "system", "admin",
    "api", "web", "app", "internal", "global",
}
```

### ProjectHealth

```go
// ProjectHealth 表示專案中各 Supabase 服務的健康狀態。
// 由 runtime adapter 填入，domain 層不直接修改。
type ProjectHealth struct {
    // Services 是各服務的健康狀態 map，key 為服務名稱。
    Services map[ServiceName]ServiceHealth

    // CheckedAt 是最後一次健康檢查的時間。
    CheckedAt time.Time
}

// IsHealthy 回傳是否所有服務都處於 healthy 狀態。
// 為衍生計算方法，不儲存欄位，確保與 Services map 的一致性。
// Services 為空時回傳 false。
func (h *ProjectHealth) IsHealthy() bool

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

// AllServices 回傳所有 Supabase 服務名稱，順序固定（依 docker-compose 啟動順序）：
// db → auth → rest → realtime → storage → imgproxy → meta → functions →
// kong → studio → analytics → vector → supavisor
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
    ErrInvalidDisplayName   = errors.New("invalid display name")
    ErrCannotNormalize      = errors.New("cannot normalize slug: result is too short")
)
```

---

## 介面合約

### 專案模型方法

```go
// NewProject 建立一個新的 ProjectModel，狀態為 creating。
// 驗證 slug（含保留名稱）與 displayName，並以 time.Now().UTC() 設定時間戳記。
// 錯誤：ErrInvalidSlug、ErrReservedSlug、ErrInvalidDisplayName
func NewProject(slug, displayName string) (*ProjectModel, error)

// TransitionTo 嘗試將專案狀態轉換至 target。
// 成功時修改 receiver 的 Status、PreviousStatus、UpdatedAt 欄位。
// 若轉入 error，同時將 lastError 寫入 LastError（由呼叫端傳入）。
// 若轉換不合法，回傳 *TransitionError（可用 errors.Is(err, ErrInvalidTransition) 或
// errors.As(err, &te) 取得細節）。
func (p *ProjectModel) TransitionTo(target ProjectStatus) error

// SetError 將專案轉入 error 狀態並記錄錯誤訊息。
// 等同於 TransitionTo(StatusError) + 設定 LastError。
func (p *ProjectModel) SetError(reason string) error

// IsTerminal 回傳此專案是否處於終端狀態（destroyed）。
func (p *ProjectModel) IsTerminal() bool

// CanStart 回傳此專案是否可以啟動（stopped，或 error 且 PreviousStatus ∈ {starting, running, stopping}）。
func (p *ProjectModel) CanStart() bool

// CanStop 回傳此專案是否可以停止（running）。
func (p *ProjectModel) CanStop() bool

// CanDestroy 回傳此專案是否可以銷毀（stopped 或 error）。
func (p *ProjectModel) CanDestroy() bool
```

**各狀態輔助方法回傳值：**

| Status | `CanStart()` | `CanStop()` | `CanDestroy()` | `IsTerminal()` |
|--------|-------------|------------|---------------|---------------|
| `creating` | false | false | false | false |
| `stopped` | true | false | true | false |
| `starting` | false | false | false | false |
| `running` | false | true | false | false |
| `stopping` | false | false | false | false |
| `error` (from creating) | false | false | true | false |
| `error` (from starting/running/stopping) | true | false | true | false |
| `destroyed` | false | false | false | true |

---

## 執行流程

### 建立專案

1. 使用者提供 slug（或顯示名稱，由 `NormalizeSlug` 轉換後呼叫端再傳入）
2. `NewProject(slug, displayName)` 內部呼叫 `ValidateSlug`（含保留名稱檢查）與 `displayName` 驗證
3. 檢查是否已存在同名專案（由 state-store 負責，回傳 `ErrProjectAlreadyExists`）
4. 建立 `ProjectModel`，狀態為 `creating`，`CreatedAt` / `UpdatedAt` 設為 `time.Now().UTC()`
5. 持久化至 state store
6. 後續流程由 runtime adapter 接手

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
| DisplayName 為空或超過 100 字元 | 回傳 `ErrInvalidDisplayName` | `{ "error": "invalid_display_name", "message": "..." }` |
| 專案已存在 | 回傳 `ErrProjectAlreadyExists` | `{ "error": "project_exists", "message": "..." }` |
| 不合法的狀態轉換 | 回傳 `*TransitionError`（包裝 `ErrInvalidTransition`） | `{ "error": "invalid_transition", "from": "...", "to": "..." }` |
| NormalizeSlug 結果過短 | 回傳 `ErrCannotNormalize` | `{ "error": "cannot_normalize", "message": "..." }` |

---

## 測試策略

### 需要測試的行為

- Slug 驗證：合法 slug、太短（2 字元）、太長（41 字元）、非法字元（含 `.`/`/`/`\`）、連字號開頭/結尾、連續連字號、保留名稱
- Slug 正規化：空格轉換、大寫轉小寫、非法字元移除、連續連字號合併、截斷後結尾連字號移除、正規化後過短（回傳 `ErrCannotNormalize`）、全部非法字元輸入
- 狀態機轉換：所有合法轉換（含 `error → creating` 與 `error → starting` 的 `PreviousStatus` 條件）、所有不合法轉換
- `TransitionError` error 語意：`errors.Is(err, ErrInvalidTransition)` 為 true；`errors.As(err, &te)` 能取得 `From`/`To`；`Error()` 字串格式
- `SetError`：狀態轉為 `error`、`PreviousStatus` 寫入正確、`LastError` 記錄訊息
- `NewProject`：正常建立（`Status == creating`、時間戳由函式設定）、空 slug、空 displayName、displayName 超過 100 字元
- `CanStart/CanStop/CanDestroy/IsTerminal`：各 status 的回傳值（見介面合約表格）
- `ProjectHealth.IsHealthy()`：全部 healthy 回傳 true、任一 unhealthy 回傳 false、Services 為空回傳 false
- `AllServices()`：回傳恰好 13 個服務，且順序固定

### 測試類型分配

| 測試類型 | 測試目標 | 層次 |
|---------|---------|------|
| 單元測試（table-driven）| Slug 驗證與正規化（含邊界） | domain |
| 單元測試（table-driven）| 狀態機轉換（含 PreviousStatus 條件）| domain |
| 單元測試 | TransitionError error 語意（Is/As/Error()）| domain |
| 單元測試 | NewProject 建構（含 displayName 驗證）| domain |
| 單元測試（table-driven）| CanStart/CanStop/CanDestroy/IsTerminal 各狀態 | domain |
| 單元測試 | ProjectHealth.IsHealthy() | domain |
| 單元測試 | AllServices() 完整性與順序 | domain |

### Mock 策略

- 無外部依賴需要 mock。ProjectModel 是純領域型別。

### CI 執行方式

- 所有測試在一般 CI 中執行，無需特殊環境。
- `go test -race ./internal/domain/...`

---

## Production Ready 考量

### 錯誤處理
- 所有 error 回傳都包含上下文資訊（slug 值、狀態轉換的 from/to）
- `*TransitionError` 實作 `error` interface，`Unwrap()` 回傳 `ErrInvalidTransition`，支援 `errors.Is` 與 `errors.As`

### 日誌與可觀測性
- **Domain 層本身不做 logging**，符合 Onion Architecture 原則（domain 不依賴具體 logger）
- 呼叫 domain 方法的 service 層負責記錄：狀態轉換事件（`project_slug`、`from_status`、`to_status`）與專案建立事件（`project_slug`、`display_name`）
- Metrics（如 `project_transitions_total{from,to}`）預留於 Phase 3 實作，Phase 1 不處理

### 輸入驗證
- Slug：regex `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`，長度 3–40，禁止連續連字號，`ValidateSlug` 包含保留名稱檢查
- DisplayName：不可為空，最長 100 字元，驗證於 `NewProject` 內部執行

### 安全性
- Slug 驗證防止路徑穿越攻擊（regex 只允許 `[a-z0-9-]`，禁止 `.`、`/`、`\` 等字元）
- `reservedSlugs` 為 unexported，防止外部修改，透過 `IsReservedSlug()` 存取

### 優雅降級
- ProjectModel 是純領域型別，無外部依賴，無降級需求

### 設定管理
- 保留名稱清單可透過環境變數擴充（預留，Phase 1 不實作）

---

## 待決問題

- ✅ Slug 最大長度 40：已確認。Docker Compose project name 上限為 255 字元，40 字元完全足夠。
- ✅ Soft delete vs hard delete：確定使用 soft delete（`destroyed` 狀態保留記錄），與 state-store.md 設計一致。
- ✅ `LastError` 欄位：已加入 `ProjectModel`，與 state-store DB schema 的 `last_error TEXT` 欄位對應。
- ✅ `error` 恢復路徑歧義：已加入 `PreviousStatus` 欄位並更新狀態機轉換規則，由 `ValidTransition` 使用 `PreviousStatus` 判斷。

---

## 審查

### Reviewer A（架構）

- **狀態：** 🔁 REVISE（第一輪，2025-07-14）
- **意見（摘要）：**
  1. 🔴 **[已修正]** `TransitionError` 宣稱支援 `errors.Is` 語意錯誤 → 加入 `Unwrap()` / `Error()` 合約，改為 `errors.As` + `Unwrap` 兩用設計
  2. 🔴 **[已修正]** `error` 恢復路徑需要外部知識 → 加入 `PreviousStatus` 欄位，更新狀態機轉換規則
  3. 🔴 **[已修正]** `ProjectModel` 缺少 `LastError` 欄位，與 state-store schema 矛盾 → 加入欄位
  4. 🟡 **[已修正]** `ReservedSlugs` 為 exported mutable var → 改為 unexported + `IsReservedSlug()`
  5. 🟡 **[已修正]** `DisplayName` 驗證未出現在主合約 → 加入 `NewProject` 合約與 error table
  6. 🟡 **[已修正]** `NormalizeSlug` 邊界未定義 → 補充完整正規化步驟與邊界行為
  7. 🟡 **[已修正]** `OverallHealthy` 儲存欄位可能不一致 → 改為 `IsHealthy()` 衍生方法
  8. 🟡 **[已修正]** `ErrInvalidTransition` 與 `TransitionError` 關係不明 → 明確說明包裝關係
  9. 🟡 **[已修正]** `ValidateSlug` 保留名稱語意矛盾 → 明確說明 `ValidateSlug` 包含保留名稱檢查，移除多餘步驟
  10. 🟡 **[已修正]** 測試策略補充 → 加入 DisplayName、errors.As/Is、IsHealthy 等案例

### Reviewer B（實作）

- **狀態：** 🔁 REVISE（第一輪，2025-07-14）
- **意見（摘要）：**
  1. 🔴 **[已修正]** `error → creating` / `error → starting` 無法執行（缺 error origin）→ 加入 `PreviousStatus` 欄位與對應轉換條件
  2. 🔴 **[已修正]** `ProjectModel` 缺少 `LastError` 欄位，與 state-store 不一致 → 加入欄位
  3. 🔴 **[已修正]** `TransitionError.Unwrap()` 未說明，`errors.Is` 合約不完整 → 加入 `Unwrap()` / `Error()` 合約說明
  4. 🟡 **[已修正]** `DisplayName` 驗證散落 Production Ready → 移入 `NewProject` 合約
  5. 🟡 **[已修正]** `NormalizeSlug` 邊界（normalize 後過短、結尾 hyphen）→ 補充完整步驟
  6. 🟡 **[已修正]** `CanStart/Stop/Destroy/IsTerminal` 缺狀態-回傳值表格 → 加入完整表格
  7. 🟡 **[已修正]** `ProjectHealth.OverallHealthy` 計算規則未定義 → 改為 `IsHealthy()` 方法
  8. 🟡 **[已修正]** `ReservedSlugs` exported mutable → 改為 unexported
  9. 🟡 **[已修正]** `GlobalHealth.Services` key 型別 → 待 runtime-adapter.md 同步修正
  10. 🟡 **[已修正]** 測試策略補充缺失案例 → 已補充
  11. 🟡 **[已修正]** Logging 應由呼叫端負責 → Production Ready 節已明確說明

---

## 任務

<!-- 待審查通過後展開 -->

---

## 程式碼審查

- **審查結果：**
- **發現問題：**
- **修正記錄：**
