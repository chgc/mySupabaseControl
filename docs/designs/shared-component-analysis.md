> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# 多專案共用元件分析

## 來源

`docs/designs/phase-0-plan.md` — 功能 2

前置依賴：`docs/designs/supabase-arch-analysis.md`（功能 1）

---

## 分析目標

基於架構分析的結果，逐一評估每個服務在多專案部署下的共用可行性，並給出最終建議。

---

## 評估標準

每個服務依以下四個面向評估：

| 面向 | 說明 |
|------|------|
| **技術可行性** | 該服務是否具備 multi-tenant 或 multi-target 能力？ |
| **隔離安全性** | 共用後，專案間的資料/設定是否仍能安全隔離？ |
| **複雜度成本** | 實作共用方案需要多少額外開發/維護工作？ |
| **資源節省幅度** | 共用後能節省多少記憶體/CPU？值得嗎？ |

---

## 逐服務評估

### ❌ 不可共用的服務（8 個）

以下服務因 JWT secret 綁定、DB 直連或專案專屬資料，**無法安全共用**：

#### db（PostgreSQL）

- **技術可行性：** 理論上可用 schema-per-project 隔離，但 Supabase 的 init SQL（roles、RLS、jwt settings）假設單一租戶。
- **隔離安全性：** 極高風險。`app.settings.jwt_secret` 是 DB-level 設定，無法 per-schema 區分。
- **結論：** ❌ 不可共用。DB 是 Supabase 的核心錨點，所有服務的隔離都建立在 DB 隔離之上。

#### auth（GoTrue）

- **技術可行性：** GoTrue 不支援 multi-tenant。`GOTRUE_JWT_SECRET` 為單值設定。
- **隔離安全性：** 使用者資料、session、OAuth state 均為專案專屬。
- **結論：** ❌ 不可共用。

#### rest（PostgREST）

- **技術可行性：** PostgREST 綁定單一 `PGRST_DB_URI`，不支援動態切換 DB。
- **隔離安全性：** API 自動從 DB schema 產生，不同專案 schema 不同。
- **結論：** ❌ 不可共用。

#### realtime

- **技術可行性：** Realtime 有內建 tenant 機制（解析 subdomain），但 self-hosted 模式預設 single-tenant（`SEED_SELF_HOST: true`）。
- **隔離安全性：** WebSocket 連線狀態、Postgres CDC 監聽為專案專屬。
- **複雜度成本：** 若啟用 multi-tenant 模式，需要修改 tenant 註冊流程、Kong 路由。
- **結論：** ❌ 不建議共用。雖有 multi-tenant 基礎，但 self-hosted 場景下的複雜度不值得。

#### storage

- **技術可行性：** Storage API 綁定單一 DB 與單一檔案系統目錄。
- **隔離安全性：** 檔案與 metadata 為專案專屬。`TENANT_ID` 為單值。
- **結論：** ❌ 不可共用。

#### functions（Edge Runtime）

- **技術可行性：** Edge Runtime 從本地 volume 載入函式碼，不支援 multi-project 函式隔離。
- **隔離安全性：** 函式碼為專案專屬，且函式透過 `SUPABASE_DB_URL` 直連專案 DB。
- **結論：** ❌ 不可共用。

#### meta（postgres-meta）

- **技術可行性：** 1:1 綁定 PostgreSQL，無 multi-DB 支援。
- **隔離安全性：** DB introspection 結果為專案專屬。
- **結論：** ❌ 不可共用。

#### supavisor

- **技術可行性：** 有 multi-tenant 能力（`POOLER_TENANT_ID`），理論上可路由到多個 DB。
- **隔離安全性：** 連線池隔離可行，但需要額外的 tenant 註冊邏輯。
- **複雜度成本：** 高。需要動態 tenant 管理、per-tenant pool 設定。
- **資源節省幅度：** 低（supavisor 約 50–100 MB，Elixir VM 共用不會等比節省）。
- **結論：** ❌ 不建議共用。複雜度過高，資源節省有限。

---

### ⚠️ 有條件可共用的服務（3 個）

#### kong — API Gateway

| 面向 | 評估 |
|------|------|
| 技術可行性 | ✅ 可行。透過 path-prefix routing 或 custom Lua plugin 路由到不同專案後端 |
| 隔離安全性 | ✅ 可行。API key 驗證仍為每專案獨立（不同 `ANON_KEY`/`SERVICE_ROLE_KEY`） |
| 複雜度成本 | ⚠️ 中等。靜態 YAML 方案需模板化 kong.yml；Lua plugin 方案需開發 |
| 資源節省 | 低（kong 僅 30–50 MB） |

**共用方案：**

方案 A（靜態 YAML 模板化）：
```yaml
# 每個專案產生一組路由
services:
  - name: rest-v1-project-a
    url: http://rest-project-a:3000/
    routes:
      - paths: ["/project-a/rest/v1/"]
        strip_path: true
```
- 優點：簡單，無需 Lua 開發
- 缺點：kong.yml 線性增長，每次新增/刪除專案需重啟 Kong

方案 B（Custom Lua Plugin）：
- 從 request path/header 解析 project identifier
- 動態構建 upstream URL
- 優點：可擴展，無需重啟
- 缺點：需要 Lua 開發與維護

