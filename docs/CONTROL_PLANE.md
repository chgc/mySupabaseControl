> **文件語言：繁體中文**
> 本專案所有文件均以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# Control Plane — 架構與開發路線圖

## 問題

目前的 Local Supabase 工作流程，是以複製一個大型基底 `.env` 到 `projects/<project>/.env`，再透過 `just` + shell 腳本操作 Docker Compose 的方式管理各個專案。這個設計存在三個問題：

1. **環境變數管理混亂** — 環境變數在每個專案間重複複製，難以釐清，所有專案共用同一份基底 secrets，建立後幾乎不受管理。
2. **使用門檻高** — 建立新專案需要手動決定 port、直接了解 env 結構，以及手動檢查產生的檔案。
3. **Runtime 綁定問題** — 目前的設計直接綁定 Docker Compose。未來目標 runtime 為 **Mac Mini 上的本地 Kubernetes**，因此架構不可將控制邏輯與 Docker Compose 耦合。

## 現況摘要

| 元件 | 目前做法 | 相關檔案 |
|---|---|---|
| 基底設定 | 69 個變數的 `.env` 範本 | `.env` |
| 專案建立 | 複製基底 `.env` 並附加覆寫值 | `scripts/new-project.{ps1,sh}` |
| Runtime 生命週期 | `docker compose --env-file ... -p ...` | `justfile` |
| 重置 | 刪除 volumes + compose down | `reset.ps1`, `scripts/reset.sh` |
| 隔離方式 | Compose 專案名稱前綴 | `-p supabase-<slug>` |
| Port 分配 | 使用者手動輸入，無衝突偵測 | `new-project` 的 CLI 參數 |
| Secrets | 所有專案共用基底 | 基底 `.env` 複製 |

---

## 提議架構

引入一個 **Control Plane 後端**，作為專案 metadata、設定、secrets 與生命週期編排的唯一真實來源（source of truth）。同時導入 **Runtime Adapter** 抽象層，將控制邏輯與執行 runtime 分離。

### 核心架構圖

```
┌─────────────────────────────────────────────────┐
│                 Control Plane                    │
│                                                  │
│  ┌──────────┐  ┌──────────┐  ┌───────────────┐  │
│  │  專案     │  │  設定     │  │    Secret     │  │
│  │  Registry │  │  Schema  │  │   管理器      │  │
│  └──────────┘  └──────────┘  └───────────────┘  │
│                                                  │
│  ┌──────────────────────────────────────────┐    │
│  │         生命週期編排器                     │    │
│  │  create / up / down / reset / list      │    │
│  └──────────────┬───────────────────────────┘    │
│                 │                                │
│  ┌──────────────▼───────────────────────────┐    │
│  │         Runtime Adapter 介面              │    │
│  └──────┬───────────────────┬───────────────┘    │
│         │                   │                    │
│  ┌──────▼──────┐     ┌──────▼──────┐             │
│  │   Docker    │     │    K8s      │             │
│  │   Compose   │     │  Adapter    │             │
│  │   Adapter   │     │（未來）      │             │
│  └─────────────┘     └─────────────┘             │
└─────────────────────────────────────────────────┘
         │                    │
    .env 渲染              ConfigMap/Secret
    compose up/down        kubectl apply
    bind mounts            PV/PVC
    -p 專案名稱             namespace
```

### 關鍵設計決策

- **Runtime 無關的專案模型** — Control Plane 以專案、設定 schema 與隔離邊界為思考單位，不關心 compose 檔案或 env 檔案。
- **Runtime Adapter 模式** — Docker Compose Adapter 渲染 `.env` 並執行 `docker compose`；未來 K8s Adapter 渲染 ConfigMap/Secret 並執行 `kubectl`/Helm。兩者實作同一個介面。
- **設定 Schema，而非設定檔** — Control Plane 持有一個有型別的設定 schema，由 Runtime Adapter 渲染成各自 runtime 所需的格式。
- **以 Supabase 作為持久化層** — Control Plane 使用自己的 Supabase 實例儲存 metadata，但不管理該實例的基礎設施（透過 `cli/just` 手動 bootstrap，視為基礎設施）。
- **CLI/just 作為後端 adapter** — `cli` 與 `just` 僅可作為 Docker Compose Runtime Adapter 內部的執行機制，不作為使用者介面的入口。

### Docker Compose vs K8s 差異對照

