> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# 設計文件：Compose Port Allocator

## 狀態

done

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
// Safe for concurrent use within a single process: an internal mutex serialises
// concurrent AllocatePorts calls, preventing two goroutines from selecting the
// same ports. This is best-effort concurrency protection: simultaneous sbctl
// invocations (multiple processes) are not protected by this mutex.
// The caller must persist the returned PortSet to the store before any other
// sbctl invocation can observe it; the store's UNIQUE constraint on config values
// acts as a final safety net for multi-process races.
func (a *ComposePortAllocator) AllocatePorts(ctx context.Context) (*domain.PortSet, error)
```

### ComposePortAllocator（struct 欄位）

```go
type ComposePortAllocator struct {
    projectRepo store.ProjectRepository
    configRepo  store.ConfigRepository
    bases       portBases
    mu          sync.Mutex
    // probeFunc is the TCP port availability probe.
    // Default: probePort; replaced in tests via newComposePortAllocatorWithProbe.
    probeFunc func(port int) bool
}

// newComposePortAllocatorWithProbe is a package-internal constructor for tests.
func newComposePortAllocatorWithProbe(
    projectRepo store.ProjectRepository,
    configRepo  store.ConfigRepository,
    probe func(int) bool,
    opts ...AllocatorOption,
) *ComposePortAllocator
```

> `probeFunc` 為 **white-box 欄位**，僅限同套件測試（`package compose`）存取，不對外公開。

---

## 執行流程

```
AllocatePorts(ctx)
│
├─ 1. a.mu.Lock()  ← 防止同程序內兩個並發呼叫拿到相同 port
│
├─ 2. ProjectRepository.List(ctx) 取得所有非 destroyed 專案
│     ← ctx cancellation checkpoint
│
├─ 3. 對每個專案呼叫 ConfigRepository.GetConfig(ctx, slug)
│     若 store.ErrConfigNotFound → 跳過（專案建立中，尚無 config）
│     其他 error → Unlock，回傳包裝後的 error
│     ← ctx cancellation checkpoint（每次 GetConfig 後）
│
├─ 4. 對每個有效 config 呼叫 domain.ExtractPortSet(config)
│     失敗（config 資料損壞）→ 記錄 warn 日誌，跳過此專案
│     收集 usedSet map[int]struct{}（所有類型 port 統一扁平化）
│
├─ 5. 針對每個 portType，從 base 開始依序掃描：
│     候選 port c = base, base+1, base+2, ...
│     a. 若 c > 65535 → Unlock，回傳 domain.ErrNoAvailablePort
│     b. 若 c 在 usedSet 中 → 跳過
│     c. TCP probe(c) 為 false → 跳過
│     → 選定 c，加入 usedSet（防止後續 portType 選到相同 port）
│
│     KongHTTP 特殊規則：候選 c 被選定前，
│       額外確認 probe(c+1) 為 true 且 c+1 < 65535
│       （c+1 = KongHTTPS，不在 PortSet 中但 Compose 會使用）
│       若 c+1 不可用 → 跳過 c，繼續嘗試 c+1
│
├─ 6. a.mu.Unlock()
│
└─ 7. 回傳 &domain.PortSet{...}
```

> **扁平化 usedSet 的重要性：** 如果 Project A 的 MetaPort = 54381，而 ImgProxy 的 base = 54381，使用 per-type 追蹤會讓 Project B 誤以為 54381 可用於 ImgProxy。扁平化 usedSet 確保所有 portType 之間不衝突。

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
| `ConfigRepository.GetConfig` 回傳 `store.ErrConfigNotFound` | 跳過此專案（config 尚未儲存），繼續掃描 |
| `ConfigRepository.GetConfig` 回傳其他 error | 解除 mutex，回傳包裝後的 error：`fmt.Errorf("port allocator: get config %q: %w", slug, err)` |
| `domain.ExtractPortSet` 失敗 | 記錄警告日誌後跳過（`log.Warn("port allocator: skipping project %q: %v", slug, err)`） |
| 候選 port > 65535 | 解除 mutex，回傳 `domain.ErrNoAvailablePort` |
| `ctx.Done()` 在 List 或任一 GetConfig 後觸發 | 解除 mutex，回傳 `ctx.Err()` |

---

## 測試策略

### 需要測試的行為

- **基本分配**：第一個專案（store 空）拿到所有 base port
- **衝突跳過（store，同類型）**：已有專案佔用 `KONG_HTTP_PORT=28081`，新專案拿到 28083（跳過 28082 = KongHTTPS）
- **跨類型衝突（flat usedSet）**：Project A 的 MetaPort=54381 使 Project B 的 ImgProxyPort 跳過 54381
- **衝突跳過（TCP probe）**：主機上 port 被佔用（probe 回傳 false），回傳下一個可用 port
- **KongHTTPS 聯合探測**：候選 N 可用，但 N+1 被佔用，應繼續往下找
- **context cancellation**：ctx 已取消時，AllocatePorts 在 List/GetConfig 後回傳 ctx.Err()
- **ErrNoAvailablePort**：所有候選 port 均不可用（probe 全 false）→ 回傳 ErrNoAvailablePort
- **store.ErrConfigNotFound 跳過**：無 config 的專案不影響分配
- **ExtractPortSet 失敗跳過**：config 資料損壞時跳過專案，繼續掃描
- **port > 65535 邊界**：強制回傳 ErrNoAvailablePort 而非 panic 或 overflow
- **靜態介面斷言**：`var _ domain.PortAllocator = (*ComposePortAllocator)(nil)` 存在於非測試檔案

### 測試類型分配

| 測試類型 | 測試目標 | 層次 |
|---------|---------|------|
| 單元測試 | 掃描演算法全部行為（store mock + probe stub） | adapter |
| 整合測試（`//go:build integration`）| 對真實 DB 執行 List + GetConfig | adapter |

