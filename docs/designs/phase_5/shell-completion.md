> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# 設計文件：Shell 補全

## 狀態

done

## Phase

- **Phase：** Phase 5
- **Phase Plan：** docs/designs/phase-5-plan.md

---

## 目的

目前 `sbctl` 的所有命令、子命令與旗標都需要完整手動輸入。使用者無法透過 Tab 鍵補全命令名稱、project slug 或旗標，降低了操作效率。

本功能新增 `sbctl completion` 子命令，產生 bash、zsh、fish 三種 shell 的補全腳本。補全範圍包含：

1. **靜態補全** — 命令名稱（`project`、`mcp`）、子命令名稱（`create`、`list`、`get` 等）、旗標名稱（`--output`、`--no-color` 等）
2. **動態補全** — project slug（需要 project 名稱作為參數的子命令中，Tab 鍵列出現有 slug）
3. **旗標值補全** — `--output` 的可選值（`table`、`json`、`yaml`）、`--runtime` 的可選值（`docker-compose`、`kubernetes`）

---

## 範圍

### 包含

- `sbctl completion bash` — 輸出 bash 補全腳本
- `sbctl completion zsh` — 輸出 zsh 補全腳本
- `sbctl completion fish` — 輸出 fish 補全腳本
- 動態 project slug 補全（`ValidArgsFunction`）
- `--output` 旗標值補全（`table`、`json`、`yaml`）
- `--runtime` 旗標值補全（`docker-compose`、`kubernetes`）
- 使用說明（安裝方式）顯示在 `sbctl completion --help` 中

### 不包含

- 自動安裝補全腳本（使用者需手動 `source` 或放入補全目錄）
- PowerShell 補全（目前專案不在 Windows 上使用）
- MCP serve 子命令的參數補全（無需補全的參數）

---

## 資料模型

本功能不修改任何資料模型。

---

## 介面合約

### 新增 CLI 命令

```
sbctl completion
├── bash    # 輸出 bash completion 腳本到 stdout
├── zsh     # 輸出 zsh completion 腳本到 stdout
└── fish    # 輸出 fish completion 腳本到 stdout
```

### Cobra 內建補全機制

使用 Cobra v1.10.2 內建的補全功能：

```go
// completion 子命令（Cobra 內建 GenBashCompletionV2、GenZshCompletion、GenFishCompletion）
func buildCompletionCmd() *cobra.Command
```

### 動態 slug 補全函式

```go
// projectSlugCompletion returns a ValidArgsFunction that queries
// the database for existing project slugs.
// If deps is nil or DB is unavailable, returns an empty list (graceful degradation).
func projectSlugCompletion(deps **Deps, dbURL *string) func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective)
```

此函式會：
1. 檢查 `*deps != nil`，若是，直接呼叫 `ProjectService.List()`
2. 若 `*deps == nil`（completion 模式，`PersistentPreRunE` 被跳過）：
   - 檢查 `*dbURL` 是否非空
   - 建立帶 3 秒 timeout 的輕量 DB 連線
   - 直接查詢 slug 列表（不需完整 service 層）
   - 任何失敗靜默回傳空列表 + `ShellCompDirectiveNoFileComp`
3. 以 `toComplete` 前綴過濾 slug
4. 回傳匹配的 slug 列表，附帶 display name 作為描述

### 旗標值補全

```go
// 在 buildRootCmd() 定義 --output 後立即註冊：
rootCmd.RegisterFlagCompletionFunc("output",
    func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
        return []string{"table", "json", "yaml"}, cobra.ShellCompDirectiveNoFileComp
    })

// 在 buildCreateCmd() 定義 --runtime 後立即註冊：
cmd.RegisterFlagCompletionFunc("runtime",
    func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
        return []string{"docker-compose", "kubernetes"}, cobra.ShellCompDirectiveNoFileComp
    })
```

### 命令掛載點

在 `buildRootCmd()` 中新增：

