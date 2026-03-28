> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# Phase Plan：Phase 6 — K8s Runtime Adapter（OrbStack K8s on Mac Mini）

## 來源

`docs/CONTROL_PLANE.md` — Phase 6

---

## 目標

Phase 6 完成後，`sbctl` 可透過 `--runtime kubernetes` 旗標，在 OrbStack 提供的 K8s 環境（底層為 k3s）上建立、啟動、停止及銷毀 Supabase 專案，體驗與 Docker Compose 一致。

具體可觀測成果：

1. `sbctl project create my-app --runtime kubernetes` 建立 K8s namespace 與 Helm values.yaml
2. `sbctl project start my-app` 透過 `helm upgrade --install` 部署全套 Supabase 服務
3. `sbctl project status my-app` 透過 `kubectl get pods` 查詢服務健康狀態
4. `sbctl project stop my-app` 透過 `helm uninstall` 停止服務（保留 PVC 資料）
5. `sbctl project delete my-app` 刪除 K8s namespace 與本地檔案
6. Compose 專案與 K8s 專案可共存於同一個 Control Plane 中

---

## 進入條件

- ✅ Phase 3 完成：
  - `internal/domain` 所有核心型別已實作
  - `RuntimeAdapter` 介面已定義且有 Docker Compose 完整實作
  - `ProjectService`（use-case 層）已實作
  - `sbctl` CLI 與 MCP Server 已實作
- OrbStack 已安裝於 Mac Mini 且 K8s 叢集可啟動（`orb start k8s`）
- `helm` CLI 與 `kubectl` CLI 可用（OrbStack 內建 kubectl）

---

## 架構審計結果

在開始功能設計前，對目前 Control Plane 進行了深度審計。以下是**必須先修正的架構缺口**，
否則無法支援多 runtime 共存：

### 🔴 需要修改的部分

| # | 缺口 | 影響檔案 | 說明 |
|---|------|---------|------|
| 1 | `ProjectModel` 無 `RuntimeType` 欄位 | domain, store, migrations | 不知道專案使用哪種 runtime |
| 2 | DB schema 無 `runtime_type` 欄位 | migrations | 專案 runtime type 未持久化 |
| 3 | `BuildDeps` 硬編碼 `ComposeAdapter` | cmd/sbctl/deps.go | 無法動態選取 adapter |
| 4 | `ProjectService` 持有單一 `Adapter` 實例 | usecase | 無法根據專案的 runtime type 選取不同 adapter |
| 5 | `PortAllocator` 僅有 Compose 實作 | adapter/compose | K8s 使用 NodePort 範圍（30000–32767） |
| 6 | `ConfigRenderer` 僅有 `.env` 實作 | adapter/compose | K8s 需要 Helm `values.yaml` |
| 7 | `computePerProjectVars` 含 Docker-only 值 | domain/project_config.go | `DOCKER_SOCKET_LOCATION` 等不適用 K8s |

### 🟢 不需修改（已為 K8s 就緒）

- `RuntimeAdapter` 介面本身 — 7 方法合約完全適用 K8s
- `ProjectStatus` 狀態機 — runtime-agnostic
- `ServiceName`、`ServiceHealth`、`ProjectHealth` — runtime-agnostic
- `AdapterError`、`StartError`、sentinel errors — runtime-agnostic
- `ConfigEntry`、`ConfigSchema()` — key-value 模型不限渲染目標
- `Artifact` 型別 — path + content + mode 足以表達 values.yaml
- `SecretGenerator` — runtime-agnostic

---

## OrbStack K8s 環境特性

| 關注點 | OrbStack K8s 行為 | 對設計的影響 |
|--------|-------------------|-------------|
| ClusterIP 存取 | ✅ Mac 可直接存取 ClusterIP | 內部服務可免 NodePort |
| cluster.local DNS | ✅ Mac 可解析 | 可用 `{svc}.{ns}.svc.cluster.local` |
| NodePort | `localhost:PORT` 可存取 | Kong 外部暴露使用 NodePort |
| LoadBalancer | ✅ 自動分配 `*.k8s.orb.local` | 備選方案 |
| StorageClass | `local-path`（k3s 內建） | PVC 設定使用此 class |
| kubectl | OrbStack 內建 | 不需額外安裝 |
| 映像 | 本地 build 直接可用 | 不需 registry |