### Mock 策略

- `ProjectRepository` 和 `ConfigRepository` 使用介面 mock（手寫 struct，欄位為 func 型別，同 `domain.MockRuntimeAdapter` 風格），存放於 `internal/adapter/compose/mock_test.go`（`package compose`）
- TCP probe 透過 `newComposePortAllocatorWithProbe` 內部建構子注入（white-box，同 `package compose`），生產程式碼不暴露 `probeFunc`

```go
// mock_test.go（package compose）
type mockProjectRepo struct {
    ListFn func(ctx context.Context, filters ...store.ListFilter) ([]*domain.ProjectModel, error)
}
func (m *mockProjectRepo) List(ctx context.Context, f ...store.ListFilter) ([]*domain.ProjectModel, error) {
    if m.ListFn != nil {
        return m.ListFn(ctx, f...)
    }
    return nil, nil
}
// ... 其他方法回傳零值
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

- `NewComposePortAllocator` 驗證 base port 值合理（1024–65533，留出 KongHTTPS 的 +1 空間）；超出範圍時 panic（程式設定錯誤，非 runtime error）

### 安全性

- 回傳的 `PortSet` 不含 secret 值，可安全記錄日誌

### 優雅降級

- Store 不可用時，`AllocatePorts` 回傳 error，讓 caller（use-case 層）決定是否重試
- TCP probe 失敗（timeout）視為 port 不可用，繼續掃描下一個
- **多程序競態（multi-process）**：本設計僅保護 single-process 並發；若兩個 sbctl 程序同時 `create`，store 層的 UNIQUE constraint 為最終防線。Phase 2 不引入 DB advisory lock（本地工具，接受此限制）。

### 設定管理

- 各 port base 值透過 `AllocatorOption` 設定，default 值為標準 Supabase 起始值
- 呼叫端（Phase 3 use-case 層）可透過環境變數覆寫 base 值

### 靜態介面合規斷言

```go
// 確保 ComposePortAllocator 始終滿足 domain.PortAllocator 介面
var _ domain.PortAllocator = (*ComposePortAllocator)(nil)
```

存放於 `internal/adapter/compose/port_allocator.go` 的非測試部分。

---

## 待決問題

- 目前無

---

## 審查

### Reviewer A（架構）

- **狀態：** REVISE

- **意見：**

  整體設計方向正確，架構分層（adapter/compose 套件、介面注入、functional options）與現有程式碼一致，error wrapping 格式符合 Coding Guidelines。但有兩個**必須修正的結構性問題**，以及三個需要釐清的設計細節：

  ---

  #### 🔴 必須修正（Critical）

  **1. Mutex 釋放與 Store 寫入之間的 TOCTOU 競態（Race Condition）**

  執行流程的步驟 6 顯示 `a.mu.Unlock()` 在 `AllocatePorts` **回傳前**即執行。然而 `AllocatePorts` 的 docstring 寫道：
  > "The returned PortSet is a point-in-time snapshot; the caller must commit it to the store before releasing the mutex scope (handled by the use-case layer in Phase 3)."

  這兩者互相矛盾。Mutex 在函式回傳前已釋放，use-case 層拿到 PortSet 後到真正寫入 store 之間，另一個並發的 `AllocatePorts` 呼叫已可進入並掃描，此時 store 中尚無新分配的 ports，兩個呼叫將取得相同的 PortSet。

  **必須選擇其中一種解法並明確設計：**
  - **(A) 接受 best-effort 語意**：移除誤導性的 docstring，改為說明此為 best-effort（single-process 工具中 Mutex 已大幅降低碰撞機率），並補充 Phase 3 use-case 層應盡快寫入 store 以縮短 window。
  - **(B) 強化語意**：將 store 寫入（`SaveConfig`）移入 `AllocatePorts` 的 mutex 範圍內，讓 `ComposePortAllocator` 同時持有 mutex 與 store write，但這會使介面合約變重，需要調整 `AllocatePorts` 簽名（傳入 projectSlug + config 或 save callback）。

  選項 (A) 更符合目前介面定義（`AllocatePorts` 不應知道呼叫者的寫入邏輯），但 docstring 必須準確反映實際語意。

  ---

  **2. 跨 portType 的 port 衝突未防護（Cross-Type Port Collision）**

  步驟 4 收集 `usedPorts map[portType][]int`，步驟 5 對每個 portType 只檢查 `usedPorts[type]`。

  **問題**：MetaPort base = 54380，ImgProxyPort base = 54381。若 Project A 的 MetaPort = 54381（因 54380 被佔，往後掃描取到 54381），Project B 建立時 ImgProxyPort 從 54381 開始掃，`usedPorts[ImgProxyPort]` 中**不包含** 54381（它是 Project A 的 MetaPort），TCP probe 也可能通過（54381 此時可能空閒），B 的 ImgProxyPort 與 A 的 MetaPort 就會相同。

  **修正方向**：將所有現有專案所有 portType 的所有已用 port 值合併為一個**扁平** `usedPorts map[int]bool`（或 `set[int]`），掃描時只需查詢 `usedPorts[candidatePort]`，不分類型。

  ---

  #### 🟡 應釐清（Significant）

  **3. 掃描上限 `base + 1000` 未處理 65535 邊界**

  PoolerPort base = 64300，掃描最高到 65300，在有效範圍內。但設計文件未明確要求 `candidatePort <= 65535` 的 guard，若未來 base 值被設定為 64800，掃描範圍就會溢出合法 port 範圍。`AllocatePorts` 或掃描迴圈中應加入 `if candidatePort > 65535 { break }` 或提前回傳 `ErrNoAvailablePort`。

  **4. Context cancellation 的檢查點未明確**

  錯誤表格中提到 `ctx.Done()` 在掃描過程中觸發時應回傳 `ctx.Err()`，但執行流程與 TCP probe 實作都未指出 ctx 在哪個位置被檢查。建議在掃描迴圈**每次迭代開始**（或每個 portType 掃描開始）加上：
  ```go
  select {
  case <-ctx.Done():
      return nil, ctx.Err()
  default:
  }
  ```
  設計文件應明確描述此 checkpoint 的位置。

  **5. `ErrConfigNotFound` 套件前綴不明確**

  錯誤表格中直接寫 `ErrConfigNotFound`，但依 `store/repository.go`，正確引用為 `store.ErrConfigNotFound`（domain package 中沒有同名 sentinel）。設計文件應明確標注套件前綴以避免實作時混淆。

  ---

  #### 🟢 無阻礙的觀察

  - `probeFunc func(port int) bool` 作為 unexported 欄位注入測試 stub，與 `MockRuntimeAdapter` 的風格一致，可接受。
  - `NewComposePortAllocator` 回傳具體型別 `*ComposePortAllocator` 而非介面，符合 Go 慣例。
  - 整合測試 tag `//go:build integration` 與 CI 策略清晰，符合測試規範。
  - KongHTTPS 聯合探測（N 和 N+1 同時可用才選定）設計正確，與 `computePerProjectVars` 中 `KONG_HTTPS_PORT = KongHTTP + 1` 一致。

