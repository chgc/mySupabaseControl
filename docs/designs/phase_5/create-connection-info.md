> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# 設計文件：建立專案連線資訊

## 狀態

done

## Phase

- **Phase：** Phase 5
- **Phase Plan：** docs/designs/phase-5-plan.md

---

## 目的

目前 `sbctl project create` 執行成功後，僅顯示基本資訊（SLUG、DISPLAY NAME、STATUS、UPDATED），使用者無法立即得知專案的連線方式（API URL、Studio URL、Postgres 連線資訊等）。使用者必須另外執行 `sbctl project credentials <slug>` 才能取得連線資訊，增加了操作步驟。

同樣地，`sbctl project get` 的 table 格式也僅顯示這 4 個欄位，儘管 `ProjectView` 已包含 `URLs`（API、Studio、Inbucket）資訊。

本功能改善 `create` 與 `get` 指令的輸出，在 table 格式中顯示 URLs，並在 `create` 成功後自動顯示連線摘要，使使用者能在單次操作中獲取所有必要的連線資訊。

---

## 範圍

### 包含

- `sbctl project create` 成功後，在 project view 之後額外顯示 credentials 摘要
- `sbctl project get` 的 table 格式新增 URLs 區段
- `writeProjectView` 在 table 格式中展示 URLs（若存在且非空）
- `create` 指令自動呼叫 `ProjectService.GetCredentials()` 並在 project view 下方顯示

### 不包含

- 修改 JSON/YAML 輸出格式（`ProjectView` 已透過 `json:"urls,omitempty"` 包含 URLs）
- 修改 `list` 指令（`List()` 不回傳 URLs，效能考量）
- 修改 `CredentialsView` 的欄位（欄位不變，僅改顯示時機）
- 新增 CLI 旗標控制是否顯示連線資訊

---

## 資料模型

本功能不修改任何資料模型。使用既有的 `ProjectView`（含 `URLs *ProjectURLs`）與 `CredentialsView`。

### 既有結構（參考）

```go
type ProjectURLs struct {
    API      string `json:"api"`      // Kong API gateway URL
    Studio   string `json:"studio"`   // Supabase Studio URL
    Inbucket string `json:"inbucket"` // Email testing URL (dev only)
}

type CredentialsView struct {
    Slug              string `json:"slug"`
    StudioURL         string `json:"studio_url"`
    DashboardUsername string `json:"dashboard_username"`
    DashboardPassword string `json:"dashboard_password"`
    APIURL            string `json:"api_url"`
    AnonKey           string `json:"anon_key"`
    ServiceRoleKey    string `json:"service_role_key"`
    PostgresHost      string `json:"postgres_host"`
    PostgresPort      string `json:"postgres_port"`
    PostgresDB        string `json:"postgres_db"`
    PostgresPassword  string `json:"postgres_password"`
    PoolerPort        string `json:"pooler_port"`
}
```

---

## 介面合約

### 修改 `writeProjectView`（`output.go`）

table 格式新增 URLs 區段。使用現有函式簽名（若 Feature 1 先合併，需新增 `*colorer` 參數）：

```go
func writeProjectView(w io.Writer, output string, view *usecase.ProjectView) error {
    // ... 既有 table 輸出（SLUG, DISPLAY NAME, STATUS, UPDATED）

    // 新增：若 URLs 存在且有非空值，在 table 下方顯示
    if output == "table" && view.URLs != nil {
        fmt.Fprintf(w, "\nURLs:\n")
        tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
        if view.URLs.API != "" {
            fmt.Fprintf(tw, "  API\t%s\n", view.URLs.API)
        }
        if view.URLs.Studio != "" {
            fmt.Fprintf(tw, "  Studio\t%s\n", view.URLs.Studio)
        }
        if view.URLs.Inbucket != "" {
            fmt.Fprintf(tw, "  Inbucket\t%s\n", view.URLs.Inbucket)
        }
        tw.Flush()
    }
}
```

> 注意：目前 `buildURLs()` 的實作中，API 與 Studio 指向同一個 Kong endpoint（同 host:port），Inbucket URL 目前不會被填充。上述條件印出確保未來欄位擴充時自動生效。

### 新增 `writeCreateSummary`（`output.go`）

在 `create` 成功後，於 project view 下方顯示連線摘要：

