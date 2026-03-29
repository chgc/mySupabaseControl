> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# 設計文件：全專案狀態總覽

## 狀態

done

## Phase

- **Phase：** Phase 5
- **Phase Plan：** docs/designs/phase-5-plan.md

---

## 目的

目前 `sbctl project list` 以 table 格式列出所有專案，但無法快速總覽整體狀態分佈（例如：3 個 running、1 個 stopped、1 個 error）。使用者需逐一閱讀每個專案的狀態欄位。

本功能新增 `sbctl status` 頂層命令，提供一個精簡的全系統狀態總覽，包含：
1. 專案計數與狀態分佈摘要
2. 所有專案的簡易狀態清單（含色彩標示）
3. 需要注意的項目提示（error 狀態的專案）

---

## 範圍

### 包含

- 新增 `sbctl status` 頂層命令
- 狀態分佈摘要行（例如 `5 projects: 3 running, 1 stopped, 1 error`）
- 專案列表（含色彩化狀態 — 依賴功能 1）
- error 狀態專案的醒目提示
- 支援 `--output table|json|yaml` 輸出格式

### 不包含

- 個別專案的詳細健康檢查（使用 `sbctl project get <slug>` 取得）
- 系統資源使用量（CPU、記憶體、磁碟）
- Docker/Kubernetes 層級的狀態（僅顯示控制面板層級的專案狀態）

---

## 資料模型

本功能不新增資料模型。使用既有的 `ProjectService.List()` 回傳 `[]*ProjectView`。

### 新增輸出結構（僅供 JSON/YAML 輸出）

```go
// StatusOverview is the JSON/YAML output for 'sbctl status'.
type StatusOverview struct {
    Total    int                `json:"total"`
    Summary  map[string]int     `json:"summary"`            // status → count
    Projects []*ProjectSummary  `json:"projects"`
    Alerts   []string           `json:"alerts,omitempty"`    // error 專案提示
}

// ProjectSummary is a compact project entry for the status overview.
type ProjectSummary struct {
    Slug        string `json:"slug"`
    DisplayName string `json:"display_name"`
    Status      string `json:"status"`
    UpdatedAt   string `json:"updated_at"` // time.RFC3339 格式
}
```

此結構定義在 `cmd/sbctl/status.go` 中，為 CLI 展示層結構，不放入 `usecase` 層。

---

## 介面合約

### 新增 `cmd/sbctl/status.go`

```go
package main

// buildStatusCmd creates the 'sbctl status' command.
func buildStatusCmd(deps **Deps, output *string, colorOut *colorer) *cobra.Command
```

### 命令掛載點

在 `buildRootCmd()` 中新增：

```go
root.AddCommand(buildStatusCmd(&deps, &output, colorOut))
```

置於 `root.AddCommand(buildProjectCmd(...))` 之後。

### `writeStatusOverview`

```go
// writeStatusOverview renders the status overview output.
func writeStatusOverview(w io.Writer, output string, views []*usecase.ProjectView, c *colorer) error
```

---

## 執行流程

### `sbctl status` 執行流程

1. `buildStatusCmd` 的 `RunE` 呼叫 `(*deps).ProjectService.List(ctx)` 取得 `[]*ProjectView`
2. 呼叫 `writeStatusOverview()` 格式化輸出
3. table 格式的輸出邏輯：
   a. 計算狀態分佈（`map[string]int`）
   b. 印出摘要行：`N projects: X running, Y stopped, Z error`
      - 狀態按固定順序列出：`running` → `starting` → `creating` → `stopping` → `stopped` → `destroying` → `destroyed` → `error`
      - 計數為 0 的狀態不列出
   c. 若有 error 狀態專案，印出醒目提示（紅色）
   d. 印出所有專案的簡易 table（SLUG、STATUS、UPDATED）
4. JSON/YAML 格式：序列化 `StatusOverview` 結構

### 輸出範例

