> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# High-Level Plan 調整

## 來源

`docs/designs/phase-0-plan.md` — 功能 3

前置依賴：
- `docs/designs/supabase-arch-analysis.md`（功能 1）
- `docs/designs/shared-component-analysis.md`（功能 2）

---

## 研究結論摘要

經過功能 1（架構分析）與功能 2（共用元件分析），得出以下結論：

1. **13 個服務中，僅 vector 建議共用。** 其餘服務因 JWT secret 綁定、DB 直連或專案專屬資料，不適合共用。
2. **kong 與 imgproxy 技術上可共用，但投入產出比不佳。** Kong 節省 30–50 MB 但需開發路由模板或 Lua plugin；imgproxy 節省 30–50 MB 但需掛載多專案 volume。
3. **現有架構設計（每專案獨立服務組）基本正確。** 不需要大幅重新設計。
4. **JWT_SECRET 是最強的隔離邊界。** 8 個服務共用同一 JWT secret，這決定了以「專案」為單位的隔離粒度。
5. **每個完整專案約需 0.7–1.6 GB RAM，12–13 個容器。**

---

## 是否需要調整 CONTROL_PLANE.md？

### 結論：需要微幅調整，不需要大幅重寫。

現有架構的核心假設——「每個專案是一組獨立的 Supabase 服務，由 Runtime Adapter 管理生命週期」——經驗證後**完全正確**。

需要微調的內容：

| 調整項目 | 影響段落 | 調整幅度 |
|---------|---------|---------|
| 新增「全域共用服務」概念 | 提議架構 → 架構圖 | 小 |
| 設定 Schema 區分全域 vs 每專案 | Phase 1 deliverables | 小 |
| Phase 0 標記為已完成 | 開發路線圖 → Phase 0 | 小 |

---

## 具體調整內容

### 1. 架構圖微調

在現有架構圖中，新增「全域共用服務」的概念。這不改變 Runtime Adapter 的核心設計，只是在編排層認知到「有些服務是 per-host 而非 per-project」。

**建議更新後的架構概念：**

```
┌─────────────────────────────────────────────────────┐
│                  Control Plane                       │
│                                                      │
│  ┌──────────┐  ┌──────────┐  ┌───────────────┐      │
│  │  專案     │  │  設定     │  │    Secret     │      │
│  │  Registry │  │  Schema  │  │   管理器      │      │
│  └──────────┘  └──────────┘  └───────────────┘      │
│                                                      │
│  ┌──────────────────────────────────────────────┐    │
│  │         生命週期編排器                         │    │
│  │  create / up / down / reset / list           │    │
│  │                                              │    │
│  │  ┌────────────────┐  ┌────────────────────┐  │    │
│  │  │ 專案服務管理    │  │ 全域服務管理        │  │    │
│  │  │ (12 containers) │  │ (vector 等)        │  │    │
│  │  └────────────────┘  └────────────────────┘  │    │
│  └──────────────┬───────────────────────────────┘    │
│                 │                                    │
│  ┌──────────────▼───────────────────────────────┐    │
│  │         Runtime Adapter 介面                  │    │
│  └──────┬───────────────────┬───────────────────┘    │
│         │                   │                        │
│  ┌──────▼──────┐     ┌──────▼──────┐                 │
│  │   Docker    │     │    K8s      │                 │
│  │   Compose   │     │  Adapter    │                 │
│  │   Adapter   │     │（未來）      │                 │
│  └─────────────┘     └─────────────┘                 │
└─────────────────────────────────────────────────────┘
```

### 2. 設定 Schema 調整

Phase 1 的設定 Schema 設計中，建議區分兩個層級：

| 設定層級 | 說明 | 範例 |
|---------|------|------|
| **Global Config** | 全 host 共用的設定 | `DOCKER_SOCKET_LOCATION`、vector 路由規則 |
| **Project Config** | 每專案獨立的設定 | `JWT_SECRET`、`POSTGRES_PASSWORD`、`KONG_HTTP_PORT` |

這不改變原有的四分類（共用靜態預設值、每專案設定、產生的 secrets、使用者可覆寫），
而是在更上層加入一個全域/專案的維度。

### 3. Runtime Adapter 介面微調

建議在 Runtime Adapter 介面中，考慮加入全域服務的生命週期管理：

```go
type RuntimeAdapter interface {
    // 每專案服務管理（已有）
    Create(project Project) error
    Start(project Project) error
    Stop(project Project) error
    Destroy(project Project) error
    Status(project Project) (ProjectStatus, error)
    RenderConfig(project Project) ([]Artifact, error)

    // 全域服務管理（新增）
    EnsureGlobalServices() error   // 確保 vector 等全域服務運行
    GlobalStatus() (GlobalStatus, error)
}
```

這是可選的設計選項，也可以在 Phase 1 設計文件中再決定是否需要。
若認為複雜度過高，可完全不處理全域服務，讓使用者手動管理 vector。

---

## Phase 1–5 影響評估

| Phase | 影響 | 說明 |
|-------|------|------|
| Phase 1 | 微調 | 設定 Schema 加入 global/project 維度；RuntimeAdapter 介面考慮全域服務 |
| Phase 2 | 無影響 | Docker Compose Adapter 仍以 per-project 為主；vector 可選共用 |
| Phase 3 | 無影響 | API 端點與 Web UI 不受影響 |
| Phase 4 | 無影響 | UX 改善不受架構影響 |
| Phase 5 | 無影響 | K8s Adapter 同樣以 per-project namespace 為主 |

**結論：Phase 1–5 的 deliverables 不需要調整。** 設定 Schema 的全域/專案維度可在 Phase 1 設計階段自然納入。

---

## 對 CONTROL_PLANE.md 的建議變更

### 需要更新的段落

1. **Phase 0 段落** — 標注 Phase 0 研究已完成，附上三份分析文件的連結。
2. **Phase 1 段落** — 在設定 Schema 的描述中，加入「區分全域設定與每專案設定」的提示。

### 不需要更新的段落

- 核心架構圖 — 目前的架構圖已足夠清楚，全域服務概念可在 Phase 1 設計文件中細化。
- 技術決策表 — 無需變更。
- Phase 2–5 — 無需變更。

---

## 最終決策

| 決策項目 | 結論 |
|---------|------|
| 是否調整核心架構？ | **否。** 每專案獨立服務組的設計正確。 |
| 是否需要共用層？ | **僅 vector。** 可在 Phase 2 的 Docker Compose Adapter 中實作。 |
| 是否調整 Phase 1–5？ | **否。** deliverables 不需調整，細節在各 Phase 設計文件中處理。 |
| CONTROL_PLANE.md 更新幅度 | **極小。** Phase 0 完成標注 + Phase 1 設定 Schema 提示。 |

---

## 變更記錄

| 日期 | 變更內容 | 原因 |
|------|---------|------|
| 2026-03-22 | 初始建立 | Phase 0 功能 3：High-Level Plan 調整 |
