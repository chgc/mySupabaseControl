> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# 設計文件：設定 Schema 與環境變數目錄（Config Schema）

## 狀態

revising（第三輪審查後修訂完成）

## Phase

- **Phase：** Phase 1
- **Phase Plan：** `docs/designs/phase_1/phase-1-plan.md`

---

## 目的

定義一套有型別的設定 schema，將 `docker-compose.yml` 中 94 個環境變數結構化管理，並提供渲染介面以支援多種 runtime 目標（`.env`、K8s ConfigMap/Secret）。

本功能解決的核心問題：目前環境變數以未分類的平面 `.env` 檔案管理，所有專案共用基底 secrets，使用者無法清楚區分哪些值可修改、哪些由系統產生。

---

## 範圍

### 包含

- 94 個環境變數的完整分類目錄
- 有型別的設定 schema Go struct
- 全域設定 vs 每專案設定的維度
- ConfigRenderer 介面定義
- Secret 產生規則

### 不包含

- ConfigRenderer 的具體實作（Phase 2 Docker Compose Adapter）
- K8s ConfigMap/Secret 渲染邏輯（Phase 5）
- 環境變數的 runtime 注入機制

---

## 資料模型

### 環境變數分類

每個環境變數歸屬於以下四個分類之一：

| 分類 | 說明 | 行為 |
|------|------|------|
| `StaticDefault` | 所有專案相同的預設值 | 內建於設定 schema，不需使用者輸入 |
| `PerProject` | 各專案不同的設定值 | 由 Control Plane 自動計算或分配（如 port） |
| `GeneratedSecret` | 每專案獨立產生的密鑰 | 建立專案時自動產生，不可由使用者手動輸入 |
| `UserOverridable` | 使用者可選擇性覆寫的值 | 有預設值，使用者可在建立或編輯時覆寫 |

### 環境變數完整目錄

#### StaticDefault（共用靜態預設值）

| 環境變數 | 預設值 | 使用服務 | 說明 |
|---------|--------|---------|------|
| `POSTGRES_HOST` | `db` | auth, rest, realtime, storage, meta, analytics, supavisor | Docker 服務名稱 |
| `POSTGRES_DB` | `postgres` | 同上 | 預設資料庫名稱 |
| `GOTRUE_API_HOST` | `0.0.0.0` | auth | 監聽所有介面 |
| `GOTRUE_API_PORT` | `9999` | auth | GoTrue 內部 port |
| `GOTRUE_DB_DRIVER` | `postgres` | auth | DB driver |
| `GOTRUE_JWT_ADMIN_ROLES` | `service_role` | auth | JWT admin 角色 |
| `GOTRUE_JWT_AUD` | `authenticated` | auth | JWT audience |
| `GOTRUE_JWT_DEFAULT_GROUP_NAME` | `authenticated` | auth | 預設群組 |
| `PGRST_DB_ANON_ROLE` | `anon` | rest | PostgREST anonymous role |
| `PGRST_DB_USE_LEGACY_GUCS` | `false` | rest | PostgREST GUC 設定 |
| `KONG_DATABASE` | `off` | kong | 無 DB 模式 |
| `KONG_DNS_ORDER` | `LAST,A,CNAME` | kong | DNS 解析順序 |
| `KONG_DNS_NOT_FOUND_TTL` | `1` | kong | DNS 快取 TTL |
| `DB_AFTER_CONNECT_QUERY` | `SET search_path TO _realtime` | realtime | Realtime DB search path |
| `DB_ENC_KEY` | `supabaserealtime` | realtime | Realtime 加密 key（TODO：Phase 2 應改為每專案獨立產生的 GeneratedSecret，避免多專案共用同一加密金鑰） |
| `ERL_AFLAGS` | `-proto_dist inet_tcp` | realtime, supavisor | Erlang TCP 協議 |
| `APP_NAME` | `realtime` | realtime | 服務名稱 |
| `SEED_SELF_HOST` | `true` | realtime | 自動建立 tenant |
| `RUN_JANITOR` | `true` | realtime | 自動清理 |
| `STORAGE_BACKEND` | `file` | storage | 檔案系統儲存 |
| `FILE_SIZE_LIMIT` | `52428800` | storage | 50 MB 限制 |
| `ENABLE_IMAGE_TRANSFORMATION` | `true` | storage | 啟用圖片轉換 |
| `IMGPROXY_LOCAL_FILESYSTEM_ROOT` | `/` | imgproxy | 檔案系統根目錄 |
| `IMGPROXY_USE_ETAG` | `true` | imgproxy | ETag 快取 |
| `LOGFLARE_NODE_HOST` | `127.0.0.1` | analytics | Logflare 節點 |
| `DB_SCHEMA` | `_analytics` | analytics | Analytics schema |
| `LOGFLARE_SINGLE_TENANT` | `true` | analytics | 單一租戶 |
| `LOGFLARE_SUPABASE_MODE` | `true` | analytics | Supabase 模式 |
| `CLUSTER_POSTGRES` | `true` | supavisor | 使用 Postgres 儲存 |
| `REGION` | `local` | storage, supavisor | 區域 |
| `POOLER_POOL_MODE` | `transaction` | supavisor | 連線池模式 |
| `NEXT_PUBLIC_ENABLE_LOGS` | `true` | studio | 啟用日誌 |
| `NEXT_ANALYTICS_BACKEND_PROVIDER` | `postgres` | studio | 日誌後端 |
| `HOSTNAME` | `::` | studio | IPv4+IPv6 |
| `DISABLE_HEALTHCHECK_LOGGING` | `true` | realtime | 隱藏健康檢查日誌 |
| `REQUEST_ALLOW_X_FORWARDED_PATH` | `true` | storage | 允許 proxy 轉發 |

