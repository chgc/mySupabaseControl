> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# 設計文件：CLI — `sbctl`

## 狀態

approved

## Phase

- **Phase：** Phase 3
- **Phase Plan：** `docs/designs/phase-3-plan.md`

---

## 目的

提供終端機操作介面，讓使用者透過 `sbctl` CLI 管理 Supabase 專案的完整生命週期。
CLI 直接呼叫 `usecase.ProjectService`，不經過 HTTP。

---

## 範圍

### 包含

- `sbctl` 二進位（`cmd/sbctl/main.go`）
- `project` 子命令群組（7 個命令）
- 全域旗標 `--output [table|json|yaml]`（預設 `table`）
- `--db-url` 全域旗標（PostgreSQL DSN，必要）
- 依賴注入：在 `cmd/sbctl/deps.go` 的 `BuildDeps()` 函數中組裝，CLI 與 MCP server 共用
- 結構化輸出：table（人類可讀）、json（AI agent / script）、yaml
- `--version` 旗標（從 ldflags 注入版本字串）

### 不包含

- MCP Server（`sbctl mcp serve`，屬功能 3）
- 互動式 TUI 模式
- Shell completion（可後續添加）
- 認證 / multi-user 存取控制

---

## 依賴變更

```
需新增至 go.mod（go get）：
  github.com/spf13/cobra v1.9.x  （或最新穩定版）

需提升為 direct dependency：
  gopkg.in/yaml.v3               （目前為 indirect，已存在）
```

---

## 資料模型

CLI 輸出使用 `usecase.ProjectView`（由 use-case 層回傳）。
不定義額外的 CLI 專屬資料結構。

實際 `ProjectView` 定義（以 `internal/usecase/project_service.go` 為準）：

```go
type ProjectView struct {
    Slug           string            `json:"slug"`
    DisplayName    string            `json:"display_name"`
    Status         string            `json:"status"`
    PreviousStatus string            `json:"previous_status,omitempty"`
    LastError      string            `json:"last_error,omitempty"`
    CreatedAt      time.Time         `json:"created_at"`
    UpdatedAt      time.Time         `json:"updated_at"`
    Health         *HealthView       `json:"health,omitempty"`
    Config         map[string]string `json:"config,omitempty"`
    URLs           *ProjectURLs      `json:"urls,omitempty"`
}
```

`table` 輸出顯示欄位：SLUG、DISPLAY NAME、STATUS、UPDATED（`UpdatedAt`）。

---

## 共用 Wiring（deps.go）

CLI 與 MCP server 共用同一套依賴初始化邏輯，定義於 `cmd/sbctl/deps.go`：

```go
// Deps 持有所有已初始化的依賴。
type Deps struct {
    ProjectService usecase.ProjectService
}

// BuildDeps 根據設定組裝所有依賴。
// 若任何初始化步驟失敗，回傳 error。
func BuildDeps(ctx context.Context, dbURL, projectsDir string) (*Deps, error) {
    // 1. 建立 pgx 連線池
    // 2. 建立 store（postgres）
    // 3. 建立 ComposeAdapter
    // 4. 建立 ComposePortAllocator
    // 5. 建立 SecretGenerator
    // 6. NewProjectService(Config{...})
    return &Deps{ProjectService: svc}, nil
}
```

CLI 命令在 `PersistentPreRunE` 中呼叫 `BuildDeps()`，並以 closure 將 `deps.ProjectService` 注入子命令。

---

## 介面合約

### 命令結構

```
sbctl [global flags] <command> [args] [flags]

全域旗標：
  --output, -o  string   輸出格式：table | json | yaml（預設：table）
  --db-url      string   PostgreSQL DSN（env: SBCTL_DB_URL）
  --projects-dir string  專案目錄根路徑（env: SBCTL_PROJECTS_DIR，預設：./projects）

project 子命令：
  sbctl project create <slug> --display-name <name>
  sbctl project list
  sbctl project get <slug>
  sbctl project start <slug>
  sbctl project stop <slug>
  sbctl project reset <slug>
  sbctl project delete <slug> [--force]
```

### 各命令旗標

| 命令 | 旗標 | 說明 |
|------|------|------|
| `create` | `--display-name, -n string` | 專案顯示名稱（必要） |
| `delete` | `--force` | 略過確認提示 |
| 其他 | — | 無額外旗標 |

### 輸出格式

