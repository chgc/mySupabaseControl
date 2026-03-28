> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# 設計文件：K8s Port Allocator（NodePort）

## 狀態

done

## Phase

- **Phase：** Phase 6
- **Phase Plan：** `docs/designs/phase-6-plan.md`

---

## 目的

在 Kubernetes runtime 中，Supabase 服務需要透過 NodePort 暴露給 host 使用（特別是在 OrbStack
本地開發環境中）。本功能實作 `domain.PortAllocator` 介面的 K8s 版本，負責在 NodePort 合法範圍
（30000–32767）內分配無衝突的 port set。

與 Compose 版本的主要差異：

1. **Port 範圍不同** — K8s NodePort 限定 30000–32767，Compose 使用任意高位 port
2. **不需要 TCP probe** — K8s 的 port 衝突由 cluster 在 `helm install` 時驗證，allocator 只需避開本系統已管理的 project 之間的衝突
3. **PoolerPort 不使用** — K8s Helm chart 不包含 supavisor，PoolerPort 固定為 0

---

## 範圍

### 包含

- `K8sPortAllocator` struct 實作 `domain.PortAllocator` 介面
- 透過 `store.ProjectRepository` 與 `store.ConfigRepository` 查詢已使用的 port
- 僅計算 `RuntimeType == "kubernetes"` 的 project 之已用 port
- NodePort 範圍內的 port 搜尋演算法
- 完整的單元測試（含 mock repo、併發安全性測試）

### 不包含

- TCP probe（K8s 不需要）
- 修改 `domain.PortAllocator` 介面或 `domain.PortSet` struct
- 修改現有 `ComposePortAllocator`
- `helm install` 或 `kubectl` 操作（屬於功能 6 — K8s Adapter）
- Supavisor / PoolerPort 的 K8s 支援

---

## 資料模型

### Domain 介面（既有）

```go
// domain/port_allocator.go
type PortAllocator interface {
    AllocatePorts(ctx context.Context) (*PortSet, error)
}
```

### PortSet（既有）

```go
// domain/config_types.go
type PortSet struct {
    KongHTTP     int // External API port (starting from 28081).
    PostgresPort int // PostgreSQL port (starting from 54320).
    PoolerPort   int // Supavisor transaction port (starting from 64300).
}
```

> 在 K8s 實作中，`KongHTTP` 與 `PostgresPort` 將使用 NodePort 範圍內的值，`PoolerPort` 固定為 0。

### K8s Port 基準值

| Port 類型 | 基準值 | 用途 | NodePort 合法 |
|---|---|---|---|
| KongHTTP | 30080 | API gateway 入口（`helm install` 時設為 Service NodePort） | ✅ |
| PostgresPort | 30432 | PostgreSQL 直接存取（開發工具連線用） | ✅ |
| PoolerPort | 0（不使用） | K8s chart 無 supavisor | N/A |

---

## 介面合約

### K8sPortAllocator struct

```go
// internal/adapter/k8s/port_allocator.go
type K8sPortAllocator struct {
    projectRepo store.ProjectRepository
    configRepo  store.ConfigRepository
    mu          sync.Mutex
}

// 靜態介面斷言
var _ domain.PortAllocator = (*K8sPortAllocator)(nil)
```

### Constructor

```go
func NewK8sPortAllocator(
    projectRepo store.ProjectRepository,
    configRepo  store.ConfigRepository,
) *K8sPortAllocator
```

### 常數

```go
const (
    nodePortMin      = 30000 // Kubernetes NodePort 最小值
    nodePortMax      = 32767 // Kubernetes NodePort 最大值
    baseKongHTTP     = 30080 // KongHTTP NodePort 基準值
    basePostgresPort = 30432 // PostgresPort NodePort 基準值
)
```

### findPort（內部方法）

```go
func (a *K8sPortAllocator) findPort(base int, usedSet map[int]struct{}) (int, error) {
    for port := base; port <= nodePortMax; port++ {
        if _, used := usedSet[port]; !used {
            return port, nil
        }
    }
    return 0, fmt.Errorf("k8s port allocator: no available NodePort in range %d-%d: %w", base, nodePortMax, domain.ErrNoAvailablePort)
}
```