#### PerProject（每專案設定）

| 環境變數 | 計算規則 | 使用服務 | 說明 |
|---------|---------|---------|------|
| `KONG_HTTP_PORT` | 自動分配（起始 28081） | kong | 對外 API port |
| `KONG_HTTPS_PORT` | `KONG_HTTP_PORT + 1` | kong | 對外 HTTPS port |
| `POSTGRES_PORT` | 自動分配（起始 54320） | db, supavisor, 所有 DB 連線 | PostgreSQL port |
| `POOLER_PROXY_PORT_TRANSACTION` | 自動分配（起始 64300） | supavisor | 連線池 port |
| `API_EXTERNAL_URL` | `http://localhost:${KONG_HTTP_PORT}` | auth | 對外 API URL |
| `SUPABASE_PUBLIC_URL` | `http://localhost:${KONG_HTTP_PORT}` | storage, functions, studio | 對外 URL |
| `SITE_URL` | `http://localhost:3000` | auth | 前端 redirect URL |
| `PROJECT_DATA_DIR` | `./projects/${SLUG}/volumes` | studio, storage, imgproxy, db, functions | 專案資料目錄 |
| `STUDIO_DEFAULT_ORGANIZATION` | `Default Organization` | studio | 預設組織 |
| `STUDIO_DEFAULT_PROJECT` | `${DISPLAY_NAME}` | studio | 預設專案名稱 |
| `STORAGE_TENANT_ID` | `stub` | storage | 儲存租戶 ID（Phase 1 暫用 stub 值，Phase 2 將改為從 SLUG 衍生） |
| `POOLER_TENANT_ID` | `${SLUG}` | supavisor | 連線池租戶 ID |
| `DOCKER_SOCKET_LOCATION` | `/var/run/docker.sock` | vector | Docker socket 路徑（TODO：Phase 2 評估改為 UserOverridable 以支援非標準 Docker Desktop 路徑） |
| `STUDIO_PORT` | 自動分配（起始 54323） | studio | Studio UI port |
| `PG_META_PORT` | 自動分配（起始 54380） | meta | Meta API port |
| `IMGPROXY_BIND` | `:${IMGPROXY_PORT}`（自動分配，起始 54381）| imgproxy | ImgProxy 監聽 port |

#### GeneratedSecret（每專案自動產生）

