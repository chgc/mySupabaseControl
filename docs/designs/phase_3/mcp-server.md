> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# 設計文件：MCP Server（`sbctl mcp serve`）

## 狀態

approved

## Phase

- **Phase：** Phase 3
- **Phase Plan：** `docs/designs/phase-3-plan.md`

---

## 目的

將所有 Control Plane 操作以 [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) tools 形式暴露，
讓 AI agent（GitHub Copilot、Claude Desktop、Cursor 等）可直接管理 Supabase 專案。

MCP Server 以 `sbctl mcp serve` 啟動，使用 **stdio transport**（標準 MCP 整合方式）。
直接呼叫 `usecase.ProjectService`，不透過 HTTP。

---

## 範圍

### 包含

- `sbctl mcp serve` 子命令（cobra subcommand，作為 sbctl binary 的一部分）
- 7 個 MCP tools（對應 ProjectService 的 7 個方法）
- stdio transport（stdin/stdout JSON-RPC）
- 結構化 JSON 回傳（無 ANSI 色碼）
- MCP SDK 選擇與整合
- 依賴注入（與 CLI 共用 `cmd/sbctl/deps.go` 的 `BuildDeps()`）
- OS signal 處理（SIGTERM/SIGINT → graceful shutdown）

### 不包含

- HTTP/SSE transport（Phase 3 僅 stdio）
- MCP Resources / Prompts（Phase 3 僅 Tools）
- 認證（MCP stdio 信任 host process）
- Telegram Bot（Phase 4）

---

## 依賴變更

```
需新增至 go.mod（go get）：
  github.com/mark3labs/mcp-go v0.32.0（或最新穩定版，需在 go get 時確認）
  github.com/spf13/cobra      v1.9.x（與 CLI 共用，一起新增）
```

**注意**：mcp-go 目前為 v0.x（pre-1.0），API 仍可能有 breaking change。採用時應釘定版本，升級前需確認 changelog。

---

## 目錄結構

```
control-plane/
  cmd/
    sbctl/
      main.go         ← rootCmd 入口
      deps.go         ← BuildDeps()（CLI 與 MCP 共用）
      project/        ← project 子命令群組（CLI）
      mcp/            ← MCP tool handlers
        handlers.go   ← 所有 tool handler 函數
        server.go     ← 建立並啟動 MCP server
```

`cmd/server/`（現有空目錄）保留不動，與 `cmd/sbctl/` 並存，為獨立功能（若無用途可後續清理）。

---

## 共用 Wiring（deps.go）

MCP server 與 CLI 共用 `cmd/sbctl/deps.go` 的 `BuildDeps()`，詳見 CLI 設計文件。

```go
type Deps struct {
    ProjectService usecase.ProjectService
}

func BuildDeps(ctx context.Context, dbURL, projectsDir string) (*Deps, error) { ... }
```

`sbctl mcp serve` 在 RunE 中呼叫 `BuildDeps()`，不透過 `PersistentPreRunE`（MCP server 命令無需 `--output` 旗標）。

---

## SDK 選擇

### 評估

| 選項 | 版本狀態 | 優點 | 缺點 |
|------|---------|------|------|
| `github.com/mark3labs/mcp-go` | v0.x（活躍） | 最活躍的 Go MCP SDK、API 簡潔、stdio 支援 | pre-1.0，需釘定版本 |
| `github.com/metoro-io/mcp-golang` | v0.x（較不活躍） | 另一個選項 | 活躍度較低 |
| 手動實作 JSON-RPC | — | 無外部依賴 | 工作量大，需自行維護 MCP spec |

**決策：使用 `github.com/mark3labs/mcp-go`**

理由：Go 生態中最活躍的 MCP 實作，避免重複造輪。釘定版本以防 API 破壞性變更。

---

## 資料模型

### Tool Input Schema

每個 tool 的 input 為 JSON object。`Required` 欄位以 `mcp.Required()` 標記。

| Tool | Input Fields | Required |
|------|-------------|---------|
| `list_projects` | — | — |
| `get_project` | `slug: string` | slug |
| `create_project` | `slug: string`, `display_name: string` | slug, display_name |
| `start_project` | `slug: string` | slug |
| `stop_project` | `slug: string` | slug |
| `reset_project` | `slug: string` | slug |
| `delete_project` | `slug: string` | slug |

### Tool Output

**成功**：`mcp.NewToolResultText(jsonStr), nil`
- `get_project` / mutating tools：JSON 序列化的 `usecase.ProjectView`
- `list_projects`：JSON array（`[]*usecase.ProjectView`），直接 array 不包 wrapper object