```go
// writeCreateSummary prints a credentials summary after project creation.
// Only used for table output format.
func writeCreateSummary(w io.Writer, creds *usecase.CredentialsView) error {
    fmt.Fprintf(w, "\nConnection Info:\n")
    tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
    fmt.Fprintf(tw, "  API URL\t%s\n", creds.APIURL)
    fmt.Fprintf(tw, "  Anon Key\t%s\n", creds.AnonKey)
    fmt.Fprintf(tw, "  DB Host\t%s\n", creds.PostgresHost)
    fmt.Fprintf(tw, "  DB Port\t%s\n", creds.PostgresPort)
    fmt.Fprintf(tw, "  DB Password\t%s\n", creds.PostgresPassword)
    if err := tw.Flush(); err != nil {
        return err
    }
    _, err := fmt.Fprintf(w, "\n  Run 'sbctl project credentials %s' for full credentials.\n", creds.Slug)
    return err
}
```

> Studio URL 不另外顯示於 Connection Info（已在 URLs 區段顯示，且與 API URL 相同）。

### 修改 `create` RunE（`project.go`）

```go
// 在 create RunE 中，建立成功後：
view, err := (*deps).ProjectService.Create(cmd.Context(), slug, displayName, runtime)
if err != nil { return projectErr(cmd, err) }

if err := writeProjectView(cmd.OutOrStdout(), *output, view); err != nil {
    return err
}

// 新增：table 格式時自動顯示 credentials 摘要
if *output == "table" {
    creds, err := (*deps).ProjectService.GetCredentials(cmd.Context(), slug)
    if err != nil {
        // credentials 取得失敗不應中斷 create 的成功回報
        fmt.Fprintf(cmd.ErrOrStderr(), "\nWarning: Could not retrieve credentials: %v\n", err)
        return nil
    }
    return writeCreateSummary(cmd.OutOrStdout(), creds)
}
return nil
```

> 注意：若 Feature 1（CLI 彩色輸出）先合併，`writeProjectView` 簽名將新增 `*colorer` 參數，此處需同步更新。

---

## 執行流程

### `sbctl project create <slug>` 流程（增強後）

1. CLI 呼叫 `ProjectService.Create()` → 取得 `*ProjectView`（狀態為 `stopped`，含 URLs）
2. 呼叫 `writeProjectView()` 輸出基本資訊 + URLs 區段
3. 若 output 為 `table`：
   a. 呼叫 `ProjectService.GetCredentials()` 取得 `*CredentialsView`
   b. 呼叫 `writeCreateSummary()` 輸出連線摘要
   c. 若 `GetCredentials` 呼叫失敗，印出 warning 到 stderr，不影響 exit code
4. 若 output 為 `json` 或 `yaml`：不額外呼叫（`ProjectView` JSON 已含 URLs）

### `sbctl project get <slug>` 流程（增強後）

1. CLI 呼叫 `ProjectService.Get()` → 取得 `*ProjectView`（含 URLs）
2. 呼叫 `writeProjectView()` 輸出基本資訊 + URLs 區段

### 輸出範例

#### `sbctl project create my-app -n "My App"` (table)

```
SLUG          DISPLAY NAME  STATUS   UPDATED
my-app        My App        stopped  2025-01-15T10:30:00Z

URLs:
  API     http://localhost:54321
  Studio  http://localhost:54321

Connection Info:
  API URL      http://localhost:54321
  Anon Key     eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
  DB Host      localhost
  DB Port      54322
  DB Password  postgres-password-here

  Run 'sbctl project credentials my-app' for full credentials.
```

> 注意：API 與 Studio URL 相同（皆透過 Kong reverse proxy），這是現有 `buildURLs()` 的行為。Inbucket URL 目前不填充，故不顯示。

#### `sbctl project get my-app` (table)

```
SLUG          DISPLAY NAME  STATUS   UPDATED
my-app        My App        running  2025-01-15T10:35:00Z

URLs:
  API     http://localhost:54321
  Studio  http://localhost:54321
```

#### `sbctl project create my-app -n "My App" -o json`

```json
{
  "slug": "my-app",
  "display_name": "My App",
  "status": "stopped",
  "urls": {
    "api": "http://localhost:54321",
    "studio": "http://localhost:54321",
    "inbucket": ""
  },
  ...
}
```

---

## 錯誤處理

| 情境 | 處理方式 | 回應格式 |
|---|---|---|
| `Create()` 失敗 | 回傳 error，與既有行為一致 | `projectErr(cmd, err)` |
| `GetCredentials()` 失敗（create 後） | 印出 warning 到 stderr，exit code 0 | `Warning: Could not retrieve credentials: ...` |
| `URLs` 為 nil（`List()` 回傳的 view） | 不顯示 URLs 區段 | — |
| `Get()` 回傳的 view URLs 為 nil | 不顯示 URLs 區段 | — |

---

## 測試策略

### 需要測試的行為

