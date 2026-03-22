> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# 設計文件：狀態儲存層設計（State Store）

## 狀態

revising（第一輪審查後修訂中）

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
    previous_status TEXT,
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
    // Health 欄位為 runtime-only，GetBySlug 回傳的 ProjectModel.Health 固定為 nil，
    // 由 runtime adapter 在查詢後填充（不持久化至 DB）。
    GetBySlug(ctx context.Context, slug string) (*ProjectModel, error)

    // List 列出所有非 destroyed 的專案。
    // 可選 filters：依狀態篩選。
    List(ctx context.Context, filters ...ListFilter) ([]*ProjectModel, error)

    // UpdateStatus 更新專案狀態與 previous_status。
    // lastError 僅在 status 為 StatusError 時有意義；
    // 其他狀態下傳入空字串，實作應清空 DB 中的 last_error 欄位。
    // previousStatus 為轉換前的狀態，應在更新前由呼叫端傳入（通常從 ProjectModel.Status 取得）。
    // 在更新前不在此層驗證狀態轉換合法性（此為 domain 層職責）。
    UpdateStatus(ctx context.Context, slug string, status, previousStatus ProjectStatus, lastError string) error

    // Delete 將專案標記為 destroyed（soft delete）。
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

    // SaveOverrides 儲存使用者覆寫值。
    SaveOverrides(ctx context.Context, projectSlug string, overrides map[string]string) error

    // GetOverrides 取得使用者覆寫值。
    GetOverrides(ctx context.Context, projectSlug string) (map[string]string, error)

    // DeleteConfig 刪除專案的所有設定（專案 destroy 時呼叫）。
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

1. `repo.UpdateStatus(ctx, slug, newStatus, previousStatus, lastError)` — UPDATE projects SET status, previous_status, last_error, updated_at

### 刪除專案

1. `repo.Delete(ctx, slug)` — UPDATE projects SET status = 'destroyed'
2. `project_configs` 與 `project_overrides` 記錄**保留不刪除**，供審計用途。
   （soft delete 不觸發 FK CASCADE；若需清除，請明確呼叫 `DeleteConfig`）

---

## 錯誤處理

| 情境 | 處理方式 | 回應格式 |
|------|---------|---------|
| 專案不存在 | 回傳 `ErrProjectNotFound` | `{ "error": "not_found" }` |
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
- Migration 工具選擇：使用 Supabase Migration 或 golang-migrate？建議使用 Supabase Migration。

---

## 審查

### Reviewer A（架構）

- **狀態：** 🔁 REVISE（第一輪）
- **第一輪意見（摘要）：**
  1. 🔴 **[已修正]** DDL 缺 `previous_status` 欄位 → 加入欄位與 CHECK 約束，更新 UpdateStatus 簽名
  2. 🔴 **[已修正]** ON DELETE CASCADE 與 soft delete 語意衝突 → 移除 CASCADE，保留審計記錄
  3. 🟡 **[已修正]** ProjectHealth 未持久化說明缺失
  4. 🟡 **[已修正]** ErrInvalidTransition 責任邊界錯誤 → 改為 ErrStoreInternal
  5. 🟡 **[已修正]** Exists() 對 destroyed slug 語意未定義
  6. 🟡 **[已修正]** UpdateStatus lastError 清除行為未說明
  7. 🔵 **[已修正]** Store 介面 ISP 說明、RLS TODO、N4 備注

### Reviewer B（實作）

- **狀態：** 🔁 REVISE（第一輪）
- **第一輪意見（摘要）：**
  1. 🔴 **[已修正]** DDL 缺 `previous_status` 欄位（與 Reviewer A 一致）
  2. 🔴 **[已修正]** ON DELETE CASCADE 與 soft delete 衝突（與 Reviewer A 一致）
  3. 🔴 **[已修正]** ProjectHealth 完全無持久化策略 → 明確聲明 runtime-only，GetBySlug 回傳 nil Health
  4. 🔴 **[已修正]** 缺少 display_name 更新路徑 → 明確聲明 Phase 1 不可變
  5. 🟡 **[已修正]** UpdateStatus lastError 應為 *string 或說明空字串語意 → 加入 godoc 說明
  6. 🟡 **[已修正]** Exists() TOCTOU 說明
  7. 🟡 **[已修正]** SaveConfig UPSERT 語意說明
  8. 🟡 **[已修正]** PostgreSQL error code 映射
  9. 🟡 **[已修正]** UpdateStatus 狀態轉換驗證責任 → 移至 domain 層

---

## 任務

<!-- 待審查通過後展開 -->

---

## 程式碼審查

- **審查結果：**
- **發現問題：**
- **修正記錄：**