| 環境變數 | 產生規則 | 使用服務 | 說明 |
|---------|---------|---------|------|
| `JWT_SECRET` | 64 字元 hex random | auth, rest, realtime, storage, functions, supavisor, db | JWT 簽名密鑰 |
| `ANON_KEY` | JWT token（role=anon, 由 JWT_SECRET 簽發） | kong, storage, functions, studio | 公開 API key |
| `SERVICE_ROLE_KEY` | JWT token（role=service_role, 由 JWT_SECRET 簽發） | kong, storage, functions, studio | 管理 API key |
| `POSTGRES_PASSWORD` | 32 字元 alphanumeric random | 所有 DB 連線服務 | DB 密碼 |
| `DASHBOARD_PASSWORD` | 32 字元 alphanumeric random | kong | Studio 登入密碼 |
| `SECRET_KEY_BASE` | 64 字元 hex random | realtime, supavisor | Phoenix session key |
| `VAULT_ENC_KEY` | 32 字元 alphanumeric random | supavisor | Vault 加密金鑰 |
| `PG_META_CRYPTO_KEY` | 32 字元 alphanumeric random | meta, studio | Meta 加密金鑰 |
| `LOGFLARE_PUBLIC_ACCESS_TOKEN` | 32 字元 alphanumeric random | analytics, studio, vector | Logflare 公開 token |
| `LOGFLARE_PRIVATE_ACCESS_TOKEN` | 32 字元 alphanumeric random | analytics, studio | Logflare 私有 token |
| `S3_PROTOCOL_ACCESS_KEY_ID` | 32 字元 alphanumeric random | storage | S3 協議 access key |
| `S3_PROTOCOL_ACCESS_KEY_SECRET` | 64 字元 alphanumeric random | storage | S3 協議 secret key |

#### UserOverridable（使用者可覆寫）

| 環境變數 | 預設值 | 使用服務 | 說明 |
|---------|--------|---------|------|
| `PGRST_DB_SCHEMAS` | `public,storage,graphql_public` | rest, studio | 暴露的 DB schema |
| `PGRST_DB_MAX_ROWS` | `1000` | rest, studio | 最大回傳列數 |
| `PGRST_DB_EXTRA_SEARCH_PATH` | `public` | rest | 額外 search path |
| `ADDITIONAL_REDIRECT_URLS` | （空） | auth | 額外 redirect URL |
| `DISABLE_SIGNUP` | `false` | auth | 停用註冊 |
| `ENABLE_EMAIL_SIGNUP` | `true` | auth | 啟用 email 註冊 |
| `ENABLE_ANONYMOUS_USERS` | `false` | auth | 啟用匿名使用者 |
| `ENABLE_EMAIL_AUTOCONFIRM` | `true` | auth | 自動確認 email |
| `ENABLE_PHONE_SIGNUP` | `true` | auth | 啟用電話註冊 |
| `ENABLE_PHONE_AUTOCONFIRM` | `true` | auth | 自動確認電話 |
| `SMTP_ADMIN_EMAIL` | `admin@example.com` | auth | SMTP 寄件者 |
| `SMTP_HOST` | `supabase-mail` | auth | SMTP 主機 |
| `SMTP_PORT` | `2500` | auth | SMTP port |
| `SMTP_USER` | `fake_mail_user` | auth | SMTP 帳號 |
| `SMTP_PASS` | `fake_mail_password` | auth | SMTP 密碼 |
| `SMTP_SENDER_NAME` | `fake_sender` | auth | SMTP 寄件者名稱 |
| `MAILER_URLPATHS_INVITE` | `/auth/v1/verify` | auth | 邀請信 URL 路徑 |
| `MAILER_URLPATHS_CONFIRMATION` | `/auth/v1/verify` | auth | 確認信 URL 路徑 |
| `MAILER_URLPATHS_RECOVERY` | `/auth/v1/verify` | auth | 密碼重設 URL 路徑 |
| `MAILER_URLPATHS_EMAIL_CHANGE` | `/auth/v1/verify` | auth | Email 變更 URL 路徑 |
| `FUNCTIONS_VERIFY_JWT` | `false` | functions | Edge Functions JWT 驗證 |
| `IMGPROXY_AUTO_WEBP` | `true` | imgproxy | 自動轉 WebP |
| `IMGPROXY_MAX_SRC_RESOLUTION` | `16.8` | imgproxy | 最大來源解析度 |
| `GLOBAL_S3_BUCKET` | `stub` | storage | S3 bucket 名稱 |
| `OPENAI_API_KEY` | （空） | studio | OpenAI API key |
| `POOLER_DEFAULT_POOL_SIZE` | `15` | supavisor | 預設連線池大小 |
| `POOLER_MAX_CLIENT_CONN` | `200` | supavisor | 最大客戶端連線數 |
| `POOLER_DB_POOL_SIZE` | `5` | supavisor | DB 連線池大小 |
| `JWT_EXPIRY` | `3600` | auth, rest, db | JWT token 過期時間（秒），使用者可依需求調整 |
| `DASHBOARD_USERNAME` | `supabase` | kong | Studio 登入帳號，使用者可自訂 |