---

## 功能拆解

Phase 6 分為兩組：先進行**架構調整**（使 Control Plane 支援多 runtime），再進行 **K8s adapter 實作**。

### Phase 6-A：多 Runtime 架構調整

| # | 功能名稱 | 設計文件路徑 | 狀態 | 說明 |
|---|----------|-------------|------|------|
| 1 | Multi-Runtime 基礎架構 | `docs/designs/phase_6/multi-runtime-infra.md` | approved | DB migration（runtime_type 欄位）、ProjectModel 加入 RuntimeType、Store 更新、AdapterRegistry 介面、ProjectService 重構、CLI --runtime 旗標、computePerProjectVars 參數化 |

> **設計決策：** 將所有架構調整合併為一個功能而非拆成 7 個，因為它們高度耦合、
> 無法獨立交付驗證（例如加了 DB 欄位但 service 層不用就無意義），且總量級適中。

### Phase 6-B：K8s Adapter 實作

| # | 功能名稱 | 設計文件路徑 | 狀態 | 說明 |
|---|----------|-------------|------|------|
| 2 | Helm Chart 研究與 Values 對映 | `docs/designs/phase_6/helm-values-mapping.md` | done | 研究社群 Helm chart values 結構，建立 ProjectConfig → values.yaml 完整對映表 |
| 3 | K8s Config Renderer（Helm Values） | `docs/designs/phase_6/k8s-values-renderer.md` | done | 實作 `ConfigRenderer` 介面，將 `ProjectConfig` 渲染為 Helm values.yaml 格式 |
| 4 | K8s Status Parser | `docs/designs/phase_6/k8s-status-parser.md` | done | 解析 `kubectl get pods -o json` 為 `ProjectHealth` |
| 5 | K8s Port Allocator（NodePort） | `docs/designs/phase_6/k8s-port-allocator.md` | done | 在 NodePort 範圍分配無衝突的 port set |
| 6 | K8s Adapter 核心 | `docs/designs/phase_6/k8s-adapter.md` | done | 實作完整 `RuntimeAdapter` 介面（7 方法），整合 renderer、parser、allocator，shell out to helm/kubectl |

---

## 依賴關係

```
功能 1：multi-runtime-infra（無外部依賴 — 純重構）
  ↓
功能 2：helm-values-mapping（依賴功能 1 — 需知道 RuntimeType 如何流動）
  ↓
功能 3：k8s-values-renderer（依賴功能 2 — 需要對映表）
  │
功能 4：k8s-status-parser（無依賴 — 僅依賴 domain 介面，可與功能 3 平行）
功能 5：k8s-port-allocator（無依賴 — 僅依賴 domain 介面，可與功能 3 平行）
  │
  └── 功能 3 + 4 + 5 完成後
        ↓
功能 6：k8s-adapter（依賴功能 3、4、5 — 整合所有子元件）
```

| 功能 | 依賴於 | 原因 |
|------|--------|------|
| 2 | 1 | 需要 AdapterRegistry、RuntimeType 等基礎設施才能理解 values 如何流入 K8s adapter |
| 3 | 2 | Renderer 依賴對映表知道 ProjectConfig key → values.yaml path |
| 4 | — | 僅依賴 `domain.ProjectHealth`、`domain.ServiceName`，Phase 1 已定義 |
| 5 | — | 僅依賴 `domain.PortAllocator`、`domain.PortSet`，Phase 1 已定義 |
| 6 | 1, 3, 4, 5 | Adapter 整合 AdapterRegistry（功能 1）、Renderer（功能 3）、StatusParser（功能 4）、PortAllocator（功能 5） |

---

## 建議實作順序