與 `ComposePortAllocator.findPort` 的主要差異：

| 面向 | ComposePortAllocator | K8sPortAllocator |
|---|---|---|
| Port 上限 | 65535 | 32767（NodePort 限制） |
| TCP probe | 需要（`probeFunc`） | 不需要 |
| KongHTTPS successor | 需要檢查 c+1 | 不需要（K8s 中 HTTPS 由 Ingress 處理） |
| Context 取消檢查 | 逐 port 檢查 `ctx.Err()` | 不需要（搜尋範圍小，< 2688 個 port） |

---

## 執行流程

### AllocatePorts 完整流程

1. **取得 mutex lock**（`defer mu.Unlock()`）
2. **列出所有 project** — 呼叫 `projectRepo.List(ctx)`
   - 若失敗，回傳包裝後的 error
3. **篩選 K8s project 並收集已用 port**
   - 遍歷所有 project
   - 僅處理 `RuntimeType == domain.RuntimeKubernetes` 的 project
   - 對每個 K8s project，呼叫 `configRepo.GetConfig(ctx, slug)`
   - 使用 `domain.ExtractPortSet(cfg)` 取得 port 值
   - 將 `KongHTTP` 與 `PostgresPort` 加入 `usedSet map[int]struct{}`
   - 忽略 `PoolerPort`（K8s 中不使用）
4. **分配 KongHTTP** — 從 30080 開始，呼叫 `findPort(baseKongHTTP, usedSet)`
   - 將結果加入 `usedSet`，避免後續 port 衝突
5. **分配 PostgresPort** — 從 30432 開始，呼叫 `findPort(basePostgresPort, usedSet)`
6. **設定 PoolerPort = 0**
7. **回傳 `&domain.PortSet{...}`**

### 與 ComposePortAllocator 的流程差異

```
ComposePortAllocator:
  List ALL projects → 全部載入 config → 收集所有 port
  → findPort(28081, usedSet, probe=true, needSuccessor=true)
  → findPort(54320, usedSet, probe=true, needSuccessor=false)
  → findPort(64300, usedSet, probe=true, needSuccessor=false)

K8sPortAllocator:
  List ALL projects → 僅載入 K8s project config → 收集 KongHTTP + PostgresPort
  → findPort(30080, usedSet)    ← 無 probe，無 successor
  → findPort(30432, usedSet)    ← 無 probe
  → PoolerPort = 0              ← 固定值
```

---

## 錯誤處理

| 情境 | 處理方式 | 回應格式 |
|---|---|---|
| `projectRepo.List` 失敗 | 回傳包裝後的 error | `"k8s port allocator: list projects: %w"` |
| `configRepo.GetConfig` 失敗 | 跳過該 project（記錄 warning log），繼續處理其他 project | 不回傳 error |
| `configRepo.GetConfig` 回傳 `store.ErrConfigNotFound` | 靜默跳過（不記 log） | 不回傳 error |
| `domain.ExtractPortSet` 失敗（port 字串無法解析） | 跳過該 project（記錄 warning log），不加入 `usedSet` | 不回傳 error |
| KongHTTP 無可用 NodePort | 回傳包裝 `domain.ErrNoAvailablePort` 的 error | `"k8s port allocator: no available NodePort in range 30080-32767: no available port"` |
| PostgresPort 無可用 NodePort | 回傳包裝 `domain.ErrNoAvailablePort` 的 error | `"k8s port allocator: no available NodePort in range 30432-32767: no available port"` |
| 非 K8s runtime 的 project | 跳過（不載入 config） | N/A |

### 優雅降級策略

- 單一 project 的 config 載入失敗不影響其他 project 的 port 收集
- `ExtractPortSet` 解析失敗視為該 project「無已用 port」，不阻塞新 project 的分配
- GetConfig 非 ErrConfigNotFound 錯誤的處理與 Compose 版本不同：Compose 版本回傳 error，K8s 版本選擇跳過並記錄 warning，因為 K8s 環境中 port 衝突最終由 cluster 在 helm install 時驗證。

---

## 測試策略

### 需要測試的行為

