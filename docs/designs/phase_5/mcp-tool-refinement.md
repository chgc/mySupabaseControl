> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# 設計文件：MCP Tool 說明精細化

## 狀態

done

## Phase

- **Phase：** Phase 5
- **Phase Plan：** docs/designs/phase-5-plan.md

---

## 目的

目前 MCP Server 的 7 個 tool descriptions 過於簡短（例如 `"List all active Supabase projects."`），AI agent 在判斷何時使用哪個工具時缺乏足夠資訊。具體問題：

1. **缺少回傳值描述** — AI 不知道 `get_project` 回傳哪些欄位（config、health、URLs），無法判斷是否需要額外呼叫 `credentials`
2. **缺少使用時機指引** — AI 不知道 `get_project` 與 `list_projects` 的差異（List 不含 config/URLs/health）
3. **缺少副作用說明** — AI 不知道 `reset_project` 會清除所有資料
4. **缺少參數說明** — `create_project` 的 `runtime` 參數未說明可選值與預設值
5. **語意不精確** — `create_project` 的描述是 `"Create and start a new Supabase project."`，但實際上 create 後專案狀態為 `creating`，需另外呼叫 start

本功能重寫所有 MCP tool 與 parameter 的 descriptions，使 AI agent 能更準確地選擇工具、提供正確參數、並理解回傳結果。

---

## 範圍

### 包含

- 重寫 7 個 MCP tool 的 `Description` 欄位
- 重寫所有 tool parameter 的 `Description` 欄位
- 確保描述涵蓋：功能、回傳值摘要、副作用、使用時機

### 不包含

- 新增 MCP tools（tool 數量不變）
- 修改 tool 的功能邏輯或參數結構
- 修改 tool 的 JSON 回傳格式

---

## 資料模型

本功能不修改任何資料模型。所有變更僅為字串常數替換。

### 現有 Tool Descriptions（待替換）

| Tool | 現有 Description |
|------|-----------------|
| `list_projects` | `"List all active Supabase projects."` |
| `get_project` | `"Get details of a Supabase project including config and health."` |
| `create_project` | `"Create and start a new Supabase project."` |
| `start_project` | `"Start a stopped Supabase project."` |
| `stop_project` | `"Stop a running Supabase project."` |
| `reset_project` | `"Reset a Supabase project: wipes all data and re-provisions."` |
| `delete_project` | `"Permanently delete a Supabase project and destroy all its data. This action is irreversible."` |

### 新 Tool Descriptions

#### `list_projects`

```
List all Supabase projects managed by this control plane.

Returns an array of project summaries with: slug, display_name, runtime_type, status, and timestamps.
Does NOT include config values, URLs, or service health details — use get_project for those.

Use this tool to get an overview of all projects or to find a project's slug.
```

#### `get_project`

```
Get full details of a single Supabase project by its slug.

Returns: slug, display_name, runtime_type, status, timestamps, config (masked sensitive values show "***"), URLs (api, studio, inbucket), and service health breakdown (per-service status when the project is running).

Use this tool when you need project configuration, connection URLs, or health diagnostics.
Note: Config values containing secrets are masked with "***". To obtain unmasked credentials, the operator must use the CLI command 'sbctl project credentials <slug>' directly.
```

#### `create_project`

```
Create a new Supabase project with auto-allocated ports and generated secrets.

This provisions the project directory, generates per-project JWT secrets and API keys, allocates non-conflicting ports, and renders runtime configuration. The project status after creation is "stopped" — services are NOT started automatically. Call start_project to bring up the services.

Returns the full project view including slug, status, allocated URLs, and config.
```

#### `start_project`

```
Start a stopped Supabase project, bringing up all its services.

Works on projects in "stopped" or "error" status. Starting an already running project returns an error. Projects in error state can be retried with this tool — the control plane will attempt to restart from the last known good state.

Services started depend on the project's runtime_type (docker-compose uses 'docker compose up', kubernetes uses 'helm upgrade --install').

Returns the updated project view with new status.
```

#### `stop_project`

```
Stop a running Supabase project, shutting down all services but preserving data.

Only works on projects in "running" status. Data volumes and configuration are preserved — the project can be restarted with start_project.

Returns the updated project view with new status.
```

#### `reset_project`

```
Reset a Supabase project: destroys all data, then re-creates and restarts with fresh secrets and ports.

WARNING: This permanently deletes all database content, storage files, and other project data. The project is then re-provisioned with newly generated secrets, newly allocated ports, and fresh empty services. Only the project slug and display name are preserved — all other values (secrets, ports, config) are regenerated.

Use this when you need a completely clean slate for the project.

Returns the updated project view after re-provisioning.
```

#### `delete_project`

