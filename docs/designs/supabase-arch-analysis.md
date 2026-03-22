> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# Supabase 架構分析

## 來源

`docs/designs/phase-0-plan.md` — 功能 1

---

## 分析方法

以本專案 `docker-compose.yml` 為主要分析對象，逐一拆解 13 個服務的：
- 架構角色與技術棧
- 服務間依賴與通訊方式
- 資料庫耦合程度（DB user、schema）
- JWT/Secret 共用關係
- 狀態性與持久化方式
- 對外 port 暴露
- 每專案差異化的環境變數

---

## 服務總覽

```
                            ┌──────────┐
                  ┌────────►│  studio  │ (Web UI)
                  │         └────┬─────┘
                  │              │ STUDIO_PG_META_URL
                  │              ▼
 Host Port ──► ┌──┴───┐    ┌────────┐
 KONG_HTTP_PORT │ kong │    │  meta  │──► DB
               └──┬───┘    └────────┘
                  │
       ┌──────────┼──────────┬──────────┬──────────┐
       ▼          ▼          ▼          ▼          ▼
   ┌──────┐  ┌──────┐  ┌────────┐  ┌────────┐  ┌──────────┐
   │ auth │  │ rest │  │realtime│  │storage │  │functions │
   └──┬───┘  └──┬───┘  └───┬────┘  └───┬────┘  └──────────┘
      │         │           │           │
      ▼         ▼           ▼           ▼
   ┌──────────────────────────────────────────┐
   │              db (PostgreSQL)              │
   └──────────────────────────────────────────┘
      ▲              ▲              ▲
      │              │              │
   ┌──────┐    ┌──────────┐   ┌──────────┐
   │ meta │    │analytics │   │supavisor │
   └──────┘    └──────────┘   └──────────┘
                    ▲
                    │
               ┌────────┐
               │ vector │ (日誌收集)
               └────────┘

   ┌──────────┐
   │ imgproxy │ ◄── storage (圖片轉換)
   └──────────┘
```

---

## 逐服務分析

### 1. db（PostgreSQL）

| 項目 | 內容 |
|------|------|
| **Image** | `supabase/postgres:15.8.1.085` |
| **角色** | 核心資料庫，所有 stateful 服務的持久化層 |
| **依賴** | 無（最底層） |
| **狀態性** | **Stateful** — PGDATA 持久化至 `${PROJECT_DATA_DIR}/db/data` |
| **對外 Port** | 無直接暴露（透過 supavisor 代理） |
| **技術棧** | PostgreSQL 15 + pgsodium + 自訂 extensions |

**DB Users（由 init SQL 建立）：**

| DB User | 使用者 | 用途 |
|---------|--------|------|
| `postgres` | functions | 完整權限，Edge Functions 用 |
| `supabase_auth_admin` | auth (GoTrue) | 認證資料管理 |
| `authenticator` | rest (PostgREST) | API 請求入口，以 `SET ROLE` 切換為 `anon`/`authenticated` |
| `supabase_admin` | meta, realtime, analytics, supavisor | 管理用途 |
| `supabase_storage_admin` | storage | 檔案 metadata 管理 |

**Init SQL Scripts（掛載至 `docker-entrypoint-initdb.d/`）：**
- `97-_supabase.sql` — 建立 `_supabase` 內部資料庫
- `98-webhooks.sql` — Webhook 支援
- `99-realtime.sql` — Realtime 所需的 schema
- `99-roles.sql` — 建立上述 DB users 與權限
- `99-jwt.sql` — JWT 相關的 DB 設定（`app.settings.jwt_secret`）
- `99-logs.sql` — Analytics 日誌 schema
- `99-pooler.sql` — Supavisor 連線池 schema

**Volumes：**
- `${PROJECT_DATA_DIR}/db/data` → `/var/lib/postgresql/data`（PGDATA）
- `db-config` named volume → `/etc/postgresql-custom`（pgsodium key）

**JWT 耦合：** `JWT_SECRET` 與 `JWT_EXP` 寫入 DB 的 `app.settings`，供 PostgREST RLS 使用。