- 無既有 project 時，回傳基準 port（30080, 30432, 0）
- 有既有 K8s project 時，跳過已用 port
- 僅計算 K8s project 的 port，忽略 Compose project
- NodePort 範圍耗盡時回傳 error
- `projectRepo.List` 失敗時回傳 error
- `configRepo.GetConfig` 失敗時跳過該 project（不 error）
- port 字串無法解析時跳過（不 error）
- 併發安全性 — 多個 goroutine 並行呼叫無 data race（透過 `-race` flag 驗證）

### 測試類型分配

| 測試類型 | 測試目標 | 層次 |
|---|---|---|
| 單元測試 | `K8sPortAllocator.AllocatePorts` 所有路徑 | adapter |
| 單元測試 | `findPort` 邊界條件（範圍耗盡） | adapter（內部方法透過 `AllocatePorts` 間接測試） |
| 單元測試 | 併發安全性（goroutine race detection） | adapter |

### 測試案例清單

| 測試函式 | 場景 | 預期結果 |
|---|---|---|
| `TestAllocatePorts_NoExistingProjects` | 無既有 project | `PortSet{30080, 30432, 0}` |
| `TestAllocatePorts_WithExistingProjects` | 已有 project 使用 30080 和 30432 | 回傳 30081 和 30433 |
| `TestAllocatePorts_OnlyCountsK8sProjects` | 混合 Compose 與 K8s project | 僅跳過 K8s project 的 port |
| `TestAllocatePorts_ExhaustedRange` | 30080–32767 全部已用 | 回傳 error，`errors.Is(err, domain.ErrNoAvailablePort)` 為 true |
| `TestAllocatePorts_RepoError` | `projectRepo.List` 回傳 error | 回傳包裝後的 error |
| `TestAllocatePorts_InvalidPortString` | config 中 port 值為非數字字串 | 跳過，回傳基準 port |
| `TestAllocatePorts_ConcurrentSafety` | 多個 goroutine 並行呼叫 | 無 data race（透過 `-race` flag 驗證） |
| `TestAllocatePorts_ConfigNotFound` | `configRepo.GetConfig` 回傳 `ErrConfigNotFound` | 靜默跳過，回傳基準 port |

### Mock 策略

使用 function-field pattern 的 test-local mock struct，與 `ComposePortAllocator` 測試風格一致（參見 `compose/mock_test.go`）。
所有 interface method 皆需 stub，僅測試用到的 method 使用 function field 注入行為：

```go
type mockProjectRepo struct {
    ListFn func(ctx context.Context, filters ...store.ListFilter) ([]*domain.ProjectModel, error)
}

func (m *mockProjectRepo) List(ctx context.Context, f ...store.ListFilter) ([]*domain.ProjectModel, error) {
    if m.ListFn != nil {
        return m.ListFn(ctx, f...)
    }
    return nil, nil
}

func (m *mockProjectRepo) Create(_ context.Context, _ *domain.ProjectModel) error { return nil }
func (m *mockProjectRepo) GetBySlug(_ context.Context, _ string) (*domain.ProjectModel, error) {
    return nil, nil
}
func (m *mockProjectRepo) UpdateStatus(_ context.Context, _ string, _, _ domain.ProjectStatus, _ string) error {
    return nil
}
func (m *mockProjectRepo) Delete(_ context.Context, _ string) error { return nil }
func (m *mockProjectRepo) Exists(_ context.Context, _ string) (bool, error) { return false, nil }

type mockConfigRepo struct {
    GetConfigFn func(ctx context.Context, slug string) (*domain.ProjectConfig, error)
}

func (m *mockConfigRepo) GetConfig(ctx context.Context, slug string) (*domain.ProjectConfig, error) {
    if m.GetConfigFn != nil {
        return m.GetConfigFn(ctx, slug)
    }
    return nil, nil
}

func (m *mockConfigRepo) SaveConfig(_ context.Context, _ string, _ *domain.ProjectConfig) error {
    return nil
}
func (m *mockConfigRepo) SaveOverrides(_ context.Context, _ string, _ map[string]string) error {
    return nil
}
func (m *mockConfigRepo) GetOverrides(_ context.Context, _ string) (map[string]string, error) {
    return nil, nil
}
func (m *mockConfigRepo) DeleteConfig(_ context.Context, _ string) error { return nil }
```