```
Permanently delete a Supabase project and all its resources.

This stops all services (if running), removes data volumes, deletes configuration files and the project directory, and removes the project record from the control plane database. This action is IRREVERSIBLE — there is no undo or recovery.

Use reset_project instead if you want to clear data but keep the project.

Returns confirmation of deletion.
```

### 新 Parameter Descriptions

#### `create_project` parameters

| Parameter | 現有 Description | 新 Description |
|---|---|---|
| `slug` | `"Project slug (unique identifier)"` | `"Unique project identifier. Lowercase alphanumeric and hyphens only, 3-40 characters. Used as directory name and compose project name prefix. Cannot be changed after creation."` |
| `display_name` | `"Human-readable project name"` | `"Human-readable display name for the project (max 100 characters). Shown in project listings and the Supabase Studio dashboard."` |
| `runtime` | `"Runtime type (docker-compose or kubernetes)"` | `"Runtime backend for this project. Options: 'docker-compose' (default, uses Docker Compose on the local host) or 'kubernetes' (uses Helm chart on an OrbStack/k3s K8s cluster). Choose based on your deployment target."` |

#### 其他 tool 的 `slug` parameter

```
"The unique slug identifier of the target project. Use list_projects to find available slugs."
```

---

## 介面合約

僅修改 `mcp.go` 中的 `mcp.Tool` 定義的字串常數。不修改任何函式簽名、參數結構或回傳格式。

```go
// 修改前
mcp.NewTool("list_projects",
    mcp.WithDescription("List all active Supabase projects."),
)

// 修改後
mcp.NewTool("list_projects",
    mcp.WithDescription("List all Supabase projects managed by this control plane.\n\nReturns an array of project summaries with: slug, display_name, runtime_type, status, and timestamps.\nDoes NOT include config values, URLs, or service health details — use get_project for those.\n\nUse this tool to get an overview of all projects or to find a project's slug."),
)
```

---

## 執行流程

無 runtime 流程變更。MCP Server 啟動時讀取 tool 定義（含新 descriptions），AI agent 透過 MCP protocol 的 `tools/list` 方法取得 tool schema 與 descriptions。

---

## 錯誤處理

本功能不引入新的錯誤路徑。所有變更為字串常數替換。

| 情境 | 處理方式 | 回應格式 |
|---|---|---|
| 不適用 | — | — |

---

## 測試策略

### 需要測試的行為

- MCP Server 初始化成功（`buildMCPServer()` 不 panic）
- 所有 7 個 tool 的 description 非空
- 所有 tool parameter 的 description 非空
- MCP `tools/list` 回傳的 tool descriptions 與程式碼中定義的一致

### 測試類型分配

| 測試類型 | 測試目標 | 層次 |
|---|---|---|
| 單元測試 | `buildMCPServer()` 回傳的 server 包含 7 個 tools 且 descriptions 非空 | `cmd/sbctl` |

### Mock 策略

- `ProjectService` — 使用 mock（與既有 MCP 測試相同模式）
- 無需實際 MCP client 連線

### CI 執行方式

- 所有測試在一般 CI 中執行

---

## Production Ready 考量

### 錯誤處理

不適用（純文字變更）。

### 日誌與可觀測性

不適用。

### 輸入驗證

不適用。

### 安全性

- Description 文字中明確說明 MCP 不暴露 raw secrets（`get_project` 的 config 以 `"***"` 遮蔽）
- 引導 AI agent 使用 CLI `credentials` 命令取得敏感值，避免透過 MCP 傳遞

### 優雅降級

不適用。

### 設定管理

不適用。

---

## 待決問題

- 無

---

## 審查

### Reviewer A（架構）

- **狀態：** 🔁 REVISE（第一輪）→ ✅ APPROVED（第二輪）
- **意見：**

**第二輪：** 2 個 blocking 事實性錯誤均已正確修正，新描述與源碼行為一致。殘留建議：`delete_project` 可將 "removes the project record" 改為 "marks the project as destroyed"（soft-delete），不阻擋。

**第一輪原始意見：**

整體方向正確，精細化 MCP tool descriptions 是必要的改進。但有兩個 **事實性錯誤** 會導致 AI agent 產生錯誤行為，必須修正後才能進入實作。

#### ❌ 必須修正（Blocking）

**1. `create_project` 描述與實作行為矛盾**

設計中的新描述寫道：

> "The project status after creation is "running" — services are started automatically."

但 `ProjectService.Create()` 的實際行為是：
- `domain.NewProject()` 設定初始狀態為 `StatusCreating`（`project_model.go`）
- 成功後轉為 `StatusStopped`（`project_service_impl.go:155`）
- **不會呼叫 `Start()`**，專案不會自動啟動

介面合約（`project_service_impl.go:17-19`）明確標示：
> "Terminates in stopped state. Does NOT start the project."