**每專案差異化：** `POSTGRES_PORT`、`POSTGRES_PASSWORD`、`POSTGRES_DB`、`JWT_SECRET`、`JWT_EXP`

---

### 2. auth（GoTrue）

| 項目 | 內容 |
|------|------|
| **Image** | `supabase/gotrue:v2.186.0` |
| **角色** | 認證與使用者管理（註冊、登入、OAuth、MFA、Email/Phone） |
| **依賴** | db |
| **DB User** | `supabase_auth_admin` |
| **狀態性** | **Stateful（透過 DB）** — 使用者、session、OTP 等存於 DB |
| **對外 Port** | 無（透過 Kong `/auth/v1/*`） |
| **內部 Port** | 9999 |
| **技術棧** | Go binary |

**關鍵耦合：**
- `GOTRUE_JWT_SECRET` — 簽發 JWT token
- `GOTRUE_DB_DATABASE_URL` — 直連 PostgreSQL
- `API_EXTERNAL_URL` — 對外 URL（每專案不同）
- `GOTRUE_SITE_URL` — 前端 redirect URL

**每專案差異化：** `JWT_SECRET`、`POSTGRES_PASSWORD`、`POSTGRES_PORT`、`API_EXTERNAL_URL`、`SITE_URL`、SMTP 設定、OAuth provider 設定

**共用可行性：** ❌ 不可共用。使用者資料、JWT secret、認證設定均為每專案獨立。

---

### 3. rest（PostgREST）

| 項目 | 內容 |
|------|------|
| **Image** | `postgrest/postgrest:v14.6` |
| **角色** | 自動將 PostgreSQL schema 轉為 RESTful API |
| **依賴** | db |
| **DB User** | `authenticator`（透過 `SET ROLE` 切換為 `anon`/`authenticated`） |
| **狀態性** | **Stateless** — 無本地狀態，所有資料來自 DB |
| **對外 Port** | 無（透過 Kong `/rest/v1/*`） |
| **內部 Port** | 3000 |
| **技術棧** | Haskell binary |

**關鍵耦合：**
- `PGRST_DB_URI` — 直連 PostgreSQL
- `PGRST_JWT_SECRET` — 驗證 JWT token（與 auth 簽發的 token 配對）
- `PGRST_DB_SCHEMAS` — 暴露的 schema

**每專案差異化：** `POSTGRES_PASSWORD`、`POSTGRES_PORT`、`JWT_SECRET`、`PGRST_DB_SCHEMAS`

**共用可行性：** ❌ 不可共用。每個專案有不同的 DB schema、JWT secret、DB 連線。

---

### 4. realtime

| 項目 | 內容 |
|------|------|
| **Image** | `supabase/realtime:v2.76.5` |
| **角色** | 即時訂閱（Postgres Changes、Broadcast、Presence） |
| **依賴** | db |
| **DB User** | `supabase_admin` |
| **狀態性** | **Stateful** — WebSocket 連線狀態、Presence channel 狀態 |
| **對外 Port** | 無（透過 Kong `/realtime/v1/`） |
| **內部 Port** | 4000 |
| **技術棧** | Elixir/Phoenix |

**關鍵耦合：**
- DB 連線（`DB_HOST`、`DB_PORT`）
- `API_JWT_SECRET` — JWT 驗證
- `SECRET_KEY_BASE` — Phoenix session key
- `SEED_SELF_HOST: true` — 自動建立 tenant `realtime-dev`

**特殊注意：** Realtime 的 Docker service name 被 Kong 以 `realtime-dev.supabase-realtime` 格式引用，因為 Realtime 透過解析 subdomain 來識別 tenant。

**每專案差異化：** `DB_HOST`、`DB_PORT`、`DB_PASSWORD`、`JWT_SECRET`、`SECRET_KEY_BASE`

**共用可行性：** ❌ 不可共用。WebSocket 連線狀態與 DB 監聽均為每專案獨立。Realtime 有內建 tenant 機制，但 self-hosted 模式下預設為 single-tenant。

---

### 5. storage

