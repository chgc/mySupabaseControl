> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# 設計文件：狀態儲存層設計（State Store）

## 狀態

revising（第三輪審查後修訂完成）

## Phase

- **Phase：** Phase 1
- **Phase Plan：** `docs/designs/phase_1/phase-1-plan.md`

---

## 目的

設計 Control Plane 的持久化層，以 Supabase（PostgreSQL + PostgREST）為後端，儲存專案 metadata、設定與狀態。定義 Repository 介面，讓 domain 層不直接依賴 Supabase client。

---

## 範圍

### 包含

- Supabase DB schema（資料表、RLS policies）
- Repository Go interface 定義
- CRUD 操作合約
- Migration 策略

### 不包含

- Repository 介面的具體 Supabase client 實作（Phase 2）
- Web API 層（Phase 3）
- 資料備份與復原策略

---

## 資料模型

### DB Schema

> **Health 欄位：** `ProjectModel.Health *ProjectHealth` 為 runtime-only 欄位，不持久化至 DB。`GetBySlug` 回傳的 `Health` 固定為 `nil`，由呼叫端（runtime adapter）負責填充。

#### `projects` 資料表

```sql
CREATE TABLE projects (
    slug            TEXT PRIMARY KEY,
    display_name    TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'creating',
    previous_status TEXT,  -- NULL 在 Go ProjectModel.PreviousStatus 中映射為空字串（零值）
    last_error      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT valid_slug CHECK (
        slug ~ '^[a-z0-9]([a-z0-9-]*[a-z0-9])?$'
        AND length(slug) BETWEEN 3 AND 40
    ),
    CONSTRAINT valid_status CHECK (
        status IN ('creating', 'stopped', 'starting', 'running',
                   'stopping', 'destroyed', 'error')
    ),
    CONSTRAINT valid_previous_status CHECK (
        previous_status IS NULL OR
        previous_status IN ('creating', 'stopped', 'starting', 'running',
                            'stopping', 'destroyed', 'error')
    ),
    CONSTRAINT valid_display_name CHECK (
        length(display_name) BETWEEN 1 AND 100
    )
);

-- 自動更新 updated_at
CREATE OR REPLACE FUNCTION update_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER set_updated_at
    BEFORE UPDATE ON projects
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at();

-- 索引
CREATE INDEX idx_projects_status ON projects (status)
    WHERE status != 'destroyed';
```

#### `project_configs` 資料表

```sql
CREATE TABLE project_configs (
    project_slug TEXT NOT NULL REFERENCES projects(slug),
    key          TEXT NOT NULL,
    value        TEXT NOT NULL,
    is_secret    BOOLEAN NOT NULL DEFAULT false,
    category     TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (project_slug, key),

    CONSTRAINT valid_category CHECK (
        category IN ('static_default', 'per_project',
                     'generated_secret', 'user_overridable')
    )
);

CREATE TRIGGER set_config_updated_at
    BEFORE UPDATE ON project_configs
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at();
```

#### `project_overrides` 資料表

```sql
-- 使用者明確設定的覆寫值，與計算值分開儲存。
-- 當使用者覆寫一個 UserOverridable 值時，記錄在此表。
CREATE TABLE project_overrides (
    project_slug TEXT NOT NULL REFERENCES projects(slug),
    key          TEXT NOT NULL,
    value        TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (project_slug, key)
);

CREATE TRIGGER set_override_updated_at
    BEFORE UPDATE ON project_overrides
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at();
```

### RLS Policies

```sql
-- TODO: Phase 1 暫用 USING (true) 允許所有存取（因後端已用 service_role key 連線）。
-- 若未來需細緻 RLS，應改為 USING (auth.role() = 'service_role')。
-- Control Plane 後端使用 service_role key 存取，繞過 RLS。
-- 若未來需要開放 PostgREST 直接存取（非透過後端），則需定義 RLS policies。
-- Phase 1 暫不啟用 RLS，所有存取透過後端 service_role。

-- 預留 RLS 結構：
ALTER TABLE projects ENABLE ROW LEVEL SECURITY;
ALTER TABLE project_configs ENABLE ROW LEVEL SECURITY;
ALTER TABLE project_overrides ENABLE ROW LEVEL SECURITY;

-- service_role 完整存取
-- TODO: Phase 1 暫用 USING (true) 允許所有存取（因後端已用 service_role key 連線）。
-- 若未來需細緻 RLS，應改為 USING (auth.role() = 'service_role')。
CREATE POLICY "service_role_full_access" ON projects
    FOR ALL USING (true) WITH CHECK (true);
-- TODO: Phase 1 暫用 USING (true) 允許所有存取（因後端已用 service_role key 連線）。
-- 若未來需細緻 RLS，應改為 USING (auth.role() = 'service_role')。
CREATE POLICY "service_role_full_access" ON project_configs
    FOR ALL USING (true) WITH CHECK (true);
-- TODO: Phase 1 暫用 USING (true) 允許所有存取（因後端已用 service_role key 連線）。
-- 若未來需細緻 RLS，應改為 USING (auth.role() = 'service_role')。
CREATE POLICY "service_role_full_access" ON project_overrides
    FOR ALL USING (true) WITH CHECK (true);
```