**`table`（預設）：**
```
SLUG         DISPLAY NAME    STATUS    UPDATED
my-project   My Project      stopped   2026-03-25T12:00:00Z
```
使用 `text/tabwriter`（標準庫），減少外部依賴。使用方式：
```go
w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
fmt.Fprintln(w, "SLUG\tDISPLAY NAME\tSTATUS\tUPDATED")
for _, p := range projects {
    fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.Slug, p.DisplayName, p.Status, p.UpdatedAt.Format(time.RFC3339))
}
w.Flush() // 必須呼叫
```

**`json`：**
```json
{"slug":"my-project","display_name":"My Project","status":"stopped",...}
```
（直接序列化 `usecase.ProjectView`）

**`yaml`：**
```yaml
slug: my-project
display_name: My Project
status: stopped
```
（使用 `gopkg.in/yaml.v3`）

**`project list` 空結果**：輸出空 table header（table）或 `[]`（json/yaml）。

### delete 成功輸出

`project delete` 成功時輸出確認訊息（非 ProjectView）：
- `table`：`Deleted project "slug".`（輸出至 stdout）
- `json`：`{"deleted": true, "slug": "my-project"}`
- `yaml`：`deleted: true\nslug: my-project`

### 結束碼

| 情況 | Exit Code |
|------|-----------|
| 成功 | 0 |
| 取消操作（delete 未確認） | 0 |
| 使用者錯誤（invalid input, not found, invalid state） | 1 |
| 系統錯誤（internal error, db unavailable） | 2 |

---

## 執行流程

### 啟動流程

**重要：不在 `main()` 頂層建立 DB 連線**，避免 `sbctl --help` 等不需要 DB 的命令也觸發連線。

```
main()
  ↓ 定義 rootCmd（persistent flags: --output, --db-url, --projects-dir）
  ↓ 以 closure capture 模式預建 project 子命令群組
  ↓ rootCmd.AddCommand(projectCmd)
  ↓ rootCmd.ExecuteContext(ctx)

rootCmd.PersistentPreRunE（每個真實命令執行前觸發，--help 除外）：
  ↓ 驗證 --output 值（只接受 table | json | yaml）
  ↓ 驗證 --db-url 不為空（若無 --db-url 也無 SBCTL_DB_URL，回傳 error，exit 1）
  ↓ 呼叫 BuildDeps(ctx, dbURL, projectsDir)
  ↓ 將 deps 存入 closure 供子命令使用
```

**`--db-url` 不使用 `cobra.MarkPersistentFlagRequired`**，因為該方式會讓 `--help` 也要求此旗標。改以 `PersistentPreRunE` 手動驗證。

### 子命令注入模式（Closure Capture）

```go
// main.go
func buildRootCmd() *cobra.Command {
    var (
        dbURL       string
        projectsDir string
        output      string
        deps        *Deps
    )

    root := &cobra.Command{Use: "sbctl", Version: version}
    root.SilenceErrors = true  // 防止 cobra 重複印 error（main() 統一處理）
    root.SilenceUsage = true   // RunE 出錯時不自動印 usage

    root.PersistentFlags().StringVar(&dbURL, "db-url", os.Getenv("SBCTL_DB_URL"), "PostgreSQL DSN")
    root.PersistentFlags().StringVar(&projectsDir, "projects-dir", envOr("SBCTL_PROJECTS_DIR", "./projects"), "")
    root.PersistentFlags().StringVarP(&output, "output", "o", "table", "Output format: table|json|yaml")

    root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
        if err := validateOutput(output); err != nil {
            fmt.Fprintln(cmd.ErrOrStderr(), "Error:", err)
            return &ExitError{Code: 1, Err: err}
        }
        if dbURL == "" {
            err := fmt.Errorf("--db-url or SBCTL_DB_URL is required")
            fmt.Fprintln(cmd.ErrOrStderr(), "Error:", err)
            return &ExitError{Code: 1, Err: err}
        }
        var err error
        deps, err = BuildDeps(cmd.Context(), dbURL, projectsDir)
        if err != nil {
            fmt.Fprintln(cmd.ErrOrStderr(), "Error:", err)
            return &ExitError{Code: 2, Err: err}  // DB 連線失敗 = 系統錯誤
        }
        return nil
    }

    // 子命令透過 closure 捕捉 deps 與 output
    root.AddCommand(buildProjectCmd(&deps, &output))
    return root
}

func buildProjectCmd(deps **Deps, output *string) *cobra.Command {
    cmd := &cobra.Command{Use: "project"}
    // ⚠️ 子命令禁止定義自己的 PersistentPreRunE。
    // Cobra 的 PersistentPreRunE 不會自動串接（chain）——子命令的 PersistentPreRunE
    // 會完全覆蓋父命令的，導致 BuildDeps 被靜默跳過（deps 為 nil → panic）。
    // 若未來需要子命令前置邏輯，請抽出 helper function 供兩層分別呼叫。
    cmd.AddCommand(buildListCmd(deps, output))
    // ...
    return cmd
}
```