| 項目 | 內容 |
|------|------|
| **Image** | `supabase/storage-api:v1.44.2` |
| **角色** | 檔案上傳、下載、權限控制（S3 相容 API） |
| **依賴** | db, rest, imgproxy |
| **DB User** | `supabase_storage_admin` |
| **狀態性** | **Stateful** — 檔案持久化至 `${PROJECT_DATA_DIR}/storage` |
| **對外 Port** | 無（透過 Kong `/storage/v1/*`） |
| **內部 Port** | 5000 |
| **技術棧** | Node.js（TypeScript） |

**關鍵耦合：**
- DB 連線 — 儲存檔案 metadata
- `POSTGREST_URL: http://rest:3000` — 透過 PostgREST 執行 RLS 檢查
- `AUTH_JWT_SECRET` — JWT 驗證
- `IMGPROXY_URL: http://imgproxy:5001` — 圖片轉換
- `STORAGE_BACKEND: file` — 檔案系統後端（可切換為 S3）

**每專案差異化：** `POSTGRES_PASSWORD`、`POSTGRES_PORT`、`JWT_SECRET`、`ANON_KEY`、`SERVICE_ROLE_KEY`、`SUPABASE_PUBLIC_URL`

**共用可行性：** ❌ 不可共用。檔案儲存與 DB metadata 均為每專案獨立。

---

### 6. imgproxy

| 項目 | 內容 |
|------|------|
| **Image** | `darthsim/imgproxy:v3.30.1` |
| **角色** | 即時圖片轉換（resize、format conversion） |
| **依賴** | 無 |
| **DB User** | 無 |
| **狀態性** | **Stateless** — 純計算，無本地狀態 |
| **對外 Port** | 無 |
| **內部 Port** | 5001 |
| **技術棧** | Go binary |

**關鍵耦合：**
- Volume 掛載 `${PROJECT_DATA_DIR}/storage` — 與 storage 服務共用檔案系統
- `IMGPROXY_LOCAL_FILESYSTEM_ROOT: /` — 讀取本地檔案

**每專案差異化：** 幾乎無（僅 volume 路徑不同）

**共用可行性：** ⚠️ **有條件可共用。** imgproxy 本身是 stateless 的，但因為需要掛載每個專案的 storage volume，共用時需要掛載所有專案的 storage 目錄，或改用 S3 backend。若採用 S3 backend，完全可共用。

---

### 7. meta（postgres-meta）

| 項目 | 內容 |
|------|------|
| **Image** | `supabase/postgres-meta:v0.95.2` |
| **角色** | 提供 PostgreSQL metadata API（表格、欄位、schema、角色等資訊） |
| **依賴** | db |
| **DB User** | `supabase_admin` |
| **狀態性** | **Stateless** — 所有資料來自 DB introspection |
| **對外 Port** | 無（透過 Kong `/pg/*`） |
| **內部 Port** | 28080 |
| **技術棧** | Node.js（TypeScript） |

**關鍵耦合：**
- `PG_META_DB_HOST`、`PG_META_DB_PORT` — 直連 PostgreSQL
- `CRYPTO_KEY` — 加密金鑰

**每專案差異化：** `POSTGRES_HOST`、`POSTGRES_PORT`、`POSTGRES_PASSWORD`、`PG_META_CRYPTO_KEY`

**共用可行性：** ❌ 不可共用。每個 meta 實例綁定一個特定的 PostgreSQL 資料庫。

---

### 8. functions（Edge Runtime）

| 項目 | 內容 |
|------|------|
| **Image** | `supabase/edge-runtime:v1.71.2` |
| **角色** | 執行 Deno-based Edge Functions |
| **依賴** | kong |
| **DB User** | `postgres`（完整權限） |
| **狀態性** | **Stateless** — 函式碼從 volume 載入，無持久狀態 |
| **對外 Port** | 無（透過 Kong `/functions/v1/*`） |
| **內部 Port** | 9000 |
| **技術棧** | Rust + Deno runtime |