---

## 介面合約

### ProjectRepository

```go
package domain

import "context"

// ProjectRepository 定義專案的持久化操作。
// 實作由 store 套件提供（如 Supabase client）。
type ProjectRepository interface {
    // Create 建立一個新專案。
    // 若 slug 已存在，回傳 ErrProjectAlreadyExists。
    Create(ctx context.Context, project *ProjectModel) error

    // GetBySlug 以 slug 查詢專案。
    // 若不存在，回傳 ErrProjectNotFound。
    // 對 status = 'destroyed' 的專案**正常回傳資料**，不回傳 ErrProjectNotFound。
    // Health 欄位為 runtime-only，GetBySlug 回傳的 ProjectModel.Health 固定為 nil，
    // 由 runtime adapter 在查詢後填充（不持久化至 DB）。
    // 注意：DB 中 previous_status 的 NULL 值，在回傳的 ProjectModel 中
    // 對應 PreviousStatus 零值（空字串 ""）。
    GetBySlug(ctx context.Context, slug string) (*ProjectModel, error)

    // List 列出專案。預設排除 destroyed 狀態的專案。
    // 若需查詢 destroyed 專案，可明確傳入 WithStatus(StatusDestroyed)。
    // SQL 邏輯：
    //   - 不帶 filter：WHERE status != 'destroyed'
    //   - 帶 WithStatus(s)：WHERE status = s（覆蓋預設排除，包括 s = StatusDestroyed 的情況）
    // 回傳結果依 created_at ASC 排序。
    List(ctx context.Context, filters ...ListFilter) ([]*ProjectModel, error)

    // UpdateStatus 更新專案狀態與 previous_status。
    // 若 slug 不存在（rows affected = 0），回傳 ErrProjectNotFound。
    // lastError 僅在 status 為 StatusError 時有意義；
    // 其他狀態下傳入空字串，實作應使用 NULLIF($4, '') 清空 DB 中的 last_error 欄位。
    // previousStatus 為更新前的狀態（通常從 ProjectModel.Status 讀取）。
    // 若為首次狀態轉換（無前一狀態），傳入零值 ""，實作應使用 NULLIF($3, '') 儲存為 NULL。
    // 注意：若 domain 層已在 error 狀態再次呼叫（error→error），呼叫端應傳入原有的
    // PreviousStatus（而非當前 Status），以避免覆寫診斷資訊。此為 domain 層責任，DB 層不做 guard。
    // 在更新前不在此層驗證狀態轉換合法性（此為 domain 層職責）。
    UpdateStatus(ctx context.Context, slug string, status, previousStatus ProjectStatus, lastError string) error

    // Delete 將專案標記為 destroyed（soft delete）。
    // 若 slug 不存在（rows affected = 0），回傳 ErrProjectNotFound。
    Delete(ctx context.Context, slug string) error

    // Exists 檢查 slug 是否已存在。
    // 注意：destroyed 狀態的專案也算「存在」（返回 true），
    // 因為 destroyed slug 不可被新專案復用。
    // 注意：Exists() 為 best-effort 預檢查。真正的唯一性保證由 Create() 的
    // PK 衝突（轉為 ErrProjectAlreadyExists）提供，不應依賴 Exists() 作為唯一性保證。
    Exists(ctx context.Context, slug string) (bool, error)

    // 注意：display_name 在 Phase 1 為不可變欄位。
    // 若未來需支援重命名，應加入 UpdateDisplayName 方法。
}

// ListFilter 是 List 方法的篩選選項。
type ListFilter func(*listOptions)

type listOptions struct {
    Status *ProjectStatus
}

// WithStatus 篩選特定狀態的專案。
func WithStatus(status ProjectStatus) ListFilter {
    return func(o *listOptions) {
        o.Status = &status
    }
}
```

### ConfigRepository