**[Round 2] 狀態：APPROVED**
**意見：**

前一輪的五個問題逐一核查如下：

**🔴 #1 Mutex TOCTOU 矛盾** ✅ 已解決
`AllocatePorts` docstring 已改為 best-effort 語意，明確說明「同程序內 mutex 大幅降低碰撞機率，多程序競態由 store UNIQUE constraint 作最終防線」，與執行流程 step 6 在函式回傳前即 Unlock 的行為完全一致。

**🔴 #2 跨 portType port 衝突未防護** ✅ 已解決
Step 4 改收集 `usedSet map[int]struct{}`（扁平化），Step 5 掃描時查詢此單一 set，文件並附上「扁平化 usedSet 的重要性」說明與對應測試案例（`跨類型衝突（flat usedSet）`）。

**🟡 #3 port > 65535 邊界未 guard** ✅ 已解決
Step 5a 明確：`若 c > 65535 → Unlock，回傳 domain.ErrNoAvailablePort`；`NewComposePortAllocator` 驗證 base 上界為 65533（留出 KongHTTPS +1 空間）；測試案例清單亦含邊界測試。

**🟡 #4 Context cancellation checkpoint 位置不明確** ✅ 實質解決
執行流程在 Step 2（`List` 後）及 Step 3（每次 `GetConfig` 後）均標記 ctx cancellation checkpoint，錯誤表格亦對應記錄回傳 `ctx.Err()`。Port 掃描迴圈（Step 5）本身僅做記憶體查詢與 TCP probe，無長時間阻塞 I/O，Phase 2 單程序工具情境下可接受不在迴圈內額外加 checkpoint；此限制無需進一步修正。

