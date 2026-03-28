> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# 設計文件：Multi-Runtime 基礎架構

## 狀態

done

## Phase

- **Phase：** Phase 6
- **Phase Plan：** `docs/designs/phase-6-plan.md`

---

## 目的

目前 Control Plane 的架構硬編碼假設所有專案都跑在 Docker Compose 上：

- `BuildDeps()` 直接建構 `compose.NewComposeAdapter`，無 factory/registry 可選取其他 adapter
- `usecase.Config.Adapter` 是全域唯一的 `domain.RuntimeAdapter` 實例
- `ProjectModel` 不記錄專案使用哪種 runtime
- DB schema 無 `runtime_type` 欄位
- `computePerProjectVars()` 含 Docker-only 值（`DOCKER_SOCKET_LOCATION`）

本功能將 Control Plane 重構為**多 runtime 架構**，使 Compose 與 K8s 專案能共存於同一個系統中。
重構完成後，所有既有 Compose 功能不受影響，同時具備接入 K8s adapter 的能力。

**K8s 隔離策略：每個 K8s 專案擁有獨立的 K8s namespace**，namespace 名稱為 `supabase-{slug}`。
此設計確保專案間完全隔離（網路、RBAC、資源配額、PVC），且與 Compose 的「每專案獨立目錄」語義一致。

---

## 範圍

### 包含

- DB migration：`projects` 表新增 `runtime_type` 欄位（含 CHECK constraint）
- Domain model：`ProjectModel` 加入 `RuntimeType` 欄位
- Store 層：PostgreSQL repository 的 INSERT/SELECT 加入 `runtime_type`
- 新增 `AdapterRegistry` 介面及預設實作 `defaultAdapterRegistry`
- `ProjectService` 從持有單一 adapter 改為持有 `AdapterRegistry`
- CLI `project create` 加入 `--runtime` 旗標（預設 `docker-compose`）
- MCP `create_project` tool 加入 `runtime` 參數（可選，預設 `docker-compose`）
- `computePerProjectVars()` 參數化，根據 runtime type 決定是否加入 Docker-only 值
- `buildURLs()` 參數化，K8s 環境使用 `localhost:{nodePort}` 或 OrbStack DNS
- `ProjectService.Create()` 簽名調整（接受 `RuntimeType` 參數）

### 不包含

- K8s adapter 實作（功能 6：`k8s-adapter.md`）
- Helm values renderer（功能 3：`k8s-values-renderer.md`）
- K8s port allocator（功能 5：`k8s-port-allocator.md`）
- K8s status parser（功能 4：`k8s-status-parser.md`）
- Helm chart 研究（功能 2：`helm-values-mapping.md`）
- OrbStack 環境管理（叢集啟停等）

---

## 資料模型

### DB Migration（`migrations/002_add_runtime_type.sql`）

```sql
-- Migration: 002_add_runtime_type.sql
-- Adds runtime_type column to support multiple runtime backends.
-- Idempotent: safe to run multiple times.

ALTER TABLE projects
    ADD COLUMN IF NOT EXISTS runtime_type TEXT NOT NULL DEFAULT 'docker-compose';

DO $$ BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'valid_runtime_type'
  ) THEN
    ALTER TABLE projects ADD CONSTRAINT valid_runtime_type CHECK (
        runtime_type IN ('docker-compose', 'kubernetes')
    );
  END IF;
END $$;
```

**設計考量：**
- `DEFAULT 'docker-compose'`：既有專案自動歸類為 Compose
- CHECK constraint 限制合法值，與 `domain.RuntimeType` 常數對齊
- 不加 INDEX：`runtime_type` 基數極低（2 值），index 無收益
- 使用 `IF NOT EXISTS` / `DO $$ ... END $$` 確保冪等性（可重複執行）

### ProjectModel 變更

```go
type ProjectModel struct {
    Slug           string
    DisplayName    string
    RuntimeType    RuntimeType    // 新增：專案使用的 runtime backend
    Status         ProjectStatus
    PreviousStatus ProjectStatus
    LastError      string
    CreatedAt      time.Time
    UpdatedAt      time.Time
    Health         *ProjectHealth
}
```

### NewProject 簽名變更

```go
func NewProject(slug, displayName string, runtimeType RuntimeType) (*ProjectModel, error)
```

**選擇直接參數而非 functional option 的理由：**
- `RuntimeType` 是**必要欄位**（每個專案必須有 runtime type），不適合 option pattern
- 只新增一個參數，不造成簽名膨脹
- 所有 caller 必須明確指定 runtime type — 這是刻意的設計（避免遺漏）

### K8s Namespace 命名規則

K8s 專案的 namespace 命名為 `supabase-{slug}`：

