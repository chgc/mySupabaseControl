> **文件語言：繁體中文**
> 本專案所有文件（設計文件、審查記錄、規範說明）均以**繁體中文**撰寫。
> 程式碼本身（變數名稱、函式名稱、註解）及技術術語（如 slug、adapter、signal、runtime）保留英文。

---

# 審查閘門（Review Gateway）規則

本專案所有功能在實作前，都必須通過 **審查閘門（Review Gateway）** 的審核。
任何尚未通過此流程取得 APPROVED 狀態的功能，不得開始撰寫程式碼。

---

## 核心原則

> **先設計，再寫碼。先審查，再實作。測試完整，方為完成。**

每個可交付成果（deliverable）都必須遵循以下順序：

```
Phase 規劃 → 設計 → 審查（2 個 subagent） → APPROVED → 產生任務 → 實作 → 程式碼審查（code-review subagent） → DONE
```

任何步驟都不得跳過。功能必須到達 **APPROVED** 狀態後，才能開始實作。
實作完成後，必須通過所有測試完整度與 production ready 要求，才能標記為 **DONE**。

---

## 流程階段

### 前置階段 — Phase 規劃

每個 Phase 開始前，必須先產出一份 **Phase Plan 文件**（使用 `docs/designs/_PHASE_TEMPLATE.md` 範本）。

Phase Plan 負責將 `docs/CONTROL_PLANE.md` 中的 Phase 高階描述，拆解為具體的功能清單：

1. **建立 Phase Plan** — 複製 `_PHASE_TEMPLATE.md` 為 `docs/designs/phase-<N>-plan.md`
2. **功能拆解** — 將 Phase 的 deliverables 分解為可獨立設計與實作的功能單位
3. **定義依賴** — 標記功能間的依賴關係，決定建議實作順序
4. **定義退出標準** — 明確此 Phase 完成的驗收條件

Phase Plan 完成後，依照功能拆解清單與建議順序，逐一進入下方的功能設計流程。

**跨功能依賴處理規則：**

- 若功能 A 的設計依賴功能 B 的介面定義，功能 B 的設計文件必須先達到 `approved` 狀態
- 若功能 A 與功能 B 無依賴關係，可平行進行設計與審查
- 功能間的共用資料模型或介面，應在依賴鏈最上游的功能設計中定義

**Phase 完成驗收：**

- 所有功能的設計文件狀態為 `done`
- Phase Plan 中的退出標準全部滿足
- Phase 整合驗證通過（編譯、測試、煙霧測試）
- Phase Plan 文件狀態更新為「已完成」

---

### 第一階段 — 設計

在撰寫任何程式碼之前，必須先為該功能產出一份設計文件。設計文件必須涵蓋：

- **目的** — 這個功能解決什麼問題？
- **範圍** — 包含哪些內容？不包含哪些內容？
- **資料模型** — Struct、DB schema、API 請求/回應格式。
- **介面合約** — 函式簽名、API 端點定義、Adapter 介面。
- **執行流程** — runtime 執行時的逐步描述。
- **錯誤處理** — 已知的失敗情境與各自的處理方式。
- **測試策略** — 詳細描述測試計畫（見下方「測試完整度要求」章節）。
- **待決問題** — 尚未確定的事項。

設計文件存放於 `docs/designs/`。命名格式：`<feature-slug>.md`。
可使用 `docs/designs/_TEMPLATE.md` 作為起始範本。

### 第二階段 — 審查

設計文件完成後，必須送交給 **兩個獨立的 review subagent** 進行審查。
兩份審查都完成後，功能才能推進。

每位審查者評估以下項目：

| 類別                        | 評估問題                                                                           |
| --------------------------- | ---------------------------------------------------------------------------------- |
| **正確性**                  | 設計是否正確地解決了問題？                                                         |
| **完整性**                  | 是否有遺漏的情境、錯誤路徑或邊界條件？                                             |
| **介面清晰度**              | 合約、schema 與流程是否無歧義？                                                    |
| **一致性**                  | 設計是否與專案架構及既有決策一致？                                                 |
| **Coding guideline 合規性** | 設計是否暗示了任何違反 Go 或 Angular 規範的實作？                                  |
| **測試策略充分性**          | 測試策略是否涵蓋所有關鍵路徑、錯誤路徑與邊界條件？是否達到 production ready 基準？ |
| **Production Ready 評估**   | 設計是否考慮了日誌、可觀測性、錯誤回報、優雅降級等生產環境需求？                   |

每位審查者回覆以下三種結果之一：