**失敗**：`mcp.NewToolResultError("message"), nil`（`isError: true`）
- 業務邏輯錯誤（UsecaseError）一律使用此形式

**框架錯誤**：`nil, err`
- 僅用於無法回傳 MCP response 的框架層異常（幾乎不用）
- **業務邏輯錯誤絕對不使用此形式**，避免 JSON-RPC protocol-level error 語意混淆

---

## 介面合約

### MCP Tools 定義

```
Tool: list_projects
  Description: List all Supabase projects (excluding deleted ones).
  Input schema: {}（空 object，無必填欄位）
  Output: JSON array of ProjectView

Tool: get_project
  Description: Get details of a Supabase project (masked config, health status).
  Input schema: {slug: string (required)}
  Output: JSON ProjectView

Tool: create_project
  Description: Create a new Supabase project (allocates ports, generates secrets). Status terminates as "stopped".
  Input schema: {slug: string (required), display_name: string (required)}
  Output: JSON ProjectView (status=stopped)

Tool: start_project
  Description: Start all Docker Compose services for a project.
  Input schema: {slug: string (required)}
  Output: JSON ProjectView (status=running)

Tool: stop_project
  Description: Stop project services (data is preserved).
  Input schema: {slug: string (required)}
  Output: JSON ProjectView (status=stopped)

Tool: reset_project
  Description: Reset project (destroy all data and re-provision). Status terminates as "running".
  Input schema: {slug: string (required)}
  Output: JSON ProjectView (status=running)

Tool: delete_project
  Description: Delete a project (removes runtime resources, audit log preserved).
  Input schema: {slug: string (required)}
  Output: JSON ProjectView (status=destroyed)
```

**Tool description 使用英文**，讓以英文訓練的 LLM 能更精準理解工具用途。

**`delete_project` 無確認機制設計決策**：
MCP stdio transport 的 host process（AI agent）負責安全控制，不適合在 server 端實作互動式確認。AI agent 在呼叫 `delete_project` 前應自行向使用者確認。設計文件明確說明此為 by design。

### `sbctl mcp serve` 命令旗標

```
sbctl mcp serve [flags]

旗標：
  --db-url       string   PostgreSQL DSN（env: SBCTL_DB_URL）
  --projects-dir string   專案目錄根路徑（env: SBCTL_PROJECTS_DIR，預設：./projects）
```

---

## 執行流程

### 啟動流程

```
sbctl mcp serve
  ↓ 解析 --db-url, --projects-dir
  ↓ 若 --db-url 為空 → 回傳 error，stderr 印出訊息，exit 1
  ↓ BuildDeps(ctx, dbURL, projectsDir)
  ↓ s := server.NewMCPServer("sbctl", version)
  ↓ 依序呼叫 registerTools(s, deps.ProjectService)
  ↓ server.ServeStdio(s)  ← 阻塞至 stdin 關閉（SDK 內建 SIGTERM/SIGINT 處理）
```

### OS Signal 處理與 Graceful Shutdown

`server.ServeStdio(s)` 為阻塞呼叫。**SDK 內部已自行管理 SIGTERM/SIGINT signal**（v0.32.0 確認）：

```go
// 正確的啟動程式碼（不需外層 signal.NotifyContext）
if err := server.ServeStdio(s); err != nil {
    slog.Error("mcp server error", "err", err)
    os.Exit(2)
}
```

**不需要外層的 `signal.NotifyContext`**：SDK 內部已呼叫 `signal.Notify`；若外層再包一層，同一 signal 會被兩個 goroutine 競爭接收，行為不確定。

若未來需注入自訂 context（如 trace ID），使用 SDK 正確的 option：
```go
server.ServeStdio(s, server.WithStdioContextFunc(func(ctx context.Context) context.Context {
    return ctx // 可在此注入自訂 value
}))
```

收到 SIGTERM/SIGINT 時：
- SDK 內部 ctx cancel → `ServeStdio` 退出迴圈
- 正在執行中的 tool handler 透過 ctx.Done() 感知（usecase/adapter 層的 context propagation）
- Phase 3 不實作 drain logic

### Log 輸出

**MCP server 的 stdout 專用於 JSON-RPC 協議通訊**，任何 log 必須輸出至 stderr。

- `slog.Default()` 的 handler 預設輸出至 stderr（Go 標準行為）
- Cobra 啟動前的錯誤訊息（flag 驗證失敗等）需確保輸出至 stderr：
  ```go
  mcpCmd.SetOut(os.Stderr) // mcp serve 命令的 cobra output 導向 stderr
  ```
