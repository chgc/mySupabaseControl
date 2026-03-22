> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# Phase Plan：Phase 1 — 定義 Runtime 無關的 Control Plane 模型

## 來源

`docs/CONTROL_PLANE.md` — Phase 1

---

## 目標

定義 Control Plane 的核心領域模型，包含專案模型、設定 schema、Runtime Adapter 介面與狀態儲存設計。

Phase 1 完成後，系統應具備：
1. 清晰的、有型別的專案模型定義（Go struct + DB schema）
2. 涵蓋所有 89 個環境變數的設定 schema，可渲染至 `.env` 與 K8s ConfigMap
3. Runtime Adapter 抽象介面，解耦控制邏輯與執行 runtime
4. 以 Supabase 為後端的持久化層設計

---

## 進入條件

- Phase 0 研究已完成：
  - ✅ Supabase 架構分析（`docs/designs/supabase-arch-analysis.md`）
  - ✅ 共用元件分析結論：每專案獨立服務組正確，僅 vector 建議共用
  - ✅ High-Level Plan 調整：Phase 1–5 deliverables 不需調整

---

## 功能拆解

| # | 功能名稱 | 設計文件路徑 | 狀態 | 說明 |
|---|----------|-------------|------|------|
| 1 | 專案模型定義 | `docs/designs/phase_1/project-model.md` | design_in_progress | ProjectModel struct、狀態機、slug 驗證規則 |
| 2 | 設定 Schema 與環境變數目錄 | `docs/designs/phase_1/config-schema.md` | design_in_progress | 89 個 env var 分類、有型別設定 schema、ConfigRenderer 介面 |
| 3 | Runtime Adapter 介面 | `docs/designs/phase_1/runtime-adapter.md` | design_in_progress | RuntimeAdapter Go interface、方法合約、錯誤型別 |
| 4 | 狀態儲存層設計 | `docs/designs/phase_1/state-store.md` | design_in_progress | Supabase DB schema、Repository interface、CRUD 操作 |

---

## 依賴關係

```
功能 1：project-model（無依賴 — 核心型別定義）
  ├── 功能 2：config-schema（依賴功能 1 — config 引用 ProjectModel）
  ├── 功能 4：state-store（依賴功能 1 — 持久化 ProjectModel）
  └── 功能 3：runtime-adapter（依賴功能 1、功能 2 — 方法操作 Project，renderConfig 使用 schema）
```

| 功能 | 依賴於 | 原因 |
|------|--------|------|
| 功能 2 | 功能 1 | 設定 schema 引用 ProjectModel 的 slug、port 等欄位 |
| 功能 3 | 功能 1、功能 2 | Adapter 方法接收 Project 參數，renderConfig 使用設定 schema |
| 功能 4 | 功能 1 | Repository 操作 ProjectModel 的 CRUD |

---

## 建議實作順序

1. **project-model** — 定義核心型別，其他所有功能都依賴它
2. **config-schema** 與 **state-store** — 可平行進行
3. **runtime-adapter** — 最後完成，整合前面所有定義

---

## 退出標準

### 設計階段（✅ 全部完成）

- [x] 所有功能的設計文件狀態為 `approved`（通過兩位 reviewer 審查）
  - [x] `project-model.md` — approved（四輪）
  - [x] `state-store.md` — approved（五輪）
  - [x] `config-schema.md` — approved（六輪）
  - [x] `runtime-adapter.md` — approved（兩輪）

### 實作階段

#### Group 0 — Bootstrap

| 任務 ID | 說明 | 狀態 |
|---------|------|------|
| `bootstrap` | `go mod init`、目錄結構、`.golangci.yml`、justfile targets | ✅ done |

#### Group 1 — Domain 基礎型別

| 任務 ID | 說明 | 狀態 |
|---------|------|------|
| `domain-service-name` | `ServiceName` type + 13 個常數 | ✅ done |
| `domain-project-status` | `ProjectStatus` type + 8 個常數 | ✅ done |
| `domain-project-health` | `ServiceStatus`、`ServiceHealth`、`ProjectHealth`、`IsHealthy()` | ✅ done |