```go
root.AddCommand(buildCompletionCmd())
```

置於現有 `root.AddCommand(buildProjectCmd(...))` 與 `root.AddCommand(buildMCPCmd())` 之後。

### 需設定 ValidArgsFunction 的命令

| 命令 | 補全內容 | 說明 |
|------|---------|------|
| `project get <slug>` | 動態 slug | 列出所有既有 slug |
| `project start <slug>` | 動態 slug | 列出所有既有 slug |
| `project stop <slug>` | 動態 slug | 列出所有既有 slug |
| `project reset <slug>` | 動態 slug | 列出所有既有 slug |
| `project delete <slug>` | 動態 slug | 列出所有既有 slug |
| `project credentials <slug>` | 動態 slug | 列出所有既有 slug |
| `project create <slug>` | 無補全 | slug 是新建的名稱，無法預測 |

---

## 執行流程

### `PersistentPreRunE` 相容策略

**關鍵問題：** 目前 `main.go` 的 `PersistentPreRunE` 要求 `--db-url`，且 Cobra 的 `__complete` 隱藏命令會繼承執行此 hook。若 `SBCTL_DB_URL` 未設定，所有補全（包含靜態命令/旗標補全）都會失敗。

**解決方案：** 在 `PersistentPreRunE` 中偵測 completion 相關命令，跳過 `BuildDeps`：

```go
root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
    // Skip dependency initialization for completion commands.
    // Cobra's __complete hidden command and 'completion' subcommands
    // do not need DB access for static completions.
    if cmd.Name() == cobra.ShellCompRequestCmd || isCompletionCmd(cmd) {
        return nil
    }
    // ... existing validation and BuildDeps logic ...
}

// isCompletionCmd checks if cmd or any of its parents is the completion command.
func isCompletionCmd(cmd *cobra.Command) bool {
    for c := cmd; c != nil; c = c.Parent() {
        if c.Name() == "completion" {
            return true
        }
    }
    return false
}
```

### 補全腳本產生流程

1. 使用者執行 `sbctl completion bash`
2. Cobra 呼叫 `rootCmd.GenBashCompletionV2(os.Stdout, true)`
3. 腳本輸出到 stdout
4. 使用者將腳本 source 到 shell（例如 `source <(sbctl completion bash)`）

### 動態 slug 補全流程（Runtime）

1. 使用者輸入 `sbctl project get my-<TAB>`
2. Shell 呼叫 `sbctl __complete project get my-`
3. `PersistentPreRunE` 偵測到 `__complete` 命令，跳過 `BuildDeps`（`deps == nil`）
4. `projectSlugCompletion` 被呼叫：
   a. 檢查 `deps == nil`，若是則嘗試僅建立輕量 DB 連線：
      - 讀取 `dbURL`（已由 Cobra 從旗標/env 解析）
      - 若 `dbURL` 為空 → 回傳空列表 + `ShellCompDirectiveNoFileComp`
      - 建立帶 3 秒 timeout 的 context
      - 直接呼叫 `ProjectRepository.ListSlugs(ctx)` 取得 slug 列表（不需完整 `BuildDeps`）
      - 若任何步驟失敗 → 回傳空列表 + `ShellCompDirectiveNoFileComp`（靜默降級）
   b. 以 `toComplete` 前綴過濾 slug
5. 回傳結果：`my-app\tMy Application\nmy-test\tTest Project`
6. Shell 顯示補全候選

### 動態補全 timeout 策略

- 所有動態補全的 DB 查詢使用 3 秒 timeout（`context.WithTimeout`）
- 超時時靜默回傳空列表，不影響 shell 操作
- 3 秒對本地 DB 查詢綽綽有餘，但能防止 DB 無回應時的長時間阻塞

### 安裝方式

`sbctl completion --help` 顯示安裝指引：