- ✅ **APPROVED** — 設計可行，測試策略充分，可推進至產生任務階段。
- 🔁 **REVISE** — 設計有需要修正的問題，必須處理後重新審查。審查者須具體列出反對意見。
- ❌ **REJECTED** — 設計根本上有缺陷，必須重新思考。

**兩位審查者都必須回覆 APPROVED，功能才能推進。**
任何一位回覆 REVISE 或 REJECTED 都會擋住功能推進。

若審查者回覆 REVISE，設計者須處理反對意見、更新設計文件，然後重複審查流程。兩位相同的審查者重新審查修訂後的文件。

### 第三階段 — 產生任務

兩位審查者都回覆 APPROVED 後，將設計展開為具體可執行的任務。

每個任務必須：

- 引用來源設計文件。
- 可獨立執行（無隱含的前置依賴）。
- 明確指出建立或修改哪些檔案。
- 足夠小，可在一次專注的工作階段中完成並驗證。
- 包含明確的驗收標準。
- 包含對應的測試任務（測試任務與實作任務並列，不可省略）。

任務追蹤於 session SQL 資料庫（`todos` 資料表）以及設計文件的 `## 任務` 段落中。

**任務狀態追蹤規則：**

每個任務必須在以下兩處同步追蹤狀態：

1. **SQL `todos` 資料表** — 狀態值為 `pending`、`in_progress`、`done`（或 `blocked`）
2. **設計文件 `## 任務` 段落** — 完成的任務標記為「✅ 已完成」

狀態流轉順序如下：

```
pending → in_progress（開始前設定）→ done（完成並通過驗收後設定）
```

實作者在**開始**任務前必須將狀態改為 `in_progress`；**完成**並通過所有驗收標準且已提交 commit 後，才能將狀態改為 `done`。

### 第四階段 — 實作

任務依照依賴順序實作。每個任務必須：

- 遵循 `docs/CODING_GUIDELINES.md` 中的 coding 規範。
- 使用 Conventional Commit 格式提交（詳見 `docs/CODING_GUIDELINES.md`）。
- 通過完整測試套件（`go test -race ./...`）後才能標記為完成，不得只執行「相關」測試。
- 符合本文件「Production Ready 要求」章節中的所有基準。

**每任務一 Commit 規則：**

每個任務完成後，必須**立即**以一個獨立的 commit 提交，且須符合以下條件：

- 遵循 Conventional Commit 格式，scope 帶有功能 slug（例如 `feat(cp/domain): ...`）
- commit message 的 body 或 footer 中記錄對應任務 ID（SQL `todos.id`）
- commit 提交成功是將任務狀態設為 `done` 的**前置條件**，未 commit 不得標記完成

**預設使用 Git Worktree 工作流程**（詳見下方「Git Worktree 工作流程」章節）。

**預提交驗證指令：**

每次 commit 前，必須**依序執行**以下指令，全部無錯誤通過後才能提交：

```bash
# 1. 編譯檢查
go build ./...

# 2. 靜態分析
go vet ./...

# 3. Linter（若已設定 golangci-lint）
golangci-lint run ./...

# 4. 完整測試套件（含 race condition 檢查）
go test -race -count=1 ./...

# 5. Angular 前端（若有修改）
ng build
ng test --watch=false
```

若任一指令失敗，必須修正問題後重新執行全部指令，不得跳過。
此指令序列是任務完成的**硬性前置條件**，不可用「我認為程式碼沒問題」替代實際執行。

### 第五階段 — 程式碼審查

所有任務實作完成後、功能標記為 DONE 前，必須對 feature branch 的完整變更進行程式碼審查。

**審查方式：**

啟動一個 `code-review` subagent，以 feature branch 對 `main` 的完整 diff 作為輸入。

**審查者評估以下項目：**

| 類別               | 評估問題                                                                      |
| ------------------ | ----------------------------------------------------------------------------- |
| **正確性**         | 程式碼是否正確實現了設計文件中描述的行為？                                    |
| **一致性**         | 程式碼風格是否與既有程式碼一致？命名、結構、錯誤處理模式是否統一？            |
| **安全性**         | 是否有 secret 洩漏、未驗證的輸入、或不安全的操作？                            |
| **錯誤處理**       | 所有 error 是否都有檢查？錯誤訊息是否提供足夠上下文？是否有使用 panic？       |
| **測試品質**       | 測試是否真正驗證了行為（非僅斷言 `err == nil`）？是否涵蓋錯誤路徑與邊界條件？ |
| **複雜度**         | 是否有不必要的複雜度或冗餘程式碼？是否可以更簡單地達成同樣效果？              |
| **Guideline 合規** | 是否符合 `docs/CODING_GUIDELINES.md` 的所有要求？                             |

