> **文件語言：繁體中文**
> 本專案所有文件均以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# docs/

本專案的所有文件集中放置於此資料夾。

## 文件索引

| 文件 | 說明 |
|---|---|
| [CONTROL_PLANE.md](./CONTROL_PLANE.md) | 系統架構、技術選型與開發路線圖 |
| [REVIEW_GATEWAY.md](./REVIEW_GATEWAY.md) | 審查閘門規則 — 所有功能在實作前都必須通過此流程 |
| [CODING_GUIDELINES.md](./CODING_GUIDELINES.md) | Go（Google 風格指南）、Angular（官方最佳實踐）與 Conventional Commit 規範 |
| [designs/](./designs/) | 各功能的設計文件，每個功能一個檔案，命名格式為 `<feature-slug>.md` |

## 新增功能的流程

### 0. Phase 規劃（每個 Phase 執行一次）

1. 複製 `docs/designs/_PHASE_TEMPLATE.md` → `docs/designs/phase-<N>-plan.md`
2. 將 CONTROL_PLANE.md 中對應 Phase 的高階描述拆解為具體的功能清單
3. 定義功能間的依賴關係與建議實作順序
4. 定義 Phase 退出標準

### 1. 功能設計與實作（每個功能執行一次）

1. 複製 `docs/designs/_TEMPLATE.md` → `docs/designs/<feature-slug>.md`
2. 填寫所有段落（Phase 歸屬、目的、範圍、資料模型、介面合約、執行流程、錯誤處理、測試策略）
3. 將狀態設為 `design_complete`
4. 提交給兩個 review subagent（Reviewer A：架構、Reviewer B：實作）
5. 處理所有 REVISE 意見並重複審查，直到兩位都回覆 APPROVED
6. 在文件的 `## 任務` 段落與 SQL `todos` 資料表中產生具體可執行的任務
7. 開始實作

詳細規則請參閱 `docs/REVIEW_GATEWAY.md`。