| 關注點 | Docker Compose（現在） | K8s（未來，Mac Mini） |
|---|---|---|
| 設定傳遞 | `.env` 檔案 | ConfigMap + Secret |
| Port 對外 | 主機 port 映射（`-p 28081:8000`） | NodePort / Ingress |
| 專案隔離 | Compose 專案名稱（`-p supabase-<slug>`） | Namespace |
| Volume 持久化 | Bind mount（`./volumes/db/data`） | PV + PVC（local-path 或 hostPath） |
| 健康檢查 | Compose healthcheck | Liveness/readiness probes |
| 服務探索 | Compose 網路內的容器名稱 | K8s Service DNS |
| 部署描述子 | `docker-compose.yml` | Helm chart / Kustomize overlay |

Runtime Adapter 必須抽象化上述所有差異。

---

## 開發路線圖

> 每個 Phase 開始前，須依 `docs/designs/_PHASE_TEMPLATE.md` 產出對應的 Phase Plan 文件，
> 將下方高階描述拆解為具體功能清單與依賴關係。詳見 `docs/REVIEW_GATEWAY.md`「前置階段 — Phase 規劃」。

### Phase 0 — High-Level Design Discussion ✅

> **Phase Plan：** `docs/designs/phase-0-plan.md`
>
> **研究文件：**
> - `docs/designs/supabase-arch-analysis.md` — Supabase 架構分析
> - `docs/designs/shared-component-analysis.md` — 多專案共用元件分析
> - `docs/designs/plan-adjustment.md` — High-Level Plan 調整結論

本階段為研究與討論階段，產出為分析文件而非程式碼。目標是在定義 Control Plane 模型之前，深入理解 Supabase 的架構設計，並探討多專案部署下的元件共用可能性。

**研究結論摘要：**

1. **每專案獨立服務組的架構正確。** 13 個 Supabase 服務中，因 JWT secret 綁定與 DB 直連的強耦合，12 個服務必須每專案獨立部署。
2. **僅 vector（日誌收集）建議共用。** 一個 Docker host 只需一個日誌收集器，技術簡單且無隔離風險。
3. **Kong、imgproxy 技術上可共用，但投入產出比不佳。** 各僅節省 30–50 MB，不值得增加路由複雜度。
4. **Phase 1–5 的 deliverables 不需調整。** 設定 Schema 可在 Phase 1 設計時區分「全域設定」與「每專案設定」。
5. **每個完整專案約需 0.7–1.6 GB RAM、12–13 個容器。**

### Phase 1 — 定義 Runtime 無關的 Control Plane 模型

> **Phase Plan：** `docs/designs/phase_1/phase-1-plan.md`
> **審查狀態：✅ 全部通過（REVIEW_GATEWAY 兩位審查者均 APPROVE）**
>
> | 文件 | 狀態 |
> |------|------|
> | `docs/designs/phase_1/project-model.md` | ✅ approved（四輪） |
> | `docs/designs/phase_1/state-store.md` | ✅ approved（五輪） |
> | `docs/designs/phase_1/config-schema.md` | ✅ approved（六輪） |
> | `docs/designs/phase_1/runtime-adapter.md` | ✅ approved（兩輪） |

- 盤點 `docker-compose.yml` 中所有環境變數並分類：
  - **共用靜態預設值** — 所有專案相同（例如 `POSTGRES_HOST=db`、`SMTP_PORT=2500`）
  - **每專案設定** — 各專案不同（例如 port、slug、URL）
  - **產生的 secrets** — 每個專案應獨立產生（例如 `JWT_SECRET`、`POSTGRES_PASSWORD`、API keys）
  - **使用者可覆寫** — 使用者可自訂的值（例如 `PGRST_DB_MAX_ROWS`、`ENABLE_EMAIL_AUTOCONFIRM`）
- 定義 **有型別的專案模型**（slug、顯示名稱、狀態、時間戳記、健康狀態）— Runtime 無關。
- 定義可渲染至多個目標的 **有型別設定 schema**：
  - 區分**全域設定**（per-host，如 `DOCKER_SOCKET_LOCATION`）與**每專案設定**（如 `JWT_SECRET`、port）— 參見 Phase 0 研究結論
  - `.env` → Docker Compose Adapter
  - ConfigMap + Secret YAML → K8s Adapter（未來）