> **備註：** OAuth provider 設定（Google、GitHub、Azure）、SMS provider 設定、MFA 設定等預設為註解狀態，
> 在 Phase 1 設計中列為 `UserOverridable`，但暫不實作 UI。使用者可透過 override 機制啟用。

### Go 型別定義

```go
package domain

// ServiceName 定義 Supabase stack 中的服務名稱。
type ServiceName string

const (
    ServiceDB        ServiceName = "db"
    ServiceAuth      ServiceName = "auth"
    ServiceRest      ServiceName = "rest"
    ServiceRealtime  ServiceName = "realtime"
    ServiceStorage   ServiceName = "storage"
    ServiceImgProxy  ServiceName = "imgproxy"
    ServiceKong      ServiceName = "kong"
    ServiceMeta      ServiceName = "meta"
    ServiceFunctions ServiceName = "functions"
    ServiceAnalytics ServiceName = "analytics"
    ServicePooler    ServiceName = "supavisor"
    ServiceStudio    ServiceName = "studio"
    ServiceVector    ServiceName = "vector"
)

// ConfigCategory 定義環境變數的分類。
type ConfigCategory string

const (
    CategoryStaticDefault   ConfigCategory = "static_default"
    CategoryPerProject      ConfigCategory = "per_project"
    CategoryGeneratedSecret ConfigCategory = "generated_secret"
    CategoryUserOverridable ConfigCategory = "user_overridable"
)

// ConfigScope 定義設定的作用域。
type ConfigScope string

const (
    ScopeGlobal  ConfigScope = "global"   // 全 host 共用
    ScopeProject ConfigScope = "project"  // 每專案獨立
)

// 注意：Phase 1 所有環境變數均為 ScopeProject。
// ScopeGlobal 保留給未來全 host 共用的設定（如 Traefik 路由設定）。

// ConfigEntry 定義一個環境變數的 metadata。
type ConfigEntry struct {
    Key          string         // 環境變數名稱
    Category     ConfigCategory // 分類
    Scope        ConfigScope    // 作用域
    DefaultValue string         // 預設值（StaticDefault、UserOverridable 使用）
    Description  string         // 說明
    Services     []ServiceName  // 使用此變數的服務清單
    Sensitive    bool           // 是否為敏感值（影響日誌、顯示）
    Required     bool           // 是否為必要值
}

// ConfigSchema 定義完整的設定 schema。
func ConfigSchema() []ConfigEntry
```

### ProjectConfig

```go
// ProjectConfig 表示一個專案的完整設定。
// 由 ConfigSchema + 專案特定值 + 使用者覆寫合併產生。
type ProjectConfig struct {
    // ProjectSlug 所屬專案
    ProjectSlug string

    // Values 為最終計算後的所有環境變數 key-value pairs
    Values map[string]string

    // Overrides 為使用者明確覆寫的值（僅記錄 UserOverridable 類別）
    Overrides map[string]string
}

// ResolveConfig 從 schema、專案模型、已產生的 secrets、port 分配結果與使用者覆寫，
// 計算出完整的 ProjectConfig。
// 優先順序：使用者覆寫 > 每專案計算值 > 產生的 secret > 靜態預設值
// secrets：由 GenerateProjectSecrets 產生（新建專案）或從 ConfigRepository 載入（已有專案）
// portSet：由 PortAllocator 分配（新建專案）或從 ConfigRepository 載入（已有專案）
func ResolveConfig(
    project *ProjectModel,
    secrets map[string]string,
    portSet *PortSet,
    overrides map[string]string,
) (*ProjectConfig, error)

// Get 取得指定 key 的值。
// 對 Sensitive=true 的欄位回傳遮罩值（"***"）。
func (c *ProjectConfig) Get(key string) (string, bool)

// GetSensitive 取得敏感值（僅在授權情境下使用）。
// 與 Get 的差異：Get 對 Sensitive=true 的欄位回傳遮罩值（"***"）；
// GetSensitive 回傳原始值，應只在需要實際值的情境（如渲染 .env）中使用。
func (c *ProjectConfig) GetSensitive(key string) (string, bool)
```