```
# Bash（加入 ~/.bashrc）
echo 'source <(sbctl completion bash)' >> ~/.bashrc

# Zsh（加入 ~/.zshrc）
echo 'source <(sbctl completion zsh)' >> ~/.zshrc

# Fish
sbctl completion fish > ~/.config/fish/completions/sbctl.fish
```

---

## 錯誤處理

| 情境 | 處理方式 | 回應格式 |
|---|---|---|
| DB 連線失敗（動態 slug 補全時） | 回傳空列表，靜默降級 | `([]string{}, cobra.ShellCompDirectiveNoFileComp)` |
| `dbURL` 未設定（補全模式下） | 回傳空列表，靜態補全仍正常 | `([]string{}, cobra.ShellCompDirectiveNoFileComp)` |
| DB 查詢超時（>3 秒） | context 取消，回傳空列表 | `([]string{}, cobra.ShellCompDirectiveNoFileComp)` |
| `ProjectService.List()` 回傳錯誤 | 回傳空列表，靜默降級 | `([]string{}, cobra.ShellCompDirectiveNoFileComp)` |
| `*deps == nil`（PersistentPreRunE 被跳過） | 嘗試輕量 DB 連線；失敗則空列表 | `([]string{}, cobra.ShellCompDirectiveNoFileComp)` |
| 無任何專案存在 | 回傳空列表 | `([]string{}, cobra.ShellCompDirectiveNoFileComp)` |

---

## 測試策略

### 需要測試的行為

- `buildCompletionCmd()` 存在且正確掛載到 rootCmd（`root.AddCommand`）
- `PersistentPreRunE` 在偵測到 `__complete` 命令時跳過 `BuildDeps`（不要求 `--db-url`）
- `PersistentPreRunE` 在偵測到 `completion` 子命令時跳過 `BuildDeps`
- `sbctl completion bash` 在 `SBCTL_DB_URL` 未設定時仍能產生腳本
- `sbctl completion bash` 產生的腳本包含 `_sbctl` 或 `sbctl` completion function 關鍵字
- `sbctl completion zsh` 產生的腳本包含 `#compdef` 或 `compdef` 關鍵字
- `sbctl completion fish` 產生的腳本包含 `complete` 關鍵字
- `projectSlugCompletion` 在 `*deps != nil` 時回傳正確的 slug 列表
- `projectSlugCompletion` 在 `*deps == nil` 且 DB 可用時回傳正確的 slug 列表
- `projectSlugCompletion` 在 `*deps == nil` 且 DB 不可用時回傳空列表（不 panic）
- `projectSlugCompletion` 在 `toComplete` 有前綴時正確過濾
- `projectSlugCompletion` 在 DB 查詢超時時回傳空列表
- `--output` 旗標補全回傳 `["table", "json", "yaml"]`
- `--runtime` 旗標補全回傳 `["docker-compose", "kubernetes"]`

### 測試類型分配

| 測試類型 | 測試目標 | 層次 |
|---|---|---|
| 單元測試 | `projectSlugCompletion` 邏輯（正常、過濾、錯誤、nil deps） | `cmd/sbctl` |
| 單元測試 | `PersistentPreRunE` + `__complete` 命令跳過 BuildDeps | `cmd/sbctl` |
| 單元測試 | `PersistentPreRunE` + `completion` 子命令跳過 BuildDeps | `cmd/sbctl` |
| 單元測試 | completion 腳本產生（驗證輸出非空且包含關鍵字） | `cmd/sbctl` |
| 單元測試 | 旗標值補全函式 | `cmd/sbctl` |

### Mock 策略

- `ProjectService.List()` — 使用 mock `ProjectService` 介面實作
- DB 連線 — 不在 completion 測試中直接測試 DB；透過 mock service 層隔離

### CI 執行方式

- 所有測試在一般 CI 中執行，不需特殊環境
- 不測試實際的 shell completion 互動（需要真實 shell 環境），僅測試腳本產生與 `ValidArgsFunction` 邏輯