**關鍵耦合：**
- `JWT_SECRET` — 驗證呼叫者身份
- `SUPABASE_URL: http://kong:8000` — 回呼 Supabase API
- `SUPABASE_DB_URL` — 直連 PostgreSQL
- Volume：`${PROJECT_DATA_DIR}/functions` → 函式碼
- Named volume：`deno-cache` → Deno 模組快取

**每專案差異化：** `JWT_SECRET`、`ANON_KEY`、`SERVICE_ROLE_KEY`、`POSTGRES_PASSWORD`、函式碼 volume

**共用可行性：** ❌ 不可共用。函式碼、JWT secret、DB 連線均為每專案獨立。

---

### 9. kong

| 項目 | 內容 |
|------|------|
| **Image** | `kong/kong:3.9.1` |
| **角色** | API Gateway — 所有外部請求的統一入口 |
| **依賴** | studio（healthcheck），實際路由至所有後端服務 |
| **DB User** | 無（`KONG_DATABASE: off`，使用 declarative config） |
| **狀態性** | **Stateless** — 無資料庫，路由規則從 YAML 載入 |
| **對外 Port** | `${KONG_HTTP_PORT}:8000`、`${KONG_HTTPS_PORT}:8443` |
| **內部 Port** | 8000 |
| **技術棧** | Kong（OpenResty/Nginx + Lua） |

**路由規則（from kong.yml）：**

| 路徑 | 後端服務 | 認證方式 |
|------|---------|---------|
| `/auth/v1/*` | `http://auth:9999/` | key-auth（部分路徑開放） |
| `/rest/v1/*` | `http://rest:3000/` | key-auth |
| `/graphql/v1` | `http://rest:3000/rpc/graphql` | key-auth |
| `/realtime/v1/` | `ws://realtime:4000/socket/` | key-auth |
| `/storage/v1/*` | `http://storage:5000/` | request-transformer |
| `/functions/v1/*` | `http://functions:9000/` | 無 |
| `/pg/*` | `http://meta:8080/` | key-auth（admin only） |
| `/`（catch-all） | `http://studio:3000/` | basic-auth |

**認證機制：**
- 三個 consumer：`DASHBOARD`（basic-auth）、`anon`（key-auth）、`service_role`（key-auth）
- `request-transformer` plugin 將 API key 轉換為 JWT 放入 `Authorization` header
- ACL plugin 控制 `anon`/`admin` group 的路由存取

**每專案差異化：** `KONG_HTTP_PORT`、`KONG_HTTPS_PORT`、`ANON_KEY`、`SERVICE_ROLE_KEY`、`DASHBOARD_USERNAME`、`DASHBOARD_PASSWORD`

**共用可行性：** ⚠️ **有條件可共用，但複雜度高。** 目前路由規則使用硬編碼 Docker service name（`auth`、`rest` 等），需改為 project-scoped service name 或使用 custom Lua plugin 做動態路由。方案選項：
1. **Path-prefix routing** — `/project-a/rest/v1/*` → `http://rest-a:3000/`（kong.yml 線性增長）
2. **Custom Lua plugin** — 動態解析 project identifier 並路由（可擴展但需開發）
3. **每專案獨立 Kong** — 最簡單但浪費資源

---

### 10. studio

| 項目 | 內容 |
|------|------|
| **Image** | `supabase/studio:2026.03.16-sha-5528817` |
| **角色** | Web 管理介面（SQL Editor、Table Editor、Auth UI、Storage UI、Logs） |
| **依賴** | analytics |
| **DB User** | 無（透過 meta + kong 存取 DB） |
| **狀態性** | **Stateless** — UI 狀態在瀏覽器端 |
| **對外 Port** | 無（透過 Kong `/`） |
| **內部 Port** | 3000 |
| **技術棧** | Next.js（Node.js） |

**關鍵耦合：**
- `STUDIO_PG_META_URL: http://meta:28080` — **1:1 綁定**，指向單一 meta 實例
- `SUPABASE_URL: http://kong:8000` — **1:1 綁定**，指向單一 Kong
- `SUPABASE_ANON_KEY`、`SUPABASE_SERVICE_KEY`、`AUTH_JWT_SECRET` — 專案專屬
- `LOGFLARE_URL: http://analytics:4000` — 日誌查看

