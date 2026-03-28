> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# 設計文件：K8s Values Renderer

## 狀態

done

## Phase

- **Phase：** Phase 6
- **Phase Plan：** `docs/designs/phase-6-plan.md`

---

## 目的

K8s adapter 需要將 `ProjectConfig` 轉換為 Helm chart 可使用的 `values.yaml` 檔案。本功能實作
`domain.ConfigRenderer` 介面的 K8s 版本，透過組合 Feature 2 提供的 `HelmValuesMapper`（將
`ProjectConfig` 轉為 `map[string]any` 樹狀結構）與 `yaml.Marshal`（將樹狀結構序列化為 YAML），
產出單一個 `values.yaml` Artifact。

此設計為純粹的轉換層（adapter），不涉及任何 I/O 或外部服務呼叫。與 Docker Compose 的
`ComposeEnvRenderer`（產出 `.env` 檔案）對稱，K8s 端由 `K8sValuesRenderer` 產出 `values.yaml`。

---

## 範圍

### 包含

- `K8sValuesRenderer` struct：實作 `domain.ConfigRenderer` 介面
- `NewK8sValuesRenderer()` 建構函式
- `Render(*ProjectConfig) ([]Artifact, error)` 方法
- 使用 `HelmValuesMapper.MapValues()` 取得 values 樹
- 使用 `gopkg.in/yaml.v3` 將樹序列化為 YAML bytes
- 完整的單元測試覆蓋

### 不包含

- `HelmValuesMapper` 本身的實作（屬於 Feature 2，已完成）
- `helm install` / `kubectl apply` 操作（屬於 K8s Adapter 後續功能）
- ConfigMap / Secret 的分拆（本版本產出單一 `values.yaml`）
- 修改現有的 `ComposeEnvRenderer` 或 `domain.ConfigRenderer` 介面

---

## 資料模型

### 輸入

```go
// domain.ProjectConfig — 包含 93 個 key-value 對的專案設定
type ProjectConfig struct {
    ProjectSlug string
    Values      map[string]string
    Overrides   map[string]string
}

// GetSensitive 回傳指定 key 的原始值（包含敏感值）。
// 僅在需要真實值的上下文中使用（例如 ConfigRenderer.Render）。
func (c *ProjectConfig) GetSensitive(key string) (string, bool)
```

### 輸出

```go
// domain.Artifact — 代表一個已渲染的設定檔案
type Artifact struct {
    Path    string // "values.yaml"
    Content []byte // YAML 序列化後的 bytes
    Mode    uint32 // 0600
}
```

### 內部中間結構

```go
// HelmValuesMapper.MapValues() 回傳的 Helm values 樹狀結構
map[string]any{
    "studio": map[string]any{
        "environment": map[string]any{...},
    },
    "auth": map[string]any{
        "environment": map[string]any{...},
    },
    // ... 其他 Helm chart 元件
}
```

---

## 介面合約

### ConfigRenderer 介面（已定義於 `domain/renderer.go`）

```go
type ConfigRenderer interface {
    Render(config *ProjectConfig) ([]Artifact, error)
}
```

### K8sValuesRenderer struct

```go
// K8sValuesRenderer renders ProjectConfig into a Helm values.yaml Artifact.
type K8sValuesRenderer struct {
    mapper *HelmValuesMapper
}

// 靜態介面斷言 — 編譯期確保實作正確
var _ domain.ConfigRenderer = (*K8sValuesRenderer)(nil)
```

### 建構函式

```go
func NewK8sValuesRenderer() *K8sValuesRenderer {
    return &K8sValuesRenderer{mapper: NewHelmValuesMapper()}
}
```

### Render 方法簽名

```go
func (r *K8sValuesRenderer) Render(config *domain.ProjectConfig) ([]domain.Artifact, error)
```

### 依賴的 HelmValuesMapper 介面（Feature 2，`internal/adapter/k8s/helm_values_mapper.go`）

```go
func NewHelmValuesMapper() *HelmValuesMapper
func (m *HelmValuesMapper) MapValues(config *ProjectConfig) (map[string]any, error)
```

---

## 執行流程

`Render` 方法的逐步執行流程：

1. **檢查 `config` 是否為 `nil`**（Renderer 自行檢查）
   - 若 `config == nil`，直接回傳 `fmt.Errorf("k8s renderer: config is nil")`
   - 與 `ComposeEnvRenderer` 採用相同的 guard 模式