---

## Production Ready 考量

### 錯誤處理

動態補全失敗時靜默降級為空列表，不影響使用者的 shell 操作。不會產生 error output 到 stderr（避免干擾 shell completion 機制）。

### 日誌與可觀測性

補全功能為終端機互動輔助，不需日誌記錄。

### 輸入驗證

- `toComplete` 前綴由 shell 自動傳入，僅用於字串前綴過濾，無需額外驗證
- 不接受使用者直接輸入（`sbctl __complete` 由 shell 自動呼叫）

### 安全性

- 動態 slug 補全會建立 DB 連線，但僅讀取 project slug 與 display name
- 不暴露 secrets 或敏感設定

### 優雅降級

- DB 不可用時，動態補全降級為空列表，靜態補全（命令、旗標）仍正常運作
- 不影響 CLI 的正常操作

### 設定管理

無新增設定。補全功能使用與 CLI 相同的 `--db-url` 連線設定。

---

## 待決問題

- 無

---

## 審查

### Reviewer A（架構）

- **狀態：** 🔁 REVISE（第一輪）→ ✅ APPROVED（第二輪）
- **意見：**

**第二輪：** 6/6 項 Round 1 問題全數解決。PersistentPreRunE 相容策略、輕量 DB 連線路徑、3 秒 timeout 三者構成完整一致的架構。觀察：建議實作時一併檢查 `cobra.ShellCompNoDescRequestCmd`（`__completeNoDesc`），但不阻擋。

**第一輪原始意見：**

整體方向正確：使用 Cobra 內建補全機制、`ValidArgsFunction` 做動態 slug 補全、`RegisterFlagCompletionFunc` 做旗標值補全，這些都是標準且合理的做法。`completion` 子命令放在 root 層級亦符合業界慣例。Cobra 版本確認為 v1.10.2，支援 `GenBashCompletionV2` 等 API。

但設計存在以下架構層級問題，必須修正後才能進入實作：

---

**問題 1（關鍵）：`PersistentPreRunE` 會阻擋 `completion bash/zsh/fish` 命令**

現行 `buildRootCmd()`（`main.go:90-107`）的 `PersistentPreRunE` 執行兩件事：(a) 驗證 `--db-url` 非空，否則回傳 exit code 1；(b) 呼叫 `BuildDeps()` 建立完整依賴圖。

`sbctl completion bash` 僅需輸出靜態腳本，不需要 DB 連線。但 `PersistentPreRunE` 會在 `RunE` 之前執行，若使用者未設定 `--db-url`（在安裝補全腳本的場景下很常見），命令會直接失敗。

設計必須明確說明如何讓 `completion` 子命令跳過 `PersistentPreRunE`。建議方案：在 `PersistentPreRunE` 中檢查 `cmd.Name()` 或使用 Cobra 的 `cmd.CalledAs()` / annotation 機制，對不需 DB 的命令提前 return nil。

---

**問題 2（關鍵）：優雅降級路徑在架構上不可達**

設計的錯誤處理表格宣稱「DB 連線失敗時，動態 slug 補全回傳空列表」。但實際上，Cobra 的 `__complete` 隱藏命令同樣會觸發 `PersistentPreRunE`。若 DB 不可用，`BuildDeps()` 會回傳 error，`PersistentPreRunE` 會回傳 `ExitError{Code: 2}`，整個命令在 `ValidArgsFunction` 被呼叫之前就已終止。

因此，`projectSlugCompletion` 中的 graceful degradation 邏輯永遠不會被執行。設計必須重新考慮初始化策略，使 `PersistentPreRunE` 在補全路徑上不會因 DB 失敗而中斷，或者讓動態補全走獨立的初始化路徑（例如 lazy init）。

---

**問題 3（重要）：完整依賴圖對 Tab 補全而言過於沉重**

