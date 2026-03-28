> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# 設計文件：Helm Chart 研究與 Values 對映

## 狀態

code_review

## Phase

- **Phase：** Phase 6
- **Phase Plan：** `docs/designs/phase-6-plan.md`

---

## 目的

K8s adapter 的 `Create` 操作需要將 `ProjectConfig`（93 個 key-value 環境變數）轉換為 Helm chart 的
`values.yaml` 格式。本功能的目的是：

1. **研究** `supabase-community/supabase-kubernetes` Helm chart（v0.5.2）的完整 values 結構
2. **建立** `ProjectConfig` key → `values.yaml` path 的完整對映表（Go 資料結構）
3. **實作** `HelmValuesMapper` — 將 `ProjectConfig` 轉換為 `map[string]any`（可直接序列化為 values.yaml）

此功能是功能 3（K8s Config Renderer）的前置條件。Renderer 直接使用本功能的 `HelmValuesMapper` 產出
values.yaml artifact。

---

## 範圍

### 包含

- Helm chart values.yaml 結構分析與文件記錄
- `ProjectConfig` 93 個 key 到 values.yaml path 的完整對映表
- `HelmValuesMapper` struct：接受 `*domain.ProjectConfig`，輸出 `map[string]any`
- 對映表的單元測試（覆蓋所有 4 個 category 的 key）
- 處理「此 key 在 K8s 環境不適用」的情況（如 `DOCKER_SOCKET_LOCATION`）

### 不包含

- 實際的 YAML 序列化（屬於功能 3 — K8s Config Renderer）
- `helm install` / `kubectl` 操作（屬於功能 6 — K8s Adapter）
- Helm chart 的安裝或部署測試
- 修改現有 `ConfigSchema()` 或 `computePerProjectVars()`

---

## Helm Chart 研究結果

### Chart 資訊

- **Chart：** `supabase-community/supabase-kubernetes`
- **Version：** 0.5.2
- **App Version：** Supabase（多個子元件版本）

### values.yaml 頂層結構

```yaml
secret:          # 所有敏感值（JWT、DB password、dashboard、SMTP 等）
deployment:      # 每個服務的 Pod 設定（enabled, replicaCount, probes...）
image:           # 每個服務的容器映像設定
environment:     # 每個服務的環境變數
persistence:     # 每個服務的 PVC 設定
service:         # 每個服務的 K8s Service 設定（type, port, nodePort...）
autoscaling:     # HPA 設定
ingress:         # Ingress 設定
serviceAccount:  # ServiceAccount 設定
```

### Chart 服務列表（12 個）

| Chart 服務 | 對應 domain.ServiceName | 內部 Port | 備註 |
|-----------|------------------------|-----------|------|
| analytics | ServiceAnalytics | 4000 | Logflare |
| auth | ServiceAuth | 9999 | GoTrue |
| db | ServiceDB | 5432 | PostgreSQL |
| functions | ServiceFunctions | 9000 | Edge Runtime |
| imgproxy | ServiceImgProxy | 5001 | |
| kong | ServiceKong | 8000 | API Gateway |
| meta | ServiceMeta | 8080 | Postgres Meta |
| realtime | ServiceRealtime | 4000 | WebSocket |
| rest | ServiceRest | 3000 | PostgREST |
| storage | ServiceStorage | 5000 | |
| studio | ServiceStudio | 3000 | Admin UI |
| vector | ServiceVector | 9001 | Logs Pipeline |

**注意：** Chart 另有 `minio` 服務（disabled by default），我們不使用。
Chart **無** supavisor 元件。

### Secret 結構

```yaml
secret:
  jwt:
    anonKey: ""
    serviceKey: ""
    secret: ""
  db:
    password: ""
    database: "postgres"
  analytics:
    publicAccessToken: ""
    privateAccessToken: ""
  smtp:
    username: ""
    password: ""
  dashboard:
    username: ""
    password: ""
    openAiApiKey: ""
  s3:
    keyId: ""
    accessKey: ""
  realtime:
    secretKeyBase: ""
  meta:
    cryptoKey: ""
```

### Service 暴露策略（我們的設計）

| 服務 | K8s Service Type | 原因 |
|------|-----------------|------|
| kong | **NodePort** | 外部 API 存取入口，需從 Mac host 存取 |
| db | **NodePort** | 直接 DB 存取（`psql`、開發工具） |
| studio | ClusterIP | 透過 Kong 代理（OrbStack 可直接存取 ClusterIP） |
| 其他 | ClusterIP | 內部服務，不需外部暴露 |

