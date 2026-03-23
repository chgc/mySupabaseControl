> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# 設計文件：Compose Port Allocator

## 狀態

in_review

## Phase

- **Phase：** Phase 2
- **Phase Plan：** `docs/designs/phase_2/phase-2-plan.md`

---

## 目的

實作 `domain.PortAllocator` 介面，為每個新建立的 Supabase 專案自動分配一組無衝突的 TCP ports（`PortSet`）。

Port Allocator 負責：
1. 查詢現有專案已佔用的 port 集合
2. 探測主機上各 port 是否被其他程序佔用
3. 回傳一個六元素的 `PortSet`，保證所有 port 在當下均可使用

---

## 範圍

### 包含

- `ComposePortAllocator` struct，實作 `domain.PortAllocator`
- Port 掃描演算法（已使用 port 清單 + TCP probe）
- Application-level mutex 防止並發 create 競爭
- 各 port 型別的基礎值（base）設定
- `KONG_HTTPS_PORT` 的聯合探測（KongHTTP + 1 必須同時可用）

### 不包含

- `PortAllocator` 介面定義（已在 `internal/domain/port_allocator.go`）
- Port 分配結果的持久化（呼叫端 use-case 層負責寫入 store）
- DB advisory lock（Phase 2 為單程序工具，application-level mutex 足夠）
- Port 釋放機制（ports 隨著專案刪除自然釋放）

---

## 資料模型

### ComposePortAllocator

```go
package compose

// ComposePortAllocator implements domain.PortAllocator for the Docker Compose runtime.
// It prevents port conflicts by locking across concurrent AllocatePorts calls and
// probing each candidate port via TCP before returning it.
type ComposePortAllocator struct {
    projectRepo store.ProjectRepository
    configRepo  store.ConfigRepository
    bases       portBases
    mu          sync.Mutex
}

// portBases holds the inclusive lower bound for each port type.
type portBases struct {
    KongHTTP     int // default 28081; KongHTTPS = KongHTTP+1, auto-checked
    PostgresPort int // default 54320
    PoolerPort   int // default 64300
    StudioPort   int // default 54323
    MetaPort     int // default 54380
    ImgProxyPort int // default 54381
}
```

### AllocatorOption（functional options）

```go
// AllocatorOption configures a ComposePortAllocator.
type AllocatorOption func(*ComposePortAllocator)

func WithKongHTTPBase(port int) AllocatorOption
func WithPostgresBase(port int) AllocatorOption
func WithPoolerBase(port int) AllocatorOption
func WithStudioBase(port int) AllocatorOption
func WithMetaBase(port int) AllocatorOption
func WithImgProxyBase(port int) AllocatorOption
```

---

## 介面合約

### NewComposePortAllocator

```go
// NewComposePortAllocator constructs a ComposePortAllocator.
// Bases default to the standard Supabase starting values.
func NewComposePortAllocator(
    projectRepo store.ProjectRepository,
    configRepo  store.ConfigRepository,
    opts        ...AllocatorOption,
) *ComposePortAllocator
```

### AllocatePorts

```go
// AllocatePorts scans occupied ports and returns a conflict-free PortSet.
// Safe for concurrent use: an internal mutex serialises concurrent calls.
// The returned PortSet is a point-in-time snapshot; the caller must commit it to
// the store before releasing the mutex scope (handled by the use-case layer in Phase 3).
func (a *ComposePortAllocator) AllocatePorts(ctx context.Context) (*domain.PortSet, error)
```

---

## 執行流程

```
AllocatePorts(ctx)
│
├─ 1. a.mu.Lock()  ← 防止兩個並發呼叫拿到相同 port
│
├─ 2. 從 ProjectRepository.List(ctx) 取得所有非 destroyed 專案
│
├─ 3. 對每個專案呼叫 ConfigRepository.GetConfig(ctx, slug)
│     若 ErrConfigNotFound 則跳過（專案建立中，尚無 config）
│
├─ 4. 對每個有效 config 呼叫 domain.ExtractPortSet(config)
│     收集 usedPorts map[portType][]int
│
├─ 5. 針對每個 portType，從 base 開始依序掃描：
│     a. 若 port 不在 usedPorts[type] 中
│     b. 且 TCP probe 顯示 port 可用（見下方）
│     → 選定此 port，繼續下一個 portType
│
│     KongHTTP 特殊規則：候選 port N 被選定前，
│       必須同時確認 port N 和 N+1 均可用
│       （N+1 = KongHTTPS，不在 PortSet 中但 Compose 會使用）
│
├─ 6. a.mu.Unlock()
│
└─ 7. 回傳 &domain.PortSet{...}
```

