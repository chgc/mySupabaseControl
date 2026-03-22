> **文件語言：繁體中文**
> 本專案所有文件均以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# Coding 規範

本文件定義本專案的程式碼撰寫標準。
所有實作都必須符合這些規範。審查閘門（見 `docs/REVIEW_GATEWAY.md`）在設計審查階段就會檢查合規性。

---

## Go（Control Plane 後端）

### 風格指南

所有 Go 程式碼**必須**遵循 [Google Go Style Guide](https://google.github.io/styleguide/go/)。
以下是關鍵規則摘要，但上游指南具有最高權威性。

### 格式化與工具鏈

- 提交前所有程式碼必須使用 `gofmt` 或 `goimports` 格式化。
- 所有套件執行 `go vet` 與 `staticcheck`。
- 使用 `golangci-lint`，最少啟用：`errcheck`、`govet`、`staticcheck`、`revive`、`gosimple`。

### 命名規則

- 套件名稱：全小寫，單一單字，無底線（例如 `adapter`、`domain`、`store`）。
- 匯出名稱：清晰、描述性，避免重複（例如 `adapter.DockerCompose`，而非 `adapter.DockerComposeAdapter`）。
- 縮寫詞：一致地大寫 — `URL`、`ID`、`HTTP`、`API`（例如 `ProjectID`，而非 `ProjectId`）。
- 錯誤變數：以 `Err` 前綴（例如 `var ErrProjectNotFound = errors.New(...)`）。
- 介面名稱：自然時使用 `-er` 後綴（例如 `RuntimeAdapter`），但以清晰為優先。

### 錯誤處理

- 絕不忽略錯誤。每一個 `error` 回傳值都必須被檢查。
- 使用 `fmt.Errorf("...: %w", err)` 加入上下文包裝錯誤。
- 不對可恢復的錯誤使用 `panic` — 回傳 `error`。
- 使用哨兵錯誤（sentinel errors）與 `errors.Is` / `errors.As` 進行型別化錯誤處理。

### 套件與目錄結構

遵循 `docs/CONTROL_PLANE.md` 中定義的佈局：

```
control-plane/
├── cmd/              # 僅作為 main 進入點；不包含任何商業邏輯
├── internal/
│   ├── api/          # HTTP handlers — 薄層，委派給 domain
│   ├── domain/       # 核心型別、介面、商業邏輯
│   ├── adapter/
│   │   ├── compose/  # Docker Compose Runtime Adapter
│   │   └── k8s/      # K8s Runtime Adapter（Phase 5）
│   └── store/        # Supabase 持久化層
```

- 商業邏輯放在 `internal/domain`，不放在 `internal/api`。
- `internal/api` 中的 handler 只做：解析請求 → 呼叫 domain → 寫入回應。
- 外部依賴透過介面注入，不在 domain 中直接 import。
- 避免全域狀態，使用依賴注入。

### 介面

- 介面定義在**使用**它的套件中，而非實作它的套件。
- 保持介面小巧 — 一到兩個方法為佳。
- `RuntimeAdapter` 介面定義於 `internal/domain`。

### 測試

- 測試檔案與原始碼並列：`foo.go` → `foo_test.go`。
- 多個案例使用表格驅動測試（table-driven tests）。
- 使用介面 mock 外部依賴（Supabase、Docker、shell 執行）。
- 著重 domain 邏輯的有意義覆蓋率；Adapter 進行整合測試。

### 並發

- 優先使用 `context.Context` 傳遞取消信號。
- 使用 `sync.Mutex` 或 channel 管理共享狀態 — 記錄選擇哪個及原因。
- 不啟動沒有明確生命週期邊界的 goroutine。

---

## Angular（Web 前端）

### 風格指南

所有 Angular 程式碼**必須**遵循 [Angular 官方最佳實踐指南](https://angular.dev/assets/context/best-practices.md)。
以下是關鍵規則摘要。

### 元件產生規則

**所有 Angular 元件、服務、pipe、guard 及其他 Angular artifacts，都必須透過 Angular CLI 指令產生。**
不允許手動建立 Angular artifact 檔案。

```bash
# 元件
ng generate component <name> --style=css

# 服務
ng generate service <name>

# Guard
ng generate guard <name>

# Pipe
ng generate pipe <name>
```

### 樣式表

- **只使用 CSS。** 不使用 SCSS、SASS 或 LESS。
- 所有 `ng generate component` 指令都必須加上 `--style=css`。

### 元件規範

- 一律使用**獨立元件（standalone components）**（Angular v20+ 中 `standalone: true` 為預設值，**不得**在 decorator 中明確設定）。
- 每個 `@Component` decorator 都必須設定 `changeDetection: ChangeDetectionStrategy.OnPush`。
- 使用 `input()` 與 `output()` signal 函式，不使用 `@Input()` / `@Output()` decorator。
- 使用 `computed()` 計算衍生狀態。
- 保持元件小而專注於單一職責。
- 所有靜態圖片使用 `NgOptimizedImage`。
- **不得**使用 `@HostBinding` 或 `@HostListener` — 改將 host 綁定放在 `@Component` 或 `@Directive` 的 `host` 物件中。
- **不得**使用 `ngClass` — 改用 `class` 綁定。
- **不得**使用 `ngStyle` — 改用 `style` 綁定。

### 模板規範

- 使用 Angular 原生控制流程：`@if`、`@for`、`@switch` — 不使用 `*ngIf`、`*ngFor`、`*ngSwitch`。
- 保持模板邏輯簡單；複雜邏輯移至元件類別或服務。
- 使用 `async` pipe 處理 observable。
- 外部模板與樣式使用相對於元件 TS 檔案的路徑。

### 狀態管理

- 使用 Angular **signals** 管理本地元件狀態。
- 使用 `computed()` 計算衍生值。
- 使用 `update()` 或 `set()` 更新 signal — 不使用 `mutate()`。
- 保持狀態轉換的純粹性與可預測性。

### 服務規範

- 每個服務只有單一職責。
- 使用 `providedIn: 'root'` 設定 singleton 服務。
- 使用 `inject()` 函式注入依賴，不使用建構子注入。

### 表單

- 優先使用 **Reactive Forms**，而非 Template-driven Forms。

### TypeScript

- 啟用 `strict` 模式。
- 型別明顯時優先使用型別推斷。
- 避免使用 `any`；型別不確定時使用 `unknown`。

### 無障礙性

- 所有元件必須通過 AXE 檢查。
- 遵循 WCAG AA 標準：焦點管理、顏色對比、ARIA 屬性。

---

## Commit 格式

所有 commit 必須遵循 **Conventional Commit** 格式：

```
<type>(<scope>): <簡短描述>

[選填 body]

[選填 footer]
```

### Type 類型

| Type | 使用時機 |
|---|---|
| `feat` | 新功能 |
| `fix` | 錯誤修正 |
| `docs` | 只修改文件 |
| `style` | 不影響邏輯的格式調整 |
| `refactor` | 既非修錯也非新功能的程式碼重構 |
| `test` | 新增或更新測試 |
| `chore` | 建置、工具、依賴套件變更 |
| `ci` | CI/CD 設定變更 |
| `perf` | 效能改善 |

### Scope 範圍

使用一致的 scope 標識程式碼的區域：

| Scope | 區域 |
|---|---|
| `cp` | Control Plane 後端（Go） |
| `web` | Angular 前端 |
| `adapter/compose` | Docker Compose Runtime Adapter |
| `adapter/k8s` | K8s Runtime Adapter |
| `domain` | Domain 模型與介面 |
| `store` | 持久化層 |
| `api` | HTTP API handlers |
| `scripts` | Shell/PS1 腳本 |
| `docs` | 文件 |
| `infra` | 基礎設施與部署 |

### 範例

```
feat(cp/domain): 定義 RuntimeAdapter 介面與 ProjectModel struct

fix(cp/adapter/compose): 修正自動 port 分配時的衝突處理

docs(docs): 新增 REVIEW_GATEWAY 與 CODING_GUIDELINES 文件

feat(web): 新增包含狀態指標的專案總覽元件

chore(cp): 設定 golangci-lint 並啟用 errcheck 與 staticcheck
```

### Breaking Changes

在 type/scope 後加上 `!`，並在 footer 加入 `BREAKING CHANGE:`：

```
feat(cp/api)!: 專案 slug 在建立後即不可變更

BREAKING CHANGE: PATCH /projects/:slug/rename 端點已移除。Slug 在建立時設定，之後無法更改。
```

---

## 任務完成檢查清單

每個任務標記為完成前，必須確認以下所有項目：

- [ ] Go：`gofmt`、`go vet`、`staticcheck` 無任何錯誤
- [ ] Go：所有錯誤都已檢查並加上上下文包裝
- [ ] Go：`cmd/` 與 `internal/api/` 中不包含商業邏輯
- [ ] Angular：所有 artifact 都透過 Angular CLI 產生
- [ ] Angular：每個元件都設定了 `OnPush`
- [ ] Angular：使用 signals 管理狀態，不使用 `@Input`/`@Output` decorator
- [ ] Angular：使用原生控制流程（`@if`、`@for`）— 不使用結構性指令
- [ ] Angular：只使用 CSS（不使用 SCSS/SASS/LESS）
- [ ] Commit 訊息遵循 Conventional Commit 格式
- [ ] 本功能存在狀態為 `approved` 的設計文件
