> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# 設計文件：Compose `.env` Config Renderer

## 狀態

done

## Phase

- **Phase：** Phase 2
- **Phase Plan：** `docs/designs/phase_2/phase-2-plan.md`

---

## 目的

實作 `domain.ConfigRenderer` 介面，將 `domain.ProjectConfig` 的解析後設定值渲染為 Docker Compose 所使用的 `.env` 檔案格式，並包裝為 `domain.Artifact`。

這是 Docker Compose Adapter 的設定渲染核心。每次建立或更新專案設定時，此 Renderer 負責產生寫入磁碟的 `.env` 檔案內容。

---

## 範圍

### 包含

- `ComposeEnvRenderer` struct，實作 `domain.ConfigRenderer`
- `.env` 檔案格式規則（key-value 排序、換行、值跳脫）
- 輸出 `Artifact` 定義（路徑、content、file mode）
- 排序策略（確保輸出具決定性，利於 diff 與測試）

### 不包含

- `ConfigRenderer` 介面定義（已在 `internal/domain/renderer.go`）
- `Artifact` 型別定義（已在 `internal/domain/renderer.go`）
- 將 `Artifact` 寫入磁碟的邏輯（由 `ComposeAdapter.ApplyConfig` 負責）
- 設定值的解析與合併（由 `domain.ResolveConfig` 負責，呼叫前已完成）
- `.env` 以外的格式渲染（K8s ConfigMap/Secret 由未來 Phase 6 處理）

---

## 資料模型

### ComposeEnvRenderer

```go
package compose

// ComposeEnvRenderer implements domain.ConfigRenderer, converting a ProjectConfig
// into a Docker Compose–compatible .env file Artifact.
// It is stateless and safe for concurrent use.
type ComposeEnvRenderer struct{}
```

### 輸出 Artifact

| 欄位 | 值 |
|------|-----|
| `Path` | `.env`（相對路徑；Adapter 在寫入磁碟時加上專案目錄前綴） |
| `Content` | `.env` 格式的 UTF-8 bytes |
| `Mode` | `0600`（owner read/write only；包含 secrets） |

---

## 介面合約

### NewComposeEnvRenderer

```go
// NewComposeEnvRenderer constructs a stateless ComposeEnvRenderer.
func NewComposeEnvRenderer() *ComposeEnvRenderer
```

### Render

```go
// Render converts the resolved ProjectConfig into a Docker Compose .env Artifact.
// The output is deterministic: keys are sorted lexicographically.
// Sensitive values are obtained via GetSensitive to ensure real values (not "***")
// are written to the .env file.
// Returns a single Artifact; errors on nil config, nil Values, or values containing
// newline (\n) or carriage return (\r) characters.
func (r *ComposeEnvRenderer) Render(config *domain.ProjectConfig) ([]domain.Artifact, error)
```

> **注意：** `config.Overrides` 不需要直接處理 — `ResolveConfig()` 已在呼叫 `Render` 前將 overrides 合併進 `config.Values`。

---

## 執行流程

```
Render(config)
│
├─ 1. 若 config == nil → 回傳 nil, fmt.Errorf("env renderer: config is nil")
│
├─ 2. 若 config.Values == nil → 回傳 nil, fmt.Errorf("env renderer: config.Values is nil")
│
├─ 3. 取出 config.Values 的所有 key，進行字典序排序
│     （確保輸出具決定性，方便比對與 git diff）
│
├─ 4. 對每個 key，呼叫 config.GetSensitive(key) 取得原始值
│     （GetSensitive 回傳真實值，包含 sensitive 欄位的明文；
│      Get() 會對 sensitive 欄位回傳 "***"，不得用於 .env 渲染）
│
├─ 5. 驗證值不含 \n 或 \r（回傳 error）
│
├─ 6. 套用跳脫規則（見下方），產生行 "<KEY>=<escaped_value>\n"
│
├─ 7. 產生 Artifact：
│     Path:    ".env"
│     Content: 以上步驟產生的 bytes
│     Mode:    0600
│
└─ 8. 回傳 []domain.Artifact{artifact}, nil
```