**Multi-project 能力：** ❌ **不支援。** `DEFAULT_ORGANIZATION_NAME` 與 `DEFAULT_PROJECT_NAME` 為靜態初始值，無法動態切換。所有 API 連線（meta、kong）均為單一目標，啟動後不可變更。

**每專案差異化：** 幾乎所有環境變數（meta URL、kong URL、API keys、JWT secret）

**共用可行性：** ❌ 不可共用。每個 Studio 實例只能管理一個專案。

---

### 11. analytics（Logflare）

| 項目 | 內容 |
|------|------|
| **Image** | `supabase/logflare:1.31.2` |
| **角色** | 日誌收集、查詢與分析 |
| **依賴** | db |
| **DB User** | `supabase_admin` |
| **DB Schema** | `_analytics`（在 `_supabase` 資料庫中） |
| **狀態性** | **Stateful（透過 DB）** — 日誌儲存於 `_supabase._analytics` schema |
| **對外 Port** | 無 |
| **內部 Port** | 4000 |
| **技術棧** | Elixir/Phoenix |

**關鍵耦合：**
- `POSTGRES_BACKEND_URL` — 直連 `_supabase` 資料庫
- `LOGFLARE_PUBLIC_ACCESS_TOKEN`、`LOGFLARE_PRIVATE_ACCESS_TOKEN` — 存取 token
- `LOGFLARE_SINGLE_TENANT: true` — 單一租戶模式
- `LOGFLARE_SUPABASE_MODE: true` — Supabase 整合模式

**每專案差異化：** `POSTGRES_PASSWORD`、`POSTGRES_PORT`、`LOGFLARE_*_ACCESS_TOKEN`

**共用可行性：** ⚠️ **理論上可共用但不建議。** Logflare 有 multi-tenant 能力（`LOGFLARE_SINGLE_TENANT: true` 可改為 `false`），但日誌隔離、token 管理的複雜度會大幅增加。在本地開發場景下，日誌量有限，獨立部署更簡單。

---

### 12. vector

| 項目 | 內容 |
|------|------|
| **Image** | `timberio/vector:0.53.0-alpine` |
| **角色** | 日誌收集與轉發（從 Docker containers → Logflare） |
| **依賴** | 無（需要 Docker socket） |
| **DB User** | 無 |
| **狀態性** | **Stateless** — 純 pipeline 處理 |
| **對外 Port** | 無 |
| **內部 Port** | 9001（healthcheck） |
| **技術棧** | Rust binary |

**關鍵耦合：**
- `${DOCKER_SOCKET_LOCATION}:/var/run/docker.sock:ro` — 讀取 Docker 容器日誌
- `LOGFLARE_PUBLIC_ACCESS_TOKEN` — 發送日誌到 Logflare
- `volumes/logs/vector.yml` — 路由規則配置

**每專案差異化：** `LOGFLARE_PUBLIC_ACCESS_TOKEN`

**共用可行性：** ✅ **可共用。** Vector 從 Docker socket 收集所有容器的日誌，一個 Vector 實例即可收集所有專案的容器日誌。但需要調整 vector.yml 將日誌路由到對應專案的 Logflare 實例（或 multi-tenant Logflare）。

---

### 13. supavisor

| 項目 | 內容 |
|------|------|
| **Image** | `supabase/supavisor:2.7.4` |
| **角色** | PostgreSQL 連線池管理（類似 PgBouncer） |
| **依賴** | db |
| **DB User** | `supabase_admin` |
| **DB Schema** | `_supabase` 資料庫 |
| **狀態性** | **Stateful** — 維護 connection pool 狀態 |
| **對外 Port** | `${POSTGRES_PORT}:5432`、`${POOLER_PROXY_PORT_TRANSACTION}:6543` |
| **內部 Port** | 4000（API）、5432（Postgres proxy）、6543（transaction mode） |
| **技術棧** | Elixir |