**🟡 #5 ErrConfigNotFound 套件前綴不明確** ✅ 已解決
執行流程 Step 3 與錯誤表格均已明確使用 `store.ErrConfigNotFound`，與 `store/repository.go` 定義一致。

**🟢 無阻礙觀察**
- 第 143 行注釋以日文撰寫，與文件語言規範（繁體中文）不符，建議後續整理時改為繁體中文，不影響本次審查。
- `c+1 < 65535`（Step 5 KongHTTP 聯合探測）意指 c ≤ 65533，與 base 上界驗證一致，語意安全；若要更明確表達語意可改寫為 `c <= 65534`，實作時自行選擇即可。

---

### Reviewer B（實作）

- **狀態：** REVISE
- **意見：**

#### 🔴 Critical — 必須修正

**1. Mutex 無法保護 store commit 前的競爭窗口（設計自相矛盾）**

`AllocatePorts` 的 flow diagram 在步驟 6 `a.mu.Unlock()` 之後才在步驟 7 回傳。這表示 mutex 在 caller 取得 PortSet 之前就已釋放（無論是 explicit unlock 還是 `defer unlock`，mutex 都在 `AllocatePorts` 返回前或返回時釋放）。

但函式的文件卻寫道：

> The returned PortSet is a point-in-time snapshot; the caller must commit it to the store **before releasing the mutex scope**.

這是矛盾的：caller 收到 PortSet 時 mutex 已不在 caller 的掌控中。兩個並發的 `AllocatePorts` 呼叫可以產生以下 race：

1. Goroutine A：鎖定 → 掃描 → 選定 port 28081 → **解鎖** → 返回 PortSet
2. Goroutine B：（A 解鎖後立即）鎖定 → 掃描 → **store 中還沒有 A 的 port** → 也選定 28081 → 解鎖 → 返回 PortSet
3. A 存入 store → B 存入 store → **port 衝突**

設計中的 `application-level mutex` 並不能解決這個問題，除非 store commit 也在 mutex 保護範圍內執行。請在設計文件中選擇以下其中一種方案：

