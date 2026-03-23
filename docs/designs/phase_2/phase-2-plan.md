> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# Phase Plan：Phase 2 — 實作 Docker Compose Runtime Adapter

## 來源

`docs/CONTROL_PLANE.md` — Phase 2

---

## 目標

Phase 2 完成後，Control Plane 應能透過 Docker Compose 管理完整的 Supabase 專案生命週期：

1. **建立專案** — 自動分配無衝突的 port set、產生 per-project secrets、渲染 `.env` 檔案至 `projects/<slug>/`
2. **啟動 / 停止專案** — 透過 `docker compose up/down` 管理服務
3. **查詢健康狀態** — 解析 `docker compose ps` 輸出，回傳結構化 `ProjectHealth`
4. **刪除專案** — `compose down -v` + 完整目錄清理

`domain.RuntimeAdapter` 介面（Phase 1 已定義）由本 Phase 的 Docker Compose 實作取代 Phase 1 的 stub。

---

## 進入條件

- ✅ Phase 1 完成：
  - `internal/domain` 所有型別、介面已實作並測試通過
  - `RuntimeAdapter` 介面已定義（`Create`、`Start`、`Stop`、`Destroy`、`Status`、`RenderConfig`、`ApplyConfig`）
  - `PortAllocator` 介面已定義
  - `ConfigRenderer` 介面已定義
  - `ProjectConfig`、`PortSet`、`Artifact` 型別已定義
  - Store 層（`ProjectRepository`、`ConfigRepository`）已實作
- Docker Compose v2 安裝於目標機器（`docker compose` 指令可用）
- `docker-compose.yml` 存在於 repo 根目錄

---

## 功能拆解

| # | 功能名稱 | 設計文件路徑 | 狀態 | 說明 |
|---|----------|-------------|------|------|
| 1 | Port Allocator | `docs/designs/phase_2/compose-port-allocator.md` | 未開始 | 實作 `PortAllocator` 介面，掃描現有專案與系統 port，分配無衝突 `PortSet` |
| 2 | `.env` Config Renderer | `docs/designs/phase_2/compose-env-renderer.md` | 未開始 | 實作 `ConfigRenderer` 介面，將 `ProjectConfig` 渲染為 `.env` 格式的 `Artifact` |
| 3 | Docker Compose Adapter | `docs/designs/phase_2/compose-adapter.md` | 未開始 | 實作完整 `RuntimeAdapter` 介面（7 個方法），整合 Port Allocator 與 Config Renderer |

---

## 依賴關係

```
功能 1：compose-port-allocator（無依賴 — 僅依賴 domain 介面）
功能 2：compose-env-renderer（無依賴 — 僅依賴 domain 介面）
  ↓
功能 3：compose-adapter（依賴功能 1 + 功能 2）
```

| 功能 | 依賴於 | 原因 |
|------|--------|------|
| 功能 3 | 功能 1 | `Create` 方法呼叫 `PortAllocator.AllocatePorts()` |
| 功能 3 | 功能 2 | `ApplyConfig` / `RenderConfig` 呼叫 `ConfigRenderer.Render()` |

功能 1 與功能 2 之間**無依賴關係**，可平行進行設計與審查。

---

## 建議實作順序

1. **功能 1（compose-port-allocator）** 與 **功能 2（compose-env-renderer）** — 平行進行設計與審查
2. **功能 3（compose-adapter）** — 功能 1 與 2 皆通過審查後開始

---

## 退出標準

### 設計階段

- [ ] 所有功能的設計文件狀態為 `approved`（通過兩位 reviewer 審查）
  - [ ] `compose-port-allocator.md` — approved
  - [ ] `compose-env-renderer.md` — approved
  - [ ] `compose-adapter.md` — approved

### 實作階段

| 任務 ID | 說明 | 狀態 |
|---------|------|------|
| 待設計審查通過後產生 | — | 未開始 |

### Phase 整合驗證

```bash
cd control-plane

# 編譯
go build ./...

# 靜態分析
go vet ./...

# 單元測試（含 race）
go test -race ./...

# 整合測試（需要 Docker）
go test -race -tags integration ./internal/adapter/compose/...
```

**煙霧測試（手動）：**

```bash
# 確認 compose adapter 可被建立
go run ./cmd/sbctl project create test-project  # Phase 3 實作後啟用
```

---

## 風險與待決事項

| 風險 / 待決事項 | 影響範圍 | 處理方式 |
|----------------|---------|---------|
| Port 分配的 race condition — 兩個並發 create 可能拿到相同 port | compose-port-allocator | 設計文件中定義序列化策略（DB advisory lock 或 application-level mutex） |
| `docker compose ps --format json` 輸出格式因版本不同而異 | compose-adapter Status 方法 | 設計文件中明確定義支援的 Docker Compose 最低版本，並測試輸出格式 |
| `.env` 渲染結果的字元跳脫規則（含特殊字元的 secrets）| compose-env-renderer | 設計文件中定義 `.env` 跳脫規則，參考 Compose 規範 |
| `docker compose up` 的 timeout 上限 — Supabase 服務冷啟動較慢 | compose-adapter Start 方法 | 預設 timeout 設為可設定，建議預設 5 分鐘 |
| `projects/` 目錄下的 volume 資料權限（bind mount vs named volume）| compose-adapter Destroy 方法 | 設計文件中定義 volume 清理策略（`-v` flag + bind mount 目錄刪除） |

---

## 變更記錄

| 日期 | 變更內容 | 原因 |
|------|---------|------|
| 2026-03-23 | 初始建立 | Phase 2 規劃，拆解為 3 個功能 |