1. **功能 1（multi-runtime-infra）** — 最先完成；所有後續功能的前置條件
2. **功能 2（helm-values-mapping）** — 研究型功能，產出對映表供功能 3 使用
3. **功能 3（k8s-values-renderer）**、**功能 4（k8s-status-parser）**、**功能 5（k8s-port-allocator）** — 可平行
4. **功能 6（k8s-adapter）** — 最後整合

---

## 退出標準

### 設計階段

- [ ] 所有功能的設計文件狀態為 `approved`（通過兩位 reviewer 審查）
  - [ ] `multi-runtime-infra.md` — approved
  - [ ] `helm-values-mapping.md` — approved
  - [ ] `k8s-values-renderer.md` — approved
  - [ ] `k8s-status-parser.md` — approved
  - [ ] `k8s-port-allocator.md` — approved
  - [ ] `k8s-adapter.md` — approved

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

# 完整測試套件（含 race condition 檢查）
go test -race -count=1 ./...

# K8s 整合測試（需 OrbStack K8s 環境）
go test -race -tags=integration ./internal/adapter/k8s/...

# E2E 驗證（需 OrbStack K8s 環境 + Helm repo）
sbctl project create k8s-test --runtime kubernetes
sbctl project start k8s-test
sbctl project status k8s-test
sbctl project stop k8s-test
sbctl project delete k8s-test

# 驗證 Compose 專案不受影響（回歸測試）
sbctl project create compose-test
sbctl project start compose-test
sbctl project status compose-test
sbctl project stop compose-test
sbctl project delete compose-test
```

---

## 關鍵技術決策

| 關注點 | 決策 | 理由 |
|--------|------|------|
| Helm Chart | 社群 `supabase-community/supabase-kubernetes` v0.5.2 | 官方社群維護，values 結構完整 |
| K8s 操作方式 | Shell out to `helm` / `kubectl` CLI | 與 ComposeAdapter 風格一致；避免引入龐大的 `client-go` 依賴 |
| Kong 暴露方式 | NodePort | 單節點 k3s 最簡單；OrbStack 支援 `localhost:PORT` |
| 其他服務暴露 | ClusterIP（OrbStack 可直接從 Mac 存取） | 免 NodePort，減少 port 競爭 |
| 持久儲存 | local-path-provisioner（k3s/OrbStack 內建） | 零配置，StorageClass `local-path` |
| Create/Start 語義 | Create = namespace + write values.yaml；Start = `helm upgrade --install` | 與 ComposeAdapter Create（不啟動容器）語義一致 |
| Stop 語義 | `helm uninstall`（保留 namespace + PVC） | 乾淨釋放 pod 資源；PVC 保留資料 |
| Destroy 語義 | `helm uninstall` + `kubectl delete namespace` | PVC 隨 namespace cascade 刪除 |
| 多 runtime 支援 | `AdapterRegistry` 介面 + 按 `project.RuntimeType` 動態選取 | 避免全域單一 adapter 限制 |

---

## 風險與待決事項

| 風險 / 待決事項 | 影響範圍 | 處理方式 |
|----------------|---------|---------|
| Helm chart 無 supavisor 元件 | 功能 4（Status Parser） | 接受：K8s 環境中 ServiceSupavisor 回傳 `unknown` |
| AdapterRegistry 重構影響面廣 | 功能 1 | 先確保所有現有 Compose 測試通過再繼續 |
| Helm chart values 結構隨版本更新 | 功能 3 | Pin chart version；功能 2 中記錄 chart version |
| OrbStack K8s 行為可能隨版本變化 | 整合測試 | 文件記錄 OrbStack 版本要求 |
| `computePerProjectVars` 重構可能影響現有專案 | 功能 1 | 確保 RuntimeDockerCompose 的輸出完全不變 |
| NodePort 範圍衝突（與其他 K8s 應用） | 功能 5 | 透過 `kubectl get svc` 查詢已用 NodePort |
| 跨 runtime Reset 語義 | 功能 6 | Reset = Delete + Create + Start；必須從 DB 讀取原始 runtime_type |

---

## 變更記錄

| 日期 | 變更內容 | 原因 |
|------|---------|------|
| 2026-03-28 | 初稿建立；包含完整架構審計結果 | Phase 6 規劃開始 |