---

## 資料模型

### HelmMapping — 單一 key 的對映規則

```go
// HelmMapping describes how a single ProjectConfig key maps to a Helm values.yaml path.
type HelmMapping struct {
    // ConfigKey is the ProjectConfig key (e.g., "JWT_SECRET").
    ConfigKey string

    // ValuesPath is the dot-separated path in values.yaml (e.g., "secret.jwt.secret").
    // Empty string means this key is not mapped to values.yaml (skipped).
    ValuesPath string

    // Transform is an optional function to convert the config value before placing it
    // in the values map. If nil, the raw string value is used.
    // Example: converting "8000" to int 8000 for port values.
    Transform func(value string) (any, error)
}
```

### HelmValuesMapper — 主要元件

```go
// HelmValuesMapper converts a ProjectConfig into a Helm values map.
// It is stateless and safe for concurrent use.
type HelmValuesMapper struct {
    mappings []HelmMapping
}

// NewHelmValuesMapper creates a mapper with the default supabase chart mapping table.
func NewHelmValuesMapper() *HelmValuesMapper

// MapValues converts a ProjectConfig into a nested map suitable for Helm values.yaml.
// Keys with empty ValuesPath are silently skipped.
// Returns error if any Transform function fails.
func (m *HelmValuesMapper) MapValues(config *domain.ProjectConfig) (map[string]any, error)
```

---

## 介面合約

### NewHelmValuesMapper

```go
// Package: internal/adapter/k8s

func NewHelmValuesMapper() *HelmValuesMapper
```

回傳一個內建完整對映表的 mapper 實例。對映表在建構時固定，不可修改。

### MapValues

```go
func (m *HelmValuesMapper) MapValues(config *domain.ProjectConfig) (map[string]any, error)
```

**輸入：** `*domain.ProjectConfig`（已 resolve 完成的 config，包含所有 93 個 key 的值）

**輸出：** `map[string]any` — 巢狀 map，可直接用 `yaml.Marshal` 序列化為 values.yaml

**行為：**
1. 若 `config` 為 nil → 回傳 `ErrNilConfig`
2. 建立空的 `result := map[string]any{}`
3. 注入靜態 Helm 值（service type、persistence、ingress、autoscaling 等）
4. 遍歷 `m.mappings`：
   a. 若 `ValuesPath` 為空 → 跳過（此 key 在 K8s 環境不適用）
   b. 呼叫 `config.GetSensitive(mapping.ConfigKey)` 取得原始值
   c. 若 `ok == false`（key 不存在於 config） → 靜默跳過
   d. 若有 Transform → 呼叫 `Transform(value)` 取得轉換後的值；失敗回傳 error
   e. 將值透過 `setNestedValue(result, mapping.ValuesPath, transformedValue)` 設入巢狀 map
5. 設定 NodePort 值（`service.kong.nodePort` = KONG_HTTP_PORT，`service.db.nodePort` = POSTGRES_PORT）
6. 回傳 `result`

---

## 完整對映表

### GeneratedSecret keys（12 個）

| ConfigKey | ValuesPath | Transform | 備註 |
|-----------|-----------|-----------|------|
| `JWT_SECRET` | `secret.jwt.secret` | — | |
| `ANON_KEY` | `secret.jwt.anonKey` | — | |
| `SERVICE_ROLE_KEY` | `secret.jwt.serviceKey` | — | |
| `POSTGRES_PASSWORD` | `secret.db.password` | — | |
| `DASHBOARD_PASSWORD` | `secret.dashboard.password` | — | |
| `SECRET_KEY_BASE` | `secret.realtime.secretKeyBase` | — | |
| `VAULT_ENC_KEY` | _(skip)_ | — | Supavisor 專用，chart 無 supavisor |
| `PG_META_CRYPTO_KEY` | `secret.meta.cryptoKey` | — | |
| `LOGFLARE_PUBLIC_ACCESS_TOKEN` | `secret.analytics.publicAccessToken` | — | |
| `LOGFLARE_PRIVATE_ACCESS_TOKEN` | `secret.analytics.privateAccessToken` | — | |
| `S3_PROTOCOL_ACCESS_KEY_ID` | `secret.s3.keyId` | — | |
| `S3_PROTOCOL_ACCESS_KEY_SECRET` | `secret.s3.accessKey` | — | |