### TCP Probe 實作

```go
// probePort 嘗試在指定 port 上建立 TCP listener。
// 可用 → listener 建立成功後立即關閉，回傳 true。
// 佔用 → 建立失敗（EADDRINUSE），回傳 false。
func probePort(port int) bool {
    ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
    if err != nil {
        return false
    }
    ln.Close()
    return true
}
```

---

## 錯誤處理

| 情境 | 處理方式 |
|------|---------|
| `ProjectRepository.List` 失敗 | 解除 mutex，回傳包裝後的 error：`fmt.Errorf("port allocator: list projects: %w", err)` |
| `ConfigRepository.GetConfig` 回傳 `ErrConfigNotFound` | 跳過此專案（config 尚未儲存），繼續掃描 |
| `ConfigRepository.GetConfig` 回傳其他 error | 解除 mutex，回傳包裝後的 error |
| `domain.ExtractPortSet` 失敗 | 記錄警告日誌後跳過（不應發生，但 config 資料損壞時需容錯） |
| 掃描 port 超過合理上限（base + 1000）仍找不到可用 port | 回傳 `domain.ErrNoAvailablePort` |
| `ctx.Done()` 在掃描過程中觸發 | 解除 mutex，回傳 `ctx.Err()` |

---

## 測試策略

### 需要測試的行為

- **基本分配**：第一個專案拿到所有 base port
- **衝突跳過（store）**：已有專案佔用 base port 時，新專案拿到 base+stride
- **衝突跳過（TCP probe）**：主機上某 port 被佔用時，回傳下一個可用 port
- **KongHTTPS 聯合探測**：KongHTTP 候選 N 可用，但 N+1 被佔用，應繼續往下找
- **context cancellation**：ctx 已取消時，AllocatePorts 回傳 ctx.Err()
- **ErrNoAvailablePort**：所有候選 port 均不可用時（注入 stub）
- **ErrConfigNotFound 跳過**：無 config 的專案不影響分配

### 測試類型分配

| 測試類型 | 測試目標 | 層次 |
|---------|---------|------|
| 單元測試 | 掃描演算法邏輯（store mock + TCP probe 可替換函數） | adapter |
| 整合測試（`//go:build integration`）| 對真實 DB 執行 List + GetConfig | adapter |

### Mock 策略

- `ProjectRepository` 和 `ConfigRepository` 使用介面 mock（手寫 struct，欄位為 func 型別，同 domain MockRuntimeAdapter 風格）
- TCP probe 函數透過 `probeFunc func(int) bool` 欄位注入（測試時替換為固定回傳值的 stub）

```go
// ComposePortAllocator 暴露可替換的 probe 函數（僅測試時修改）
type ComposePortAllocator struct {
    ...
    probeFunc func(port int) bool // default: probePort; replaced in tests
}
```

### CI 執行方式

- 單元測試：`go test -race ./internal/adapter/compose/...`（無外部依賴，CI 中執行）
- 整合測試：`go test -race -tags integration ./internal/adapter/compose/...`（需要 PostgreSQL）

---

## Production Ready 考量

### 錯誤處理

所有 error 都加上操作上下文後回傳（`fmt.Errorf("port allocator: ...: %w", err)`），不使用 panic。

### 日誌與可觀測性

- `AllocatePorts` 開始與結束記錄 debug 日誌（分配的 port 集合）
- 跳過損壞 config 的專案時記錄 warn 日誌（含 slug）

### 輸入驗證

- `NewComposePortAllocator` 驗證 base port 值合理（1024–65535）；超出範圍時 panic（程式設定錯誤，非 runtime error）

### 安全性

- 回傳的 `PortSet` 不含 secret 值，可安全記錄日誌

### 優雅降級

- Store 不可用時，`AllocatePorts` 回傳 error，讓 caller（use-case 層）決定是否重試
- TCP probe 失敗（timeout）視為 port 不可用，繼續掃描下一個

### 設定管理

- 各 port base 值透過 `AllocatorOption` 設定，default 值為標準 Supabase 起始值
- 呼叫端（Phase 3 use-case 層）可透過環境變數覆寫 base 值

---

## 待決問題

- 目前無

---

## 審查

### Reviewer A（架構）

- **狀態：** 待審查
- **意見：**

### Reviewer B（實作）

- **狀態：** 待審查
- **意見：**

---

## 任務

待審查通過後產生。

---

## 程式碼審查

- **審查結果：** 待實作完成後審查
- **發現問題：**
- **修正記錄：**