- `writeProjectView` table 格式在 URLs 存在時顯示 URLs 區段
- `writeProjectView` table 格式在 URLs 為 nil 時不顯示 URLs 區段
- `writeProjectView` JSON/YAML 格式不受影響（透過 struct tag 自動處理）
- `writeCreateSummary` 正確顯示所有連線欄位
- `writeCreateSummary` 末尾提示 `sbctl project credentials` 命令
- `writeProjectView` table 格式在 URLs 欄位為空字串時不印出該欄位
- `writeCreateSummary` 回傳 error（與既有 write 函式慣例一致）
- `create` RunE 在 table 格式時呼叫 `GetCredentials()` 並顯示摘要
- `create` RunE 在 JSON/YAML 格式時不呼叫 `GetCredentials()`
- `create` RunE 在 `GetCredentials()` 失敗時印出 warning 且 exit code 為 0
- 既有 `create` 與 `get` 測試在新增 URLs 顯示後仍通過

### 測試類型分配

| 測試類型 | 測試目標 | 層次 |
|---|---|---|
| 單元測試 | `writeProjectView` URLs 區段（有/無 URLs） | `cmd/sbctl` |
| 單元測試 | `writeCreateSummary` 輸出格式 | `cmd/sbctl` |
| 單元測試 | `create` RunE Credentials 失敗時的 warning 行為 | `cmd/sbctl` |
| 單元測試 | 既有 `create`/`get` 測試向後相容 | `cmd/sbctl` |

### Mock 策略

- `ProjectService.Create()` — 使用既有 mock 介面（`main_test.go` 中的 `mockSvc`）
- `ProjectService.GetCredentials()` — 使用既有 mock 介面（`mockSvc.CredentialsFn`）
- `ProjectService.Get()` — 使用既有 mock 介面

### CI 執行方式

- 所有測試在一般 CI 中執行，不需特殊環境

---

## Production Ready 考量

### 錯誤處理

`GetCredentials()` 失敗不影響 `create` 的成功回報。Warning 印到 stderr，不干擾 stdout 的結構化輸出（JSON/YAML 管線安全）。

### 日誌與可觀測性

不需日誌。展示層變更。

### 輸入驗證

不新增輸入。使用既有的 `slug` 參數。

### 安全性

`writeCreateSummary` 會顯示敏感資訊（Anon Key、DB Password）。這與 `sbctl project credentials` 的行為一致——CLI 是本地工具，使用者已有主機存取權。此處僅為簡化操作流程，無額外安全風險。

### 優雅降級

`GetCredentials()` 呼叫失敗時，create 仍成功回報，使用者可另外呼叫 `credentials` 指令取得完整資訊。

### 設定管理

無新增設定。

---

## 待決問題

- 無

---

## 審查

### Reviewer A（架構）

- **狀態：** ✅ APPROVED（附帶一項非阻塞修正）

#### Round 1 阻塞問題驗證

| # | 問題 | 狀態 |
|---|---|---|
| A-1 | `Credentials()` → `GetCredentials()` | ✅ 已修正（程式碼第 146 行、範圍描述第 36 行） |
| A-2 | `writeProjectView` 簽名含不存在的 `*colorer` | ✅ 已修正（第 85 行使用正確簽名，`*colorer` 僅作為未來附註） |
| A-3 | `handleError(err)` → `projectErr(cmd, err)` | ✅ 已修正（第 138 行） |
| A-4 | 輸出範例 URL 重複/不準確（非阻塞） | ✅ 已修正（範例統一為 `:54321`，附註說明 `buildURLs()` 行為） |

#### 🟡 殘留問題（非阻塞）

**A-5. Production Ready 區段方法名稱殘留**

第 283 行「`Credentials()` 失敗不影響…」仍使用舊名稱，應改為 `GetCredentials()`。同區段第 299 行已正確使用 `GetCredentials()`，此處為遺漏。屬散文描述非程式碼，不影響實作，但建議一併修正保持一致。

#### 架構評估

- **職責分離**：`writeProjectView`（URLs 展示）與 `writeCreateSummary`（Credentials 摘要）職責清晰，符合單一職責原則 ✅
- **錯誤降級策略**：`GetCredentials()` 失敗時 warning 到 stderr、exit code 0，不阻斷 create 成功回報，設計合理 ✅
- **輸出格式隔離**：Credentials 呼叫僅限 `table` 格式，JSON/YAML 管線不受影響 ✅
- **Inbucket 防禦性檢查**：空字串跳過顯示，為未來擴充留出空間 ✅
- **`writeCreateSummary` 回傳 error**：與既有 `writeProjectView`、`writeCredentialsView` 慣例一致 ✅

### Reviewer B（實作）

- **狀態：** ✅ APPROVED
- **意見：**

#### Round 2 審查（驗證 Round 1 修正）