`BuildDeps()`（`deps.go`）會建立 pgxpool 連線池並 ping、初始化 ComposeAdapter、K8sAdapter、PortAllocator、SecretGenerator、AdapterRegistry 等。Tab 補全只需要 `ProjectService.List()` — 也就是只需要 `pgxpool` 和 `ProjectRepository`。每次按 Tab 都建立完整依賴圖是不必要的開銷，會直接影響補全的回應速度（UX）。

建議方案之一：提供一個輕量的 `BuildCompletionDeps()` 函式，只建立 pool + ProjectRepository + 一個只實作 `List()` 的 service wrapper。或者改用 lazy initialization，讓 `Deps` 在首次存取時才建立。

---

**問題 4（重要）：缺少補全超時策略**

動態 slug 補全牽涉 DB 查詢。若 DB 回應緩慢（例如首次連線、網路延遲），使用者按 Tab 後可能等待數秒無回應，嚴重影響 UX。設計應為補全路徑設定一個短超時（建議 2-3 秒），超時後回傳空列表。可透過 `context.WithTimeout` 實現。

---

**問題 5（次要）：未說明 `buildCompletionCmd()` 的掛載位置**

設計定義了 `buildCompletionCmd()` 函式簽名，但未說明它如何被加入 `buildRootCmd()`。應明確指出在 `main.go` 的 `root.AddCommand(...)` 區段新增 `root.AddCommand(buildCompletionCmd())`，並說明此命令不需要 `deps` 參數。

---

**問題 6（次要）：`--output` 旗標補全的註冊位置**

設計說在 `rootCmd` 定義 `--output` 時註冊補全，但現有程式碼中 `--output` 是用 `PersistentFlags().StringVarP` 定義的。`RegisterFlagCompletionFunc` 應在同一處呼叫。設計應明確指出修改 `buildRootCmd()` 函式的哪個位置。

---

**總結：** 核心補全機制的設計方向正確，但在與現有 `PersistentPreRunE` 架構的整合上存在關鍵缺陷。問題 1 和問題 2 會導致補全功能在常見場景下直接失敗或降級邏輯無法觸發。請修正後重新提交審查。

### Reviewer B（實作）

- **狀態：** 🔁 REVISE（第一輪）→ ✅ APPROVED（第二輪）
- **意見：**

**第二輪：** 5/6 項已正確修正，第 5 項（`--no-color` 引用）為 Minor 文字描述問題不影響實作。核心阻塞問題（PersistentPreRunE + completion 整合）有完整合理的解決方案。

**第一輪原始意見：**

設計方向正確且整體結構合理，但存在一個**阻塞性實作缺陷**與數個需補充的細節。在解決以下問題前，本設計無法正確構建。

#### 1. 阻塞性問題：`PersistentPreRunE` 阻斷所有補全（Critical）

Cobra v1.10.2 的 `__complete` 隱藏命令在執行時**會觸發 root 的 `PersistentPreRunE`**（見 `cobra@v1.10.2/command.go:985-986`：Cobra 從 `__complete` 向上遍歷 parent chain 找到 root 的 `PersistentPreRunE` 並執行）。

當前 `main.go:90-107` 的 `PersistentPreRunE` 會：
1. 驗證 `--output` 格式
2. **強制要求 `--db-url` 非空**（否則回傳 `ExitError`）
3. 呼叫 `BuildDeps` 連線 DB 並 ping

這意味著：
- **若 `SBCTL_DB_URL` 環境變數未設定，所有補全（包括靜態的命令名稱與旗標補全）都會失敗。** Shell 呼叫 `sbctl __complete ...` 時不會帶 `--db-url` flag，因此 `dbURL` 只來自 `envOr` 的預設空字串。
- **`sbctl completion bash/zsh/fish` 也會被阻斷** — 產生補全腳本不需要 DB，但 `PersistentPreRunE` 強制要求它。
- 設計文件中「DB 不可用時…靜態補全（命令、旗標）仍正常運作」的描述**不正確**。實際行為是 `PersistentPreRunE` 回傳錯誤後 Cobra 終止執行，`ValidArgsFunction` 根本不會被呼叫。