### PerProject keys（13 個）

| ConfigKey | ValuesPath | Transform | 備註 |
|-----------|-----------|-----------|------|
| `KONG_HTTP_PORT` | `service.kong.port` | toInt | 同時設 nodePort |
| `KONG_HTTPS_PORT` | _(skip)_ | — | K8s 不需 HTTPS port |
| `POSTGRES_PORT` | `service.db.port` | toInt | 同時設 nodePort |
| `POOLER_PROXY_PORT_TRANSACTION` | _(skip)_ | — | Chart 無 supavisor |
| `API_EXTERNAL_URL` | `environment.auth.API_EXTERNAL_URL` | — | |
| `SUPABASE_PUBLIC_URL` | `environment.studio.SUPABASE_PUBLIC_URL` | — | |
| `SITE_URL` | `environment.auth.GOTRUE_SITE_URL` | — | 注意 key 名稱不同 |
| `PROJECT_DATA_DIR` | _(skip)_ | — | K8s 使用 PVC，不用 host path |
| `STUDIO_DEFAULT_ORGANIZATION` | `environment.studio.DEFAULT_ORGANIZATION_NAME` | — | |
| `STUDIO_DEFAULT_PROJECT` | `environment.studio.DEFAULT_PROJECT_NAME` | — | |
| `STORAGE_TENANT_ID` | `environment.storage.TENANT_ID` | — | |
| `POOLER_TENANT_ID` | _(skip)_ | — | Chart 無 supavisor |
| `DOCKER_SOCKET_LOCATION` | _(skip)_ | — | K8s 不使用 docker socket |

### StaticDefault keys（38 個）— 大部分直接對映到 environment

| ConfigKey | ValuesPath | Transform | 備註 |
|-----------|-----------|-----------|------|
| `POSTGRES_HOST` | _(skip)_ | — | Chart 內部自動設定 pod DNS |
| `POSTGRES_DB` | `secret.db.database` | — | |
| `GOTRUE_API_HOST` | `environment.auth.GOTRUE_API_HOST` | — | |
| `GOTRUE_API_PORT` | `environment.auth.GOTRUE_API_PORT` | — | |
| `GOTRUE_DB_DRIVER` | `environment.auth.DB_DRIVER` | — | |
| `GOTRUE_JWT_ADMIN_ROLES` | `environment.auth.GOTRUE_JWT_ADMIN_ROLES` | — | |
| `GOTRUE_JWT_AUD` | `environment.auth.GOTRUE_JWT_AUD` | — | |
| `GOTRUE_JWT_DEFAULT_GROUP_NAME` | `environment.auth.GOTRUE_JWT_DEFAULT_GROUP_NAME` | — | |
| `PGRST_DB_ANON_ROLE` | `environment.rest.PGRST_DB_ANON_ROLE` | — | |
| `PGRST_DB_USE_LEGACY_GUCS` | `environment.rest.PGRST_DB_USE_LEGACY_GUCS` | — | |
| `KONG_DATABASE` | `environment.kong.KONG_DATABASE` | — | |
| `KONG_DNS_ORDER` | `environment.kong.KONG_DNS_ORDER` | — | |
| `KONG_DNS_NOT_FOUND_TTL` | _(skip)_ | — | Chart 未使用此 key |
| `DB_AFTER_CONNECT_QUERY` | `environment.realtime.DB_AFTER_CONNECT_QUERY` | — | |
| `DB_ENC_KEY` | `environment.realtime.DB_ENC_KEY` | — | |
| `ERL_AFLAGS` | `environment.realtime.ERL_AFLAGS` | — | |
| `APP_NAME` | `environment.realtime.APP_NAME` | — | |
| `SEED_SELF_HOST` | `environment.realtime.SEED_SELF_HOST` | — | |
| `RUN_JANITOR` | `environment.realtime.RUN_JANITOR` | — | |
| `STORAGE_BACKEND` | _(skip)_ | — | Chart 管理 storage backend |
| `FILE_SIZE_LIMIT` | `environment.storage.FILE_SIZE_LIMIT` | — | |
| `ENABLE_IMAGE_TRANSFORMATION` | `environment.storage.ENABLE_IMAGE_TRANSFORMATION` | — | |
| `IMGPROXY_LOCAL_FILESYSTEM_ROOT` | `environment.imgproxy.IMGPROXY_LOCAL_FILESYSTEM_ROOT` | — | |
| `IMGPROXY_USE_ETAG` | `environment.imgproxy.IMGPROXY_USE_ETAG` | — | |
| `LOGFLARE_NODE_HOST` | `environment.analytics.LOGFLARE_NODE_HOST` | — | |
| `DB_SCHEMA` | `environment.analytics.DB_SCHEMA` | — | |
| `LOGFLARE_SINGLE_TENANT` | `environment.analytics.LOGFLARE_SINGLE_TENANT` | — | |
| `LOGFLARE_SUPABASE_MODE` | `environment.analytics.LOGFLARE_SUPABASE_MODE` | — | |
| `CLUSTER_POSTGRES` | _(skip)_ | — | Supavisor 專用 |
| `REGION` | `environment.storage.REGION` | — | |
| `POOLER_POOL_MODE` | _(skip)_ | — | Supavisor 專用 |
| `NEXT_PUBLIC_ENABLE_LOGS` | `environment.studio.NEXT_PUBLIC_ENABLE_LOGS` | — | |
| `NEXT_ANALYTICS_BACKEND_PROVIDER` | `environment.studio.NEXT_ANALYTICS_BACKEND_PROVIDER` | — | |
| `HOSTNAME` | _(skip)_ | — | K8s 自動管理 hostname |
| `DISABLE_HEALTHCHECK_LOGGING` | `environment.realtime.DISABLE_HEALTHCHECK_LOGGING` | — | |
| `REQUEST_ALLOW_X_FORWARDED_PATH` | `environment.storage.REQUEST_ALLOW_X_FORWARDED_PATH` | — | |
| `PG_META_PORT` | `environment.meta.PG_META_PORT` | — | |
| `IMGPROXY_BIND` | `environment.imgproxy.IMGPROXY_BIND` | — | |