諷刺的是，設計文件的「目的」第 5 點正確指出了現有描述的這個問題，但新描述卻引入了**反方向的同類錯誤**（舊：暗示會 start → 新：直接聲稱 status 是 running）。

**建議修正：**
```
The project status after creation is "stopped" — use start_project to bring up services.
```

**2. `reset_project` 描述錯誤聲稱 secrets 與 ports 被保留**

設計中的新描述寫道：

> "re-creates and restarts the project with the same slug, config, and ports"
> "Configuration and secrets are preserved."

但 `Reset()` 的實際行為（`project_service_impl.go:336-341`）：
- 呼叫 `provisionConfig(ctx, project, nil)`
- `provisionConfig()` 呼叫 `GenerateProjectSecrets()` → **產生全新 secrets**
- `provisionConfig()` 呼叫 `AllocatePorts()` → **配置全新 ports**
- 程式碼註解明確寫著：`// Re-provision: new secrets + new ports.`

唯一被保留的是 **slug** 與 **display_name**，而非 config 與 ports。

**建議修正：**
```
WARNING: This permanently deletes all database content, storage files, and other project data. The project is re-provisioned with fresh services, new secrets, and newly allocated ports. Only the project slug and display name are preserved.
```

#### ⚠️ 建議改進（Non-blocking）

**3. `start_project` 的 error 狀態描述過度簡化**

描述說：
> "Only works on projects in 'stopped' or 'error' status."

但 `CanStart()` 對 error 狀態有更細緻的限制：只有 `PreviousStatus ∈ {starting, running, stopping}` 的 error 專案才允許 start。並非所有 error 狀態都可啟動（例如在 creating 階段失敗的專案不能直接 start）。

建議改為：
```
Only works on projects in "stopped" status, or in "error" status if the failure occurred during a start/run/stop operation.
```

**4. `delete_project` 描述中 "removes the project from the control plane database" 語意不精確**

`projectRepo.Delete()` 實際執行的是 soft-delete（`UPDATE SET status = 'destroyed'`），config 行也被保留用於審計（介面註解明確說明）。Runtime 層面 `adapter.Destroy()` 確實會刪除磁碟上的專案目錄。

建議區分：
```
This stops all services, removes data volumes, deletes the project directory, and marks the project as destroyed in the control plane database (soft-delete; config is retained for audit).
```

**5. `get_project` 的 credentials 引導語措辭**

> "use the CLI command 'sbctl project credentials <slug>' instead"

這對透過 MCP 整合的 AI agent 可能產生混淆——agent 不一定有能力執行 CLI 命令。建議改為更中性的措辭：
```
MCP does not expose raw secrets. Unmasked credentials are available via the CLI: sbctl project credentials <slug>.
```

#### ✅ 正面評價

- `list_projects` vs `get_project` 的使用時機區分清晰，有助於 AI agent 選擇正確工具
- 安全性考量（MCP 不暴露 raw secrets）方向正確
- Parameter descriptions 的增強（slug 格式、runtime 選項）對 AI agent 選擇正確參數值很有幫助
- 測試策略符合變更範圍——純字串常數替換不需要複雜測試
- 與 coding guidelines 無衝突（變更僅為 `mcp.go` 中的字串常數）

### Reviewer B（實作）

- **狀態：** 🔁 REVISE（第一輪）→ ✅ APPROVED（第二輪）
- **意見：**

**第二輪：** 2 個 critical 事實錯誤均已修正，描述與原始碼一致。error 狀態描述以合理方式回應。全文一致性測試等效覆蓋 substring 需求。修訂未引入新的事實性錯誤。

**第一輪原始意見：**

#### SDK 相容性確認（無問題）

- `mcp-go v0.45.0` 的 `WithDescription()` 直接將 `string` 賦值給 `Tool.Description`（`tools.go:828-831`），無字數限制、無字元限制。
- `Description` 欄位以 `json:"description,omitempty"` 序列化，`\n` 在 JSON 中為 `\\n`，完全合法。
- MCP 規範未對 tool description 長度設限。多行描述不會造成問題。
- 同樣，`mcp.Description()` property option 亦為純字串賦值（`tools.go:1011`），parameter descriptions 無相容性問題。

#### 現有測試影響（無問題）

- 現有 `mcp_test.go` 僅測試 tool handler 行為（`makeMCPListProjects` 等），不驗證 description 內容。純文字變更不會破壞任何現有測試。

#### 🚨 問題 1（Critical）：`create_project` description 與實作不符

設計文件描述：

> *"The project status after creation is "running" — services are started automatically."*

但實際實作（`project_service_impl.go:154-159`）：

```go
// domain.NewProject sets status=creating; transition to stopped on success.
if err := s.projectRepo.UpdateStatus(ctx, slug, domain.StatusStopped, domain.StatusCreating, ""); err != nil {
```