**必須修改 `PersistentPreRunE`**，在 completion 相關命令時跳過 `BuildDeps`。建議方案：

```go
root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
    // completion 命令與腳本產生不需要 DB
    if cmd.Name() == cobra.ShellCompRequestCmd ||
       cmd.Name() == cobra.ShellCompNoDescRequestCmd ||
       cmd.Parent() != nil && cmd.Parent().Name() == "completion" ||
       cmd.Name() == "completion" {
        // best-effort: 嘗試建立 deps 但不 fail
        if dbURL != "" {
            deps, _ = BuildDeps(cmd.Context(), dbURL, projectsDir)
        }
        return nil
    }
    // ... 原有邏輯
}
```

設計文件需在「執行流程」或新增的「對既有程式碼的修改」段落中明確描述此變更。

#### 2. `projectSlugCompletion` 必須處理 nil deps（Important）

若 `PersistentPreRunE` 因上述修改而允許 completion 在無 DB 時通過，則 `*deps` 可能為 `nil`。`projectSlugCompletion` 需要在函式開頭加入 nil guard：

```go
if *deps == nil {
    return nil, cobra.ShellCompDirectiveNoFileComp
}
```

建議使用 `ShellCompDirectiveNoFileComp` 而非 `ShellCompDirectiveError`，因為這代表「無可用資料，但非錯誤」，避免部分 shell 顯示錯誤訊息。

#### 3. 測試策略缺漏（Important）

測試策略需新增以下案例：
- **`PersistentPreRunE` + `__complete` 互動測試**：驗證在 `dbURL` 為空時，靜態補全（命令名稱、旗標）仍正常運作。
- **`projectSlugCompletion` 在 `*deps == nil` 時的行為**：驗證回傳空列表而非 panic。
- **`sbctl completion bash` 在無 DB 時仍能產生腳本**。

現有的 `newTestRootCmd` helper 使用 no-op `PersistentPreRunE`，這合適用於單元測試 `ValidArgsFunction` 邏輯，但無法覆蓋上述整合情境。建議新增一組使用 `buildRootCmd()` 的測試（不連 DB），確認 completion 路徑的端對端行為。

#### 4. 缺少 `buildCompletionCmd()` 掛載說明（Minor）

設計定義了 `buildCompletionCmd()` 函式，但未在「介面合約」或「執行流程」中明確說明需在 `buildRootCmd()` 中加入 `root.AddCommand(buildCompletionCmd())`。應補充。

#### 5. `--no-color` 旗標不存在（Minor）

「範圍 > 包含」段落提及 `--no-color` 旗標補全，但現有程式碼中不存在此旗標。若為未來功能，應移至「不包含」或標註為前瞻性參考。

#### 6. `ShellCompDirectiveError` 的使用場景建議（Minor）

錯誤處理表中，「DB 連線失敗」與「`List()` 回傳錯誤」使用 `ShellCompDirectiveError`。此 directive 會導致部分 shell（如 zsh）顯示錯誤提示。建議統一改用 `cobra.ShellCompDirectiveNoFileComp`，提供更安靜的降級體驗，與「不影響使用者的 shell 操作」的設計目標一致。

#### 總結

核心設計（使用 Cobra 內建 completion、`ValidArgsFunction` 動態補全、mock service 層測試）是正確的。但 `PersistentPreRunE` 與 `__complete` 的互動是一個**實作上必須解決的基礎問題**，否則補全功能在最常見的使用情境（`SBCTL_DB_URL` 未設定時）完全無法運作。修復此問題後即可 APPROVED。

---

## 任務

### T-F5-1: 建立 completion 命令與 PersistentPreRunE 修改（`f5-completion-cmd`）