#### `sbctl status` (table)

```
5 projects: 3 running, 1 stopped, 1 error

⚠ Projects needing attention:
  my-broken-app  error  2025-01-15T10:30:00Z

SLUG             STATUS    UPDATED
my-app           running   2025-01-15T10:35:00Z
my-api           running   2025-01-15T10:34:00Z
staging          running   2025-01-15T10:33:00Z
dev-test         stopped   2025-01-15T09:00:00Z
my-broken-app    error     2025-01-15T10:30:00Z
```

#### `sbctl status` (table，無專案)

```
No projects found. Create one with: sbctl project create <slug> -n <name>
```

#### `sbctl status -o json`

```json
{
  "total": 5,
  "summary": {
    "running": 3,
    "stopped": 1,
    "error": 1
  },
  "projects": [
    {"slug": "my-app", "display_name": "My App", "status": "running", "updated_at": "2025-01-15T10:35:00Z"},
    ...
  ],
  "alerts": [
    "my-broken-app is in error state (updated: 2025-01-15T10:30:00Z)"
  ]
}
```

---

## 錯誤處理

| 情境 | 處理方式 | 回應格式 |
|---|---|---|
| `List()` 失敗 | 回傳 error | `projectErr(cmd, err)` — 注意：`status` 為頂層命令，需新增獨立的 `statusErr` 或共用 error formatter |
| 無任何專案 | 顯示引導訊息 | `No projects found. Create one with: ...` |
| 不支援的 output 格式 | PersistentPreRunE 已驗證 | — |

---

## 測試策略

### 需要測試的行為

- `writeStatusOverview` table 格式正確顯示摘要行（含各狀態計數）
- `writeStatusOverview` 摘要行狀態按固定順序列出（不依賴 map 遍歷順序）
- `writeStatusOverview` table 格式在有 error 專案時顯示 alert 區段
- `writeStatusOverview` table 格式在無 error 專案時不顯示 alert 區段
- `writeStatusOverview` table 格式在無專案時顯示引導訊息
- `writeStatusOverview` JSON 格式輸出正確的 `StatusOverview` 結構
- `writeStatusOverview` YAML 格式輸出正確的 `StatusOverview` 結構
- `buildStatusCmd` 正確掛載到 rootCmd
- 狀態計數邏輯正確（多種狀態混合、全部相同狀態、含未知狀態值）
- 色彩在 status 欄位正確套用（依賴功能 1）
- `ProjectSummary.UpdatedAt` 使用 `time.RFC3339` 格式

### 測試類型分配

| 測試類型 | 測試目標 | 層次 |
|---|---|---|
| 單元測試 | `writeStatusOverview` table/json/yaml 各格式 | `cmd/sbctl` |
| 單元測試 | 狀態計數邏輯（含全同、混合、未知狀態值） | `cmd/sbctl` |
| 單元測試 | 摘要行狀態排序確定性 | `cmd/sbctl` |
| 單元測試 | alert 區段有/無 error | `cmd/sbctl` |
| 單元測試 | 空專案列表引導訊息 | `cmd/sbctl` |

### Mock 策略

- `ProjectService.List()` — 使用既有 mock 介面（`main_test.go` 中的 `mockSvc`）

### 測試基礎設施

- `status` 為頂層命令，測試需建構完整的 `buildRootCmd()` 並掛載 `buildStatusCmd`
- 或直接測試 `writeStatusOverview` 函式（傳入預建的 `[]*ProjectView`），避免依賴 Cobra 命令結構
- 後者為建議方式，與 `writeProjectView` / `writeProjectViews` 的測試模式一致

### CI 執行方式

- 所有測試在一般 CI 中執行，不需特殊環境

---

## Production Ready 考量

### 錯誤處理

`List()` 失敗時使用獨立的 error formatter 回傳錯誤（與 `projectErr` 相同模式，但適用於頂層命令）。

### 日誌與可觀測性