### CI 執行方式

- 所有測試為純單元測試，不需要 K8s cluster 或 Docker
- 在一般 CI（`go test ./...`）中執行
- 使用 `-race` flag 驗證併發安全性

---

## Production Ready 考量

### 錯誤處理

- `projectRepo.List` 錯誤向上傳播，包含 `"k8s port allocator:"` 前綴
- 單一 project 的 config 載入失敗不影響整體分配流程
- port 範圍耗盡時回傳明確的 error message，包含搜尋起始值與上限
- 使用者可見的 error 不包含敏感資訊

### 日誌與可觀測性

- config 載入失敗時記錄 warning log（含 project slug）
- `ExtractPortSet` 解析失敗時記錄 warning log（含 project slug 與錯誤詳情）
- 成功分配時可選擇記錄 info log（含分配結果）
- 目前使用 `_ = err` 佔位，待結構化 logger 引入後替換（與 `ComposePortAllocator` 做法一致）

### 輸入驗證

- port 字串解析錯誤透過 `domain.ExtractPortSet` 處理，回傳 `ErrInvalidPortSet`
- 無外部使用者輸入需要驗證（port 基準值為內部常數）

### 安全性

- 不處理任何 secret
- 不需要 K8s API 存取權限（僅讀取本地 store）
- 無存取控制考量

### 優雅降級

- 外部依賴僅有 `ProjectRepository` 與 `ConfigRepository`（均為本地 store）
- 單一 project 的 config 不可用時，其他 project 正常處理
- 無 network 呼叫，無需 timeout 策略

### 設定管理

- Port 基準值為 Go 常數（`baseKongHTTP = 30080`、`basePostgresPort = 30432`）
- NodePort 範圍為 Kubernetes 標準（30000–32767），無需設定
- 不需要額外的環境變數

---

## 待決問題

- Port 基準值是否應可透過環境變數設定？（目前決定：否，常數已足夠應付單主機開發場景。若未來需支援多 cluster 或自訂 NodePort 範圍，可再考慮）

---

## 檔案清單

| 檔案 | 狀態 | 說明 |
|---|---|---|
| `internal/adapter/k8s/port_allocator.go` | 新增 | `K8sPortAllocator` 實作 |
| `internal/adapter/k8s/port_allocator_test.go` | 新增 | 單元測試（含 mock、併發測試） |

### 依賴

| 依賴 | 來源 | 用途 |
|---|---|---|
| `store.ProjectRepository` | Phase 2 | 列出所有 project |
| `store.ConfigRepository` | Phase 2 | 取得 project config 中的 port 值 |
| `domain.PortAllocator` | Phase 1 | 實作的介面 |
| `domain.PortSet` | Phase 1 | 回傳的 port 結構 |
| `domain.ExtractPortSet` | Phase 4 | 從 `ProjectConfig` 萃取 port 值 |
| `domain.RuntimeKubernetes` | Phase 5 | 篩選 K8s project 用的常數 |

---

## 審查

### Reviewer A（架構）

- **狀態：** APPROVED（Round 2）
- **意見：**
  - Round 1：GetConfig 錯誤處理一致性聲稱不正確；ConcurrentSafety 測試預期不合理 → 已修正
  - Round 2：APPROVED

### Reviewer B（實作）

- **狀態：** APPROVED（Round 2）
- **意見：**
  - Round 1：Mock 簽名不符、findPort 缺 sentinel error、硬寫 32767、一致性聲稱不正確、ConcurrentSafety 預期不合理 → 全部已修正
  - Round 2：APPROVED

---

## 任務

<!-- 兩位審查者都回覆 APPROVED 後，根據設計展開為具體可執行的任務。 -->

---

## 程式碼審查

<!-- 所有任務完成後，由 code-review subagent 審查 feature branch 對 main 的完整 diff -->

- **審查結果：** FIX_REQUIRED → PASS
- **發現問題：**
  1. gofmt 格式化問題
  2. ComposePortAllocator 缺少 RuntimeType 過濾（跨 runtime 問題）
- **修正記錄：**
  1. 執行 `gofmt -w` 修正格式
  2. 在 ComposePortAllocator 中加入 RuntimeType 過濾，更新相關測試
