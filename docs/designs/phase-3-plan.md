> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# Phase Plan：Phase 3 — Use-case 層、CLI (`sbctl`) 與 MCP Server

## 來源

`docs/CONTROL_PLANE.md` — Phase 3

---

## 目標

Phase 3 完成後，Control Plane 應能透過兩種操作介面管理完整的 Supabase 專案生命週期：

1. **`sbctl` CLI** — 終端機操作，支援 table / json / yaml 輸出格式
2. **`sbctl mcp serve`** — AI agent 透過 MCP (Model Context Protocol) stdio transport 呼叫

業務邏輯集中於 **Use-case 層**（`internal/usecase/`），CLI 與 MCP Server 均直接呼叫此層，不重複實作邏輯。

---

## 進入條件

- ✅ Phase 2 完成：
  - `ComposePortAllocator` 已實作並測試通過
  - `ComposeEnvRenderer` 已實作並測試通過
  - `ComposeAdapter`（`RuntimeAdapter` 實作）已實作並測試通過
  - `ProjectRepository`、`ConfigRepository`（store/postgres）已實作並測試通過
  - `internal/domain` 所有型別與介面已定義

---

## 功能拆解

| # | 功能名稱 | 設計文件路徑 | 狀態 | 說明 |
|---|----------|-------------|------|------|
| 1 | Use-case 層 | `docs/designs/phase_3/usecase-layer.md` | 未開始 | `internal/usecase/` — `ProjectService`，聚合 domain + store + adapter |
| 2 | CLI (`sbctl`) | `docs/designs/phase_3/cli-sbctl.md` | 未開始 | `cmd/sbctl/` — Cobra CLI，7 個 project 子命令，`--output table/json/yaml` |
| 3 | MCP Server | `docs/designs/phase_3/mcp-server.md` | 未開始 | `sbctl mcp serve` — stdio transport，暴露 7 個 MCP tools |

---

## 依賴關係

```
功能 1：usecase-layer（依賴 domain + store + adapter 介面）
  ↓
功能 2：cli-sbctl（依賴功能 1 的 ProjectService 介面）
功能 3：mcp-server（依賴功能 1 的 ProjectService 介面）
```

| 功能 | 依賴於 | 原因 |
|------|--------|------|
| 功能 2 | 功能 1 | CLI 直接呼叫 `ProjectService` 方法 |
| 功能 3 | 功能 1 | MCP tools 直接呼叫 `ProjectService` 方法 |

功能 2 與功能 3 之間**無依賴關係**，可平行進行設計、審查與實作。

---

## 建議實作順序

1. **功能 1（usecase-layer）** — 設計 → 審查（2 個 subagent）→ 實作
2. **功能 2（cli-sbctl）** 與 **功能 3（mcp-server）** — 功能 1 審查通過後，平行進行設計與審查
3. **功能 2 + 3 實作** — 各自設計審查通過後並行實作

---

## 退出標準

### 設計階段

- [ ] 所有功能的設計文件狀態為 `approved`（通過兩位 reviewer 審查）
  - [ ] `usecase-layer.md` — approved
  - [ ] `cli-sbctl.md` — approved
  - [ ] `mcp-server.md` — approved

### 實作階段

| 任務 ID | 說明 | 狀態 |
|---------|------|------|
| `impl-usecase` | 實作 `internal/usecase/` Use-case 層 | 未開始 |
| `impl-cli` | 實作 `cmd/sbctl/` CLI | 未開始 |
| `impl-mcp` | 實作 MCP Server（`sbctl mcp serve`） | 未開始 |

### Phase 整合驗證

```bash
cd control-plane

# 編譯
go build ./...

# 靜態分析
go vet ./...

# 單元測試（含 race）
go test -race ./...

# Smoke test — CLI
go run ./cmd/sbctl project create my-project
go run ./cmd/sbctl project list
go run ./cmd/sbctl project get my-project
go run ./cmd/sbctl project start my-project
go run ./cmd/sbctl project stop my-project
go run ./cmd/sbctl project delete my-project

# Smoke test — MCP Server 啟動
go run ./cmd/sbctl mcp serve --help
```

---

## 風險與待決事項

| 風險 / 待決事項 | 影響範圍 | 處理方式 |
|----------------|---------|---------|
| MCP SDK 選擇 — Go 生態系尚無官方 SDK | mcp-server | 設計文件評估 `github.com/mark3labs/mcp-go` 等第三方 SDK，或手動實作 JSON-RPC |
| Use-case 層的錯誤轉換策略 — domain/store/adapter 錯誤如何對應到 CLI/MCP 輸出 | usecase-layer | 設計文件中定義統一的 `UsecaseError` 結構 |
| `project reset` 語意定義 — 僅清資料？還是 down + 刪目錄 + re-create？ | usecase-layer, cli-sbctl | 設計文件中明確定義 reset 流程 |
| Cobra + `--output` 的 table renderer 選擇 | cli-sbctl | 設計文件評估 `github.com/olekukonko/tablewriter` 或 `text/tabwriter` |

---

## 變更記錄

| 日期 | 變更內容 | 原因 |
|------|---------|------|
| 2026-03-25 | 初始建立 | Phase 3 規劃，拆解為 3 個功能 |