不需日誌。展示層命令。

### 輸入驗證

無使用者輸入（除 `--output` 旗標，由 `PersistentPreRunE` 驗證）。

### 安全性

不顯示敏感資訊。僅顯示 slug、status、timestamp。

### 優雅降級

DB 不可用時 `List()` 失敗，顯示錯誤訊息。與 `project list` 行為一致。

### 設定管理

無新增設定。

---

## 待決問題

- 無

---

## 審查

### Reviewer A（架構）

- **狀態：** ✅ APPROVED
- **意見：**

  **1. `status` 作為頂層命令：合理 ✅**
  `status` 是跨專案的儀表板視角，並非針對單一專案的 CRUD 操作。放在頂層符合常見 CLI 慣例（如 `git status`、`docker ps`），也保留日後擴展至非專案資源的彈性。

  **2. StatusOverview / ProjectSummary 放在 CLI 層：正確 ✅**
  這兩個結構僅服務於展示層的 JSON/YAML 序列化，不含業務邏輯。放在 `cmd/sbctl/status.go` 與現有 CLI-only 結構的慣例一致，未汙染 usecase 層。

  **3. 既有模式複用：一致 ✅**
  - `buildStatusCmd` 簽名遵循 `buildProjectCmd` 模式（`deps **Deps, output *string`，加上功能 1 的 `*colorer`）。
  - `writeStatusOverview` 遵循 `writeProjectViews` / `writeProjectView` 的 `(io.Writer, string, data, ...)` 簽名模式。
  - 透過 `root.AddCommand` 掛載，與 `buildProjectCmd` / `buildMCPCmd` 一致。
  - 錯誤處理走 `handleError()` 標準路徑，output 格式由 `PersistentPreRunE` 驗證。

  **4. 資料來源選擇：合理 ✅**
  直接使用 `ProjectService.List()` 回傳的 `[]*ProjectView` 進行彙總，不引入新的 usecase 方法。List 回傳的基本欄位（Slug、DisplayName、Status、UpdatedAt）恰好滿足需求，無需 Health 或 Config。

  **5. 細微建議（非阻擋）：**
  - 可考慮在 alerts 中納入長時間停留在過渡狀態（`creating`、`destroying`）的專案，避免使用者忽略卡住的部署。此為功能增強建議，不影響本次審查通過。

### Reviewer B（實作）

- **狀態：** ✅ APPROVED（有條件）
- **意見：**

整體設計結構清晰，可直接實作。`StatusOverview` / `ProjectSummary` 放在 `cmd/sbctl/status.go` 作為展示層結構合理。`writeStatusOverview` 簽名與既有 `writeProjectViews` 模式一致（加入 `*colorer` 依賴功能 1）。以下列出需在實作前釐清或修正的項目：

**1. `handleError()` 函式名稱不符（需修正）**

設計文件錯誤處理表格引用 `handleError()`，但程式碼中實際函式為 `projectErr(cmd *cobra.Command, err error) error`（project.go:171）。`sbctl status` 是頂層命令、非 project 子命令，需釐清：
- (a) 將 `projectErr` 重構為通用的 `cmdErr` 並共用？
- (b) 在 `status.go` 中定義自己的 `statusErr`？
- 建議：(a) 較佳，因錯誤格式邏輯完全相同。

**2. Summary 行狀態排序未定義（需補充）**

摘要行 `5 projects: 3 running, 1 stopped, 1 error` 依賴 `map[string]int` 遍歷，但 Go map 遍歷順序不確定。需定義排序規則，否則每次執行輸出順序不同。建議：
- 固定順序：`running → stopped → error → 其他`（按嚴重度）
- 或按字母排序
- 實作時需用 `[]string` 切片控制順序，非直接遍歷 map

**3. `newTestRootCmd` 需擴充（需說明）**