Create 完成後狀態為 **`stopped`**，**不會**自動啟動服務。使用者需另外呼叫 `start_project`。此錯誤與本設計的目的（修正語意不精確）直接矛盾 — 目的章節第 5 點正是指出現有描述 `"Create and start"` 的不準確性，但新描述反而引入了更嚴重的事實錯誤。

**建議修正：**

```
Create a new Supabase project with auto-allocated ports and generated secrets.

This provisions the project directory, generates per-project JWT secrets and API keys, allocates non-conflicting ports, and renders runtime configuration. The project status after creation is "stopped" — use start_project to bring up services.

Returns the full project view including slug, status, allocated URLs, and config.
```

#### 🚨 問題 2（Critical）：`reset_project` description 與實作不符

設計文件描述：

> *"re-creates and restarts the project with the same slug, config, and ports."*
> *"Configuration and secrets are preserved."*

但實際實作（`project_service_impl.go:336-337`）：

```go
// Re-provision: new secrets + new ports.
newConfig, provErr := s.provisionConfig(ctx, project, nil)
```

Reset 會重新產生 **新的 secrets 與新的 port 分配**。僅 slug 與 display_name 保留不變。描述中 "same config, and ports" 及 "Configuration and secrets are preserved" 均為事實錯誤。

此外 reset 完成後狀態為 `running`（line 367-372），這部分 "restarts" 是正確的。

**建議修正：**

```
Reset a Supabase project: destroys all data volumes, then re-creates and restarts the project with the same slug and display_name but newly generated secrets and port allocations.

WARNING: This permanently deletes all database content, storage files, and other project data. The project is re-provisioned with fresh, empty services, new JWT secrets, new API keys, and new port assignments.

Use this when you need a clean slate without changing the project's identity.

Returns the updated project view after re-provisioning.
```

#### ⚠️ 問題 3（Minor）：`start_project` error 狀態描述過度簡化

設計描述：

> *"Only works on projects in "stopped" or "error" status."*

但 `CanStart()` 邏輯（`project_model.go:226-234`）顯示 error 狀態下僅當 `PreviousStatus` 為 `starting`、`running` 或 `stopping` 時才可啟動。建議加上這個條件，或至少不造成誤解（目前寫法可接受但不精確）。

#### ⚠️ 問題 4（Minor）：測試策略可加強內容斷言

目前測試策略只驗證 "description 非空"。建議加入關鍵片段的 substring assertion（例如驗證 `reset_project` description 包含 `"WARNING"`、`get_project` 包含 `"health"`），以防止未來意外截斷或刪除關鍵資訊。這不需要 snapshot 整段文字，只需驗證核心語意片段存在。

#### 總結

| 類別 | 評估 |
|---|---|
| **正確性** | ❌ `create_project` 與 `reset_project` 的描述與實際行為不符（見問題 1、2） |
| **完整性** | ⚠️ `start_project` 的 error 狀態條件可更精確 |
| **介面清晰度** | ✅ 描述格式一致，段落結構清晰 |
| **一致性** | ✅ 與專案架構一致，未引入新模式 |
| **Coding guideline 合規性** | ✅ 純字串常數變更，無 Go 規範違反風險 |
| **測試策略充分性** | ⚠️ 建議加入關鍵片段的 substring assertion |
| **Production Ready 評估** | ✅ 不適用（純文字變更），安全性考量已涵蓋 |

---

## 任務

### T-F6-1: 更新 MCP tool 與參數描述（`f6-mcp-descriptions`）

**建立/修改檔案：**
- `cmd/sbctl/mcp.go` — 更新全部 7 個 tool 的 description 與 parameter description

**實作內容：**
1. 依設計文件「介面合約」章節，逐一替換 7 個 tool 的 `Description` 字串
2. 依設計文件「參數描述」章節，替換所有 parameter 的 `Description` 字串
3. 新增/修改測試：`cmd/sbctl/mcp_test.go`
   - 驗證 MCP server 初始化不 panic
   - 驗證 7 個 tool 都有非空 description
   - 驗證所有 parameter 都有非空 description
   - 驗證關鍵片語存在（如 `create_project` 含 "stopped"、`reset_project` 含 "WARNING"）

**驗收標準：**
- `go build ./...` 通過
- `go test -race ./...` 通過
- MCP server 可正常啟動（`sbctl mcp serve` 不 panic）

---

## 程式碼審查

- **審查結果：** ✅ APPROVED（第二輪）
- **發現問題：** R1: watch-timeout 未套用 context、gofmt 格式問題、writeCreateSummary 錯誤忽略
- **修正記錄：** 7f163fc fix watch-timeout & gofmt; cd2480d fix writeCreateSummary error handling