2. **檢查 `config.Values` 是否為 `nil`**（Renderer 自行檢查）
   - 若 `config.Values == nil`，直接回傳 `fmt.Errorf("k8s renderer: config.Values is nil")`
   - Go 允許讀取 nil map（回傳零值），不會 panic，因此 `MapValues` 不會為此回傳錯誤
   - 必須在呼叫 `MapValues` 之前明確檢查，以避免產出空白的 `values.yaml`

3. **呼叫 `mapper.MapValues(config)`**
   - 將 `ProjectConfig` 轉換為 `map[string]any` 樹狀結構
   - 若有轉換錯誤（例如無效的 port 字串），`MapValues` 回傳錯誤
   - 任何錯誤直接向上傳播，不額外包裝

4. **呼叫 `yaml.Marshal(tree)`**
   - 將 `map[string]any` 序列化為 YAML 格式的 `[]byte`
   - 若序列化失敗，回傳包裝過的錯誤

5. **建構並回傳 Artifact**
   - 回傳 `[]domain.Artifact`，僅包含一個元素
   - `Path`: `"values.yaml"`
   - `Content`: 步驟 4 產出的 YAML bytes
   - `Mode`: `0600`（owner-read/write，保護 secret 值）

```
┌──────────────────┐
│ Render(config)   │
└────────┬─────────┘
         │
         ▼
┌──────────────────────────────┐
│ config == nil?               │
│ → 回傳 "k8s renderer:       │
│   config is nil"             │
└────────┬─────────────────────┘
         │ no
         ▼
┌──────────────────────────────┐
│ config.Values == nil?        │
│ → 回傳 "k8s renderer:       │
│   config.Values is nil"      │
└────────┬─────────────────────┘
         │ no
         ▼
┌──────────────────────────────┐
│ mapper.MapValues(config)     │
│ → map[string]any 或 error   │
└────────┬─────────────────────┘
         │ error? → 直接回傳
         ▼
┌──────────────────────────────┐
│ yaml.Marshal(tree)           │
│ → []byte 或 error            │
└────────┬─────────────────────┘
         │ error? → 包裝後回傳
         ▼
┌──────────────────────────────┐
│ 回傳 []Artifact{             │
│   Path:    "values.yaml"     │
│   Content: yamlBytes         │
│   Mode:    0600              │
│ }                            │
└──────────────────────────────┘
```

---

## 錯誤處理

| 情境 | 處理方式 | 回應格式 |
|---|---|---|
| `config` 為 `nil` | `K8sValuesRenderer.Render` 自行檢查，直接回傳錯誤 | `fmt.Errorf("k8s renderer: config is nil")` |
| `config.Values` 為 `nil` | `K8sValuesRenderer.Render` 自行檢查，直接回傳錯誤 | `fmt.Errorf("k8s renderer: config.Values is nil")` |
| 轉換錯誤（如無效 port 字串） | `MapValues` 回傳錯誤，直接向上傳播 | `MapValues` 定義的 error |
| `yaml.Marshal` 失敗 | 包裝錯誤後回傳 | `fmt.Errorf("k8s renderer: yaml marshal: %w", err)` |

### 錯誤處理原則

- `Render` 方法自行檢查 `nil` config 與 `nil` Values，與 `ComposeEnvRenderer` 採用一致的 guard 模式
- `MapValues` 的錯誤不再額外包裝，因為 `MapValues` 已提供足夠的錯誤上下文
- `yaml.Marshal` 的錯誤加上 `"k8s renderer: yaml marshal:"` 前綴，以便在呼叫鏈中定位問題來源
- 所有錯誤皆透過 `%w` 包裝（適用時），支援 `errors.Is` / `errors.As` 解包

---

## 測試策略

### 需要測試的行為

- nil config 輸入時回傳 Renderer 自身的錯誤訊息（`"k8s renderer: config is nil"`）
- nil Values 輸入時回傳 Renderer 自身的錯誤訊息（`"k8s renderer: config.Values is nil"`）
- 有效 config 輸入時回傳單一 Artifact
- 產出的 Artifact `Path` 為 `"values.yaml"`
- 產出的 Artifact `Mode` 為 `0600`
- 產出的 YAML 內容可被反序列化，且包含預期的巢狀 key 結構
- 敏感值（sensitive values）在輸出中以明文出現（未遮罩）
- 恰好回傳 1 個 Artifact（不多不少）
- 無效 port 字串等轉換錯誤被正確傳播

### 測試類型分配