現有 `newTestRootCmd` 只掛載 `buildProjectCmd`，不含 `buildStatusCmd`。測試策略提到 `buildStatusCmd 正確掛載到 rootCmd`，但未說明如何修改測試輔助函式。建議：
- 在 `newTestRootCmd` 中一併掛載 `buildStatusCmd`
- 或新增 `newTestRootCmdWithStatus` 輔助函式
- 需同步處理 `*colorer` 參數（傳入停用色彩的 colorer）

**4. `ProjectSummary.UpdatedAt` 轉換格式（建議補充）**

`ProjectView.UpdatedAt` 為 `time.Time`，`ProjectSummary.UpdatedAt` 為 `string`。設計未明確指定轉換格式。建議補充：使用 `v.UpdatedAt.UTC().Format(time.RFC3339)` 以與既有 `writeProjectViews`（output.go:53）一致。

**5. Alert 中 "since" 語意可能誤導（建議修正）**

JSON 輸出中 `"my-broken-app is in error state (since 2025-01-15T10:30:00Z)"`，但 `UpdatedAt` 是最後更新時間，不等於進入 error 的時間（可能是 error 後又更新了其他欄位）。建議改為：
- `"my-broken-app is in error state (last updated: 2025-01-15T10:30:00Z)"`

**6. 測試覆蓋補充建議**

- 未測試 unknown / 非預期 status 值的計數邏輯（`map[string]int` 會自動處理，但 summary 行顯示需確認）
- 未測試所有專案都是同一狀態的邊界情況
- 未測試 `⚠` 字元在非 Unicode 終端的顯示（低優先級）

**結論：** 以上第 1–3 點為實作時必須解決的問題（不解決會導致編譯錯誤或非確定性輸出），第 4–6 點為建議改善。整體架構正確，可進入實作階段。

---

## 任務

### T-F3-1: 建立 status 命令（`f3-status-cmd`）

**依賴：** T-F1-2（colorer 整合後 writeStatusOverview 簽名含 `*colorer`）

**建立/修改檔案：**
- 新增 `cmd/sbctl/status.go` — status 命令與 overview 格式化
- 新增 `cmd/sbctl/status_test.go` — status 命令測試
- 修改 `cmd/sbctl/main.go` — 註冊 `buildStatusCmd()`

**實作內容：**
1. `status.go`：
   - CLI 層資料結構：`StatusOverview`、`ProjectSummary`（含 JSON tag）
   - `func buildStatusCmd(deps **Deps, output *string, c **colorer) *cobra.Command`
   - RunE：呼叫 `svc.List(ctx)` → `writeStatusOverview(w, output, views, *c)`
   - `func writeStatusOverview(w io.Writer, output string, views []*usecase.ProjectView, c *colorer) error`
   - table 格式：
     - 摘要行（固定順序：running→starting→creating→stopping→stopped→destroying→destroyed→error）
     - 零計數不顯示
     - ⚠ 警告區段（error 狀態專案）
     - 專案表格（SLUG, STATUS, UPDATED），status 用 `c.status()` 上色
     - 空列表顯示引導訊息
   - JSON/YAML：序列化 `StatusOverview` 結構
2. `main.go`：`rootCmd.AddCommand(buildStatusCmd(&deps, &output, &colorOut))`
3. `status_test.go`：
   - 摘要行正確計數與順序
   - 零計數不列出
   - error 專案觸發警告區段
   - 無 error 專案無警告
   - 空列表顯示引導
   - JSON 格式正確序列化
   - UpdatedAt 使用 RFC3339 格式

**驗收標準：**
- `go build ./...` 通過
- `go test -race ./...` 通過
- `sbctl status` 顯示系統總覽

---

## 程式碼審查

- **審查結果：** ✅ APPROVED（第二輪）
- **發現問題：** R1: watch-timeout 未套用 context、gofmt 格式問題、writeCreateSummary 錯誤忽略
- **修正記錄：** 7f163fc fix watch-timeout & gofmt; cd2480d fix writeCreateSummary error handling