**建立/修改檔案：**
- 新增 `cmd/sbctl/completion.go` — completion 子命令
- 新增 `cmd/sbctl/completion_test.go` — completion 測試
- 修改 `cmd/sbctl/main.go` — 註冊 completion 命令、PersistentPreRunE 加入跳過邏輯

**實作內容：**
1. 建立 `completion.go`：
   - `func buildCompletionCmd() *cobra.Command` — 含 bash/zsh/fish 三個子命令
   - bash: `cmd.Root().GenBashCompletionV2(os.Stdout, true)`
   - zsh: `cmd.Root().GenZshCompletion(os.Stdout)`
   - fish: `cmd.Root().GenFishCompletion(os.Stdout, true)`
2. 修改 `main.go`：
   - 在 `buildRootCmd()` 中加入 `rootCmd.AddCommand(buildCompletionCmd())`
   - PersistentPreRunE 加入跳過判斷：`if cmd.Name() == cobra.ShellCompRequestCmd || cmd.Name() == cobra.ShellCompNoDescRequestCmd || isCompletionCmd(cmd) { return nil }`
   - `func isCompletionCmd(cmd *cobra.Command) bool` — 檢查 cmd 或其 parent 是否為 completion
3. 測試：
   - `buildCompletionCmd()` 產生有效 bash 腳本（含 `_sbctl`）
   - `buildCompletionCmd()` 產生有效 zsh 腳本（含 `#compdef`）
   - `buildCompletionCmd()` 產生有效 fish 腳本（含 `complete`）
   - PersistentPreRunE 在 `__complete` 命令時跳過 BuildDeps

**驗收標準：**
- `go build ./...` 通過
- `go test -race ./...` 通過
- `sbctl completion bash | head` 輸出有效 bash 腳本
- completion 命令不需要 `--db-url`

---

### T-F5-2: 動態 slug 補全與 flag 值補全（`f5-dynamic-completion`）

**依賴：** T-F5-1

**建立/修改檔案：**
- 修改 `cmd/sbctl/completion.go` — 新增 slug 補全函式
- 修改 `cmd/sbctl/project.go` — 為 get/start/stop/reset/delete/credentials 設定 `ValidArgsFunction`
- 修改 `cmd/sbctl/main.go` — 為 `--output` flag 註冊值補全
- 修改 `cmd/sbctl/completion_test.go` — 新增動態補全測試

**實作內容：**
1. `completion.go`：
   - `func projectSlugCompletion(deps **Deps, dbURL *string) func(...)` — 回傳 `cobra.CompletionFunc`
   - deps 可用時用 `ProjectService.List()`，否則用 `dbURL` 建立輕量連線（3 秒 timeout）
   - 失敗時回傳空 list + `ShellCompDirectiveNoFileComp`
2. `project.go`：為需要 `<slug>` 參數的 6 個子命令設定 `ValidArgsFunction: projectSlugCompletion(&deps, &dbURL)`
3. `main.go`：`rootCmd.RegisterFlagCompletionFunc("output", func(...) { return []string{"table", "json", "yaml"}, ... })`
4. 測試：
   - slug 補全回傳正確 slugs（mock service）
   - slug 補全依 `toComplete` 前綴過濾
   - DB 不可用時回傳空 list（不 panic）
   - `--output` flag 回傳 `table`、`json`、`yaml`

**驗收標準：**
- `go build ./...` 通過
- `go test -race ./...` 通過
- `sbctl project get <TAB>` 顯示 slug 列表（若 DB 可用）
- `sbctl project get --output <TAB>` 顯示 table/json/yaml

---

## 程式碼審查

- **審查結果：** ✅ APPROVED（第二輪）
- **發現問題：** R1: watch-timeout 未套用 context、gofmt 格式問題、writeCreateSummary 錯誤忽略
- **修正記錄：** 7f163fc fix watch-timeout & gofmt; cd2480d fix writeCreateSummary error handling