### Config 持久化策略

`ProjectConfig` 的完整設定（含 secrets）透過 `state-store.md` 中定義的 `ConfigRepository` 持久化至 Supabase `project_configs` 資料表（`is_secret = true` 的敏感值 Phase 1 以明文儲存，Phase 3 評估 Supabase Vault 加密）。

**新建專案流程：**
1. `GenerateProjectSecrets(gen)` — 產生 secrets
2. `PortAllocator.AllocatePorts()` — 分配 ports
3. `ResolveConfig(project, secrets, portSet, overrides)` — 合併設定（內部計算 PerProject vars）
4. `ConfigRepository.SaveConfig(ctx, slug, config)` — 持久化

> **實作注意（SaveConfig）：** `project_configs` table 的每筆 row 需要 `is_secret`（對應 `ConfigEntry.Sensitive`）與 `category` 欄位。`ConfigRepository.SaveConfig` 的實作必須呼叫 `ConfigSchema()` 做 key lookup，以取得每個 env var 的 metadata。`ProjectConfig.Values` 本身不攜帶這些資訊。

**載入現有專案流程：**
1. `config, err := ConfigRepository.GetConfig(ctx, slug)` — 從 DB 載入完整設定
2. `portSet, err := ExtractPortSet(config)` — 從 Values 重建 PortSet
3. `secrets := extractSecrets(config)` — 從 Values 萃取 GeneratedSecret 類別的 key-value（ConfigSchema() 提供分類資訊）
4. `ResolveConfig(project, secrets, portSet, latestOverrides)` — 套用最新 overrides
   （純載入場景：`latestOverrides = config.Overrides`；編輯場景：合併 `config.Overrides` 與新請求的覆寫值）

此設計確保 JWT_SECRET 等 secrets 在服務重啟後不會被重新產生。

### ConfigRenderer 介面

```go
// Artifact 表示一個渲染後的設定檔案。
type Artifact struct {
    Path    string // 檔案路徑（相對於專案目錄）
    Content []byte // 檔案內容
    Mode    uint32 // 檔案權限（如 0600 for secrets）
}

// ConfigRenderer 將 ProjectConfig 渲染為特定 runtime 的設定檔案。
// Docker Compose Adapter 實作渲染為 .env 檔案。
// K8s Adapter 實作渲染為 ConfigMap + Secret YAML。
type ConfigRenderer interface {
    // Render 將 ProjectConfig 渲染為一組 Artifact。
    Render(config *ProjectConfig) ([]Artifact, error)
}
```

### Secret 產生

```go
// SecretGenerator 負責產生各種密鑰。
type SecretGenerator interface {
    // RandomHex 產生指定長度的十六進位隨機字串。
    RandomHex(length int) (string, error)

    // RandomAlphanumeric 產生指定長度的英數字隨機字串。
    RandomAlphanumeric(length int) (string, error)

    // GenerateJWT 以指定 secret 與 role 簽發 JWT token。
    GenerateJWT(secret string, role string, expiry int) (string, error)
}

// GenerateProjectSecrets 為新專案產生所有必要的 secrets。
// 回傳 key-value map，key 為環境變數名稱。
func GenerateProjectSecrets(gen SecretGenerator) (map[string]string, error)
```

---

## 介面合約

### 設定解析流程

```go
// 1. 取得設定 schema
schema := ConfigSchema()

// 2. 產生 secrets（僅新建專案；載入已有專案時從 ConfigRepository 讀取）
secrets, err := GenerateProjectSecrets(generator)

// 3. 分配 ports（僅新建專案；載入已有專案時從 ProjectConfig 萃取）
portSet, err := portAllocator.AllocatePorts()

// 4. 合併所有設定值（ResolveConfig 內部呼叫 ComputePerProjectVars）
config, err := ResolveConfig(project, secrets, portSet, userOverrides)

// 5. 渲染為 runtime artifacts
artifacts, err := renderer.Render(config)
```