### UserOverridable keys（30 個）

| ConfigKey | ValuesPath | Transform | 備註 |
|-----------|-----------|-----------|------|
| `PGRST_DB_SCHEMAS` | `environment.rest.PGRST_DB_SCHEMAS` | — | |
| `PGRST_DB_MAX_ROWS` | `environment.rest.PGRST_DB_MAX_ROWS` | — | |
| `PGRST_DB_EXTRA_SEARCH_PATH` | `environment.rest.PGRST_DB_EXTRA_SEARCH_PATH` | — | |
| `ADDITIONAL_REDIRECT_URLS` | `environment.auth.GOTRUE_URI_ALLOW_LIST` | — | Key 不同 |
| `DISABLE_SIGNUP` | `environment.auth.GOTRUE_DISABLE_SIGNUP` | — | |
| `ENABLE_EMAIL_SIGNUP` | `environment.auth.GOTRUE_EXTERNAL_EMAIL_ENABLED` | — | |
| `ENABLE_ANONYMOUS_USERS` | `environment.auth.GOTRUE_EXTERNAL_ANONYMOUS_USERS_ENABLED` | — | |
| `ENABLE_EMAIL_AUTOCONFIRM` | `environment.auth.GOTRUE_MAILER_AUTOCONFIRM` | — | |
| `ENABLE_PHONE_SIGNUP` | `environment.auth.GOTRUE_EXTERNAL_PHONE_ENABLED` | — | |
| `ENABLE_PHONE_AUTOCONFIRM` | `environment.auth.GOTRUE_SMS_AUTOCONFIRM` | — | |
| `SMTP_ADMIN_EMAIL` | `environment.auth.GOTRUE_SMTP_ADMIN_EMAIL` | — | |
| `SMTP_HOST` | `environment.auth.GOTRUE_SMTP_HOST` | — | |
| `SMTP_PORT` | `environment.auth.GOTRUE_SMTP_PORT` | — | |
| `SMTP_USER` | `secret.smtp.username` | — | Chart 放在 secret |
| `SMTP_PASS` | `secret.smtp.password` | — | Chart 放在 secret |
| `SMTP_SENDER_NAME` | `environment.auth.GOTRUE_SMTP_SENDER_NAME` | — | |
| `MAILER_URLPATHS_INVITE` | `environment.auth.GOTRUE_MAILER_URLPATHS_INVITE` | — | |
| `MAILER_URLPATHS_CONFIRMATION` | `environment.auth.GOTRUE_MAILER_URLPATHS_CONFIRMATION` | — | |
| `MAILER_URLPATHS_RECOVERY` | `environment.auth.GOTRUE_MAILER_URLPATHS_RECOVERY` | — | |
| `MAILER_URLPATHS_EMAIL_CHANGE` | `environment.auth.GOTRUE_MAILER_URLPATHS_EMAIL_CHANGE` | — | |
| `FUNCTIONS_VERIFY_JWT` | `environment.functions.VERIFY_JWT` | — | Key 不同 |
| `IMGPROXY_AUTO_WEBP` | _(skip)_ | — | Chart 使用 `IMGPROXY_ENABLE_WEBP_DETECTION` |
| `IMGPROXY_MAX_SRC_RESOLUTION` | _(skip)_ | — | Chart 未使用此 key |
| `GLOBAL_S3_BUCKET` | `environment.storage.GLOBAL_S3_BUCKET` | — | |
| `OPENAI_API_KEY` | `secret.dashboard.openAiApiKey` | — | |
| `POOLER_DEFAULT_POOL_SIZE` | _(skip)_ | — | Supavisor 專用 |
| `POOLER_MAX_CLIENT_CONN` | _(skip)_ | — | Supavisor 專用 |
| `POOLER_DB_POOL_SIZE` | _(skip)_ | — | Supavisor 專用 |
| `JWT_EXPIRY` | `environment.auth.GOTRUE_JWT_EXP` | — | Key 不同 |
| `DASHBOARD_USERNAME` | `secret.dashboard.username` | — | Chart 放在 secret |