### 值跳脫規則（`.env` 格式）

Docker Compose v2 的 `.env` 解析使用 `godotenv`，雙引號字串內會展開 `${VAR}`。跳脫規則如下：

| 條件 | 輸出格式 | 說明 |
|------|---------|------|
| 值為空字串 | `KEY=` | 允許空值；non-nil empty map 合法，產生空 `.env` |
| 值不含特殊字元 | `KEY=value` | 直接輸出 |
| 值含 `#`、空格（`\x20`）、Tab（`\t`） | `KEY="value"` | 雙引號包裹（godotenv `#` 在引號外為註解） |
| 值含 `$` | `KEY="val\$ue"` | 雙引號內 `$` 跳脫為 `\$`，防止 godotenv 變數插值 |
| 值含 `\` | `KEY="val\\ue"` | **先**跳脫 `\` → `\\`（後續跳脫步驟不被誤判） |
| 值含 `"` | `KEY="val\"ue"` | 雙引號內 `"` 跳脫為 `\"`（在 `\` 跳脫之後執行） |
| 值含 `\n` 或 `\r` | 回傳 error | `.env` 不支援多行值 |

**跳脫執行順序（防止 double-escape）：**
1. `\` → `\\`
2. `"` → `\"`
3. `$` → `\$`
4. 若值含上述任一步驟後仍含控制字元（`\n`、`\r`）→ error

**觸發雙引號的條件（任一即觸發）：** 值含 `#`、空格、Tab、`$`、`\`、`"`。

---

## 錯誤處理

| 情境 | 處理方式 |
|------|---------|
| `config == nil` | 回傳 `nil, fmt.Errorf("env renderer: config is nil")` |
| `config.Values == nil` | 回傳 `nil, fmt.Errorf("env renderer: config.Values is nil")` |
| 值含換行符 `\n` 或 `\r` | 回傳 `nil, fmt.Errorf("env renderer: key %q value contains control character", key)` |

---

## 測試策略

### 需要測試的行為

- **基本輸出**：三個 key-value，驗證輸出格式（`KEY=VALUE\n`）
- **字典序排序**：亂序輸入，驗證輸出按字母排序
- **空值**：`KEY=` 格式
- **non-nil empty Values**：合法輸入，產生空 `.env`（0 bytes content），Artifact Mode = 0600
- **雙引號觸發 — `#`**：含 `#` 的值被雙引號包裹
- **雙引號觸發 — 空格 / Tab**：含空格或 Tab 的值被雙引號包裹
- **`$` 跳脫**：含 `$` → `\$`（雙引號內），防止 godotenv 插值
- **`\` 跳脫順序**：含 `\` → `\\`（在 `"` 跳脫前執行，確保不 double-escape）
- **`"` 跳脫**：含 `"` → `\"`（在 `\` 跳脫後執行）
- **同時含 `\` 和 `"`**：先 `\` → `\\`，再 `"` → `\"`，驗證結果
- **換行 / CR error**：值含 `\n` 或 `\r` 時回傳 error，error message 含 key
- **nil config error**：回傳 "config is nil" error
- **nil Values error**：回傳 "config.Values is nil" error
- **Artifact 屬性**：Path == `.env`，Mode == `0600`
- **使用 GetSensitive**：注入包含 sensitive key 的 config，驗證輸出為明文而非 `"***"`
- **決定性輸出**：相同輸入多次呼叫結果相同（byte-for-byte identical）

### 測試類型分配

| 測試類型 | 測試目標 | 層次 |
|---------|---------|------|
| 單元測試 | 所有上述行為（純函數，無外部依賴） | adapter |

整合測試不需要，此元件為純計算，無 I/O。

### Mock 策略

無需 mock，`Render` 為純函數。測試直接呼叫 `Render()` 並比對輸出 bytes。