```go
// ComputePerProjectVars 根據 ProjectModel 與 PortSet 計算所有 PerProject 分類的環境變數。
// 回傳 key-value map，key 為環境變數名稱。
// 此函數為 ResolveConfig 的 internal helper，一般情況下應透過 ResolveConfig 呼叫，不直接使用。
// （Go 實作時應為 unexported：computePerProjectVars）
func ComputePerProjectVars(project *ProjectModel, ports *PortSet) map[string]string

// extractSecrets 從 ProjectConfig 的 Values 中萃取 GeneratedSecret 分類的 key-value。
// 依賴 ConfigSchema() 取得每個 key 的 Category 分類資訊。
// 為 ResolveConfig 的 internal helper（Go 實作時應為 unexported）。
func extractSecrets(config *ProjectConfig) map[string]string

// ExtractPortSet 從 ProjectConfig 的 Values 中重建 PortSet。
// 用於載入已有專案時，從 ConfigRepository.GetConfig 的回傳值中萃取 port 資訊。
// 若任何必要的 port key 遺失或值格式不正確，回傳 ErrInvalidPortSet。
//
// PortSet 欄位與 env var key 的對應關係（注意 IMGPROXY_BIND 需 strip ":" 前綴）：
//   KongHTTP     ← Values["KONG_HTTP_PORT"]      格式：整數字串
//   KongHTTPS    ← Values["KONG_HTTPS_PORT"]     格式：整數字串
//   PostgresPort ← Values["POSTGRES_PORT"]       格式：整數字串
//   PoolerPort   ← Values["POOLER_PROXY_PORT_TRANSACTION"] 格式：整數字串
//   StudioPort   ← Values["STUDIO_PORT"]         格式：整數字串
//   MetaPort     ← Values["PG_META_PORT"]        格式：整數字串
//   ImgProxyPort ← Values["IMGPROXY_BIND"]       格式：":{port}"（需 strings.TrimPrefix(":")）
func ExtractPortSet(config *ProjectConfig) (*PortSet, error)
```

---

## 執行流程

### 建立專案時的設定產生

1. `GenerateProjectSecrets(gen)` — 產生 JWT_SECRET、POSTGRES_PASSWORD 等
2. `portAllocator.AllocatePorts()` — 分配 KONG_HTTP_PORT、POSTGRES_PORT 等（回傳 *PortSet）
3. `ResolveConfig(project, secrets, portSet, overrides)` — 合併所有設定值（內部計算 PerProject vars）
4. 驗證所有 `Required` 的 ConfigEntry 都有值
5. `renderer.Render(config)` — 渲染為 .env（或 K8s YAML）
6. 寫入檔案系統
7. `ConfigRepository.SaveConfig(ctx, slug, config)` — 持久化至 Supabase

### Port 分配

```go
// PortAllocator 負責分配不衝突的 port。
type PortAllocator interface {
    // AllocatePorts 為新專案分配一組 port。
    // 掃描現有專案與系統已佔用的 port，回傳未衝突的 port 組合。
    AllocatePorts() (*PortSet, error)
}

// 注意：AllocatePorts 的實作必須確保並發安全（例如透過 DB 鎖或序列化請求），
// 避免多個並發的「建立專案」請求分配到相同的 port。

// PortSet 包含一個專案所需的所有 port。
type PortSet struct {
    KongHTTP       int // 對外 API port（起始 28081）
    KongHTTPS      int // 對外 HTTPS port（KongHTTP + 1）
    PostgresPort   int // PostgreSQL port（起始 54320）
    PoolerPort     int // Supavisor transaction port（起始 64300）
    StudioPort     int // Studio UI port（起始 54323）
    MetaPort       int // pg-meta API port（起始 54380）
    ImgProxyPort   int // imgproxy port（起始 54381）
}
```

---

## 錯誤處理