**審查結果：**

- ✅ **PASS** — 程式碼品質符合標準，可進行 merge 並標記為 DONE。
- 🔁 **FIX_REQUIRED** — 審查者發現問題，必須修正後重新審查。審查者須具體列出每個問題。

審查者回覆 FIX_REQUIRED 時，實作者須修正所有問題、重新執行預提交驗證指令、提交修正 commit，然後重新送交審查。

**審查上下文：**

審查者在審查時必須同時參考：

1. 對應的設計文件
2. Feature branch 的完整 diff
3. 被修改檔案所在 package 的既有程式碼（用以判斷一致性）

---

## Git Worktree 工作流程

### 預設工作模式

所有功能實作**預設**在獨立的 git worktree 中進行，以隔離 feature branch 與主線開發。

```bash
# 建立 feature worktree（從 main 建立新 branch）
git worktree add ../localsupabase-<feature-slug> -b feat/<feature-slug>

# 進入 worktree 目錄後開始實作
cd ../localsupabase-<feature-slug>
```

### 工作流程步驟

1. **建立 worktree**：以功能 slug 為名，從最新的 `main` 建立 feature branch
2. **逐任務實作**：在 feature worktree 中完成各任務，每個任務完成後立即 commit（見「每任務一 Commit 規則」）
3. **標記任務完成**：commit 成功後，更新 SQL `todos` 狀態為 `done`，並在設計文件 `## 任務` 標記「✅ 已完成」
4. **功能完成後 merge**：所有任務完成後，回到主 worktree 進行 merge
   ```bash
   cd ../localsupabase
   git merge feat/<feature-slug>
   ```
5. **清理 worktree**：merge 完成後移除 feature worktree
   ```bash
   git worktree remove ../localsupabase-<feature-slug>
   git branch -d feat/<feature-slug>
   ```

### Merge Conflict 解決規則

遇到 merge conflict 時，不得略過或強制覆蓋，必須依照以下規則逐一解決，直到系統恢復正常運作：

#### 解決原則

| 衝突類型                                                | 解決方式                                                                                                |
| ------------------------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| **邏輯衝突**（兩側都有實質程式碼變更）                  | 不得使用 `--ours` 或 `--theirs`；必須審查兩側變更，整合兩者邏輯，必要時重構，確保功能完整且系統正常運作 |
| **文件衝突**（兩側都修改了同一文件）                    | 合併兩側說明，確保資訊不遺失、語意一致                                                                  |
| **設定檔衝突**（`go.mod`、`go.sum`、`package.json` 等） | 以語意上更新的版本為主，確認 build 與 test 全部通過後再提交                                             |
| **自動產生檔案衝突**                                    | 重新執行對應的產生指令（如 `go mod tidy`、`ng generate`），不手動編輯                                   |

#### 解決後的驗證要求

在提交 merge commit 之前，必須執行完整驗證：

```bash
# Go 後端
go build ./...
go test -race ./...

# Angular 前端（如有修改）
ng build
ng test --watch=false
```

所有指令必須**無錯誤通過**，才能提交 merge commit。

#### Merge 後煙霧測試

Merge commit 提交後、清理 worktree 前，必須在主 worktree 中執行煙霧測試：

```bash
# 確認編譯成功
go build ./...

# 確認完整測試套件通過
go test -race ./...

# 確認主要進入點可正常啟動（啟動後立即 ctrl+c 即可）
# 若有 Angular 前端，確認 ng build 成功
```

煙霧測試失敗表示 merge 引入了問題，必須修正後再進行清理。

#### Merge Commit 格式

Merge commit 訊息必須說明衝突的處理方式：

```
merge: 合併 feat/<feature-slug> 至 main

- 解決 <檔案路徑> 的邏輯衝突：整合兩側對 <說明> 的變更
- 解決 go.mod 版本衝突：以 feat/<feature-slug> 版本為主，執行 go mod tidy 驗證
```

---

## 測試完整度要求

所有功能在標記為 DONE 前，必須滿足以下測試完整度基準。

### Go 後端測試

| 測試類型                | 要求                                                                                                      |
| ----------------------- | --------------------------------------------------------------------------------------------------------- |
| **單元測試**            | 所有 `internal/domain` 中的商業邏輯都必須有對應的單元測試；外部依賴（DB、shell、Docker）透過介面 mock     |
| **整合測試**            | Runtime Adapter 必須有針對實際 Docker / K8s 的整合測試，可透過 build tag 區分（`//go:build integration`） |
| **錯誤路徑測試**        | 每個已知的失敗情境（見設計文件「錯誤處理」段落）都必須有對應的測試案例                                    |
| **API handler 測試**    | 每個 HTTP handler 必須有 happy path 與至少一個錯誤 path 的測試，使用 `httptest`                           |
| **覆蓋率基準**          | `internal/domain` 與 `internal/api` 的行覆蓋率（line coverage）不低於 **80%**                             |
| **Race condition 檢查** | 所有測試以 `go test -race` 執行，必須無 race condition                                                    |