- **方案 A（推薦）**：在 `AllocatePorts` 中接受一個 callback `func(ctx, *PortSet) error`，在 mutex 持有期間執行 commit；若 callback 失敗則返回 error 並在 mutex 內部重試。
- **方案 B**：在 `ComposePortAllocator` 內部維護一個 `pendingPorts map[portType]int`（in-memory reserved set），在 mutex 內登記「已被本次呼叫預留」的 ports，並在 use-case 確認 commit 或逾時後才清除。
- **方案 C**：明確記載此為「盡力而為」（best-effort）設計，承認在極低機率的並發下仍有 port 衝突的可能，並由 store 的唯一性約束（UNIQUE index）作為最終保護，衝突時回傳 error 讓 caller 重試。Phase 2 為單程序工具，此方案的實際風險極低，但設計文件必須明確說明此限制，不能暗示 mutex 可以完全防止衝突。

---

#### 🟡 Moderate — 應該修正

**2. `probeFunc` 欄位的測試存取方式未指定**

設計文件說「ComposePortAllocator 暴露可替換的 probe 函數（僅測試時修改）」，但 `probeFunc` 是 unexported 欄位。請明確說明測試策略：

- 若使用 **white-box 測試**（`package compose`），可直接賦值 `allocator.probeFunc = stub`，請在文件中說明測試檔案使用 `package compose`（而非 `package compose_test`）。
- 若希望測試是 black-box，則需新增 `WithProbeFunc(fn func(int) bool) AllocatorOption`，並在 `AllocatorOption` 列表中加入此選項。

兩種方式都可以，但必須在設計文件中明確選擇一種。

**3. 測試策略缺少 `ExtractPortSet` 失敗路徑**

錯誤處理表中列出「`ExtractPortSet` 失敗 → 記錄 warn 後跳過」，但測試策略的測試案例清單中沒有對應的測試案例。應新增：

> - **損壞 config 跳過**：某專案的 config 存在但含有無效 port 值（`ExtractPortSet` 返回 `ErrInvalidPortSet`），應跳過該專案並記錄 warn，不影響分配結果。

**4. Context 檢查的位置未明確**

設計文件提到「`ctx.Done()` 觸發時解除 mutex 回傳 `ctx.Err()`」，但沒有指定在掃描 loop 的哪個位置做此檢查。`probePort` 不接受 context，因此 ctx 取消只能在 loop iteration 之間檢查。請在 flow 或 pseudo-code 中明確標示：

```go
for port := base; port < base+limit; port++ {
    select {
    case <-ctx.Done():
        return ctx.Err()  // ← 檢查點在此
    default:
    }
    // ... probe
}
```

**5. Mock struct 的檔案位置未指定**

設計文件說 `ProjectRepository` 和 `ConfigRepository` 的 mock 使用「手寫 struct，欄位為 func 型別」，但沒有指定這些 mock 的存放位置：

- 若定義為 `internal/store/mock_repository.go`（類似 `domain/mock_adapter.go`），可跨 adapter 共用。
- 若定義為 test file 內的 inline struct（`compose/port_allocator_test.go`），則只供本測試使用。

請明確選擇一種方案並在設計文件中記錄。

---

**[Round 2] 狀態：REVISE**
**意見：**

前一輪的所有問題逐一核查如下：

**🔴 #1 Mutex TOCTOU 矛盾** ✅ 已解決
`AllocatePorts` docstring 改為明確的 best-effort 語意，承認 mutex 只防護同程序內的競態，多程序競態由 store UNIQUE constraint 作最終防線，與 Step 6 在函式回傳前即 Unlock 的行為完全一致，不再自相矛盾。

**🟡 #2 probeFunc 測試存取方式未指定** ✅ 已解決
設計明確採用 white-box 方式：提供 package-internal 建構子 `newComposePortAllocatorWithProbe`，測試檔案使用 `package compose`，生產 API 不暴露 `probeFunc`。

**🟡 #3 測試策略缺少 ExtractPortSet 失敗路徑** ✅ 已解決
測試案例清單新增「**ExtractPortSet 失敗跳過**」條目，與錯誤處理表中「記錄 warn 後跳過」對應一致。

**🟡 #4 Context cancellation checkpoint 位置不明確** ✅ 已解決
執行流程在 Step 2（`List` 後）及 Step 3（每次 `GetConfig` 後）均標記 ctx cancellation checkpoint，覆蓋所有有阻塞 I/O 的位置。Port 掃描迴圈（Step 5）僅做記憶體查詢與本地 TCP probe，Phase 2 情境下可接受不在迴圈內額外 checkpoint。