| 情境 | 處理方式 | 回應格式 |
|------|---------|---------|
| Secret 產生失敗 | 回傳 error，不建立專案 | `{ "error": "secret_generation_failed" }` |
| Port 全部佔用 | 回傳 `ErrNoAvailablePort` | `{ "error": "no_available_port" }` |
| 必要設定值遺漏 | 回傳 `ErrMissingRequiredConfig` + 遺漏的 key 清單 | `{ "error": "missing_config", "keys": [...] }` |
| 使用者覆寫的 key 不在 UserOverridable 分類 | 回傳 `ErrConfigNotOverridable` | `{ "error": "not_overridable", "key": "..." }` |
| ExtractPortSet 的 port key 遺失或格式錯誤 | 回傳 `ErrInvalidPortSet` | `{ "error": "invalid_port_set" }` |

```go
// ErrMissingRequiredConfig 在必要設定值遺漏時回傳。
// 為 struct 型別以攜帶遺漏的 key 清單。
type ErrMissingRequiredConfig struct {
    Keys []string
}

func (e *ErrMissingRequiredConfig) Error() string {
    return fmt.Sprintf("missing required config keys: %v", e.Keys)
}

// ErrConfigNotOverridable 在使用者嘗試覆寫非 UserOverridable 分類的 key 時回傳。
type ErrConfigNotOverridable struct {
    Key string
}

func (e *ErrConfigNotOverridable) Error() string {
    return fmt.Sprintf("config key %q is not overridable", e.Key)
}

// ErrInvalidPortSet 在 ExtractPortSet 遇到遺失 key 或格式錯誤時回傳。
type ErrInvalidPortSet struct {
    Key   string // 遺失或格式錯誤的 env var key
    Value string // 原始值（格式錯誤時）
}

func (e *ErrInvalidPortSet) Error() string {
    if e.Value == "" {
        return fmt.Sprintf("port key %q missing from config", e.Key)
    }
    return fmt.Sprintf("port key %q has invalid value %q", e.Key, e.Value)
}
```

---

## 測試策略

### 需要測試的行為

- ConfigSchema 完整性：所有 94 個環境變數都有定義
- ResolveConfig 優先順序：使用者覆寫 > 每專案 > 產生的 secret > 預設值
- Secret 產生：JWT_SECRET 格式正確、ANON_KEY 是合法 JWT
- Port 分配：不衝突、邊界條件（全滿時回傳錯誤）
- 使用者覆寫驗證：只允許 UserOverridable 類別
- Sensitive 欄位：GetSensitive 與 Get 的行為差異
- SaveConfig / GetConfig round-trip：存入再讀出，確認 94 個 key 完整且 is_secret 正確
- 載入已有專案的 ResolveConfig 路徑：ExtractPortSet 正確重建、覆寫邏輯正確套用
- ExtractPortSet 邊界：port key 遺失時回傳 ErrInvalidPortSet

### 測試類型分配

| 測試類型 | 測試目標 | 層次 |
|---------|---------|------|
| 單元測試 | ConfigSchema 完整性 | domain |
| 單元測試 | ResolveConfig 合併邏輯 | domain |
| 單元測試 | Secret 產生格式與長度 | domain |
| 單元測試 | Port 分配邏輯 | domain |
| 單元測試 | ExtractPortSet（含 IMGPROXY_BIND ":{port}" 格式、key 遺失邊界） | domain |
| 整合測試 | SaveConfig / GetConfig round-trip（94 keys、is_secret 正確） | repository |

### Mock 策略

- `SecretGenerator` — mock crypto/rand 避免測試不穩定
- `PortAllocator` — mock port 掃描，控制回傳的可用 port

### CI 執行方式

- 所有測試在一般 CI 中執行，無需特殊環境。

---

## Production Ready 考量

### 錯誤處理
- Secret 產生使用 `crypto/rand`，不使用 `math/rand`
- 所有 error 包含上下文（哪個 key、哪個步驟失敗）

### 日誌與可觀測性
- 設定解析完成時記錄日誌：`project_slug`、`override_count`、`secret_count`
- **不記錄 sensitive 值**

### 輸入驗證
- 使用者覆寫的 key 必須存在於 ConfigSchema
- 使用者覆寫的 key 必須屬於 UserOverridable 分類
- 值的格式驗證（如 port 必須為數字且在合法範圍）