### CI 執行方式

- 全部在一般 CI 中執行：`go test -race ./internal/adapter/compose/...`（無外部依賴）

---

## Production Ready 考量

### 錯誤處理

- 兩種已定義的 error 場景均有明確 error message，包含 key 名稱作為上下文
- 無 panic

### 日誌與可觀測性

- 此 renderer 為純計算，不適合在內部記錄日誌
- Caller（`ComposeAdapter.ApplyConfig`）負責在寫入磁碟前後記錄日誌

### 輸入驗證

- 驗證 `config.Values != nil`
- 驗證值不含換行符（不合法的 `.env` 格式）

### 安全性

- 輸出 `Artifact.Mode = 0600`：僅擁有者可讀寫，保護 secrets
- Renderer 本身不記錄 sensitive 值的日誌（僅由 caller 決定是否記錄）

### 優雅降級

- 此元件為純計算，無外部依賴，不存在降級場景

### 設定管理

- 無可設定值；`.env` 格式規則為固定標準

---

## 待決問題

- 目前無

---

## 審查

### Reviewer A（架構）

- **狀態：** REVISE
- **意見：**

整體架構設計清晰，介面合約與 `domain.ConfigRenderer` 完全一致，職責劃分正確，測試策略完整，Production Ready 考量充足。以下為需要修正的問題：

---

**🔴 必須修正（Blockers）**

**[A1] `$` 值在雙引號內需要跳脫**

設計目前對含 `$` 的值採用雙引號包裹（`KEY="val$var"`），但 Docker Compose v2 使用的 `godotenv` 在解析雙引號字串時，`${VAR}` 語法（及部分版本的 `$VAR`）會被當作變數展開。若不跳脫，包含 `$` 的 user override 值（如 SMTP 密碼）可能在渲染後被截斷或替換為空值。

需在雙引號內將 `$` 跳脫為 `\$`，並在跳脫規則表中補充此條目：

| 條件 | 輸出格式 | 說明 |
|------|---------|------|
| 值含 `$` | `KEY="val\$var"` | 雙引號內跳脫 `$` → `\$`，防止 godotenv 變數展開 |

另外，目前說明的「防止意外 shell 展開」理由不準確 — Docker Compose 不透過 shell 解析 `.env`，應改為「防止 godotenv 雙引號字串中的變數插值（variable interpolation）」。

---

**[A2] `config == nil` 未保護，存在 panic 風險**

目前只驗證 `config.Values == nil`，但若 caller 傳入 nil `*ProjectConfig`，執行 `config.Values` 存取會直接 panic。

應在函式開頭補充：
```go
if config == nil {
    return nil, fmt.Errorf("env renderer: config is nil")
}
```

---

**[A3] 應使用 `GetSensitive` 而非直接存取 `config.Values`**

`ProjectConfig.GetSensitive` 的 godoc 明確寫道：
> "Use only in contexts where the real value is required (e.g. ConfigRenderer.Render)."

這個 API 是為此 Renderer 量身設計的契約點。設計文件（執行流程第 3 步）應明確指定使用 `config.GetSensitive(key)` 而非直接存取底層 map `config.Values[key]`，以遵守 domain API 設計意圖，並使程式碼具備正確的語意清晰度。

---

**🟡 建議改善（Non-blocking）**

**[A4] `\r`（Carriage Return）未處理**

設計對 `\n` 返回 error，但 `\r` 同樣會破壞 `.env` 的行解析（特別是 Windows 換行 `\r\n`）。建議在 error 處理表中加入：

| 情境 | 處理方式 |
|------|---------|
| 值含 `\r` | 回傳 `nil, fmt.Errorf("env renderer: key %q value contains carriage return", key)` |

**[A5] 空 map（non-nil but empty）行為未說明**

`ResolveConfig` 保證 `Values` 永遠是有值的 map，但介面合約應明確說明「非 nil 的空 map 是合法輸入，產生空 `.env` 檔案（Content 為空 bytes）」，以使介面語意完整。