### Exit Code 實作

`RunE` 只能回傳 `error`，exit code 需透過自訂 error type 傳遞至 `main()`。

**stderr 輸出策略（模式 A）**：
- `RunE` 與 `PersistentPreRunE` **主動寫入** `cmd.ErrOrStderr()`（所有錯誤訊息在此輸出）
- `main()` **不重複 Fprintln**，僅依 ExitError.Code 決定 os.Exit

```go
// ExitError 將 exit code 與 error 綁定。
// Err 欄位僅供 errors.As 辨別 code；訊息已由 RunE 寫入 stderr，不重複印出。
type ExitError struct {
    Code int
    Err  error
}
func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }

// main() 統一處理 exit code（不重複印訊息）
func main() {
    cmd := buildRootCmd()
    if err := cmd.Execute(); err != nil {
        var exitErr *ExitError
        if errors.As(err, &exitErr) {
            os.Exit(exitErr.Code)
        }
        os.Exit(1) // fallback（理論上不應觸發）
    }
}
```

`RunE` 中固定模式：
```go
// 使用者錯誤（exit 1）
fmt.Fprintln(cmd.ErrOrStderr(), "Error:", usecaseErr.Message)
return &ExitError{Code: 1, Err: usecaseErr}

// 系統錯誤（exit 2）
fmt.Fprintln(cmd.ErrOrStderr(), "Error: internal error (check logs for details)")
return &ExitError{Code: 2, Err: usecaseErr}
```

**exit code 對應原則：**
- 使用者錯誤（not_found, conflict, invalid_input, invalid_state）→ `ExitError{Code: 1}`
- 系統錯誤（internal error, DB 連線失敗）→ `ExitError{Code: 2}`
- `--db-url` 缺少（使用者未設定）→ `ExitError{Code: 1}`

### project create

```
1. 解析 <slug> 與 --display-name
2. svc.Create(ctx, slug, displayName)
3. 成功：輸出 ProjectView，exit 0
4. ErrCodeConflict：stderr 錯誤訊息，exit 1
5. ErrCodeInvalidInput：stderr 錯誤訊息，exit 1
6. ErrCodeInternal：stderr "internal error (check logs for details)"，exit 2
```

### project delete（需確認）

stdin 讀取透過 `cmd.InOrStdin()`（cobra 內建，預設 `os.Stdin`，測試時可注入）：
```
1. 解析 <slug>
2. 若無 --force：
   a. fmt.Fprintf(cmd.OutOrStdout(), "Delete project '%s'? [y/N]: ", slug)
   b. fmt.Fscan(cmd.InOrStdin(), &confirm)
   c. 若非 y/Y：fmt.Fprintln(cmd.OutOrStdout(), "Cancelled.")，exit 0
3. svc.Delete(ctx, slug)
4. 輸出刪除確認訊息（見「delete 成功輸出」章節）
```

---

## 錯誤處理

**原則：**
- 正常輸出至 **stdout**（`cmd.OutOrStdout()`）
- 錯誤訊息輸出至 **stderr**（`cmd.ErrOrStderr()`）
- 無論 `--output` 格式為何，**stderr 固定輸出純文字**；shell script 應以 exit code 判斷成敗

| UsecaseError.Code | CLI 行為 | stderr 訊息 | Exit Code |
|------------------|---------|------------|-----------|
| not_found | 印出錯誤訊息 | `Error: project "X" not found` | 1 |
| conflict | 印出錯誤訊息 | `Error: project "X" already exists` | 1 |
| invalid_input | 印出錯誤訊息 | `Error: <message>` | 1 |
| invalid_state（create/start/stop/reset） | 印出錯誤訊息 | `Error: project "X" cannot be <op> from status "Y"` | 1 |
| invalid_state（delete） | 印出錯誤訊息 | `Error: project "X" cannot be deleted from status "Y"` | 1 |
| internal | 印出精簡訊息 | `Error: internal error (check logs for details)` | 2 |

---

## 測試策略

### 測試工具選擇

**不使用 `os.Exit`**。所有子命令的 `RunE` 回傳 `error`，由 cobra 處理。
測試中使用 `cmd.SetOut`、`cmd.SetErr`、`cmd.SetIn` 注入 buffer，直接斷言輸出內容。