**🟡 #5 Mock struct 位置未指定** ✅ 已解決
明確指定存放於 `internal/adapter/compose/mock_test.go`（`package compose`），與 white-box 測試策略一致。

**🟢 #6 port > 65535 邊界** ✅ 已解決
**🟢 #7 靜態介面斷言** ✅ 已解決

---

**新發現問題（阻擋本輪審查）：**

**🟡 mock 範例缺少 nil guard（與 MockRuntimeAdapter 風格不一致）**

文件說 mock 使用「同 `domain.MockRuntimeAdapter` 風格」，但範例程式碼為：

```go
func (m *mockProjectRepo) List(ctx context.Context, f ...store.ListFilter) ([]*domain.ProjectModel, error) {
    return m.ListFn(ctx, f...)
}
```

`MockRuntimeAdapter` 的所有方法在呼叫 Fn 前都有 nil guard（`if m.CreateFn != nil { ... }`）。此範例在 `ListFn == nil` 時會直接 panic，與聲稱的風格相悖，且會誤導實作者。請修正範例，加入 nil guard：

```go
func (m *mockProjectRepo) List(ctx context.Context, f ...store.ListFilter) ([]*domain.ProjectModel, error) {
    if m.ListFn != nil {
        return m.ListFn(ctx, f...)
    }
    return nil, nil
}
```

---

**🟢 注記（不阻擋）：**

- 第 143 行注釋以日文撰寫，與文件語言規範（繁體中文）不符，建議整理時改寫，不影響本次審查。
- `c+1 < 65535` 條件排除了 KongHTTP=65534/KongHTTPS=65535 的合法組合；Reviewer A 已標注為 minor，建議實作時改寫為 `c+1 <= 65535` 以更精確表達語意，不阻擋審查。

---

**[Round 3] 狀態：APPROVED**
**意見：** Round 2 唯一阻擋問題已完整修正。`mockProjectRepo.List` 現已加入 `if m.ListFn != nil { return m.ListFn(ctx, f...) }; return nil, nil` nil guard，與 `MockRuntimeAdapter` 風格完全一致，不再有 panic 風險。第 143 行注釋已改為繁體中文（`probeFunc 為 white-box 欄位，僅限同套件測試存取，不對外公開。`），文件語言一致性已達標。先前所有已解決項目（Mutex 語意、probeFunc 測試存取、ExtractPortSet 失敗路徑、ctx cancellation checkpoint、mock 位置、靜態介面斷言、port 範圍邊界）狀態不變，無退化。本文件可進入實作階段。

---

#### 🟢 Minor — 觀察事項（不阻擋，但建議釐清）

**6. `base + 1000` 上界可能超過合法 port 範圍**

掃描上限 `base + 1000` 在 `NewComposePortAllocator` 的驗證只確保 `base` 在 1024–65535，但沒有防止 `base + 1000 > 65535`。此外，KongHTTP 掃描 port N 時還需同時確認 N+1 可用，若 N = 65535 則 N+1 = 65536 是無效 port。建議在掃描 loop 條件中加入 `port+1 <= 65535`（僅 KongHTTP）及 `port <= 65534`（KongHTTP 上界）的 guard。這是邊緣情境，但實作時需注意。

**7. 未說明是否需要 interface compliance 靜態斷言**

建議在設計文件中加入 `var _ domain.PortAllocator = (*ComposePortAllocator)(nil)` 的 compile-time 斷言，此為本專案其他 adapter 實作的常見模式。

---

## 任務

待審查通過後產生。

---

## 程式碼審查

**審查日期：** 2026-03-23
**審查工具：** code-review subagent（commit `f907122`）
**審查結果：** ✅ PASS

**發現問題：**
- 🟢 `Start` 方法在 `up -d` 成功後等待第一次 ticker（5 秒）才做第一次健康檢查，無法立即偵測到已健康的服務 — 屬於效能優化機會，非邏輯錯誤
- 🟢 `net.Listener` 在 `probePort` 內呼叫 `ln.Close()`，無資源洩漏 ✅

**修正記錄：** 無需修正，上述觀察皆非阻礙性問題
