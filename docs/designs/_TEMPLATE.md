> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# 設計文件：<功能名稱>

## 狀態

<!-- design_in_progress | design_complete | in_review | revise_requested | approved | tasks_generated | in_progress | code_review | done -->
design_in_progress

## Phase

<!-- 此功能所屬的 Phase 編號與 Phase Plan 文件路徑 -->
- **Phase：** <!-- Phase 1 / Phase 2 / Phase 3 / Phase 4 / Phase 5 -->
- **Phase Plan：** <!-- docs/designs/phase-<N>-plan.md -->

---

## 目的

<!-- 這個功能解決什麼問題？為什麼需要它？ -->

---

## 範圍

### 包含

-

### 不包含

-

---

## 資料模型

<!-- Go struct、DB schema（Supabase 資料表定義）、API 請求/回應格式 -->

---

## 介面合約

<!-- Go 函式簽名、API 端點定義（method、path、request body、response body）、Adapter 方法簽名 -->

---

## 執行流程

<!-- Runtime 執行時的逐步描述，使用編號步驟或 sequence diagram -->

---

## 錯誤處理

<!-- 列舉已知的失敗情境，以及各自對應的處理方式與 error response 格式 -->

| 情境 | 處理方式 | 回應格式 |
|---|---|---|
|  |  |  |

---

## 測試策略

<!-- 必須完整填寫以下四個部分，否則審查者將回覆 REVISE -->

### 需要測試的行為

<!-- 列舉所有關鍵路徑與錯誤路徑 -->

-

### 測試類型分配

| 測試類型 | 測試目標 | 層次 |
|---|---|---|
| 單元測試 |  | domain / service |
| 整合測試 |  | adapter / API |
| e2e 測試 |  | 如適用 |

### Mock 策略

<!-- 哪些外部依賴需要被 mock，使用什麼方式 -->

-

### CI 執行方式

<!-- 哪些測試在一般 CI 中執行，哪些需要特殊環境（如需要真實 Docker） -->

-

---

## Production Ready 考量

<!-- 說明這個功能如何滿足以下各項要求 -->

### 錯誤處理
<!-- API 錯誤格式、使用者可見訊息是否安全 -->

### 日誌與可觀測性
<!-- 哪些操作需要記錄日誌，包含哪些上下文欄位 -->

### 輸入驗證
<!-- 哪些輸入需要驗證，驗證規則為何 -->

### 安全性
<!-- Secret 保護、存取控制考量 -->

### 優雅降級
<!-- 外部依賴不可用時的行為，timeout 策略 -->

### 設定管理
<!-- 哪些值需要可透過環境變數設定，哪些為必要設定 -->

---

## 待決問題

<!-- 尚未確定、需要在實作前或實作中解答的事項 -->

-

---

## 審查

### Reviewer A（架構）

- **狀態：** <!-- APPROVED | REVISE | REJECTED -->
- **意見：**

### Reviewer B（實作）

- **狀態：** <!-- APPROVED | REVISE | REJECTED -->
- **意見：**

---

## 任務

<!-- 兩位審查者都回覆 APPROVED 後，根據設計展開為具體可執行的任務。
格式範例：

### 任務 1：<任務名稱>
- **來源設計：** 本文件
- **影響檔案：** `internal/domain/project.go`（新增）
- **驗收標準：** ProjectModel struct 定義完成，單元測試覆蓋率 ≥ 80%
- **測試任務：** `internal/domain/project_test.go`（新增，表格驅動測試）
-->

---

## 程式碼審查

<!-- 所有任務完成後，由 code-review subagent 審查 feature branch 對 main 的完整 diff -->

- **審查結果：** <!-- PASS | FIX_REQUIRED -->
- **發現問題：**
- **修正記錄：**