### 對映統計

| 分類 | 總 key 數 | 有對映 | 跳過（skip） | 跳過原因 |
|------|----------|--------|-------------|---------|
| GeneratedSecret | 12 | 11 | 1 | VAULT_ENC_KEY（supavisor 專用）|
| PerProject | 13 | 8 | 5 | K8s 不適用或 supavisor 專用 |
| StaticDefault | 38 | 32 | 6 | Chart 自動管理或 supavisor 專用 |
| UserOverridable | 30 | 25 | 5 | supavisor 專用或 chart 不支援 |
| **合計** | **93** | **76** | **17** | |

> 註：`ConfigSchema()` 定義 93 個 key（StaticDefault 38 + PerProject 13 + GeneratedSecret 12 + UserOverridable 30）。
> 其中 `DOCKER_SOCKET_LOCATION` 在 K8s runtime 的 `computePerProjectVars` 中不會產生值，
> 但仍列入對映表（標記為 skip），確保所有 key 都有明確的處理決策。

---

## 靜態 Helm 值（非來自 ProjectConfig）

除了 ProjectConfig 對映外，`MapValues` 還需注入 K8s 特有的靜態設定：

```go
// 這些值不來自 ProjectConfig，而是 K8s adapter 的固定設定。
// 所有路徑為明確的 dot-separated path，無通配符。
staticValues := map[string]any{
    // Kong 使用 NodePort 暴露 API
    "service.kong.type": "NodePort",

    // DB 使用 NodePort 暴露（直接 psql 存取）
    "service.db.type": "NodePort",

    // 停用 minio（使用 file backend）
    "deployment.minio.enabled": false,

    // 啟用 DB 持久化
    "persistence.db.enabled":           true,
    "persistence.db.storageClassName":  "",  // 使用 default（local-path）
    "persistence.db.size":             "5Gi",
    "persistence.db.accessModes":      []string{"ReadWriteOnce"},

    // 停用 ingress（我們用 NodePort 直接存取）
    "ingress.enabled": false,

    // 停用 autoscaling（本地開發環境）— 逐一列出 12 個服務
    "autoscaling.analytics.enabled": false,
    "autoscaling.auth.enabled":      false,
    "autoscaling.db.enabled":        false,
    "autoscaling.functions.enabled": false,
    "autoscaling.imgproxy.enabled":  false,
    "autoscaling.kong.enabled":      false,
    "autoscaling.meta.enabled":      false,
    "autoscaling.realtime.enabled":  false,
    "autoscaling.rest.enabled":      false,
    "autoscaling.storage.enabled":   false,
    "autoscaling.studio.enabled":    false,
    "autoscaling.vector.enabled":    false,
}
```

---

## 執行流程

### MapValues 流程