---

**✅ 架構亮點**

- `ComposeEnvRenderer` 為無狀態 struct，安全且正確；
- 字典序排序確保 deterministic output，利於 git diff；
- `Artifact.Mode = 0600` 正確保護 secrets；
- 錯誤訊息包含 key 名稱作為上下文，符合規範；
- Production Ready 段落充分；
- 整合測試免除理由清晰（純計算，無 I/O）。

**[Round 2] 狀態：APPROVED**
**意見：** 所有 Round 1 的五項問題均已正確修正：[A1] `$` → `\$` 跳脫規則已補入表格、跳脫順序與觸發條件；[A2] `config == nil` 保護已加入執行流程與錯誤處理表；[A3] `GetSensitive(key)` 已明確指定於流程步驟 4 與 `Render` godoc，並附禁止使用 `Get()` 的理由；[A4] `\r` 已在所有相關段落與 `\n` 並列處理；[A5] non-nil 空 map 合法性已在跳脫規則表與測試策略中說明。另注意：跳脫順序第 4 點「上述任一步驟後仍含控制字元」的措辭略有歧義（暗示跳脫步驟可能產生控制字元，實際上不可能），但主流程 step 5 的驗證先於 step 6 的跳脫描述清晰，功能上無影響，不構成阻礙。設計完整、正確，可進入實作。

### Reviewer B（實作）