**結論：** ⚠️ **技術可行但不建議在 Phase 1-2 投入。** 資源節省僅 30–50 MB，不值得增加路由複雜度。建議每專案獨立 Kong，未來再評估共用。

---

#### imgproxy — 圖片轉換

| 面向 | 評估 |
|------|------|
| 技術可行性 | ✅ 可行。Stateless 服務，無專案專屬設定 |
| 隔離安全性 | ✅ 安全。imgproxy 只做圖片轉換，不涉及認證或 DB |
| 複雜度成本 | 低。只需掛載所有專案的 storage volume |
| 資源節省 | 低（imgproxy 僅 30–50 MB） |

**共用方案：**
```yaml
imgproxy:
  volumes:
    - ./projects/project-a/volumes/storage:/storage/project-a
    - ./projects/project-b/volumes/storage:/storage/project-b
```
- 各專案的 storage 服務設定 `IMGPROXY_URL` 指向共用 imgproxy
- imgproxy 的 `IMGPROXY_LOCAL_FILESYSTEM_ROOT: /` 可存取所有路徑

**結論：** ⚠️ **技術最簡單，但資源節省極低。** 若在意簡潔性，可維持每專案獨立；若想節省少量記憶體，可共用。**建議視實際專案數量決定。**

---

#### vector — 日誌收集

| 面向 | 評估 |
|------|------|
| 技術可行性 | ✅ 完全可行。Vector 透過 Docker socket 收集所有容器日誌 |
| 隔離安全性 | ✅ 安全。日誌收集為唯讀，不影響專案運行 |
| 複雜度成本 | 低。修改 vector.yml 路由規則，依 container label 分流到不同 Logflare |
| 資源節省 | 低（vector 僅 20–40 MB），但避免重複收集 |

**共用方案：**
- 單一 Vector 實例掛載 Docker socket
- 在 vector.yml 中依容器名稱/label 篩選，路由到對應專案的 analytics 實例
- 或統一發送到一個 analytics 實例（若 analytics 也共用）

**結論：** ✅ **建議共用。** 技術簡單、無隔離風險、且邏輯上一個 Docker host 只需一個日誌收集器。

---

### ✅ 可安全共用的服務（1 個）

| 服務 | 預估節省（per project） | 複雜度 |
|------|----------------------|--------|
| vector | 20–40 MB | 低 |

---

## 資源消耗對比

### 方案 A：完全獨立部署（現行方案）

每個專案 13 個容器，約 0.7–1.6 GB RAM。

| 專案數 | 容器數 | 預估 RAM |
|--------|-------- |---------|
| 1 | 13 | 0.7–1.6 GB |
| 3 | 39 | 2.1–4.8 GB |
| 5 | 65 | 3.5–8.0 GB |
| 10 | 130 | 7.0–16.0 GB |

### 方案 B：共用 vector（建議方案）

每個專案 12 個容器 + 1 個共用 vector。

| 專案數 | 容器數 | 預估 RAM | vs 方案 A 節省 |
|--------|--------|---------|--------------|
| 1 | 13 | 0.7–1.6 GB | 0 |
| 3 | 37 | 2.1–4.7 GB | ~60 MB |
| 5 | 61 | 3.4–7.9 GB | ~120 MB |
| 10 | 121 | 6.8–15.6 GB | ~300 MB |

### 方案 C：共用 vector + imgproxy + kong（激進方案）

每個專案 10 個容器 + 3 個共用服務。

| 專案數 | 容器數 | 預估 RAM | vs 方案 A 節省 |
|--------|--------|---------|--------------|
| 1 | 13 | 0.7–1.6 GB | 0 |
| 3 | 33 | 1.9–4.5 GB | ~300 MB |
| 5 | 53 | 3.1–7.3 GB | ~600 MB |
| 10 | 103 | 5.8–14.0 GB | ~1.5 GB |

---

## 最終建議

### 建議採用方案 B（僅共用 vector）

**理由：**

1. **最小複雜度** — vector 的共用幾乎不需要額外開發，只需調整 vector.yml 路由規則。
2. **合理的架構** — 一個 Docker host 上只需一個日誌收集器，這本身就是合理的架構決策。
3. **Kong 共用不值得** — 節省 30–50 MB 但需要開發模板化 kong.yml 或 Lua plugin，投入產出比不佳。
4. **imgproxy 共用可選** — 技術簡單但節省極低，可延後決定。
5. **Control Plane 架構不需大幅調整** — 每專案仍然是一組獨立的 Supabase 服務，Runtime Adapter 的職責清晰。

### 對現有 High-Level Plan 的影響

**影響極小。** 現有的架構設計假設「每個專案是一組獨立服務」基本正確。

需要微調的部分：
- **Runtime Adapter 介面** 可加入「全域服務管理」的概念（如共用 vector 的生命週期管理）
- **設定 Schema** 可區分「per-project config」與「global config」
- Phase 1–5 的 deliverables **不需要調整**

---

## 變更記錄

| 日期 | 變更內容 | 原因 |
|------|---------|------|
| 2026-03-22 | 初始建立 | Phase 0 功能 2：多專案共用元件分析 |