```go
// ConfigRepository 定義專案設定的持久化操作。
type ConfigRepository interface {
    // SaveConfig 使用 UPSERT 語意（INSERT ... ON CONFLICT DO UPDATE），
    // 可安全地重複呼叫（幂等操作）。
    SaveConfig(ctx context.Context, projectSlug string, config *ProjectConfig) error

    // GetConfig 取得專案的完整設定。
    GetConfig(ctx context.Context, projectSlug string) (*ProjectConfig, error)

    // SaveOverrides 全量替換此專案的 overrides（先 DELETE 再 INSERT）。
    // 實作必須在單一 DB transaction（或等效原子操作）中執行，避免 partial write。
    // 若需合併（per-key UPSERT），呼叫端應先 GetOverrides 再合併後呼叫 SaveOverrides。
    SaveOverrides(ctx context.Context, projectSlug string, overrides map[string]string) error

    // GetOverrides 取得使用者覆寫值。
    GetOverrides(ctx context.Context, projectSlug string) (map[string]string, error)

    // DeleteConfig 強制刪除專案的 project_configs 與 project_overrides 所有資料。
    // 注意：正常 destroy 流程（`Delete` 方法）不應呼叫此方法。
    // destroy 後的設定記錄保留供審計用途。
    // 僅在明確需要清除資料時使用（例如：GDPR 刪除請求、測試清理）。
    DeleteConfig(ctx context.Context, projectSlug string) error
}
```

### 合併 Repository（實作便利）

```go
// Store 組合所有 repository 介面，方便依賴注入。
type Store interface {
    ProjectRepository
    ConfigRepository
}

// 注意：Store 組合介面為 Phase 1 實作便利性取捨。
// 若未來 Config 操作需獨立擴充或分別 mock，應分拆為獨立注入點。
```

---

## 執行流程

### 建立專案

1. `repo.Exists(ctx, slug)` — 檢查重複
2. `repo.Create(ctx, project)` — 寫入 projects 表
3. `repo.SaveConfig(ctx, slug, config)` — 寫入 project_configs 表（批次 INSERT）
4. 若步驟 3 失敗，不 rollback 步驟 2（允許 retry）

### 查詢專案

1. `repo.GetBySlug(ctx, slug)` — 從 projects 表讀取
2. 呼叫端視需求決定是否另外呼叫 `repo.GetConfig()`

### 更新狀態

1. `repo.UpdateStatus(ctx, slug, newStatus, previousStatus, lastError)` 執行：
   ```sql
   UPDATE projects
   SET status = $2,
       previous_status = NULLIF($3, ''),
       last_error = NULLIF($4, ''),
       updated_at = now()
   WHERE slug = $1
   ```

### 刪除專案

1. `repo.Delete(ctx, slug)` — UPDATE projects SET status = 'destroyed'
2. `project_configs` 與 `project_overrides` 記錄**保留不刪除**，供審計用途。
   （soft delete 不觸發 FK CASCADE；若需強制清除，請明確呼叫 `DeleteConfig`，但正常 destroy 不需要）

---

## 錯誤處理

| 情境 | 處理方式 | 回應格式 |
|------|---------|---------|
| 專案不存在（GetBySlug/Delete/UpdateStatus 0 rows） | 回傳 `ErrProjectNotFound` | `{ "error": "not_found" }` |
| Slug 重複 | 回傳 `ErrProjectAlreadyExists`（由 DB PK 約束捕捉） | `{ "error": "project_exists" }` |
| DB 連線失敗 | 包裝為 `ErrStoreUnavailable` | `{ "error": "store_unavailable" }` |
| 不合法的 status 字串值 | 由 DB CHECK 約束捕捉，包裝為 `ErrStoreInternal` | `{ "error": "store_internal" }` |
| Context 逾時 | 透過 `context.DeadlineExceeded` 傳播 | `{ "error": "timeout" }` |

### PostgreSQL Error Code 映射

| PostgreSQL 錯誤碼 | 錯誤名稱 | 映射為 |
|----------|---------|--------|
| `23505` | `unique_violation` | `ErrProjectAlreadyExists` |
| 查無資料（0 rows）| — | `ErrProjectNotFound` |
| 連線失敗 | — | `ErrStoreUnavailable` |

---

## 測試策略

### 需要測試的行為

- Create：正常建立、重複 slug 拒絕、slug 格式不合法拒絕
- GetBySlug：存在的專案、不存在的專案
- List：空清單、有結果、依狀態篩選、排除 destroyed
- UpdateStatus：合法更新、不存在的專案
- UpdateStatus：previous_status 持久化（驗證 DB 中欄位值正確）
- UpdateStatus：lastError 空字串清空 last_error 欄位（驗證 DB 中為 NULL）
- GetBySlug：previous_status DB NULL → Go 零值 "" 映射正確
- Exists：存在/不存在
- SaveConfig：正常儲存、覆寫已存在的設定
- GetConfig：存在/不存在
- SaveOverrides + GetOverrides：往返一致性

### 測試類型分配

| 測試類型 | 測試目標 | 層次 |
|---------|---------|------|
| 單元測試 | Repository interface 行為合約 | domain |
| 整合測試 | Supabase client 實作 | store |