### 安全性
- Generated secrets 使用 `crypto/rand`
- Sensitive 值在日誌中遮罩
- .env 檔案權限設為 0600

### 優雅降級
- 若 crypto/rand 不可用（極端情境），回傳明確錯誤而非 fallback 到不安全的 random

### 設定管理
- Port 起始範圍可透過環境變數設定
- 保留 port 範圍可設定

---

## 待決問題

- Port 自動分配的起始值與範圍需要確認（避免與常用 port 衝突）
- ✅ JWT_EXPIRY 歸類：已確定改為 UserOverridable，允許使用者調整 token 過期時間。
- OAuth/SMS/MFA 相關的環境變數（目前為註解狀態）如何處理？建議在 schema 中定義但標記為 optional。
- 是否需要設定版本管理（migration）機制？建議 Phase 1 不需要，Phase 3 再評估。

---

## 審查

### Reviewer A（架構）

- **狀態：** 🔁 REVISE（第一輪）→ 🔁 REVISE（第二輪）→ ✅ APPROVED（第三輪）
- **第一輪意見（摘要）：** 6 個阻斷性問題，全部已解決。
- **第二輪意見（摘要）：**
  1. 🔴 **[已修正]** ComputePerProjectVars 三處流程矛盾 + 參數型別錯誤 → 採用方案 A，移除顯式呼叫步驟，統一流程
  2. 🟡 **[已修正]** 載入流程 loadedPortSet 來源補充說明
  3. 🟡 **[已修正]** DOCKER_SOCKET_LOCATION / STORAGE_TENANT_ID 分類說明補充
- **第三輪意見（摘要）：**
  - 所有前輪問題均已正確修復，架構設計健全。
  - 🟡 **[已修正]** ComputePerProjectVars 應為 unexported → 文件標注 Go 實作應用 lowercase
  - 🟡 **[已修正]** extractSecrets 缺少正式簽名 → 補充函數定義
  - 🟡 **[已修正]** latestOverrides 來源說明 → 補充純載入 vs 編輯場景

### Reviewer B（實作）

- **狀態：** 🔁 REVISE（第一輪）→ 🔁 REVISE（第二輪）→ 🔁 REVISE（第三輪）
- **第一輪意見（摘要）：** 3 個阻斷性問題，全部已解決。
- **第二輪意見（摘要）：**
  1. 🔴 **[已修正]** 載入流程 loadedPortSet 來源不明 → 加入 ExtractPortSet() 函數
  2. 🔴 **[已修正]** ComputePerProjectVars 流程矛盾（與 Reviewer A 一致）
  3. 🔴 **[已修正]** SaveConfig 隱式依賴 ConfigSchema() → 加入實作注意說明
  4. 🟡 **[已修正]** 測試策略補充 round-trip、ExtractPortSet 邊界案例
- **第三輪意見（摘要）：**
  1. 🔴 **[已修正]** ExtractPortSet IMGPROXY_BIND ":{port}" 格式陷阱 → 加入完整欄位對應表含格式說明
  2. 🟡 **[已修正]** ErrInvalidPortSet / ErrConfigNotOverridable struct 定義補全
  3. 🟡 **[已修正]** latestOverrides 來源說明（與 Reviewer A 一致）
  4. 🟡 **[已修正]** extractSecrets 正式簽名
  5. 🟡 **[已修正]** 測試類型分配表補充 ExtractPortSet + SaveConfig round-trip
- **第一輪意見（摘要）：** 3 個阻斷性問題，全部已解決。
- **第二輪意見（摘要）：**
  1. 🔴 **[已修正]** 載入流程 loadedPortSet 來源不明 → 加入 ExtractPortSet() 函數，補充載入流程步驟
  2. 🔴 **[已修正]** ComputePerProjectVars 流程矛盾（與 Reviewer A 一致）
  3. 🔴 **[已修正]** SaveConfig 隱式依賴 ConfigSchema() → 加入實作注意說明
  4. 🟡 **[已修正]** 測試策略補充 round-trip、ExtractPortSet 邊界案例

---

## 任務

<!-- 待審查通過後展開 -->

---

## 程式碼審查

- **審查結果：**
- **發現問題：**
- **修正記錄：**