### Angular 前端測試

| 測試類型         | 要求                                                                        |
| ---------------- | --------------------------------------------------------------------------- |
| **元件單元測試** | 每個元件都必須有使用 `TestBed` 的單元測試，涵蓋主要渲染路徑與互動行為       |
| **服務單元測試** | 每個服務都必須有單元測試，外部 HTTP 呼叫使用 `HttpClientTestingModule` mock |
| **錯誤狀態測試** | API 呼叫失敗、空資料、載入中等狀態都必須有對應的測試案例                    |

### 測試策略設計文件要求

設計文件的「測試策略」段落必須明確說明：

1. **哪些行為需要被測試**（列舉關鍵路徑與錯誤路徑）
2. **測試類型分配**（單元 / 整合 / e2e，各測試哪些層次）
3. **Mock 策略**（哪些外部依賴需要被 mock，使用什麼方式）
4. **CI 執行方式**（哪些測試在 CI 中執行，哪些需要特殊環境）

若設計文件的測試策略不完整，審查者應回覆 REVISE。

---

## Production Ready 要求

所有功能在標記為 DONE 前，必須滿足以下 production ready 基準：

### 錯誤處理

- [ ] 所有可預期的錯誤都有明確的處理邏輯，不使用 `panic`
- [ ] API 錯誤回應使用一致的錯誤格式（含 error code 與 message）
- [ ] 使用者面對的錯誤訊息不暴露系統內部資訊

### 日誌與可觀測性

- [ ] 關鍵操作（專案建立、啟動、停止、刪除）都有結構化日誌記錄
- [ ] 日誌包含足夠的上下文（project slug、操作類型、時間戳記）
- [ ] 錯誤日誌包含 stack trace 或足夠的錯誤上下文，方便除錯
- [ ] API 請求都有存取日誌（method、path、status code、duration）

### 輸入驗證

- [ ] 所有 API 輸入都在 handler 層驗證，非法輸入返回 400 並附說明
- [ ] 所有 CLI 參數都有邊界值驗證
- [ ] 資料庫寫入前完成型別與範圍驗證

### 安全性基準

- [ ] Secret 值（JWT_SECRET、POSTGRES_PASSWORD 等）不出現在日誌或 API 回應中
- [ ] API 端點有基本認證機制（至少 API key 或 local-only 存取控制）
- [ ] 資料庫連線使用最小權限原則

### 優雅降級

- [ ] 外部依賴（Docker、Supabase）不可用時，服務仍能啟動並回傳適當錯誤
- [ ] 長時間操作（compose up）有 timeout 控制
- [ ] Context cancellation 正確傳遞至所有非同步操作

### 設定管理

- [ ] 所有設定值都可透過環境變數覆寫，不硬編碼
- [ ] 必要的設定缺失時，服務啟動時即報錯並退出（fail fast）

---

## 審查 Subagent 角色

每次設計審查指派兩個 subagent：

| 角色                  | 職責                                                                                                          |
| --------------------- | ------------------------------------------------------------------------------------------------------------- |
| **Reviewer A — 架構** | 評估設計是否符合專案架構、adapter 合約、資料模型一致性與長期可維護性。評估 production ready 考量是否充分。    |
| **Reviewer B — 實作** | 評估設計是否實際可建構、找出實作所需但設計中缺少的細節、檢查 guideline 合規性，以及確認測試策略是否足夠完整。 |

兩個角色透過啟動兩個獨立的 `explore` 或 `general-purpose` subagent 完成，並以完整的設計文件作為上下文輸入。

**審查者上下文要求：**

審查者在審查設計文件時，不得僅閱讀設計文件本身。必須同時參考以下內容，以確保設計與既有程式碼一致：

1. **既有程式碼**：設計文件中提及的 package（如 `internal/domain`、`internal/adapter/compose`）的現有程式碼
2. **相關介面定義**：設計涉及的介面（如 `RuntimeAdapter`）的目前定義
3. **既有設計文件**：`docs/designs/` 中已核准的其他功能設計，確認不衝突
4. **Coding Guidelines**：`docs/CODING_GUIDELINES.md` 的完整內容

---

## 升級處理