1. 若 `config` 為 nil → 回傳 `ErrNilConfig`
2. 建立空的 `result := map[string]any{}`
3. 注入靜態 Helm 值（service type、persistence、ingress、autoscaling 等）
4. 遍歷 `m.mappings`：
   a. 若 `ValuesPath` 為空 → 跳過（此 key 在 K8s 環境不適用）
   b. 呼叫 `config.GetSensitive(mapping.ConfigKey)` 取得 `(value, ok)`
   c. 若 `ok == false`（key 不存在於 config） → 靜默跳過
   d. 若有 Transform → 呼叫 `Transform(value)` 取得轉換後的值；若失敗回傳 error
   e. 將值透過 `setNestedValue(result, mapping.ValuesPath, transformedValue)` 設入巢狀 map
5. 設定 NodePort 值（`service.kong.nodePort` = KONG_HTTP_PORT，`service.db.nodePort` = POSTGRES_PORT）
6. 回傳 `result`

### setNestedValue 輔助函式

```go
// setNestedValue sets a value in a nested map using dot-separated path.
// Example: setNestedValue(m, "secret.jwt.secret", "abc")
// Creates intermediate maps as needed.
func setNestedValue(m map[string]any, path string, value any)
```

---

## 錯誤處理

| 情境 | 處理方式 | 回應格式 |
|------|---------|---------|
| config 中缺少 required key | MapValues 不檢查 required（已由 ResolveConfig 保證） | 不適用 |
| Transform 函式失敗（如 port 不是數字） | 回傳 `fmt.Errorf("transform %s: %w", key, err)` | error |
| config 為 nil | 回傳 `ErrNilConfig`（哨兵錯誤：`var ErrNilConfig = errors.New("helm values mapper: config must not be nil")`） | error，可用 `errors.Is` 判斷 |
| 對映表中的 ValuesPath 為空 | 靜默跳過該 key | 不回傳 error |
| `GetSensitive` 回傳 `ok == false` | 靜默跳過（key 不存在於 config，可能因為 runtime 差異） | 不回傳 error |

---

## 測試策略

### 需要測試的行為

- `NewHelmValuesMapper()` 建立 mapper 並包含正確數量的 mapping entries
- `MapValues` 正確對映所有 GeneratedSecret keys 到 `secret.*` 路徑
- `MapValues` 正確對映 PerProject keys 到 `environment.*` 或 `service.*` 路徑
- `MapValues` 正確跳過 ValuesPath 為空的 keys
- `MapValues` 的 Transform 函式正確將 port string 轉為 int
- `MapValues` 注入靜態 Helm 值（NodePort type、persistence 等）
- `MapValues` 處理 Kong 和 DB 的 NodePort 設定
- `setNestedValue` 正確建立巢狀 map 結構
- `setNestedValue` 對已存在的中間 map 不覆蓋
- Transform 失敗時回傳 error
- nil config 回傳 `ErrNilConfig`（可用 `errors.Is` 判斷）
- `GetSensitive` 回傳 `ok == false` 時靜默跳過
- config 包含不在對映表中的 key 時不影響輸出（自然忽略）

### 測試類型分配

| 測試類型 | 測試目標 | 層次 |
|---------|---------|------|
| 單元測試 | HelmValuesMapper.MapValues 各種 config input | adapter/k8s |
| 單元測試 | setNestedValue 各種路徑 | adapter/k8s |
| 單元測試 | Transform 函式（toInt） | adapter/k8s |
| 單元測試 | 對映表完整性（key count, no duplicate paths） | adapter/k8s |

### 測試風格

- 所有測試使用**表格驅動測試（table-driven tests）**，每個 test case 命名為 `name` 欄位
- `MapValues` 測試以各 category 分組：GeneratedSecret、PerProject、StaticDefault、UserOverridable
- `setNestedValue` 測試覆蓋：單層路徑、多層路徑、已存在中間 map、空路徑
- Transform 測試覆蓋：有效數字、無效數字、邊界值

### Mock 策略

- 不需要 mock — `HelmValuesMapper` 是純函式，無外部依賴
- 使用手動建構的 `domain.ProjectConfig` 作為輸入

### CI 執行方式

- 所有測試在一般 CI 中執行（`go test ./internal/adapter/k8s/...`）
- 不需要 K8s 叢集或 Helm 安裝

---

## Production Ready 考量

