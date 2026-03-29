> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# 設計文件：CLI 彩色輸出

## 狀態

done

## Phase

- **Phase：** Phase 5
- **Phase Plan：** docs/designs/phase-5-plan.md

---

## 目的

目前 `sbctl project list` 與 `sbctl project get` 的 table 輸出中，狀態欄位為純文字（例如 `running`、`stopped`、`error`），使用者在多個專案混合不同狀態時，難以一眼辨識哪些需要關注。

本功能為 table 格式的狀態欄位加入 ANSI 色彩標示，使運行中、停止、錯誤等狀態在視覺上立即可辨。同時提供 `--no-color` 旗標與 `NO_COLOR` 環境變數，讓使用者在管線（pipe）或不支援色彩的終端機中關閉色彩。

---

## 範圍

### 包含

- 為 table 格式中的 `STATUS` 欄位加入 ANSI 色彩
- 新增全域旗標 `--no-color`
- 支援 `NO_COLOR` 環境變數（遵循 [no-color.org](https://no-color.org/) 標準）
- 自動偵測 stdout 是否為 TTY，非 TTY 時自動關閉色彩
- 建立內部 `termcolor` 套件，提供可複用的色彩工具函式
- 影響所有顯示 `STATUS` 欄位的 table 輸出：`writeProjectView`、`writeProjectViews`

### 不包含

- JSON 輸出不加入色彩（已為結構化資料，不可混入 ANSI 控制碼）
- YAML 輸出不加入色彩（同上）
- 非狀態欄位的色彩化（如 slug、display name）— 留待未來需要時再擴展
- Windows legacy console 的 ANSI 支援（Windows Terminal 與 PowerShell 7+ 已原生支援）

---

## 資料模型

本功能不修改任何資料模型。所有變更僅在 CLI 輸出層。

### 色彩對映表

| ProjectStatus | ANSI 色彩 | 色碼 | 視覺意義 |
|---|---|---|---|
| `creating` | Yellow | `\033[33m` | 進行中 |
| `starting` | Yellow | `\033[33m` | 進行中 |
| `running` | Green | `\033[32m` | 健康 |
| `stopping` | Yellow | `\033[33m` | 進行中 |
| `stopped` | Dark Gray | `\033[90m` | 不活躍 |
| `destroying` | Yellow | `\033[33m` | 進行中 |
| `destroyed` | Dark Gray | `\033[90m` | 不活躍 |
| `error` | Red | `\033[31m` | 需要注意 |
| 未知狀態 | 無色彩 | — | 容錯 |

Reset 碼：`\033[0m`

---

## 介面合約

### 新檔案 `cmd/sbctl/color.go`

色彩工具放在 `cmd/sbctl/` 套件內（與 `output.go` 同層），因為僅 CLI 輸出使用，不需獨立套件。

```go
package main

// colorer controls whether ANSI color codes are emitted.
type colorer struct {
    enabled bool
}

// newColorer returns a colorer.
// Color is enabled when all of the following are true:
//   - noColorFlag is false
//   - os.LookupEnv("NO_COLOR") returns found==false (遵循 no-color.org：變數存在即停用，不論值)
//   - fd is a terminal (golang.org/x/term.IsTerminal)
//
// 新增依賴：golang.org/x/term（Go 團隊維護的準標準庫）
func newColorer(fd uintptr, noColorFlag bool) *colorer

// status wraps a ProjectStatus string with the appropriate ANSI color.
// When color is enabled, ALL statuses（含未知狀態）都包裝以維持一致的 byte overhead：
//   - 已知狀態：\033[XXm{text}\033[0m（XX 為對應色碼）
//   - 未知狀態：\033[39m{text}\033[0m（39=default foreground，視覺無變化但 byte 數一致）
// When color is disabled, returns the plain string unchanged.
func (c *colorer) status(s string) string
```

### tabwriter 對齊策略

`tabwriter` 以 byte 數計算欄位寬度，ANSI 控制碼會增加不可見的 bytes。本設計的對齊策略：

- 所有 ANSI 色碼長度固定：`\033[XXm`（5 bytes）+ `\033[0m`（4 bytes）= 9 bytes overhead
- 已知狀態使用 `\033[32m`（綠）、`\033[33m`（黃）、`\033[31m`（紅）、`\033[90m`（灰）— 皆為 5 bytes
- 未知狀態使用 `\033[39m`（預設前景色）— 同為 5 bytes
- 因此**所有列的 STATUS 欄位 ANSI overhead 一致**，tabwriter 的對齊不受影響
- 色彩停用時，所有列都不加 ANSI，overhead 皆為 0 — 同樣一致

### 新增全域旗標

```go
// 在 rootCmd 中新增：
rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false,
    "Disable colored output")
```

### 修改的函式簽名

`output.go` 中的 table 格式化函式新增 `*termcolor.Colorer` 參數：

```go
func writeProjectView(w io.Writer, output string, view *usecase.ProjectView, c *termcolor.Colorer) error
func writeProjectViews(w io.Writer, output string, views []*usecase.ProjectView, c *termcolor.Colorer) error
```

JSON/YAML 路徑中忽略 `Colorer`，僅在 table 路徑中使用 `c.Status(string(view.Status))`。

---

## 執行流程

### 初始化流程

1. `main.go` 的 `buildRootCmd()` 定義 `--no-color` 全域旗標
2. `PersistentPreRunE` 中建立 `colorer`（在 `BuildDeps` 之前，因色彩不依賴 DB）：
   ```go
   colorOut = newColorer(os.Stdout.Fd(), noColor)
   ```
3. `colorOut` 以 `*colorer` 指標傳遞給 write 函式（與 `*output` 相同的指標傳遞模式）

### 輸出流程（以 `project list` 為例）

1. CLI 呼叫 `ProjectService.List()` 取得 `[]*ProjectView`
2. 呼叫 `writeProjectViews(os.Stdout, outputFlag, views, colorOut)`
3. 若 `output == "table"`：
   - 迭代每個 view
   - 呼叫 `colorOut.status(string(view.Status))` 取得帶色彩的狀態字串
   - 寫入 tabwriter
4. 若 `output == "json"` 或 `"yaml"`：
   - 直接序列化，不呼叫 `colorer`

### 色彩停用判定優先順序

1. `--no-color` 旗標 → 最高優先
2. `NO_COLOR` 環境變數存在（使用 `os.LookupEnv`，遵循 no-color.org：變數存在即停用，不論值為何）→ 次高優先
3. stdout 不是 TTY（`!term.IsTerminal(fd)`，使用 `golang.org/x/term`）→ 自動停用
4. 以上皆不滿足 → 啟用色彩

---

## 錯誤處理

| 情境 | 處理方式 | 回應格式 |
|---|---|---|
| 未知的 `ProjectStatus` 值 | 包裝 `\033[39m`（default foreground），維持 byte overhead 一致性 | 視覺上無色彩變化 |
| ANSI 不支援的終端機 | 依賴使用者設定 `--no-color` 或 `NO_COLOR` | — |

> 注意：`term.IsTerminal()` 回傳 `bool`，不會產生 error。

---

## 測試策略

### 需要測試的行為

- `colorer.status()` 對每個已知 `ProjectStatus` 回傳正確的 ANSI 包裝字串
- `colorer.status()` 對未知狀態回傳 `\033[39m{text}\033[0m`（default foreground 包裝）
- `colorer.status()` 對空字串回傳空字串
- `colorer` 停用時，`status()` 回傳純文字（無 ANSI 碼）
- `newColorer()` 在 `noColorFlag=true` 時停用色彩
- `newColorer()` 在 `NO_COLOR` 環境變數存在時停用色彩（使用 `os.LookupEnv`）
- `writeProjectView` table 輸出中包含 ANSI 碼（色彩啟用時）
- `writeProjectView` table 輸出中不包含 ANSI 碼（色彩停用時）
- `writeProjectView` JSON/YAML 輸出中不包含 ANSI 碼（無論色彩設定）
- `writeProjectViews` 同上（多專案）
- 既有 `output_test.go`（若存在）的測試在加入 `*colorer` 參數後仍通過（傳入停用的 colorer）

### 測試類型分配

| 測試類型 | 測試目標 | 層次 |
|---|---|---|
| 單元測試 | `colorer.status()` 各狀態色彩對映（含未知狀態、空字串） | `cmd/sbctl` |
| 單元測試 | `colorer` 啟用/停用邏輯（flag、env、TTY） | `cmd/sbctl` |
| 單元測試 | `writeProjectView` / `writeProjectViews` 含色彩 | `cmd/sbctl` |
| 單元測試 | JSON/YAML 輸出不含 ANSI 碼 | `cmd/sbctl` |
| 單元測試 | 既有 output 測試在傳入停用 colorer 後仍通過（向後相容） | `cmd/sbctl` |

### Mock 策略

- `term.IsTerminal()` — 透過將 `fd` 注入為參數，測試中傳入非 TTY 的 fd（如 `os.Pipe()`）
- 環境變數 — 使用 `t.Setenv("NO_COLOR", "1")` 設定與清除

### 向後相容性

`writeProjectView` 與 `writeProjectViews` 的函式簽名新增 `*colorer` 參數，所有呼叫點（`project.go` 中的 RunE 函式）需同步更新。既有單元測試（如 `main_test.go` 或 `output_test.go`）需傳入 `newColorer(0, true)`（停用色彩的 colorer）以維持既有行為。

### CI 執行方式

- 所有測試在一般 CI 中執行，不需特殊環境
- CI 中 stdout 通常非 TTY，因此自動停用色彩是正確行為
- 測試透過直接呼叫 `Colorer` 方法驗證色彩邏輯，不依賴實際終端機

---

## Production Ready 考量

### 錯誤處理

所有色彩相關操作都是純字串拼接，不會產生 runtime error。未知狀態時容錯回傳原始字串。

### 日誌與可觀測性

色彩功能為純展示層，不需日誌記錄。

### 輸入驗證

- `--no-color` 為 boolean flag，Cobra 自動驗證
- `NO_COLOR` 環境變數遵循 no-color.org 標準：「存在且非空」即為停用

### 安全性

無安全影響。色彩碼不包含任何敏感資訊。

### 優雅降級

- 當色彩不可用時（非 TTY、`--no-color`、`NO_COLOR`），自動降級為純文字輸出
- 不會因色彩相關問題導致 CLI 指令失敗

### 設定管理

| 設定 | 型別 | 預設值 | 來源 |
|------|------|--------|------|
| `--no-color` | bool | `false` | CLI 旗標 |
| `NO_COLOR` | string | 空 | 環境變數 |

---

## 待決問題

- 無

---

## 審查

### Reviewer A（架構）

- **狀態：** ✅ APPROVED（第一輪）→ ✅ APPROVED（第二輪）
- **意見：**

**第二輪：** 確認修訂後設計架構合理，無新問題。套件位置改為 `cmd/sbctl/color.go` 合理，`*colorer` 指標傳遞模式與 `*output` 一致。

**第一輪原始意見：**

設計整體架構合理，範圍適當，降級策略完善。以下為具體觀察與建議：

**1. 套件位置 — `internal/cli/termcolor`（可接受，需留意）**

現有架構（`docs/CONTROL_PLANE.md` 與 `docs/CODING_GUIDELINES.md`）定義的 `internal/` 子目錄為 `api/`、`domain/`、`adapter/`、`store/`、`usecase/`，並無 `cli/` 層。`termcolor` 屬於 CLI 展示層關注點，不適合放在上述任何現有目錄。新增 `internal/cli/` 作為 CLI 表現層工具的命名空間是合理的擴展，但實作時應在 `CONTROL_PLANE.md` 的架構目錄結構中補充此層的定位說明。

**2. Colorer 注入方式 — 應明確選擇，不可使用全域變數**

設計文件中寫道「Colorer 存入 Deps 結構**或**作為 package-level 變數傳遞」，此處的「或」造成歧義。Coding Guidelines 明確規定「避免全域狀態，使用依賴注入」，因此 package-level 變數方案應排除。

此外，`Deps` 結構目前僅持有服務層依賴（`ProjectService`），其初始化需要 DB 連線。`Colorer` 是純展示層工具、不依賴基礎設施，混入 `Deps` 會模糊該結構的職責。建議依循現有的 `*output` 指標傳遞模式，將 `*termcolor.Colorer` 作為獨立指標變數傳入 `buildProjectCmd` 及各子命令建構函式。

**3. `golang.org/x/term` 新依賴 — 合理且正當**

`golang.org/x/term` 為 Go 官方團隊維護的準標準函式庫，是偵測 TTY 狀態的慣用方式，比 `os.Stdout.Stat()` + `ModeCharDevice` 更可靠跨平台。新增此依賴是正當的。

**4. tabwriter 與 ANSI 碼的對齊問題 — 實作時需留意**

`tabwriter` 按位元組計算欄寬。加入 ANSI 色碼後，狀態欄位會多出 9 bytes（`\033[XXm` + `\033[0m`）的不可見字元，導致 `tabwriter` 認為該欄更寬。由於所有已知狀態的 ANSI 開頭碼長度一致（皆為 5 bytes），在「全部著色」或「全部無色」的情境下對齊不受影響。但若出現未知狀態（無色彩）與已知狀態（有色彩）混合的行，該欄的對齊會偏移。建議在實作階段處理：可讓 `Status()` 對未知狀態也包裹等長的空 ANSI 序列，或改為在 tabwriter 格式化完成後再注入色碼。

**5. 錯誤處理表格修正 — `term.IsTerminal()` 不會失敗**

`golang.org/x/term.IsTerminal(fd int)` 的簽名回傳 `bool`，不回傳 `error`。錯誤處理表格中「`term.IsTerminal()` 呼叫失敗」的情境描述不準確。應移除此列或改為「`IsTerminal()` 對非標準 fd 回傳 `false`」。

**6. 色彩對映與降級策略 — 設計完善**

色彩對映涵蓋所有 8 個 `domain.ProjectStatus` 常數，語意分組（黃色=進行中、綠色=健康、灰色=不活躍、紅色=錯誤）清晰合理。`--no-color` → `NO_COLOR` → TTY 偵測的優先順序符合業界慣例（[no-color.org](https://no-color.org/)），降級為純文字的策略確保 CLI 在管線中永不產生亂碼。

**7. 測試策略 — 充分**

測試計畫涵蓋色彩對映、啟用/停用邏輯、各輸出格式的行為。Mock 策略使用 `os.Pipe()` 與 `t.Setenv()` 合理且不需特殊 CI 環境。可在實作時補充一組邊界測試：空字串狀態輸入的行為。

**總結：** 設計正確解決問題、範圍適當、介面清晰、降級策略完善。上述第 2 點（注入方式）與第 5 點（錯誤表格）應在實作前於文件中修正措辭，但不影響整體設計的正確性，故核可通過。

### Reviewer B（實作）

- **狀態：** 🔁 REVISE（第一輪）→ ✅ APPROVED（第二輪）
- **意見：**

**第二輪：** 6/6 項 Round 1 問題全數解決。tabwriter 對齊策略（統一 ANSI overhead）正確巧妙。殘留兩項文件清理備註（函式簽名中 `*termcolor.Colorer` 應改為 `*colorer`、第 229 行 NO_COLOR 描述統一為 LookupEnv 語意）可在實作階段修正。

**第一輪原始意見：**

整體設計方向正確，功能拆解清晰，測試策略合理。但有一個關鍵實作缺陷與數個需要釐清的細節，必須在進入實作前修正。

#### 1. 【關鍵】`tabwriter` 與 ANSI 碼的欄位對齊問題

設計中 STATUS 欄位透過 `tabwriter` 進行 table 對齊，但 `tabwriter` 是以**位元組數**計算欄寬，不是以視覺寬度。ANSI 色碼（如 `\033[32m` + `\033[0m`）會增加 9 個不可見位元組，導致不同長度的狀態字串對齊錯位。

例如：
- `\033[31merror\033[0m` → `tabwriter` 看到 14 bytes，視覺寬度 5
- `\033[33mcreating\033[0m` → `tabwriter` 看到 17 bytes，視覺寬度 8

`tabwriter` 會以位元組長度計算 padding，導致 UPDATED 欄在視覺上參差不齊。

**建議解法（擇一）：**
- (a) 在 ANSI 包裝前，先將狀態字串 padding 至統一寬度（例如 `fmt.Sprintf("%-10s", status)` 再包裝 ANSI 碼）
- (b) 放棄 `tabwriter`，改用固定寬度格式化（`fmt.Sprintf("%-12s %-20s %-10s %s", ...)`）
- (c) 在 `tabwriter` 格式化完成後，才注入 ANSI 碼（需後處理替換）

請在設計中選定方案並記載。

#### 2. 【中等】`Colorer` 傳遞方式未確定

設計描述為「存入 `Deps` 結構或作為 package-level 變數傳遞」，但未做出明確選擇。根據 CODING_GUIDELINES 的「避免全域狀態，使用依賴注入」原則，**不應使用 package-level 變數**。

建議明確選定將 `Colorer` 加入 `Deps` 結構，或者作為 `buildProjectCmd` 的額外參數傳入（與 `output *string` 同層級）。後者更乾淨，因為 `Colorer` 是 CLI 展示層關注點，與 `Deps` 中的 `ProjectService`（業務邏輯依賴）性質不同。

#### 3. 【中等】`internal/cli/` 為新目錄層，需確認架構一致性

目前 `internal/` 的子目錄為 `api/`、`domain/`、`adapter/`、`store/`、`usecase/`，CODING_GUIDELINES 中也只記載這些。新增 `internal/cli/termcolor` 引入了一個全新的 `cli/` 層。

這不一定是問題（termcolor 確實不屬於 domain/adapter/store），但設計應明確說明為何選擇此路徑，而非例如 `cmd/sbctl/internal/termcolor` 或直接放在 `cmd/sbctl/` 套件中（作為 unexported 工具函式）。考量到 `termcolor` 目前只有 `cmd/sbctl` 會使用，放在 `cmd/sbctl` 套件內部（如 `color.go`）可能更簡潔，也避免引入新的目錄層級。

#### 4. 【低】新增 `golang.org/x/term` 依賴未記載

設計使用 `term.IsTerminal()` 但 `go.mod` 中目前沒有 `golang.org/x/term`。這是一個新的外部依賴，設計應在範圍或待決問題中明確記載，讓審查者評估依賴影響。

#### 5. 【低】既有測試的向下相容影響

`main_test.go` 中的 `newTestRootCmd` 輔助函式與所有測試案例都透過 Cobra 間接呼叫 `writeProjectView` / `writeProjectViews`。簽名變更後：
- `project.go` 中 6+ 個呼叫點都需要更新
- `newTestRootCmd` 需要初始化並傳遞 `Colorer`
- 既有測試使用 `strings.Contains(out, "myproj")` 仍可通過（ANSI 碼不影響子字串匹配），但設計應在測試策略中明確提及這些既有測試的更新方式

#### 6. 【資訊】`NO_COLOR` 標準解讀

no-color.org 原文為 "When set"（僅需存在），但設計實作為「`os.Getenv("NO_COLOR")` is empty」（檢查非空）。這是業界常見做法且可接受，但嚴格遵循標準應使用 `os.LookupEnv` 判斷是否存在。建議在設計中記載此實作選擇的原因。

#### 總結

修正第 1 點（tabwriter 對齊問題）並釐清第 2、3 點後即可 APPROVED。其餘為建議性改善。

---

## 任務

### T-F1-1: 建立 colorer 核心（`f1-colorer-core`）

**建立/修改檔案：**
- 新增 `cmd/sbctl/color.go` — colorer 型別與方法
- 新增 `cmd/sbctl/color_test.go` — colorer 單元測試
- 修改 `go.mod` — 新增 `golang.org/x/term` 依賴

**實作內容：**
1. 執行 `go get golang.org/x/term`
2. 建立 `color.go`：
   - `type colorer struct { enabled bool }`
   - `func newColorer(fd uintptr, noColorFlag bool) *colorer` — 判斷邏輯：`--no-color` > `NO_COLOR` env（`os.LookupEnv`，存在即停用）> TTY 偵測（`term.IsTerminal`）
   - `func (c *colorer) status(s string) string` — 依狀態包裝 ANSI 色碼，reset 用 `\033[39m`（9 bytes 一致）
3. 建立 `color_test.go`：
   - 每個 ProjectStatus 的色碼對應測試（running→綠、error→紅 等）
   - unknown status 用 `\033[39m` 包裝測試
   - `--no-color` 停用測試
   - `NO_COLOR` 環境變數停用測試（設為空字串也停用）
   - disabled colorer 回傳原始字串測試

**驗收標準：**
- `go build ./...` 通過
- `go test -race -run TestColor ./cmd/sbctl/...` 通過
- 色碼 byte 數一致（enabled 時每個狀態字串增加 9 bytes）

---

### T-F1-2: 整合 colorer 至 CLI 管線（`f1-colorer-integration`）

**依賴：** T-F1-1

**建立/修改檔案：**
- 修改 `cmd/sbctl/main.go` — 新增 `--no-color` 全域 flag、在 `PersistentPreRunE` 初始化 colorer
- 修改 `cmd/sbctl/output.go` — `writeProjectView` 與 `writeProjectViews` 簽名新增 `*colorer` 參數
- 修改 `cmd/sbctl/project.go` — 傳遞 colorer 至 write 函式
- 修改 `cmd/sbctl/main_test.go` — 更新 `newTestRootCmd` 傳入 disabled colorer，更新所有 write 呼叫

**實作內容：**
1. `main.go`：
   - 新增 `var noColor bool` 與 `var colorOut *colorer`
   - `rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "Disable colored output")`
   - 在 `PersistentPreRunE` 中 `colorOut = newColorer(os.Stdout.Fd(), noColor)`
2. `output.go`：
   - `writeProjectView(w, output, view, c *colorer)` — table 格式時 `c.status(view.Status)`
   - `writeProjectViews(w, output, views, c *colorer)` — 同上
3. `project.go`：所有 RunE 中傳入 `colorOut`
4. 更新既有測試以傳入 `nil` 或 disabled colorer

**驗收標準：**
- `go build ./...` 通過
- `go test -race ./...` 全數通過（含既有測試）
- `sbctl project list` 在 TTY 中顯示彩色狀態
- `sbctl project list --no-color` 無 ANSI 碼
- `sbctl project list -o json` 無 ANSI 碼

---

## 程式碼審查

- **審查結果：** ✅ APPROVED（第二輪）
- **發現問題：** R1: watch-timeout 未套用 context、gofmt 格式問題、writeCreateSummary 錯誤忽略
- **修正記錄：** 7f163fc fix watch-timeout & gofmt; cd2480d fix writeCreateSummary error handling