| 測試類型 | 測試目標 | 層次 |
|---|---|---|
| 單元測試 | `TestRender_NilConfig` — nil config 回傳 `"k8s renderer: config is nil"`（Renderer 自行 guard） | adapter |
| 單元測試 | `TestRender_NilValues` — nil Values 回傳 `"k8s renderer: config.Values is nil"`（Renderer 自行 guard，Go 允許讀取 nil map 不會 panic） | adapter |
| 單元測試 | `TestRender_ValidConfig` — 有效 config 回傳單一 Artifact，Path=`"values.yaml"`，Mode=`0600` | adapter |
| 單元測試 | `TestRender_YAMLStructure` — 反序列化 output，驗證巢狀 key 存在 | adapter |
| 單元測試 | `TestRender_SecretValues` — 驗證敏感值在輸出中以明文出現（未遮罩） | adapter |
| 單元測試 | `TestRender_ArtifactCount` — 恰好回傳 1 個 Artifact | adapter |
| 單元測試 | `TestRender_TransformError` — 無效 port 字串觸發錯誤回傳 | adapter |

### Mock 策略

- **不需要 mock**：`K8sValuesRenderer` 直接組合 `HelmValuesMapper`（同 package 內的具體 struct），
  測試時使用真實的 `HelmValuesMapper` 實例
- `yaml.Marshal` 為標準函式庫，不需 mock
- 測試資料使用手動建構的 `domain.ProjectConfig`，包含代表性的 key-value 組合

### CI 執行方式

- 所有測試為純粹的單元測試，無外部依賴
- 在一般 CI pipeline 中透過 `go test ./internal/adapter/k8s/...` 執行
- 不需要 Docker、K8s cluster 或其他特殊環境

---

## Production Ready 考量

### 錯誤處理

- 所有錯誤皆攜帶上下文資訊向上傳播
- `MapValues` 錯誤直接傳播（已含完整上下文）
- `yaml.Marshal` 錯誤以 `"k8s renderer: yaml marshal:"` 前綴包裝
- 使用 `%w` wrapping，支援 `errors.Is` / `errors.As` 解包比對

### 日誌與可觀測性

- 不適用：本功能為純粹的記憶體內轉換，不涉及 I/O 操作
- 錯誤由呼叫端負責記錄日誌

### 輸入驗證

- `nil` config 檢查：由 `Render` 方法自行 guard，回傳 `"k8s renderer: config is nil"`
- `nil` Values 檢查：由 `Render` 方法自行 guard，回傳 `"k8s renderer: config.Values is nil"`
- 個別欄位值驗證（如 port 格式）：由 `MapValues` 的轉換邏輯處理
- 以上 guard 模式與 `ComposeEnvRenderer` 一致

### 安全性

- Artifact `Mode` 設為 `0600`（owner-read/write），保護 `values.yaml` 中的 secret 值
- 與 `ComposeEnvRenderer` 的 `.env` 檔案使用相同的權限模式
- 敏感值在 `values.yaml` 中以明文出現（Helm chart 需要），因此檔案權限為唯一保護層

### 優雅降級

- 不適用：本功能為純粹的記憶體內轉換，無外部依賴
- 不涉及網路呼叫、檔案 I/O 或其他可能失敗的外部操作

### 設定管理

- 不需要額外的環境變數或設定項
- 輸出路徑 `"values.yaml"` 為固定值，由呼叫端（K8s Adapter）決定完整的檔案寫入路徑

---

## 影響檔案

| 檔案路徑 | 操作 |
|---|---|
| `internal/adapter/k8s/values_renderer.go` | 新增 |
| `internal/adapter/k8s/values_renderer_test.go` | 新增 |

---

## 待決問題

- 無。本設計為直接的 adapter 層，組合已完成的 `HelmValuesMapper`（Feature 2）與標準的
  `yaml.Marshal`，設計清晰且無歧義。

---

## 審查

### Reviewer A（架構）

- **狀態：** APPROVED（Round 2）
- **意見：**
  - Round 1：nil Values 的錯誤路徑描述不正確，建議 Renderer 自行檢查 → 已修正
  - Round 2：APPROVED

### Reviewer B（實作）

- **狀態：** APPROVED（Round 2）
- **意見：**
  - Round 1：ProjectConfig 資料模型有誤；nil config/Values 檢查應在 Renderer 自行 guard → 已修正
  - Round 2：APPROVED

---

## 任務

<!-- 兩位審查者都回覆 APPROVED 後，根據設計展開為具體可執行的任務。 -->

---

## 程式碼審查

<!-- 所有任務完成後，由 code-review subagent 審查 feature branch 對 main 的完整 diff -->

- **審查結果：** FIX_REQUIRED → PASS
- **發現問題：** gofmt 格式化問題
- **修正記錄：** 執行 `gofmt -w` 修正格式