### 錯誤處理
- Transform 函式失敗回傳明確的 error，包含 key 名稱
- nil config 防衛性檢查

### 日誌與可觀測性
- 不需要日誌 — 純轉換函式，呼叫者（Renderer / Adapter）負責記錄

### 輸入驗證
- `ProjectConfig` 在進入 MapValues 前已經過 `ResolveConfig` 驗證
- MapValues 不重複驗證 required keys

### 安全性
- `MapValues` 使用 `config.GetSensitive()` 取得原始 secret 值
- 產出的 map 包含明文 secret — 呼叫者必須安全處理（寫入 K8s Secret 或 values file）

### 優雅降級
- 不適用 — 純同步函式

### 設定管理
- Helm chart version pinned 在對映表中
- 若 chart 更新 values 結構，需更新對映表

---

## 待決問題

- ~~Q: Kong nodePort 值是否應等於 KONG_HTTP_PORT？~~ A: 是。NodePort 範圍 30000-32767 由 K8sPortAllocator（功能 5）負責分配，此處僅消費分配後的值。

---

## 審查

### Reviewer A（架構）

- **狀態：** REVISE（第一輪）→ REVISE（第二輪）→ APPROVED（第三輪）
- **意見：**
  - 第一輪：4 項問題（autoscaling 通配符、流程邏輯矛盾、key 總數 94→93、GetSensitive ok=false）→ 全部已修正
  - 第二輪：1 項問題 — 對映統計表 StaticDefault 與 UserOverridable 的 mapped/skip 小計與明細表不一致 → ✅ 已修正（32/6, 25/5, 合計 76/17）
  - 第三輪：確認所有修正正確，統計表與明細表完全一致。APPROVED。

### Reviewer B（實作）

- **狀態：** REVISE（第一輪）→ REVISE（第二輪）→ APPROVED（第三輪）
- **意見：**
  - 第一輪：8 項問題 → 全部已修正
  - 第二輪：1 項問題 — 同 Reviewer A 第二輪，對映統計表數字不一致 → ✅ 已修正
  - 第三輪：逐行交叉比對統計表與明細表完全一致。APPROVED。

---

## 任務

### 任務 1：HelmMapping 型別與 setNestedValue 輔助函式 — ✅ 已完成
- **來源設計：** 本文件
- **影響檔案：** `internal/adapter/k8s/helm_values_mapper.go`（新增）、`internal/adapter/k8s/helm_values_mapper_test.go`（新增）
- **驗收標準：** `HelmMapping` struct 定義完成；`setNestedValue` 函式實作完成；`ErrNilConfig` 哨兵錯誤定義；表格驅動單元測試覆蓋單層/多層/已存在中間 map 等路徑
- **測試任務：** `internal/adapter/k8s/helm_values_mapper_test.go`（`TestSetNestedValue` table-driven）

### 任務 2：對映表定義與 NewHelmValuesMapper — ✅ 已完成
- **來源設計：** 本文件「完整對映表」段落
- **影響檔案：** `internal/adapter/k8s/helm_values_mapper.go`（修改）、`internal/adapter/k8s/helm_values_mapper_test.go`（修改）
- **驗收標準：** 完整 93 個 key 的對映表定義於 `defaultMappings()`；`NewHelmValuesMapper()` 回傳內含完整對映表的 mapper；`toInt` transform 函式實作；測試驗證 mapping entry 數量（93）、無重複 ValuesPath、skip 數量（17）
- **測試任務：** `TestNewHelmValuesMapper`（entry count）、`TestMappingTableIntegrity`（no duplicate paths）

### 任務 3：MapValues 實作與完整測試 — ✅ 已完成
- **來源設計：** 本文件「介面合約」與「執行流程」段落
- **影響檔案：** `internal/adapter/k8s/helm_values_mapper.go`（修改）、`internal/adapter/k8s/helm_values_mapper_test.go`（修改）
- **驗收標準：** `MapValues` 完整實作（nil 檢查、靜態值注入、mapping 遍歷、NodePort 設定）；表格驅動測試覆蓋：GeneratedSecret 對映、PerProject 對映、StaticDefault 對映、UserOverridable 對映、skip keys、GetSensitive ok=false、Transform 失敗、nil config、未知 key 不影響輸出
- **測試任務：** `TestMapValues_*` 系列表格驅動測試

---

## 程式碼審查

- **審查結果：**
- **發現問題：**
- **修正記錄：**