- `slug` 已通過 `ValidateSlug()` 驗證（3–40 字元、小寫英數 + hyphen、不以 hyphen 開頭/結尾），
  完全符合 [K8s namespace 命名規則](https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#dns-label-names)（≤63 字元、`[a-z0-9-]`）
- 加上 `supabase-` 前綴後最長 `9 + 40 = 49` 字元，仍在 63 字元限制內
- 前綴確保不與系統 namespace（`default`、`kube-system` 等）衝突
- Namespace 的建立與刪除由 K8s adapter 負責（本設計文件不含 adapter 實作）

**Domain helper（建議但非必須在本功能實作）：**

```go
// K8sNamespace returns the Kubernetes namespace for this project.
// Only meaningful when RuntimeType == RuntimeKubernetes.
func (p *ProjectModel) K8sNamespace() string {
    return "supabase-" + p.Slug
}
```

---

## 介面合約

### Store 層 SQL 變更

#### `ProjectRepository.Create()`

現有 INSERT：
```sql
INSERT INTO projects (slug, display_name, status, previous_status, last_error, created_at, updated_at)
VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6, $7)
```

變更後：
```sql
INSERT INTO projects (slug, display_name, runtime_type, status, previous_status, last_error, created_at, updated_at)
VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, $8)
```

Go 端：`project.RuntimeType` 以 `string(project.RuntimeType)` 傳入參數 `$3`。

#### `ProjectRepository.GetBySlug()`

現有 SELECT：
```sql
SELECT slug, display_name, status,
       COALESCE(previous_status, '') AS previous_status,
       COALESCE(last_error, '')      AS last_error,
       created_at, updated_at
FROM   projects
WHERE  slug = $1
```

變更後：
```sql
SELECT slug, display_name, runtime_type, status,
       COALESCE(previous_status, '') AS previous_status,
       COALESCE(last_error, '')      AS last_error,
       created_at, updated_at
FROM   projects
WHERE  slug = $1
```

Go 端 Scan 變更：
```go
var status, previousStatus, runtimeType string
err := row.Scan(
    &p.Slug,
    &p.DisplayName,
    &runtimeType,       // 新增
    &status,
    &previousStatus,
    &p.LastError,
    &p.CreatedAt,
    &p.UpdatedAt,
)
// ...
p.RuntimeType = domain.RuntimeType(runtimeType)
```

#### `ProjectRepository.List()`

兩個 query 分支（有/無 status filter）都需加入 `runtime_type`，Scan 邏輯與 `GetBySlug()` 相同。

#### soft-delete 重建（`Create()` 中的 slug 重用邏輯）

現有的 transaction 中 DELETE + re-INSERT 流程不受影響 — 新的 INSERT 自然包含 `runtime_type`。
注意：重用 slug 時，新專案的 `runtime_type` 可能與被刪除的不同（例如原本是 Compose，新建為 K8s），
這是合理的行為。

### AdapterRegistry 介面

新增 `internal/domain/adapter_registry.go`：

```go
// AdapterRegistry provides runtime-specific component lookup.
// Implementations must be safe for concurrent use.
type AdapterRegistry interface {
    // GetAdapter returns the RuntimeAdapter for the given runtime type.
    // Returns ErrUnsupportedRuntime if the runtime type is not registered.
    GetAdapter(rt RuntimeType) (RuntimeAdapter, error)

    // GetPortAllocator returns the PortAllocator for the given runtime type.
    // Returns ErrUnsupportedRuntime if the runtime type is not registered.
    GetPortAllocator(rt RuntimeType) (PortAllocator, error)
}

// ErrUnsupportedRuntime is returned when a requested runtime type has no registered adapter.
var ErrUnsupportedRuntime = errors.New("unsupported runtime type")
```

**設計決策：**
- 不包含 `GetConfigRenderer`：ConfigRenderer 已內嵌於各 adapter 的建構過程中
  （`ComposeAdapter` 接收 renderer 作為建構參數），不需在 registry 層額外暴露
- 不包含 `Register` 方法：registry 實例在 `BuildDeps()` 中完整建構，
  不需 runtime 動態註冊
- 回傳 `error` 而非 panic：runtime type 來自使用者輸入或 DB，可能無效

### defaultAdapterRegistry 實作

新增 `internal/domain/adapter_registry_impl.go`：

```go
// defaultAdapterRegistry is the production implementation of AdapterRegistry.
// It holds a fixed set of adapters and allocators, configured at startup.
type defaultAdapterRegistry struct {
    adapters   map[RuntimeType]RuntimeAdapter
    allocators map[RuntimeType]PortAllocator
}

// AdapterRegistryConfig holds the components to register for each runtime type.
type AdapterRegistryConfig struct {
    RuntimeType   RuntimeType
    Adapter       RuntimeAdapter
    PortAllocator PortAllocator
}

// NewAdapterRegistry creates a new AdapterRegistry from the given configurations.
// At least one configuration must be provided.
func NewAdapterRegistry(configs ...AdapterRegistryConfig) (AdapterRegistry, error) {
    if len(configs) == 0 {
        return nil, fmt.Errorf("at least one adapter configuration is required")
    }
    reg := &defaultAdapterRegistry{
        adapters:   make(map[RuntimeType]RuntimeAdapter, len(configs)),
        allocators: make(map[RuntimeType]PortAllocator, len(configs)),
    }
    for _, c := range configs {
        reg.adapters[c.RuntimeType] = c.Adapter
        reg.allocators[c.RuntimeType] = c.PortAllocator
    }
    return reg, nil
}

func (r *defaultAdapterRegistry) GetAdapter(rt RuntimeType) (RuntimeAdapter, error) {
    a, ok := r.adapters[rt]
    if !ok {
        return nil, fmt.Errorf("%w: %s", ErrUnsupportedRuntime, rt)
    }
    return a, nil
}

func (r *defaultAdapterRegistry) GetPortAllocator(rt RuntimeType) (PortAllocator, error) {
    a, ok := r.allocators[rt]
    if !ok {
        return nil, fmt.Errorf("%w: %s", ErrUnsupportedRuntime, rt)
    }
    return a, nil
}
```

### usecase.Config 變更

```go
type Config struct {
    ProjectRepo     store.ProjectRepository
    ConfigRepo      store.ConfigRepository
    Registry        domain.AdapterRegistry   // 取代原本的 Adapter
    SecretGenerator domain.SecretGenerator
    Logger          *slog.Logger
}
```

**移除 `PortAllocator` 欄位** — port allocator 現在透過 `Registry.GetPortAllocator()` 取得，
因為不同 runtime 使用不同的 port allocation 策略。

### ProjectService.Create() 簽名變更

```go
// Create 新增 runtimeType 參數
Create(ctx context.Context, slug, displayName string, runtimeType domain.RuntimeType) (*ProjectView, error)
```

### ProjectView 變更

僅顯示新增與變更的欄位，其餘既有欄位（`PreviousStatus`、`LastError`、`CreatedAt`、`UpdatedAt` 等）維持不變：

```go
type ProjectView struct {
    Slug           string             `json:"slug"`
    DisplayName    string             `json:"display_name"`
    RuntimeType    string             `json:"runtime_type"`    // 新增：以 string 型別維持 view 層與 domain 解耦
    Status         string             `json:"status"`
    PreviousStatus string             `json:"previous_status"` // 既有，不變
    LastError      string             `json:"last_error"`      // 既有，不變
    CreatedAt      time.Time          `json:"created_at"`      // 既有，不變
    UpdatedAt      time.Time          `json:"updated_at"`      // 既有，不變
    Health         *HealthView        `json:"health,omitempty"` // 既有型別 *HealthView，不變
    Config         map[string]string  `json:"config,omitempty"` // 既有，不變
    URLs           *ProjectURLs       `json:"urls,omitempty"`   // 既有，不變
}
```

**注意：** `RuntimeType` 欄位使用 `string`（而非 `domain.RuntimeType`），
與既有的 `Status` 欄位（也是 `string` 而非 `domain.ProjectStatus`）風格一致，
維持 view 層與 domain 層的解耦。在 `toProjectView()` 中轉換：
`RuntimeType: string(project.RuntimeType)`。

### CLI 變更

`project create` 新增 `--runtime` 旗標：

```go
func buildCreateCmd(deps **Deps, output *string) *cobra.Command {
    var displayName string
    var runtime string
    cmd := &cobra.Command{
        Use:   "create <slug>",
        Short: "Create a new Supabase project",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            rt := domain.RuntimeType(runtime)
            view, err := (*deps).ProjectService.Create(
                cmd.Context(), args[0], displayName, rt,
            )
            if err != nil {
                return projectErr(cmd, err)
            }
            return writeProjectView(cmd.OutOrStdout(), *output, view)
        },
    }
    cmd.Flags().StringVarP(&displayName, "display-name", "n", "",
        "Human-readable project name (required)")
    _ = cmd.MarkFlagRequired("display-name")
    cmd.Flags().StringVar(&runtime, "runtime", "docker-compose",
        "Runtime backend (docker-compose or kubernetes)")
    return cmd
}
```

### MCP 變更

`create_project` tool 新增 `runtime` 參數：

```go
s.AddTool(
    mcp.NewTool("create_project",
        mcp.WithDescription("Create a new Supabase project."),
        mcp.WithString("slug", mcp.Required(), ...),
        mcp.WithString("display_name", mcp.Required(), ...),
        mcp.WithString("runtime",
            mcp.Description("Runtime backend: docker-compose (default) or kubernetes"),
        ),
    ),
    makeMCPCreateProject(deps),
)
```

handler 中：

```go
runtime := "docker-compose"
if r, err := req.RequireString("runtime"); err == nil && r != "" {
    runtime = r
}
rt := domain.RuntimeType(runtime)
view, err := deps.ProjectService.Create(ctx, slug, displayName, rt)
```

---

## 執行流程

### Create 流程（重構後）

1. CLI/MCP 解析 `--runtime` 旗標，轉為 `domain.RuntimeType`
2. `ProjectService.Create(ctx, slug, displayName, runtimeType)` 被呼叫
3. 驗證 `runtimeType` 合法性（透過 `registry.GetAdapter(runtimeType)` — 若回傳 `ErrUnsupportedRuntime` 則失敗）
4. `domain.NewProject(slug, displayName, runtimeType)` 建立 ProjectModel（含 RuntimeType）
5. `projectRepo.Create(ctx, project)` 寫入 DB（含 `runtime_type` 欄位）
6. `registry.GetPortAllocator(runtimeType)` 取得對應的 port allocator
7. `portAllocator.AllocatePorts(ctx)` 分配 port set
8. `domain.ResolveConfig(project, portSet, secrets, overrides)` 產生 config
9. `configRepo.SaveConfig(ctx, slug, config)` 寫入 config
10. `adapter.Create(ctx, project, config)` 建立 runtime 資源
    - Compose：建立 `projects/{slug}/` 目錄，寫 `.env` 檔
    - K8s：建立 `supabase-{slug}` namespace，寫 `values.yaml` 至本地

### Get / Start / Stop / Delete 流程（重構後）

1. `projectRepo.GetBySlug(ctx, slug)` 取得 ProjectModel（含 RuntimeType）
2. `registry.GetAdapter(project.RuntimeType)` 取得對應 adapter
3. 呼叫 `adapter.Start() / Stop() / Destroy() / Status()`

### Reset 流程（重構後）

1. `projectRepo.GetBySlug(ctx, slug)` 取得原始專案（含 RuntimeType）
2. **保存原始 runtimeType** — Reset 重建時必須使用相同 runtime
3. Stop → Destroy → Create(slug, displayName, **原始 runtimeType**) → Start

### computePerProjectVars 參數化

```go
func computePerProjectVars(project *ProjectModel, ports *PortSet) map[string]string {
    kongHTTP := strconv.Itoa(ports.KongHTTP)
    vars := map[string]string{
        "KONG_HTTP_PORT":                kongHTTP,
        "KONG_HTTPS_PORT":               strconv.Itoa(ports.KongHTTP + 1),
        "POSTGRES_PORT":                 strconv.Itoa(ports.PostgresPort),
        "POOLER_PROXY_PORT_TRANSACTION": strconv.Itoa(ports.PoolerPort),
        "API_EXTERNAL_URL":              "http://localhost:" + kongHTTP,
        "SUPABASE_PUBLIC_URL":           "http://localhost:" + kongHTTP,
        "SITE_URL":                      "http://localhost:3000",
        "PROJECT_DATA_DIR":              "./volumes",
        "STUDIO_DEFAULT_ORGANIZATION":   "Default Organization",
        "STUDIO_DEFAULT_PROJECT":        project.DisplayName,
        "STORAGE_TENANT_ID":             "stub",
        "POOLER_TENANT_ID":              project.Slug,
    }

    switch project.RuntimeType {
    case RuntimeDockerCompose:
        vars["DOCKER_SOCKET_LOCATION"] = "/var/run/docker.sock"
    case RuntimeKubernetes:
        // K8s 不需要 DOCKER_SOCKET_LOCATION
        // API_EXTERNAL_URL 使用相同的 localhost:{port} — NodePort 在 OrbStack 上
        // 可透過 localhost 存取
    }

    return vars
}
```

**決策：** `API_EXTERNAL_URL` 在 K8s 環境仍使用 `http://localhost:{port}`，
因為 OrbStack K8s 的 NodePort 可透過 `localhost:PORT` 存取。
若未來需支援非 OrbStack 環境，可在此處加入條件分支。

### buildURLs 參數化

目前 `buildURLs()` 已從 config 取得 port，不直接依賴 runtime type。
K8s 環境下 `KONG_HTTP_PORT` 會是 NodePort（30000+），`buildURLs()` 仍可正確產生 `localhost:{port}` URL。
因此 **buildURLs() 無需修改**。

### 內部 helper 變更

#### projectService struct 變更

```go
type projectService struct {
    projectRepo     store.ProjectRepository
    configRepo      store.ConfigRepository
    registry        domain.AdapterRegistry    // 取代 adapter + portAllocator
    secretGenerator domain.SecretGenerator
    log             *slog.Logger
}
```

#### adapterFor() helper（新增）

所有 11 個 `s.adapter.X()` 呼叫改為透過此 helper 取得 adapter，統一錯誤處理：

```go
func (s *projectService) adapterFor(project *domain.ProjectModel) (domain.RuntimeAdapter, error) {
    return s.registry.GetAdapter(project.RuntimeType)
}
```

使用方式（11 處）：
```go
// 舊：
err := s.adapter.Create(ctx, project, config)

// 新：
adapter, err := s.adapterFor(project)
if err != nil {
    return nil, fmt.Errorf("get adapter: %w", err)
}
err = adapter.Create(ctx, project, config)
```

#### provisionConfig() 變更

現有 `provisionConfig()` 使用 `s.portAllocator.AllocatePorts(ctx)`。
重構後 `portAllocator` 從 struct 移除，需改為透過 registry 取得：

```go
func (s *projectService) provisionConfig(ctx context.Context, project *domain.ProjectModel, ...) (*domain.ProjectConfig, error) {
    // 舊：portSet, err := s.portAllocator.AllocatePorts(ctx)
    // 新：
    pa, err := s.registry.GetPortAllocator(project.RuntimeType)
    if err != nil {
        return nil, fmt.Errorf("get port allocator: %w", err)
    }
    portSet, err := pa.AllocatePorts(ctx)
    if err != nil {
        return nil, fmt.Errorf("allocate ports: %w", err)
    }
    // ... 其餘不變
}
```

`provisionConfig()` 的呼叫者：
- `Create()`（第 112 行）
- `Reset()`（第 305 行）
兩處呼叫時 `project` 已有正確的 `RuntimeType`，無需額外變更。

#### NewProjectService() 驗證變更

```go
// 移除：
if cfg.Adapter == nil {
    return nil, fmt.Errorf("usecase: Config.Adapter is required")
}
if cfg.PortAllocator == nil {
    return nil, fmt.Errorf("usecase: Config.PortAllocator is required")
}

// 新增：
if cfg.Registry == nil {
    return nil, fmt.Errorf("usecase: Config.Registry is required")
}
```

---

## 錯誤處理

| 情境 | 處理方式 | 回應格式 |
|------|---------|---------|
| `--runtime` 值無效（非 `docker-compose` 或 `kubernetes`） | `registry.GetAdapter()` 回傳 `ErrUnsupportedRuntime` | `ErrCodeInvalidInput`: "unsupported runtime type: {value}" |
| K8s runtime 請求但無 K8s adapter 已註冊 | `registry.GetAdapter()` 回傳 `ErrUnsupportedRuntime` | `ErrCodeInvalidInput`: "unsupported runtime type: kubernetes" |
| DB 中 `runtime_type` 值不在 registry 中（資料不一致） | `registry.GetAdapter()` 回傳 `ErrUnsupportedRuntime` | `ErrCodeInternal`: 記錄 error log，回傳 internal error |
| `NewProject()` 的 `runtimeType` 驗證失敗 | 回傳 `ErrInvalidRuntimeType` | `ErrCodeInvalidInput`: "invalid runtime type: {value}" |
| Migration 失敗（現有資料不符 constraint） | 不可能：DEFAULT 保證既有 row 合法 | — |

### 新增 Domain Error

```go
// ErrInvalidRuntimeType is returned when a runtime type value is not recognized.
var ErrInvalidRuntimeType = errors.New("invalid runtime type")
```

### RuntimeType 驗證

```go
// ValidateRuntimeType checks that the given RuntimeType is a known value.
func ValidateRuntimeType(rt RuntimeType) error {
    switch rt {
    case RuntimeDockerCompose, RuntimeKubernetes:
        return nil
    default:
        return fmt.Errorf("%w: %s", ErrInvalidRuntimeType, rt)
    }
}
```

在 `NewProject()` 中呼叫：

```go
func NewProject(slug, displayName string, runtimeType RuntimeType) (*ProjectModel, error) {
    if err := ValidateSlug(slug); err != nil {
        return nil, err
    }
    if err := ValidateRuntimeType(runtimeType); err != nil {
        return nil, err
    }
    // ... 其餘不變
}
```

---

## 測試策略

### 需要測試的行為

**Domain 層：**
- `NewProject()` 接受合法 `RuntimeType`（docker-compose、kubernetes）
- `NewProject()` 拒絕無效 `RuntimeType`（空字串、未知值）
- `ValidateRuntimeType()` 正確驗證合法/非法值
- `computePerProjectVars()` 在 Compose 模式下包含 `DOCKER_SOCKET_LOCATION`
- `computePerProjectVars()` 在 K8s 模式下不包含 `DOCKER_SOCKET_LOCATION`
- `K8sNamespace()` 回傳 `supabase-{slug}`

**AdapterRegistry：**
- `NewAdapterRegistry()` 建構成功（至少一個 config）
- `NewAdapterRegistry()` 無 config 回傳 error
- `GetAdapter()` 已註冊的 runtime type 回傳 adapter
- `GetAdapter()` 未註冊的 runtime type 回傳 `ErrUnsupportedRuntime`
- `GetPortAllocator()` 同上

**Store 層：**
- `Create()` 正確寫入 `runtime_type`
- `GetBySlug()` 正確讀取 `runtime_type`
- `List()` 正確讀取 `runtime_type`
- 既有專案（migration 前）的 `runtime_type` 為 `docker-compose`（DEFAULT）

**Usecase 層：**
- `Create()` 接受 `runtimeType` 參數，建立正確的 ProjectModel
- `Create()` 透過 `registry.GetAdapter()` 取得 adapter（而非直接使用 `s.adapter`）
- `Create()` 傳入無效 runtime type 回傳 `ErrCodeInvalidInput`
- `Get()` 從 DB 讀取 runtime type，用正確 adapter 查詢 status
- `Reset()` 重建時保持原始 runtime type
- 所有 adapter 呼叫改為透過 registry（11 處）

**CLI：**
- `project create --runtime docker-compose` 正確傳遞
- `project create --runtime kubernetes` 正確傳遞
- `project create`（省略 `--runtime`）使用預設 `docker-compose`

**MCP：**
- `create_project` 帶 `runtime` 參數正確傳遞
- `create_project` 省略 `runtime` 使用預設

### 測試類型分配

| 測試類型 | 測試目標 | 層次 |
|---------|---------|------|
| 單元測試 | `NewProject()` 驗證、`ValidateRuntimeType()`、`K8sNamespace()` | domain |
| 單元測試 | `computePerProjectVars()` runtime 分支 | domain |
| 單元測試 | `AdapterRegistry` CRUD | domain |
| 單元測試 | `ProjectService.Create()` 流程（mock repo + mock registry） | usecase |
| 單元測試 | `ProjectService.Get/Start/Stop/Delete()` adapter dispatch | usecase |
| 整合測試 | `ProjectRepository` CRUD + runtime_type 欄位 | store |
| 整合測試 | Migration 002 正確套用 | store |

### Mock 策略

- **MockAdapterRegistry**：新增至 `internal/domain/mock_registry.go`，
  與現有 `MockRuntimeAdapter` 相同的 function-based mock 模式
- **MockPortAllocator**：新增至 `internal/domain/mock_port_allocator.go`（目前僅在 `usecase/mock_test.go` 有未匯出版本）
- **現有 mock 更新**：`usecase/mock_test.go` 中的 test 改用 `MockAdapterRegistry`
- **不需 mock ConfigRenderer**：已內嵌於 adapter 建構過程

```go
// internal/domain/mock_port_allocator.go
type MockPortAllocator struct {
    AllocatePortsFn func(ctx context.Context) (*PortSet, error)
}

func (m *MockPortAllocator) AllocatePorts(ctx context.Context) (*PortSet, error) {
    if m.AllocatePortsFn != nil {
        return m.AllocatePortsFn(ctx)
    }
    return &PortSet{KongHTTP: 28081, PostgresPort: 28082, PoolerPort: 28083}, nil
}
```

```go
// internal/domain/mock_registry.go
type MockAdapterRegistry struct {
    GetAdapterFn       func(rt RuntimeType) (RuntimeAdapter, error)
    GetPortAllocatorFn func(rt RuntimeType) (PortAllocator, error)
}

func (m *MockAdapterRegistry) GetAdapter(rt RuntimeType) (RuntimeAdapter, error) {
    if m.GetAdapterFn != nil {
        return m.GetAdapterFn(rt)
    }
    return &MockRuntimeAdapter{}, nil
}

func (m *MockAdapterRegistry) GetPortAllocator(rt RuntimeType) (PortAllocator, error) {
    if m.GetPortAllocatorFn != nil {
        return m.GetPortAllocatorFn(rt)
    }
    return &MockPortAllocator{}, nil
}
```

### CI 執行方式

- 所有單元測試與整合測試在一般 CI 中執行
- 整合測試需要 Supabase DB（現有 `integration` build tag，`docker-compose.yml` 提供）
- 無需 K8s 環境（本功能不含 K8s adapter 實作）

---

## Production Ready 考量

### 錯誤處理

- 無效 `runtime` 值在早期驗證（`NewProject()` / `registry.GetAdapter()`）攔截
- DB 中 `runtime_type` 有 CHECK constraint 作為最後防線
- `ErrUnsupportedRuntime` 透過 `errors.Is()` 可被上游精確匹配

### 日誌與可觀測性

- `ProjectService.Create()` 日誌加入 `slog.String("runtime_type", string(runtimeType))` 欄位
- adapter dispatch（`registry.GetAdapter()`）失敗記錄 error log

### 輸入驗證

- `RuntimeType` 在 domain 層驗證（`ValidateRuntimeType()`）
- CLI `--runtime` 旗標值傳入後立即透過 `NewProject()` 驗證
- DB CHECK constraint 作為 defense-in-depth

### 安全性

- `runtime_type` 不是 secret，無需加密
- K8s namespace 命名使用 `supabase-{slug}` 前綴，避免衝突系統 namespace

### 優雅降級

- 若 K8s adapter 未註冊（例如只安裝了 Compose 環境），
  `registry.GetAdapter(RuntimeKubernetes)` 回傳清晰錯誤訊息
- 不影響現有 Compose 專案的任何操作

### 設定管理

- 無新增環境變數或設定項
- `--runtime` 是 per-command 參數，不需全域設定

---

## 待決問題

- ~~`computePerProjectVars` 是否需要為 K8s 產生不同的 `API_EXTERNAL_URL`？~~
  **已決定：** 使用相同的 `http://localhost:{port}`。OrbStack NodePort 可透過 localhost 存取。

---

## 審查

### Reviewer A（架構）

- **狀態：** APPROVED（第一輪）
- **意見：**

**優點：**
1. AdapterRegistry 介面精簡正交，`defaultAdapterRegistry` 建構後 immutable，goroutine-safe
2. 分層邊界清晰：domain 驗證 → registry 檢查 → DB CHECK constraint 三道防線
3. 向後相容設計穩健：DEFAULT 值確保零 migration 風險
4. K8s namespace `supabase-{slug}` 命名正確（≤49 字元 < 63 限制）
5. 擴展性優良：新增 runtime 只需常數 + CHECK + BuildDeps 註冊
6. 錯誤處理結構化：sentinel error 支援 `errors.Is()`

**非阻擋性建議：**
1. 釐清 `NewRuntimeAdapter()` 工廠函數在 AdapterRegistry 引入後的去留
2. `ValidateRuntimeType` 考慮改為 data-driven（map 而非 switch）
3. 考慮 `AdapterRegistry` 加入 `SupportedRuntimes() []RuntimeType`

### Reviewer B（實作）

- **狀態：** REVISE（第一輪）→ APPROVED（第二輪）
- **意見：**

**第一輪提出問題（5 項，全部已修正）：**
1. ~~ProjectView 結構片段遺失既有欄位~~ → 已補完所有欄位，RuntimeType 改用 `string` 型別
2. ~~MockPortAllocator 在 domain 層不存在~~ → 已新增 `MockPortAllocator` 定義
3. ~~DB Migration 非冪等~~ → 已改用 `IF NOT EXISTS` + `DO $$ ... END $$`
4. ~~Store 層 SQL 變更未詳述~~ → 已新增完整 SQL + Go Scan 程式碼片段
5. ~~provisionConfig() 變更未明確描述~~ → 已新增完整變更描述、`adapterFor()` helper、`NewProjectService()` 驗證變更

**第一輪非阻擋性建議（已採納）：**
1. ✅ 引入 `adapterFor()` helper 統一 adapter lookup
2. ✅ `NewProjectService()` 驗證邏輯更新已明確列出

**第二輪確認：** 全部 5 項修正正確無誤，無新阻擋性問題。

**第二輪非阻擋性建議：**
1. `ProjectView` json tag 建議補上 `omitempty`（與現有程式碼一致）
2. `Create()` 中 registry early validation 應在 `projectRepo.Create()` 前執行
3. 舊的未匯出 `mockPortAllocator` 應遷移至使用 `MockAdapterRegistry`

---

## 任務

### 任務 1：DB Migration — runtime_type 欄位
- **來源設計：** 本文件「資料模型 > DB Migration」
- **影響檔案：** `migrations/002_add_runtime_type.sql`（新增）
- **驗收標準：** Migration 可冪等執行，既有 projects row 的 runtime_type 為 'docker-compose'
- **測試任務：** 整合測試中驗證（任務 4）
- **狀態：** 未開始

### 任務 2：Domain — RuntimeType 驗證與 ProjectModel 變更
- **來源設計：** 本文件「資料模型 > ProjectModel 變更」+「錯誤處理 > RuntimeType 驗證」+「K8s Namespace 命名規則」
- **影響檔案：**
  - `internal/domain/runtime_adapter.go`（新增 `ValidateRuntimeType`、`ErrInvalidRuntimeType`）
  - `internal/domain/project_model.go`（`ProjectModel` 加入 `RuntimeType`、`NewProject` 加入第三參數、新增 `K8sNamespace()`）
  - `internal/domain/project_model_test.go`（更新所有 `NewProject` 呼叫、新增 RuntimeType 驗證測試、新增 K8sNamespace 測試）
  - `internal/domain/project_config_test.go`（更新 `NewProject` 呼叫）
- **驗收標準：** `NewProject("slug", "name", RuntimeDockerCompose)` 成功；`NewProject("slug", "name", "invalid")` 回傳 `ErrInvalidRuntimeType`；`K8sNamespace()` 回傳 `supabase-{slug}`
- **測試任務：** 包含於上述測試檔案
- **狀態：** 未開始

### 任務 3：Domain — AdapterRegistry 介面與實作 + Mock 型別
- **來源設計：** 本文件「介面合約 > AdapterRegistry」+「Mock 策略」
- **影響檔案：**
  - `internal/domain/adapter_registry.go`（新增：介面 + `ErrUnsupportedRuntime`）
  - `internal/domain/adapter_registry_impl.go`（新增：`defaultAdapterRegistry` + `NewAdapterRegistry`）
  - `internal/domain/adapter_registry_test.go`（新增：單元測試）
  - `internal/domain/mock_registry.go`（新增：`MockAdapterRegistry`）
  - `internal/domain/mock_port_allocator.go`（新增：`MockPortAllocator`）
- **驗收標準：** `NewAdapterRegistry(configs...)` 成功建構；`GetAdapter`/`GetPortAllocator` 對已註冊 type 回傳正確值、對未註冊 type 回傳 `ErrUnsupportedRuntime`
- **測試任務：** `adapter_registry_test.go`
- **狀態：** 未開始

### 任務 4：Store — ProjectRepository runtime_type 支援
- **來源設計：** 本文件「介面合約 > Store 層 SQL 變更」
- **影響檔案：**
  - `internal/store/postgres/project_repo.go`（`Create`、`GetBySlug`、`List` 加入 `runtime_type`）
  - `internal/store/postgres/integration_test.go`（更新 `NewProject` 呼叫、新增 runtime_type 驗證）
- **驗收標準：** Create 寫入 runtime_type；GetBySlug 讀取 runtime_type；List 讀取 runtime_type；既有資料的 runtime_type 為 'docker-compose'
- **測試任務：** `integration_test.go` 更新
- **狀態：** 未開始

### 任務 5：Usecase — ProjectService 重構為 AdapterRegistry
- **來源設計：** 本文件「介面合約 > usecase.Config 變更」+「執行流程」+「內部 helper 變更」
- **影響檔案：**
  - `internal/usecase/project_service.go`（`Config` 改用 `Registry`、`Create` 加入 `runtimeType` 參數、`ProjectView` 加入 `RuntimeType`）
  - `internal/usecase/project_service_impl.go`（`projectService` struct 改用 `registry`、新增 `adapterFor()`、11 處 adapter 呼叫改用 `adapterFor()`、`provisionConfig` 改用 registry、`NewProjectService` 驗證更新、`toProjectView` 填入 RuntimeType）
  - `internal/usecase/mock_test.go`（更新 mock 以配合新 Config 結構）
  - `internal/usecase/project_service_test.go`（`newTestService` helper 改用 `MockAdapterRegistry`、更新所有 `Create` 呼叫加入 runtimeType、新增 adapter dispatch 測試）
- **驗收標準：** 所有現有 usecase 測試通過；Create 接受 runtimeType 參數；不同 runtime type 使用對應 adapter
- **測試任務：** `project_service_test.go` 更新
- **狀態：** 未開始

### 任務 6：Domain — computePerProjectVars 參數化
- **來源設計：** 本文件「執行流程 > computePerProjectVars 參數化」
- **影響檔案：**
  - `internal/domain/project_config.go`（`computePerProjectVars` 加入 runtime switch）
  - `internal/domain/project_config_test.go`（新增 Compose 含 DOCKER_SOCKET_LOCATION、K8s 不含的測試）
- **驗收標準：** Compose 模式包含 `DOCKER_SOCKET_LOCATION`；K8s 模式不包含；其餘 key 不變
- **測試任務：** `project_config_test.go` 更新
- **狀態：** 未開始

### 任務 7：CLI + MCP — --runtime 旗標與 runtime 參數
- **來源設計：** 本文件「介面合約 > CLI 變更」+「MCP 變更」
- **影響檔案：**
  - `cmd/sbctl/project.go`（`buildCreateCmd` 加入 `--runtime` 旗標）
  - `cmd/sbctl/mcp.go`（`create_project` tool 加入 `runtime` 參數、handler 取得並傳遞 runtimeType）
  - `cmd/sbctl/deps.go`（`BuildDeps` 改用 `NewAdapterRegistry` + `AdapterRegistryConfig`）
- **驗收標準：** `sbctl project create slug -n name` 預設 docker-compose；`sbctl project create slug -n name --runtime kubernetes` 傳遞 kubernetes；MCP 同理
- **測試任務：** 手動 CLI 驗證 + MCP 整合驗證
- **狀態：** 未開始

### 任務依賴

```
任務 1（DB Migration）
  ↓
任務 2（Domain Model）──→ 任務 6（computePerProjectVars）
  ↓
任務 3（AdapterRegistry）
  ↓
任務 4（Store）
  ↓
任務 5（Usecase）
  ↓
任務 7（CLI + MCP）
```

---

## 程式碼審查

- **審查結果：**
- **發現問題：**
- **修正記錄：**