- 定義 **Runtime Adapter 介面**：
  - `create(project)` — 建立隔離邊界與持久儲存
  - `start(project)` — 部署並啟動所有服務
  - `stop(project)` — 停止服務，保留資料
  - `destroy(project)` — 移除所有資源，包含資料
  - `status(project)` — 查詢所有服務的健康狀態
  - `renderConfig(project) → runtime-specific artifacts`
- 確定 Control Plane 的狀態儲存（以 Supabase 為後端的 DB）。

### Phase 2 — 實作 Docker Compose Runtime Adapter

> **Phase Plan：** `docs/designs/phase-2-plan.md`（待建立）

- 以 Docker Compose 作為底層 runtime 實作 adapter 介面。
- `renderConfig` → 從設定 schema + 專案模型產生 `projects/<slug>/.env`。
- `create` → slug 正規化、自動 port 分配（掃描可用 port）、目錄建立、per-project secret 產生、env 渲染。
- `start` / `stop` → `docker compose --env-file ... -p ... up/down`。
- `status` → `docker compose ps` + 健康檢查解析。
- `destroy` → `compose down -v` + volume 清理。
- 保留 `justfile` 作為薄層的操作快捷方式，委派給 Control Plane。
- 現有 PS1/Bash 腳本成為參考實作後棄用。

### Phase 3 — CLI (`sbctl`) 與 MCP Server

> **Phase Plan：** `docs/designs/phase-3-plan.md`（待建立）

本 Phase 提供兩種操作介面，**無 HTTP Server、無 Web UI**：

**1. CLI — `sbctl`（基於 Cobra，支援 `--output json`）**

以 `sbctl` 作為 CLI 二進位名稱（Supabase Control），直接呼叫 use-case 層執行操作，不經過 HTTP。支援以下子命令：

| 命令 | 說明 |
|------|------|
| `sbctl project create <name>` | 建立專案（port 自動分配）|
| `sbctl project list` | 列出所有專案 |
| `sbctl project get <slug>` | 查詢專案詳情（URLs、credentials、狀態） |
| `sbctl project start <slug>` | 啟動 |
| `sbctl project stop <slug>` | 停止 |
| `sbctl project reset <slug>` | 重置 |
| `sbctl project delete <slug>` | 刪除 |

- 全域旗標 `--output [table|json|yaml]`（預設 `table`）。
- JSON/YAML 輸出適合 AI agent 透過 subprocess 呼叫並解析結果。
- 人類可讀的 table 輸出適合終端機操作。

**2. MCP Server（Model Context Protocol）**