```go
// newTestRootCmd 建立測試用的 root command：
// - 覆寫 PersistentPreRunE 為 no-op，跳過 DB 初始化
// - 直接將已初始化的 mock *Deps 寫入 closure，不呼叫 BuildDeps()
func newTestRootCmd(svc usecase.ProjectService) *cobra.Command {
    deps := &Deps{ProjectService: svc}
    output := "table"

    root := &cobra.Command{Use: "sbctl"}
    root.SilenceErrors = true
    root.SilenceUsage = true
    root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error { return nil }
    root.PersistentFlags().StringVarP(&output, "output", "o", "table", "")

    root.AddCommand(buildProjectCmd(&deps, &output))
    return root
}

func runCmd(t *testing.T, cmd *cobra.Command, args []string, stdin string) (stdout, stderr string, err error) {
    outBuf, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
    cmd.SetOut(outBuf)
    cmd.SetErr(errBuf)
    cmd.SetIn(strings.NewReader(stdin))
    cmd.SetArgs(args)
    err = cmd.Execute()
    return outBuf.String(), errBuf.String(), err
}
```

### 需要測試的行為

- 各命令的 flag 解析（slug 必填、display-name 必填）
- `--output` 旗標影響輸出格式（table / json / yaml）
- `RunE` 回傳的 `ExitError.Code` 對應正確（1 或 2）
- stderr 輸出的錯誤訊息格式（透過 `cmd.ErrOrStderr()` 注入 buffer 驗證）
- `project delete` + `--force`：略過確認直接刪除
- `project delete` 無 `--force`：stdin 輸入 "y" → 刪除；"N" → 取消
- JSON 輸出可被 `json.Unmarshal` 解析

**`validateOutput()` 與 `--db-url` 驗證的測試方式：**
- `validateOutput()` 直接以 unit test 呼叫（純函數，不需 cobra）
- `PersistentPreRunE` 中 `--db-url` 缺少的行為：直接測試 PersistentPreRunE closure，
  或建立不覆寫 PersistentPreRunE 的測試 helper（傳入空 db-url，驗證 errBuf 輸出與 ExitError.Code）

### Mock 策略

- `usecase.ProjectService` → 手工 mock（function-pointer struct），與現有風格一致
- 測試中注入 mock，不呼叫 `BuildDeps()`，不需要真實 DB 或 Docker

### CI 執行方式

- `go test -race ./cmd/sbctl/...`（一般 CI，不需特殊環境）

---

## Production Ready 考量

### 錯誤處理
- 使用者錯誤（exit 1）與系統錯誤（exit 2）分開，方便 shell script 判斷
- Internal 錯誤不暴露底層細節至 stderr
- stderr 固定純文字，不受 `--output` 影響

### 日誌與可觀測性
- CLI 不輸出結構化 log（那是 use-case 層的責任）
- `--verbose` 旗標可於後續版本加入

### 輸入驗證
- Slug 驗證由 use-case 層負責，CLI 只傳入原始字串
- `--display-name` 為必填旗標（`cobra.MarkFlagRequired`，僅 `create` 子命令層級）
- `--output` 在 `PersistentPreRunE` 驗證
- `--db-url` 在 `PersistentPreRunE` 驗證（不使用 `MarkPersistentFlagRequired`）

### 安全性
- `--db-url` 支援從環境變數 `SBCTL_DB_URL` 讀取，避免明文出現在 shell history
- JSON 輸出的 sensitive config 由 use-case 層 masked，CLI 不額外處理

### 優雅降級
- DB 連線失敗：在 `PersistentPreRunE` 回傳 error，cobra 輸出至 stderr，exit 1 或 2

### 設定管理
- 必要設定：`--db-url`（或 `SBCTL_DB_URL`）
- 可選設定：`--projects-dir`（預設 `./projects`）、`--output`（預設 `table`）
- `--version`：從 `ldflags` 注入（`-ldflags "-X main.version=0.1.0"`）

---

## 審查

### Reviewer A（架構）

- **狀態：** APPROVED（第二輪）
- **意見：** N1-N6 全部解決。N7（testRootCmd 繞過說明）為輕微非阻斷問題，實作時補充即可。

### Reviewer B（實作）

- **狀態：** APPROVED（第四輪）
- **意見：** N1-N14 全部解決。N15（no-op PersistentPreRunE 注釋）為提示，實作時補充即可。

---

## 任務

<!-- 審查通過後展開 -->

---

## 程式碼審查

- **審查結果：** <!-- PASS | FIX_REQUIRED -->
- **發現問題：**
- **修正記錄：**