- **狀態：** REVISE
- **意見：**

  **整體評估：** 結構清晰、可建置，但有 2 個必須修正的問題（1 個 Critical、1 個 Major），加上數個 Minor 項目。

  ---

  **[Critical] `$` 跳脫規則不正確**

  設計表格中「值含 `$` → `KEY="value"`（雙引號包裹）」**無法防止 godotenv 的 `$` 展開**。`godotenv`（以及 `compose-go/v2/dotenv`）在雙引號字串中仍會對 `$var` 進行環境變數展開；唯有單引號才能完全抑制展開，或在雙引號內以 `\$` 跳脫。

  可選的修正方案（擇一）：

  - **方案 A（建議）**：在跳脫表中補充：雙引號字串內的 `$` 必須跳脫為 `\$`，並在步驟 3 的跳脫邏輯中加入此規則（順序：`\` → `\\`、`"` → `\"`、`$` → `\$`）。
  - **方案 B**：改用單引號包裹，並以 `'\''` 跳脫值中出現的 `'`（較不常見，但對 `$` 天然安全）。
  - **方案 C（若確定不需要）**：明確說明「本系統產生的所有 value（hex、alphanumeric、JWT base64url、URL）均不含 `$`，使用者覆寫值亦應符合此限制」，並在 validation 中加入對 `$` 的檢查（若偵測到則 error），而非依賴跳脫。

  注意：實際上目前系統產生的 secret（hex、alphanumeric、JWT）均不含 `$`，但設計聲稱要「防止意外 shell 展開」，若不正確跳脫則此保證形同虛設。

  ---

  **[Major] 流程描述未指定使用 `GetSensitive()`**

  步驟 3「取對應 value」僅說明「取 config.Values 的 value」，但 `ProjectConfig` 提供了兩個存取方法：
  - `Get(key)` — sensitive 欄位回傳 `"***"`（遮罩版本）
  - `GetSensitive(key)` — 回傳真實值

  若實作者錯誤呼叫 `Get()`，所有 sensitive key 將被寫入 `"***"` 至 `.env` 檔，造成靜默的錯誤輸出（Supabase 服務無法啟動）且無任何 error 提示。

  **修正方式**：在執行流程的步驟 3 明確標注「必須使用 `GetSensitive()` 或直接存取 `config.Values[key]` map，禁止使用 `Get()`」。

  ---

  **[Minor] 函式 godoc 有誤**

  `Render` 的說明：「Returns a single Artifact; error is non-nil only if config.Values is nil.」描述不完整，值含 `\n` 時也會回傳 error。建議改為「error is non-nil if config.Values is nil or any value contains an unsupported character」。

  ---

  **[Minor] 跳脫順序未指定，潛藏 double-escape bug**

  值同時含 `\` 和 `"` 時（例如 `val\"ue`），跳脫必須依序：先 `\` → `\\`，再 `"` → `\"`。若順序相反，`"` 被跳脫為 `\"` 後，其中的 `\` 再次被跳脫為 `\\"` → 雙重跳脫。

  **修正方式**：在跳脫規則處加一條說明：「跳脫順序：① `\` → `\\`，② `"` → `\"`（若方案 A：③ `$` → `\$`）」，或在測試策略中加入此組合的測試案例強制驗證。

  ---

  **[Minor] `\r`（Carriage Return）未涵蓋**

  錯誤表與跳脫表均只處理 `\n`（LF），但 `\r`（CR）或 `\r\n`（CRLF）同樣無法在單行 `.env` 值中合法表示。建議補充：「值含 `\r` → 回傳 error」（或與 `\n` 合併為「含任何換行字元 `\n` 或 `\r`」）。

  ---

  **[Minor] `config.Overrides` 欄位未說明**

  設計只處理 `config.Values`，沒有解釋為何不需要處理 `config.Overrides`。應補一句話說明：「`Overrides` 已由 `ResolveConfig` 合併進 `Values`，renderer 無需另行處理。」避免實作者疑惑或誤加邏輯。

  ---

  **[Minor] 測試策略缺少跳脫組合案例**

  需新增：值同時含 `\` 和 `"` 的測試案例（驗證跳脫順序正確）。例如：`val\"ue` → `"val\\\"ue"`。

  ---

  以上 Critical 與 Major 問題修正後，設計即可通過實作審查。

---

**[Round 2] 狀態：APPROVED**
**意見：**

所有 Round 1 的 Critical / Major / Minor 問題均已正確修正，逐項確認如下：

✅ **[Critical] `$` 跳脫** — 跳脫規則表已加入「`$` → `\$`（雙引號內）」，說明明確指向 godotenv variable interpolation，觸發雙引號條件清單也已包含 `$`。

✅ **[Major] `GetSensitive()` 指定** — 執行流程步驟 4 明確標注「呼叫 `config.GetSensitive(key)`」，godoc 也同步說明「Sensitive values are obtained via GetSensitive」，並明確禁止使用 `Get()`。

✅ **[Minor] godoc 完整性** — 已涵蓋三種 error 情境（nil config、nil Values、含換行符），措辭準確。

✅ **[Minor] 跳脫順序** — 已在「跳脫執行順序」區塊列出三步驟：`\` → `\\`、`"` → `\"`、`$` → `\$`，測試策略也新增了 `\` + `"` 組合案例。

✅ **[Minor] `\r` 涵蓋** — 驗證步驟、錯誤表、godoc、測試策略均已加入 `\r`。

✅ **[Minor] `config.Overrides` 說明** — 已在 `Render` 簽章下方補充注意事項，解釋 `ResolveConfig()` 已預先合併 overrides。

---

**小缺點（不影響通過）：**

跳脫執行順序區塊的第 4 點「若值含上述任一步驟後仍含控制字元（`\n`、`\r`）→ error」，語意上略有歧義：讀起來像是跳脫之後才驗證，但 `\`/`"`/`$` 跳脫不會產生或消除 `\n`/`\r`，此檢查放在跳脫步驟之內邏輯上冗餘。主流程（步驟 5：驗證先於步驟 6：跳脫）是正確的，實作者應以主流程為準；此處措辭不影響實作正確性，後續可在 code review 階段修正為一致的文字。

---

## 任務

待審查通過後產生。

---

## 程式碼審查

**審查日期：** 2026-03-23
**審查工具：** code-review subagent（commit `f907122`）
**審查結果：** ✅ PASS

**發現問題：** 無

**修正記錄：** 無需修正
