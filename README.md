# mySupabaseControl

> 在本機以 Docker 自架並管理多個獨立 Supabase 實例的工作空間，搭配 **`sbctl`** Control Plane CLI 提供完整的專案生命週期管理，並支援 MCP Server 模式供 AI agent 整合。

## 目錄

- [架構概覽](#架構概覽)
- [快速開始](#快速開始)
- [just 指令（傳統操作）](#just-指令傳統操作)
- [sbctl — Control Plane CLI](#sbctl--control-plane-cli)
- [MCP Server（AI Agent 整合）](#mcp-serverai-agent-整合)
- [專案結構](#專案結構)
- [開發路線圖](#開發路線圖)

---

## 架構概覽

```
CLI (sbctl)          MCP Server           Telegram Bot
本機終端操作          AI agent 工具         手機遠端控制（Phase 4）
     │               (stdio)                    │
     └───────────────────┬───────────────────────┘
                         │
           ┌─────────────▼─────────────┐
           │       Use-case 層          │
           │  create / start / stop /  │
           │  reset / delete / list    │
           └──────┬────────────────────┘
                  │
     ┌────────────▼─────────────────────────┐
     │           Control Plane              │
     │  ProjectRegistry + ConfigSchema      │
     │  ┌─────────────────────────────────┐ │
     │  │     Runtime Adapter 介面         │ │
     │  └──────┬─────────────┬────────────┘ │
     │         │             │              │
     │  Docker Compose    K8s Adapter       │
     │  Adapter（現在）    （Phase 6）        │
     └──────────────────────────────────────┘
```

詳細架構說明請參閱 [`docs/CONTROL_PLANE.md`](./docs/CONTROL_PLANE.md)。

---

## 快速開始

### 前置需求

- Docker（已安裝且 daemon 執行中）
- Go 1.22+
- [`just`](https://github.com/casey/just)

```bash
# macOS/Linux
brew install just

# Windows
winget install --id Casey.Just --exact
```

### 安裝 sbctl（建議方式）

```bash
# 一鍵設定：啟動 Control Plane DB、套用 migration、建置 binary
just cp-setup
```

完成後依照輸出設定環境變數，或建立 `.sbctl.env`（程式啟動時自動載入）：

```bash
SBCTL_DB_URL=postgresql://postgres:sbctl_secret@localhost:5433/sbctl
SBCTL_PROJECTS_DIR=./projects
```

---

## just 指令（傳統操作）

`justfile` 提供跨平台（macOS/Linux/Windows）的薄層操作介面，自動分派到對應的 shell 腳本。

```bash
# 建立新專案（手動指定 ports）
just new-project project-a 28081 5432 6543
just new-project project-b 38081 15432 16543

# 啟動 / 停止 / 狀態
just up project-a
just down project-a
just ps project-a

# 重置（清除資料並重建 volumes）
just reset project-a

# Control Plane 相關
just cp-setup               # 一鍵安裝（建議）
just cp-build               # 只重新建置 binary
just cp-test                # 執行單元測試
just cp-test-integration    # 執行整合測試（需 DB）
just cp-lint                # 執行 golangci-lint

# 列出所有指令
just --list
```

### Port 對照

| 專案 | Studio / Kong | Postgres | Pooler |
|------|--------------|----------|--------|
| project-a | `28081` | `5432` | `6543` |
| project-b | `38081` | `15432` | `16543` |

`KONG_HTTP_PORT`、`POSTGRES_PORT`、`POOLER_PROXY_PORT_TRANSACTION` 均定義於各專案的 `projects/<slug>/.env`。

---

## sbctl — Control Plane CLI

`sbctl`（**S**upa**b**ase **C**on**t**ro**l**）是 Control Plane 的 Go CLI，自動管理 port 分配、secret 產生與完整的容器生命週期。

### 基本用法

```bash
# 建立並啟動新專案（自動分配 ports 與 secrets）
# 建立後自動顯示 Studio URL、API URL、Postgres DSN、API Keys
./sbctl project create my-project --display-name "My Project"

# 列出所有專案（狀態欄位以顏色標示：running=綠、stopped=灰、error=紅）
./sbctl project list

# 查詢專案詳情（URLs、狀態、健康度）
./sbctl project get my-project

# 持續輪詢狀態（--watch 模式）
./sbctl project get my-project --watch
./sbctl project list --watch --watch-interval 3s --watch-timeout 5m

# 查看連線 credentials（含未遮罩的 API keys）
./sbctl project credentials my-project

# 停止 / 啟動 / 重置 / 刪除
./sbctl project stop my-project
./sbctl project start my-project
./sbctl project reset my-project
./sbctl project delete my-project

# 所有專案狀態總覽
./sbctl status
./sbctl status --watch
```

### 輸出格式

```bash
# Table（預設，適合終端機；含 ANSI 色彩）
./sbctl project list

# 關閉色彩（管線或不支援 ANSI 的終端）
./sbctl project list --no-color
# 或設定環境變數：export NO_COLOR=1

# JSON（適合腳本與 AI agent 解析）
./sbctl -o json project list

# YAML
./sbctl -o yaml project get my-project
```

### Shell Completion

```bash
# 產生補全腳本
./sbctl completion bash   # Bash
./sbctl completion zsh    # Zsh
./sbctl completion fish   # Fish

# 安裝（以 zsh 為例）
./sbctl completion zsh > ~/.zsh/completions/_sbctl
```

### 進階安裝選項

```bash
# 自訂 DB port 與密碼
just cp-setup --db-port 5434 --db-password mypassword

# 只重建 binary（不重建 DB）
just cp-setup --no-build

# 重置 DB（清除所有資料並重建）
just cp-setup --reset-db
```

---

## MCP Server（AI Agent 整合）

`sbctl mcp serve` 以 stdio transport 啟動 MCP Server，讓 GitHub Copilot、Claude Desktop、Cursor 等 AI 工具直接管理 Supabase 專案。

```bash
./sbctl mcp serve
```

### 可用 MCP Tools

| Tool | 說明 |
|------|------|
| `list_projects` | 列出所有專案及其狀態 |
| `get_project` | 取得單一專案詳情（credentials、URLs、健康狀態） |
| `create_project` | 建立新專案（自動 port 分配、secret 產生） |
| `start_project` | 啟動專案服務 |
| `stop_project` | 停止專案服務 |
| `reset_project` | 重置專案（清除資料） |
| `delete_project` | 刪除專案 |

### 整合範例（Claude Desktop / Cursor）

在 MCP 設定檔中加入：

```json
{
  "mcpServers": {
    "sbctl": {
      "command": "/path/to/sbctl",
      "args": ["mcp", "serve"],
      "env": {
        "SBCTL_DB_URL": "postgresql://postgres:sbctl_secret@localhost:5433/sbctl",
        "SBCTL_PROJECTS_DIR": "/path/to/projects"
      }
    }
  }
}
```

---

## 專案結構

```
mySupabaseControl/
├── control-plane/          # Go CLI + MCP Server（sbctl binary）
│   ├── cmd/sbctl/          # Cobra CLI entry point
│   ├── internal/
│   │   ├── adapter/compose/  # Docker Compose RuntimeAdapter 實作
│   │   ├── domain/           # 領域模型、介面定義
│   │   ├── store/postgres/   # PostgreSQL state store
│   │   └── usecase/          # Use-case 層（業務邏輯）
│   └── migrations/           # DB migration SQL
├── docs/                   # 架構文件與設計文件
│   ├── CONTROL_PLANE.md    # 架構與路線圖
│   ├── REVIEW_GATEWAY.md   # 設計審查流程
│   └── designs/            # 各 Phase 設計文件
├── scripts/                # 安裝腳本（setup-control-plane.sh 等）
├── projects/               # 各專案 .env 與 volumes（git 忽略）
├── docker-compose.yml      # Supabase 基底 stack
├── justfile                # 跨平台指令入口
└── sbctl                   # 已建置的 binary（git 忽略）
```

---

## 開發路線圖

| Phase | 說明 | 狀態 |
|-------|------|------|
| **0** | High-Level Design Discussion | ✅ 完成 |
| **1** | 定義 Runtime 無關的 Control Plane 模型 | ✅ 完成 |
| **2** | Docker Compose Runtime Adapter | ✅ 完成 |
| **3** | Use-case 層、sbctl CLI、MCP Server | ✅ 完成 |
| **4** | Telegram Bot 遠端控制 | 🔜 規劃中 |
| **5** | CLI UX 改善與 AI Agent 整合優化 | ✅ 完成 |
| **6** | K8s Runtime Adapter（Mac Mini） | 🔜 規劃中 |

詳細設計文件：[`docs/CONTROL_PLANE.md`](./docs/CONTROL_PLANE.md)

---

> 本設定僅供本機開發使用，非 production 環境強化方案。