- mcp-go SDK 不輸出任何 log 至 stdout（已確認）

### Tool 執行流程（以 create_project 為例）

```
AI agent 傳入 JSON-RPC request
  ↓ mcp-go 解析 tool name + input args
  ↓ handler 使用 request.RequireString("slug") 取得 slug
     - 若欄位缺失：RequireString 回傳 error → handler 回傳 NewToolResultError(err.Error()), nil
  ↓ handler 呼叫 svc.Create(ctx, slug, displayName)
  ↓ 成功 → json.Marshal(view) → NewToolResultText(jsonStr), nil
  ↓ 失敗 → NewToolResultError("message"), nil
```

**安全取值規則**：
- 取 string 用 `request.RequireString("key")`（不使用 `.(string)` 直接斷言，避免 nil panic）
- 若欄位為 optional，用 `request.GetString("key")` 或自行從 `Arguments` map 安全取值

---

## 錯誤處理

| UsecaseError.Code | MCP 回應 |
|------------------|---------|
| not_found | `NewToolResultError("project \"X\" not found"), nil` |
| conflict | `NewToolResultError("project \"X\" already exists"), nil` |
| invalid_input | `NewToolResultError(err.Message), nil` |
| invalid_state | `NewToolResultError(err.Message), nil` |
| internal | `NewToolResultError("internal error"), nil` |

**原則：**
- 所有 UsecaseError 一律用 `NewToolResultError(), nil`（業務錯誤，`isError: true`）
- `nil, err` 僅用於 handler 函數簽名本身的框架層異常
- Internal 錯誤不洩漏底層詳情（log 在 use-case 層 via slog → stderr）

---

## 測試策略

### 測試方式

handler 是普通 Go 函數（`func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)`），可直接呼叫，無需啟動 stdio server：

```go
result, err := handlers.CreateProjectHandler(deps.ProjectService)(context.Background(), mcp.CallToolRequest{
    Params: mcp.CallToolRequestParams{
        Arguments: map[string]any{"slug": "test", "display_name": "Test"},
    },
})
assert.Nil(t, err)
assert.False(t, result.IsError)
```

### 需要測試的行為

- 每個 tool handler 的 happy path（mock ProjectService）
- 每個 tool handler 的 error path（UsecaseError → `isError: true`）
- Tool input 缺少必要欄位 → `isError: true`（`RequireString` 觸發）
- JSON 輸出不含 ANSI 色碼，可被 `json.Unmarshal` 解析
- `list_projects` 空結果 → 回傳 `[]`（不回傳 null）

### Mock 策略

- `usecase.ProjectService` → 手工 function-pointer mock（與 CLI 測試共用）

### CI 執行方式

- `go test -race ./cmd/sbctl/...`（一般 CI）

---

## Production Ready 考量

### 錯誤處理
- `isError: true` 讓 AI agent 可偵測 tool 失敗
- Internal 錯誤不洩漏底層詳情
- 所有業務邏輯錯誤用 `NewToolResultError(), nil`，不與框架錯誤混淆

### 日誌與可觀測性
- **stdout 僅用於 MCP 協議**，所有 log 至 stderr（via slog）
- Cobra 輸出導向 stderr，避免污染 stdio transport

### 安全性
- stdio transport 信任 host process（不需認證）
- Config sensitive 欄位由 use-case 層 masked
- `delete_project` 無 server 端確認，由 AI agent（host process）負責使用者確認

### 優雅降級
- DB 連線失敗：啟動時立即報告錯誤（exit 2），不進入 serve loop
- SIGTERM/SIGINT：SDK 內建 signal handling → ServeStdio 退出，正在執行的 tool 透過 context propagation 感知取消

### 設定管理
- 必要：`--db-url`（或 `SBCTL_DB_URL`）
- 可選：`--projects-dir`（預設 `./projects`）

---

## 審查

### Reviewer A（架構）

- **狀態：** APPROVED（第三輪）
- **意見：** N1-N5 全部解決。N6（WithContext API 不存在）、N7（signal 雙重處理）→ 已在 v3 修正。

### Reviewer B（實作）

- **狀態：** APPROVED（第三輪）
- **意見：** N1-N7 全部解決。N8（WithContext API 不存在）→ 已在 v3 修正。N9（result.IsError 驗證）→ 非阻斷，實作時補充。

---

## 任務

<!-- 審查通過後展開 -->

---

## 程式碼審查

- **審查結果：** <!-- PASS | FIX_REQUIRED -->
- **發現問題：**
- **修正記錄：**