若審查者意見分歧（一位 APPROVED，一位 REVISE），設計者必須先處理 REVISE 的反對意見才能推進，不存在多數決通過的路徑。

若審查循環超過 3 輪仍未取得兩位 APPROVED，功能必須升級為全面重新設計，才能繼續。

---

## 功能狀態值

| 狀態                 | 說明                                                     |
| -------------------- | -------------------------------------------------------- |
| `design_in_progress` | 設計文件撰寫中                                           |
| `design_complete`    | 設計文件完成，待審查                                     |
| `in_review`          | 已送交兩位審查者，等待結果                               |
| `revise_requested`   | 一位或兩位審查者回覆 REVISE                              |
| `approved`           | 兩位審查者都回覆 APPROVED，可開始產生任務                |
| `tasks_generated`    | 已從核准的設計中產生具體任務                             |
| `in_progress`        | 實作進行中                                               |
| `code_review`        | 所有任務實作完成，等待程式碼審查                         |
| `done`               | 程式碼審查通過、測試通過、所有 production ready 要求滿足 |

---

## 設計文件結構

每份設計文件（`docs/designs/<feature-slug>.md`）必須使用以下範本（亦見 `_TEMPLATE.md`）：

```markdown
# 設計文件：<功能名稱>

## 狀態

<!-- design_in_progress | design_complete | in_review | revise_requested | approved | tasks_generated | in_progress | code_review | done -->

## 目的

## 範圍

### 包含

### 不包含

## 資料模型

## 介面合約

## 執行流程

## 錯誤處理

## 測試策略

<!-- 必須包含：測試清單、測試類型分配、mock 策略、CI 執行方式 -->

## Production Ready 考量

<!-- 說明如何滿足日誌、錯誤處理、驗證、安全性、優雅降級要求 -->

## 待決問題

## 審查

### Reviewer A（架構）

- 狀態：
- 意見：

### Reviewer B（實作）

- 狀態：
- 意見：

## 任務

<!-- 兩位審查者 APPROVED 後產生，包含實作任務與對應的測試任務 -->

## 程式碼審查

<!-- 所有任務完成後，由 code-review subagent 審查 feature branch diff -->

- 審查結果：
- 發現問題：
- 修正記錄：
```

---

## 任務完成檢查清單

每個任務標記為 DONE 前，必須確認所有項目：

### 程式碼品質

- [ ] 通過 `docs/CODING_GUIDELINES.md` 中所有 checklist 項目
- [ ] 通過所有靜態分析工具（`golangci-lint` / Angular ESLint）

### 測試

- [ ] 新增程式碼的行覆蓋率達到基準（Go domain/api ≥ 80%）
- [ ] 所有測試以 `go test -race` 執行無 race condition
- [ ] 錯誤路徑都有對應測試案例
- [ ] Angular 元件 AXE 無障礙性測試通過

### Production Ready

- [ ] 符合「Production Ready 要求」章節的所有基準
- [ ] 關鍵操作有結構化日誌
- [ ] API 輸入驗證完整
- [ ] Secret 未洩漏至日誌或回應

### 流程

- [ ] Commit 訊息遵循 Conventional Commit 格式
- [ ] 已針對本任務提交一個獨立的 Conventional Commit（commit 是標記完成的前置條件）
- [ ] 已執行完整預提交驗證指令序列（build → vet → lint → test），全部通過
- [ ] SQL `todos` 資料表中本任務狀態已更新為 `done`
- [ ] 設計文件 `## 任務` 段落中本任務已標記為「✅ 已完成」
- [ ] 若實作與設計文件有偏差，已回頭更新設計文件以反映實際實作
- [ ] 對應設計文件狀態已更新

### 程式碼審查（功能級別，所有任務完成後）

- [ ] 已由 code-review subagent 審查 feature branch 的完整 diff
- [ ] 審查結果為 PASS，或所有 FIX_REQUIRED 項目已修正並重新通過審查
- [ ] 設計文件狀態已從 `code_review` 更新為 `done`

---

## 執行保障

- 任何實作某功能的 PR 或 commit，在合併前都必須有對應的設計文件，且其狀態為 `approved` 或更後期的狀態。
- `docs/designs/` 目錄是所有已設計並已核准功能的權威記錄。
- 審查者必須將審查意見直接記錄在設計文件的 `## 審查` 段落中。
- 任何標記為 DONE 的任務，都必須能提供測試執行記錄作為佐證。
- 任何標記為 DONE 的功能，都必須通過程式碼審查（第五階段），且審查結果記錄於設計文件中。
- 預提交驗證指令是不可繞過的硬性要求，「我認為程式碼沒問題」不能替代實際執行驗證指令。