將所有 Control Plane 操作以 [MCP](https://modelcontextprotocol.io/) tools 形式暴露，讓 AI agent（如 GitHub Copilot、Claude Desktop）可直接呼叫：

| MCP Tool | 對應動作 |
|----------|---------|
| `list_projects` | 列出所有 Supabase 專案及其狀態 |
| `get_project` | 取得單一專案詳情（credentials、URLs、健康狀態） |
| `create_project` | 建立新專案（自動 port 分配、secret 產生） |
| `start_project` | 啟動專案服務 |
| `stop_project` | 停止專案服務 |
| `reset_project` | 重置專案（清除資料） |
| `delete_project` | 刪除專案 |

- 以 `sbctl mcp serve` 啟動，使用 stdio transport（標準 MCP 整合方式）。
- MCP Server 直接呼叫 use-case 層，**不透過 HTTP**。
- 所有 tool 回傳結構化 JSON，不含 ANSI 色碼。
- 可整合至 Copilot CLI、Claude Desktop、Cursor 等 MCP-compatible AI 工具。

### Phase 4 — 改善 CLI 使用體驗與 AI Agent 整合

> **Phase Plan：** `docs/designs/phase-4-plan.md`（待建立）

- Shell completion（`sbctl completion bash/zsh/fish`）。
- 建立專案後顯示完整連線資訊（Studio URL、API URL、Postgres DSN、Anon/Service role key）。
- `sbctl project list` 表格含彩色狀態欄位（running=綠、stopped=灰、error=紅）。
- `--watch` 旗標：`sbctl project get <slug> --watch` 輪詢狀態直到 running/error。
- MCP tool 說明文字（description）精細化，讓 AI agent 能更準確選擇工具。
- 全專案狀態彙整總覽（`sbctl status`）。

### Phase 5 — K8s Runtime Adapter（未來，Mac Mini）

> **Phase Plan：** `docs/designs/phase-5-plan.md`（待建立）

- 以相同 adapter 介面在本地 K8s（預計使用 k3s）上實作。
- `renderConfig` → ConfigMap + Secret YAML（或 Helm values）。
- `create` → 建立 K8s namespace + PVC + Helm/Kustomize 部署。
- `start` / `stop` → 調整 Deployment replica 數量。
- `status` → 透過 K8s API 查詢 Pod 健康狀態。
- Port 對外透過 NodePort 或 Ingress。
- 持久儲存透過 local-path-provisioner 或 hostPath PV。
- Control Plane 後端本身**不需要**運行在 K8s 內部，而是從外部管理 K8s 專案。

---

## 技術決策

| 關注點 | 選擇 | 理由 |
|---|---|---|
| **語言** | Go | 單一執行檔、啟動快速、無 runtime 依賴，適合 infra 工具開發 |
| **CLI 框架** | Cobra (`github.com/spf13/cobra`) | Go CLI 事實標準；支援 subcommand、全域旗標、shell completion |
| **MCP 框架** | `github.com/mark3labs/mcp-go` | Go 語言 MCP SDK；支援 stdio transport，與 Copilot CLI / Claude Desktop 整合 |
| **無 HTTP Server** | CLI + MCP 直接呼叫 use-case 層 | 去掉 HTTP roundtrip，架構更簡單；無 Gin 依賴 |
| **無前端** | CLI + MCP 取代 Web UI | 目標使用者為開發者與 AI agent，不需要 Web 介面 |
| **狀態儲存** | Supabase | 自己的專案就用自己的產品（dogfooding） |
| **Repo 結構** | Monorepo | `control-plane/`（Go CLI + MCP）、`scripts/`（現有）、`docker-compose.yml` |
| **第一版 Runtime Adapter** | Docker Compose | Phase 1–4 目標；K8s Adapter 延後至 Phase 5 |
| **Control Plane 的 bootstrap** | 透過 `cli/just` 手動建立 | 簡單清楚；Control Plane 不自管自己的基礎設施 |

### Monorepo 目錄結構（建議）

```
localsupabase/
├── control-plane/       # Go CLI + MCP Server（無 HTTP server）
│   ├── cmd/
│   │   └── sbctl/       # CLI + MCP Server 二進位（sbctl）
│   ├── internal/
│   │   ├── cli/         # Cobra command 定義
│   │   ├── mcp/         # MCP tool 定義（mark3labs/mcp-go）
│   │   ├── usecase/     # Use-case 層（共用於 CLI 與 MCP）
│   │   ├── domain/      # 專案模型、設定 schema
│   │   ├── adapter/     # Runtime Adapter 介面與實作
│   │   │   ├── compose/ # Docker Compose Adapter
│   │   │   └── k8s/     # K8s Adapter（Phase 5）
│   │   └── store/       # Supabase 持久化層
│   └── go.mod
├── docs/                # 專案文件
│   ├── designs/         # 各功能設計文件
│   └── ...
├── scripts/             # 現有 PS1 + Bash（參考用，將棄用）
├── projects/            # 各專案 .env + volumes（由 Compose Adapter 渲染）
├── docker-compose.yml   # Supabase 服務定義（不變動）
├── justfile             # 薄層操作快捷，委派給 Control Plane
└── .env                 # 基底預設值（供 Control Plane 設定 schema 使用）
```

---

## 注意事項

- **Runtime Adapter 是核心抽象層。** 沒有它，K8s 遷移將需要重寫整個 Control Plane；有了它，K8s 只是另一個 adapter 實作。
- **設定 Schema ≠ 設定檔。** `.env` 是渲染目標，而非模型。相同的 schema 未來可渲染為 K8s ConfigMap/Secret。
- **服務內部 URL 必須參數化。** 服務間以 hostname 互相呼叫（例如 Compose 中的 `http://meta:28080` vs K8s 中的 `http://meta.{slug}.svc.cluster.local`），設定 schema 必須能表達這個差異。
- **Bootstrap 邊界。** Control Plane 自己使用的 Supabase 實例屬於基礎設施，透過 `cli/just` 手動建立，不由 Control Plane 自行管理。
- **單一實作語言。** 採用 Go 作為後端消除目前 PowerShell/Bash 雙軌的維護負擔；腳本僅作為棄用過渡期的墊片保留。
- **Mac Mini K8s 建議使用 k3s。** 輕量、單節點、目標使用標準 K8s API 以確保可攜性。