**關鍵耦合：**
- `DATABASE_URL` — ecto 連線至 `_supabase` 資料庫
- `API_JWT_SECRET`、`METRICS_JWT_SECRET` — JWT 驗證
- `SECRET_KEY_BASE`、`VAULT_ENC_KEY` — 加密金鑰
- `POOLER_TENANT_ID` — 租戶識別
- `pooler.exs` — 初始化腳本

**每專案差異化：** `POSTGRES_PORT`、`POSTGRES_PASSWORD`、`POOLER_PROXY_PORT_TRANSACTION`、`JWT_SECRET`、`SECRET_KEY_BASE`、`VAULT_ENC_KEY`、`POOLER_TENANT_ID`

**共用可行性：** ⚠️ **理論上可共用但不建議。** Supavisor 有 multi-tenant 能力（`POOLER_TENANT_ID`），但 self-hosted 模式下路由到多個 DB 需要額外設定。獨立部署更簡單可靠。

---

## 橫向分析

### JWT Secret 共用關係圖

```
JWT_SECRET
  ├── db      (寫入 app.settings.jwt_secret，供 RLS 使用)
  ├── auth    (簽發 JWT token)
  ├── rest    (驗證 JWT token)
  ├── realtime(驗證 JWT token)
  ├── storage (驗證 JWT token)
  ├── functions(驗證 JWT token)
  └── supavisor(驗證 JWT token)

→ JWT_SECRET 是最強的每專案隔離邊界。所有核心服務共用同一個 secret。
```

### DB User 權限層級

```
postgres (superuser)
  └── functions 使用

supabase_admin (管理員)
  ├── meta      (DB introspection)
  ├── realtime  (CDC 監聽)
  ├── analytics (日誌寫入 _supabase._analytics)
  └── supavisor (連線池管理)

supabase_auth_admin
  └── auth (使用者資料 CRUD)

supabase_storage_admin
  └── storage (檔案 metadata CRUD)

authenticator → SET ROLE anon/authenticated
  └── rest (API 請求，透過 RLS 控制權限)
```

### 服務間 HTTP 通訊

| 來源 | 目標 | 用途 |
|------|------|------|
| studio | `http://meta:28080` | DB metadata 查詢 |
| studio | `http://analytics:4000` | 日誌查看 |
| kong | `http://auth:9999` | 認證 API 路由 |
| kong | `http://rest:3000` | REST API 路由 |
| kong | `ws://realtime:4000` | WebSocket 路由 |
| kong | `http://storage:5000` | 儲存 API 路由 |
| kong | `http://functions:9000` | Edge Functions 路由 |
| kong | `http://meta:8080` | Postgres Meta 路由 |
| kong | `http://studio:3000` | Studio UI 路由 |
| storage | `http://rest:3000` | RLS 檢查（透過 PostgREST） |
| storage | `http://imgproxy:5001` | 圖片轉換 |
| functions | `http://kong:8000` | 回呼 Supabase API |
| vector | `http://analytics:4000` | 日誌推送 |

### 每專案必須獨立的環境變數

| 環境變數 | 影響服務 | 原因 |
|---------|---------|------|
| `JWT_SECRET` | auth, rest, realtime, storage, functions, supavisor, db | 安全隔離核心 |
| `POSTGRES_PASSWORD` | 所有連接 DB 的服務 | DB 存取控制 |
| `POSTGRES_PORT` | db, supavisor, 所有 DB 連線 | Port 隔離 |
| `ANON_KEY` | kong, storage, functions, studio | 公開 API key（由 JWT_SECRET 簽發） |
| `SERVICE_ROLE_KEY` | kong, storage, functions, studio | 管理 API key（由 JWT_SECRET 簽發） |
| `KONG_HTTP_PORT` | kong | 對外 API port |
| `API_EXTERNAL_URL` | auth | 對外 URL |
| `SUPABASE_PUBLIC_URL` | storage, functions, studio | 對外 URL |
| `SECRET_KEY_BASE` | realtime, supavisor | 加密金鑰 |

---

## 資源消耗特徵估算