### Mock 策略

- Domain 層測試使用 in-memory mock 實作 `ProjectRepository` 與 `ConfigRepository`
- Store 層整合測試使用真實 Supabase 實例（本地 Docker）

### CI 執行方式

- 單元測試：一般 CI
- 整合測試：需要本地 Supabase 實例（可用 docker compose 啟動）

---

## Production Ready 考量

### 錯誤處理
- 所有 DB error 包裝後回傳，不暴露內部細節
- 使用 `errors.Is` / `errors.As` 進行型別化錯誤處理

### 日誌與可觀測性
- 所有 CRUD 操作記錄日誌：`operation`、`project_slug`、`duration_ms`
- DB 連線池狀態可透過 health endpoint 觀測

### 輸入驗證
- 由 domain 層在呼叫 repository 前完成驗證
- DB 層的 CHECK 約束作為最後防線

### 安全性
- 使用 service_role key 存取 Supabase（繞過 RLS）
- `project_configs` 中 `is_secret = true` 的值在日誌中遮罩
- DB 連線字串使用環境變數，不寫入程式碼

### 優雅降級
- DB 連線失敗時回傳明確錯誤
- 支援連線重試（exponential backoff）
- context timeout 設定合理值（預設 5 秒）

### 設定管理
- Supabase URL、service_role key 透過環境變數設定
- 連線池大小可設定

---

## 待決問題

- Soft delete 的專案資料保留多久？是否需要定期清理 destroyed 的專案？
- `project_configs` 是否需要加密儲存 secret 值？Phase 1 先用明文，Phase 3 評估 Supabase Vault。
- 是否需要 optimistic locking（版本號）防止併發更新？Phase 1 先不實作。
- Optimistic locking 暫緩（Phase 1）。注意：`UpdateStatus` 中 `previousStatus` 在高並發情境下可能過時（stale read），建議 Phase 2 評估版本號或 SELECT FOR UPDATE。
- Migration 工具選擇：使用 Supabase Migration 或 golang-migrate？建議使用 Supabase Migration。

---

## 審查

### Reviewer A（架構）

- **狀態：** 🔁 REVISE（第一輪）→ 🔁 REVISE（第二輪）→ ✅ APPROVED（第三輪）
- **第一輪意見（摘要）：** 4 個阻斷性問題，全部已解決。
- **第二輪意見（摘要）：**
  1. 🔴 **[已修正]** DeleteConfig godoc 與 Delete 流程矛盾 → 明確為管理操作，正常 destroy 不呼叫
  2. 🔴 **[已修正]** previous_status NULL → Go 零值映射 → godoc 補充，DDL 加注解，SQL 範例加 NULLIF
  3. 🟡 **[已修正]** SaveOverrides 語意說明（全量替換）
  4. 🟡 **[已修正]** UpdateStatus stale read 風險補充到待決問題
  5. 🟡 **[已修正]** List 排序說明
- **第三輪意見（摘要）：**
  - 所有前輪問題均已正確修復，架構設計健全。
  - 🟡 **[已修正]** UpdateStatus previousStatus godoc "代表尚未進入 error" → 改為 "首次轉換無前一狀態"
  - 🟡 **[已修正]** Delete godoc 補充 0 rows → ErrProjectNotFound
  - 🟡 **[已修正]** WithStatus multiple filter 行為說明（last-write-wins）

### Reviewer B（實作）

- **狀態：** 🔁 REVISE（第一輪）→ 🔁 REVISE（第二輪）→ 🔁 REVISE（第三輪）
- **第一輪意見（摘要）：** 4 個阻斷性問題，全部已解決。
- **第二輪意見（摘要）：**
  1. 🔴 **[已修正]** DeleteConfig godoc 矛盾（與 Reviewer A 一致）
  2. 🔴 **[已修正]** List 排除 destroyed vs WithStatus(StatusDestroyed) 語意歧義 → 明確說明 filter override 邏輯
  3. 🟡 **[已修正]** 測試策略補充 previous_status 持久化案例
  4. 🟡 **[已修正]** NULLIF SQL 範例補充
- **第三輪意見（摘要）：**
  1. 🔴 **[已修正]** UpdateStatus 0 rows → ErrProjectNotFound 未定義 → godoc + 錯誤表補充
  2. 🔴 **[已修正]** SaveOverrides 無原子性保證 → 加入 transaction 要求
  3. 🟡 **[已修正]** error→error previousStatus domain 責任說明
  4. 🟡 **[已修正]** Delete SQL 完整範例
  5. 🟡 **[已修正]** GetBySlug destroyed 專案行為說明

---

## 任務

<!-- 待審查通過後展開 -->

---

## 程式碼審查

- **審查結果：**
- **發現問題：**
- **修正記錄：**