#### Group 2 — Domain 專案模型

| 任務 ID | 說明 | 狀態 |
|---------|------|------|
| `domain-project-model` | `ProjectModel`、`NewProject()`、`TransitionTo()`、`ValidateSlug()`、`TransitionError` | ✅ done |

#### Group 3 — Domain Config Schema

| 任務 ID | 說明 | 狀態 |
|---------|------|------|
| `domain-config-types` | `ConfigCategory`、`ConfigScope`、`ConfigEntry`、`PortSet`（6 欄位） | ✅ done |
| `domain-errors` | `ErrMissingRequiredConfig`、`ErrConfigNotOverridable`、`ErrInvalidPortSet`、`ErrNoAvailablePort` | ✅ done |
| `domain-config-schema` | `ConfigSchema()` — 全部 94 個環境變數定義 | ✅ done |
| `domain-port-allocator` | `PortAllocator` interface | ✅ done |
| `domain-renderer` | `Artifact` struct、`ConfigRenderer` interface | ✅ done |
| `domain-secret-gen` | `SecretGenerator` interface、`GenerateProjectSecrets()` | ✅ done |

#### Group 4 — Domain ProjectConfig

| 任務 ID | 說明 | 狀態 |
|---------|------|------|
| `domain-project-config` | `ProjectConfig`、`ResolveConfig()`、`computePerProjectVars()`、`ExtractPortSet()` | ✅ done |

#### Group 5 — Domain RuntimeAdapter

| 任務 ID | 說明 | 狀態 |
|---------|------|------|
| `domain-runtime-adapter` | `RuntimeAdapter` interface（7 方法）、`AdapterError`、`StartError`、factory stub | ✅ done |
| `domain-mock-adapter` | `MockRuntimeAdapter` struct（7 個 func 欄位） | ✅ done |

#### Group 6 — Store 層

| 任務 ID | 說明 | 狀態 |
|---------|------|------|
| `store-interfaces` | `ProjectRepository`、`ConfigRepository`、`Store` interface、store errors | ✅ done |
| `migration-ddl` | SQL migration（3 張資料表 + triggers + indexes + RLS；修正 `destroying` bug） | ✅ done |
| `store-postgres-project` | PostgreSQL `ProjectRepository` 實作 | ✅ done |
| `store-postgres-config` | PostgreSQL `ConfigRepository` 實作 | ✅ done |

#### Group 7 — 測試

| 任務 ID | 說明 | 狀態 |
|---------|------|------|
| `test-project-model` | `TransitionTo()`、`ValidateSlug()`、`IsHealthy()` 單元測試 | ✅ done |
| `test-config-schema` | `ConfigSchema()` 完整性（94 個 key、無重複、分類驗證） | ✅ done |
| `test-project-config` | `ResolveConfig()` 優先順序、`ExtractPortSet()` 邊界 | ✅ done |
| `test-secret-gen` | hex/alphanumeric 格式、JWT 格式 | [ ] pending |
| `test-store-integration` | `ProjectRepository` + `ConfigRepository` round-trip（需 DB） | [ ] pending |

### Phase 整合驗證

```bash
cd control-plane
go build ./...
go test -race ./...
go vet ./...
```

---

## 風險與待決事項

| 風險 / 待決事項 | 影響範圍 | 處理方式 |
|----------------|---------|---------|
| 環境變數分類可能有邊界案例（同時屬於多個分類） | config-schema | 在設計文件中定義明確的分類規則與優先順序 |
| Supabase DB schema 設計需考慮未來 migration | state-store | 採用 Supabase migration 機制，預留 schema 版本管理 |
| RuntimeAdapter 介面的 renderConfig 回傳型別需要同時支援 .env 與 K8s | runtime-adapter | 定義 Artifact 抽象型別，各 adapter 實作自己的 artifact |
| 專案狀態機的 error state 處理 | project-model | 在狀態機設計中定義 error 狀態與恢復路徑 |

---

## 變更記錄

| 日期 | 變更內容 | 原因 |
|------|---------|------|
| 2026-03-22 | 初始建立 | Phase 1 規劃，拆解為 4 個功能 |