| 服務 | 技術棧 | 預估記憶體 | 啟動時間 | 備註 |
|------|--------|-----------|---------|------|
| db | PostgreSQL (C) | 100–300 MB | 2–5s | 依資料量增長 |
| auth | Go binary | 20–50 MB | <1s | 輕量 |
| rest | Haskell binary | 30–80 MB | 1–2s | Schema 載入時佔用較多 |
| realtime | Elixir/BEAM | 80–200 MB | 5–10s | BEAM VM 基礎消耗 |
| storage | Node.js | 50–100 MB | 2–3s | |
| imgproxy | Go binary | 30–50 MB | <1s | 處理圖片時 CPU 突增 |
| meta | Node.js | 30–60 MB | 1–2s | |
| functions | Rust + Deno | 50–150 MB | 2–3s | 依函式數量增長 |
| kong | OpenResty/Nginx | 30–50 MB | <1s | 非常輕量 |
| studio | Next.js/Node.js | 100–200 MB | 3–5s | 最重的 UI 服務 |
| analytics | Elixir/BEAM | 80–200 MB | 5–10s | BEAM VM 基礎消耗 |
| vector | Rust binary | 20–40 MB | <1s | 非常輕量 |
| supavisor | Elixir/BEAM | 50–100 MB | 3–5s | BEAM VM 基礎消耗 |

**每個完整專案預估總記憶體：670–1,580 MB（約 0.7–1.6 GB）**

---

## 結論

### 共用可行性總結

| 服務 | 共用可行性 | 理由 |
|------|-----------|------|
| db | ❌ 不可 | 核心隔離邊界，JWT secret 寫入 DB settings |
| auth | ❌ 不可 | 使用者資料、JWT secret、認證設定均為每專案獨立 |
| rest | ❌ 不可 | DB 連線、schema、JWT secret 均為每專案獨立 |
| realtime | ❌ 不可 | WebSocket 連線狀態、DB CDC 監聽為每專案獨立 |
| storage | ❌ 不可 | 檔案儲存與 metadata 為每專案獨立 |
| functions | ❌ 不可 | 函式碼、JWT secret、DB 連線為每專案獨立 |
| meta | ❌ 不可 | 1:1 綁定 DB，無法同時 introspect 多個 DB |
| supavisor | ❌ 不可 | 連線池綁定特定 DB，multi-tenant 設定複雜 |
| analytics | ⚠️ 有條件 | 有 multi-tenant 能力但增加複雜度，不建議 |
| kong | ⚠️ 有條件 | 可透過 path-prefix 或 Lua plugin 共用，但需開發 |
| studio | ❌ 不可 | 1:1 綁定 meta + kong，無 multi-project 能力 |
| imgproxy | ⚠️ 有條件 | Stateless 可共用，但需掛載所有專案 storage volume |
| vector | ✅ 可共用 | 從 Docker socket 收集所有容器日誌 |

### 關鍵發現

1. **JWT_SECRET 是最強的隔離邊界** — 8 個服務共用同一個 JWT secret，任何共用方案都必須解決 JWT 隔離問題。

2. **DB 是核心錨點** — 除 kong、imgproxy、vector 外，所有服務都直接連接 PostgreSQL。DB 隔離決定了大部分服務的隔離邊界。

3. **真正可共用的只有 vector** — 其他「有條件可共用」的服務，共用帶來的複雜度可能超過資源節省的效益。

4. **每個專案最少需要 10 個獨立容器** — db, auth, rest, realtime, storage, imgproxy, meta, functions, analytics, supavisor。加上 kong 和 studio 則為 12 個。

5. **記憶體是主要瓶頸** — 每個完整專案約 0.7–1.6 GB，若同時運行 5 個專案則需 3.5–8 GB RAM。Elixir/BEAM VM 服務（realtime, analytics, supavisor）是主要消耗者。

---

## 變更記錄

| 日期 | 變更內容 | 原因 |
|------|---------|------|
| 2026-03-22 | 初始建立 | Phase 0 功能 1：Supabase 架構分析 |
