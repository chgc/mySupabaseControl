> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# 設計文件：Compose `.env` Config Renderer

## 狀態

in_review

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
// Returns a single Artifact; error is non-nil only if config.Values is nil.
func (r *ComposeEnvRenderer) Render(config *domain.ProjectConfig) ([]domain.Artifact, error)
```

---

## 執行流程

```
Render(config)
│
├─ 1. 驗證 config.Values 不為 nil
│     若為 nil：回傳 nil, fmt.Errorf("env renderer: config.Values is nil")
│
├─ 2. 取出 config.Values 的所有 key，進行字典序排序
│     （確保輸出具決定性，方便比對與 git diff）
│
├─ 3. 對每個 key，取對應 value，寫入：
│     "<KEY>=<escaped_value>\n"
│
├─ 4. 產生 Artifact：
│     Path:    ".env"
│     Content: 以上步驟產生的 bytes
│     Mode:    0600
│
└─ 5. 回傳 []domain.Artifact{artifact}, nil
```

### 值跳脫規則（`.env` 格式）

Docker Compose v2 的 `.env` 檔案解析遵循以下規則（參考 `godotenv` 行為）：

| 條件 | 輸出格式 | 說明 |
|------|---------|------|
| 值為空字串 | `KEY=` | 允許空值 |
| 值不含特殊字元 | `KEY=value` | 直接輸出 |
| 值含 `#`、空格、`\t`、`$` | `KEY="value"` | 雙引號包裹 |
| 值含 `"` | `KEY="val\"ue"` | 雙引號內跳脫 `"` → `\"` |
| 值含 `\` | `KEY="val\\ue"` | 雙引號內跳脫 `\` → `\\` |
| 值含換行符 `\n` | 回傳 error | `.env` 不支援多行值 |

**特殊字元定義：** `#`、空格（`\x20`）、Tab（`\t`）、`$`（防止意外 shell 展開）

> **實務說明：** Supabase 自動產生的 secret 均為 hex 或 alphanumeric 字串，JWT 以 base64url 編碼，URLs 以標準格式儲存。正常使用下不會觸發雙引號路徑，但規則明確定義以確保使用者覆寫值的安全性。

---

## 錯誤處理

| 情境 | 處理方式 |
|------|---------|
| `config.Values == nil` | 回傳 `nil, fmt.Errorf("env renderer: config.Values is nil")` |
| 值含換行符 `\n` | 回傳 `nil, fmt.Errorf("env renderer: key %q value contains newline", key)` |

---

## 測試策略

### 需要測試的行為

- **基本輸出**：有三個 key-value 的 config，驗證輸出格式正確
- **字典序排序**：輸入 keys 為亂序，驗證輸出按字母排序
- **空值**：`KEY=` 格式
- **需要雙引號的值**：含 `#`、空格、`$` 的值用雙引號包裹
- **引號跳脫**：值含 `"` → `\"`，值含 `\` → `\\`
- **換行 error**：值含 `\n` 時回傳 error
- **Artifact 屬性**：Path == `.env`，Mode == `0600`
- **nil config.Values error**：回傳適當 error
- **決定性輸出**：相同輸入多次呼叫結果相同

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

- **狀態：** 待審查
- **意見：**

### Reviewer B（實作）

- **狀態：** 待審查
- **意見：**

---

## 任務

待審查通過後產生。

---

## 程式碼審查

- **審查結果：** 待實作完成後審查
- **發現問題：**
- **修正記錄：**