| Round 1 Issue | 狀態 | 驗證結果 |
|---|---|---|
| B-1. `Credentials()` → `GetCredentials()` | ✅ 已修正 | 第 146 行已使用正確方法名 `GetCredentials()` |
| B-2. `handleError()` → `projectErr()` | ✅ 已修正 | 第 138 行已使用 `projectErr(cmd, err)` |
| B-3. `writeCreateSummary` 回傳 `error` | ✅ 已修正 | 第 115 行簽名已改為回傳 `error`，第 152 行呼叫端正確處理 |
| B-4. Inbucket 空值條件印出 | ✅ 已修正 | 第 98-99 行加入 `if view.URLs.Inbucket != ""` 條件檢查 |
| B-5. `colorer` 參數策略 | ✅ 已修正 | 簽名不含 `colorer`，以備註說明 Feature 1 合併後同步更新 |
| B-6. 輸出範例 port 不正確 | ✅ 已修正 | API 與 Studio 皆為 `:54321`，Inbucket 不顯示，符合實際行為 |

#### 🟡 微幅瑕疵（不阻擋合併）

**B-9. 「Production Ready 考量 → 錯誤處理」段落仍寫 `Credentials()`**

第 283 行散文中寫「`Credentials()` 失敗不影響…」，應為 `GetCredentials()` 以保持全文一致。此為文字層面，不影響實作正確性，可於實作 PR 中一併修正。

#### 總結

Round 1 提出的 6 項問題已全數修正。程式碼片段與 codebase 一致，輸出範例反映 `buildURLs()` 的實際行為，測試策略已補充 `writeCreateSummary` 回傳 error 的測試項目（第 252 行）。設計可進入實作階段。

---

## 任務

### T-F2-1: writeProjectView 新增 URL 顯示（`f2-url-display`）

**依賴：** T-F1-2（colorer 整合後 writeProjectView 簽名已含 `*colorer`）

**建立/修改檔案：**
- 修改 `cmd/sbctl/output.go` — `writeProjectView` 在 table 格式時顯示 URLs 區段
- 修改 `cmd/sbctl/main_test.go` — 新增 URL 顯示測試

**實作內容：**
1. `output.go`：在 `writeProjectView` 的 table 格式分支中，於 tabwriter flush 後：
   - 若 `view.URLs != nil`，印出 `\nURLs:\n`
   - 印出 `  API     {url}`、`  Studio  {url}`
   - 若 Inbucket URL 非空，印出 `  Inbucket {url}`
2. 測試：
   - URL 存在時 table 輸出含 `URLs:` 區段
   - URL 為 nil 時不顯示 URLs 區段
   - Inbucket 為空時不顯示該行
   - JSON/YAML 格式不受影響

**驗收標準：**
- `go build ./...` 通過
- `go test -race ./...` 通過

---

### T-F2-2: create 命令連線資訊摘要（`f2-create-summary`）

**依賴：** T-F2-1

**建立/修改檔案：**
- 新增函式 `writeCreateSummary()` 於 `cmd/sbctl/output.go`
- 修改 `cmd/sbctl/project.go` — create RunE 呼叫 `GetCredentials()` 並顯示摘要
- 修改 `cmd/sbctl/main_test.go` — 新增 create summary 測試

**實作內容：**
1. `output.go`：
   - `func writeCreateSummary(w io.Writer, creds *usecase.CredentialsView) error`
   - 印出 Connection Info 區段：API URL、Anon Key、DB Host、DB Port、DB Password
   - 末行提示：`Run 'sbctl project credentials {slug}' for full credentials.`
2. `project.go`：create RunE 修改：
   - `writeProjectView()` 後，若 `output == "table"`：
   - 呼叫 `svc.GetCredentials(ctx, slug)` 取得 `*CredentialsView`
   - 成功：呼叫 `writeCreateSummary(w, creds)`
   - 失敗：`fmt.Fprintf(cmd.ErrOrStderr(), "Warning: ...%v\n", err)`，不影響 exit code
3. 測試：
   - `writeCreateSummary` 輸出含所有欄位
   - create 成功時 table 格式顯示 Connection Info
   - create 成功但 `GetCredentials` 失敗時顯示 warning、exit code 0
   - create JSON/YAML 格式不呼叫 `GetCredentials`

**驗收標準：**
- `go build ./...` 通過
- `go test -race ./...` 通過

---

## 程式碼審查

- **審查結果：** ✅ APPROVED（第二輪）
- **發現問題：** R1: watch-timeout 未套用 context、gofmt 格式問題、writeCreateSummary 錯誤忽略
- **修正記錄：** 7f163fc fix watch-timeout & gofmt; cd2480d fix writeCreateSummary error handling
