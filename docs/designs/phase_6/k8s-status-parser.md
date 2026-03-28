> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# 設計文件：K8s Status Parser

## 狀態

done

## Phase

- **Phase：** Phase 6
- **Phase Plan：** `docs/designs/phase-6-plan.md`

---

## 目的

K8s adapter 需要取得 Kubernetes 上執行的 Supabase project 各服務健康狀態。本功能解析
`kubectl get pods -n {namespace} -o json` 的 JSON 輸出，將每個 pod 對映至
`domain.ServiceName` 並產生對應的 `domain.ServiceHealth`，最終組成完整的
`domain.ProjectHealth` 快照。

此功能是 Compose adapter 中 `parseComposePS`（解析 `docker compose ps --format json` NDJSON）
的 K8s 對應版本，提供相同的語意但針對 Kubernetes PodList JSON 格式。

---

## 範圍

### 包含

- 解析 `kubectl get pods -n {namespace} -o json` 的 PodList JSON 輸出
- Helm chart pod label（`app.kubernetes.io/name`）到 `domain.ServiceName` 的對映表
- Kubernetes pod phase 與 Ready condition 到 `domain.ServiceStatus` 的對映邏輯
- 完整的單元測試（覆蓋所有狀態組合與邊界情境）

### 不包含

- 實際執行 `kubectl` 命令（屬於 K8s Adapter 層的職責）
- Kubernetes API client 整合（本功能為純解析函式）
- `ServiceSupavisor` 的對映（Helm chart 中無對應元件）

---

## 資料模型

### 使用的 Domain Types

本功能使用已定義於 `control-plane/internal/domain/` 的型別，不新增任何 domain type。

```go
type ServiceName string
const (
    ServiceDB         ServiceName = "db"
    ServiceAuth       ServiceName = "auth"
    ServiceRest       ServiceName = "rest"
    ServiceRealtime   ServiceName = "realtime"
    ServiceStorage    ServiceName = "storage"
    ServiceImgProxy   ServiceName = "imgproxy"
    ServiceKong       ServiceName = "kong"
    ServiceMeta       ServiceName = "meta"
    ServiceFunctions  ServiceName = "functions"
    ServiceAnalytics  ServiceName = "analytics"
    ServiceSupavisor  ServiceName = "supavisor"
    ServiceStudio     ServiceName = "studio"
    ServiceVector     ServiceName = "vector"
)

type ServiceStatus string
const (
    ServiceStatusHealthy   ServiceStatus = "healthy"
    ServiceStatusUnhealthy ServiceStatus = "unhealthy"
    ServiceStatusStarting  ServiceStatus = "starting"
    ServiceStatusStopped   ServiceStatus = "stopped"
    ServiceStatusUnknown   ServiceStatus = "unknown"
)

type ServiceHealth struct {
    Status    ServiceStatus
    Message   string
    CheckedAt time.Time
}

type ProjectHealth struct {
    Services  map[ServiceName]ServiceHealth
    CheckedAt time.Time
}
```

### 內部 JSON 反序列化型別（未匯出）

用於解析 `kubectl` 輸出的 PodList JSON 結構，僅限 `k8s` package 內部使用：

```go
type podList struct {
    Items []podItem `json:"items"`
}

type podItem struct {
    Metadata podMetadata `json:"metadata"`
    Status   podStatus   `json:"status"`
}

type podMetadata struct {
    Name   string            `json:"name"`
    Labels map[string]string `json:"labels"`
}

type podStatus struct {
    Phase      string         `json:"phase"`
    Conditions []podCondition `json:"conditions"`
}

type podCondition struct {
    Type   string `json:"type"`
    Status string `json:"status"`
    Reason string `json:"reason"`
}
```

---

## 介面合約

### 匯出函式

```go
// parseK8sPods converts kubectl JSON output into ProjectHealth.
func parseK8sPods(output []byte) *domain.ProjectHealth
```

- **輸入：** `kubectl get pods -n {namespace} -o json` 的原始 `[]byte` 輸出
- **輸出：** `*domain.ProjectHealth`，包含所有已識別服務的健康狀態
- **保證：** 永不回傳 `nil`，永不 panic；任何無法解析的輸入均回傳空的 `Services` map

### Helm Chart Pod Label → ServiceName 對映表

透過 pod 上的 `app.kubernetes.io/name` label 識別服務。對映關係如下：

| Pod label 值 | domain.ServiceName |
|---|---|
| `supabase-analytics` | `ServiceAnalytics` |
| `supabase-auth` | `ServiceAuth` |
| `supabase-db` | `ServiceDB` |
| `supabase-functions` | `ServiceFunctions` |
| `supabase-imgproxy` | `ServiceImgProxy` |
| `supabase-kong` | `ServiceKong` |
| `supabase-meta` | `ServiceMeta` |
| `supabase-realtime` | `ServiceRealtime` |
| `supabase-rest` | `ServiceRest` |
| `supabase-storage` | `ServiceStorage` |
| `supabase-studio` | `ServiceStudio` |
| `supabase-vector` | `ServiceVector` |

> **備註：** Helm chart 中無 supavisor 元件，因此 `ServiceSupavisor` 不在對映表中。

---

## 執行流程

1. 接收 `kubectl get pods -n {namespace} -o json` 的原始 `[]byte` 輸出
2. 嘗試將輸入以 `json.Unmarshal` 解析為 `podList` 結構
3. 若解析失敗（無效 JSON 或空輸入），回傳 `ProjectHealth` 並帶空的 `Services` map
4. 遍歷 `podList.Items` 中的每個 pod：
   1. 讀取 `metadata.labels["app.kubernetes.io/name"]`
   2. 在 component→ServiceName 對映表中查找
   3. 若 label 遺失或不在對映表中 → 跳過此 pod
   4. 讀取 `status.phase` 並根據對映規則判定 `ServiceStatus`：
      - 若 phase 為 `"Running"`，進一步檢查 `status.conditions` 中 `type=="Ready"` 的 condition
      - 根據 Ready condition 的 `status` 與 `reason` 欄位細化狀態判定
   5. 建立 `ServiceHealth` 並寫入 `ProjectHealth.Services` map
5. 回傳完整的 `ProjectHealth`

### 服務名稱解析流程

```
pod.metadata.labels["app.kubernetes.io/name"]
    │
    ▼
在 componentToServiceName map 中查找
    │
    ├─ 找到 → 使用對應的 ServiceName
    │
    └─ 未找到（或 label 遺失）→ 跳過此 pod
```

### K8s Pod 狀態對映規則

Kubernetes pod 具有 `.status.phase` 與 `.status.conditions`（特別是 `"Ready"` condition）。
對映規則如下：

| phase | Ready condition | → ServiceStatus |
|---|---|---|
| `Running` | `status="True"` | `ServiceStatusHealthy` |
| `Running` | `status="False"` + `reason="ContainersNotReady"` | `ServiceStatusStarting` |
| `Running` | `status="False"`（其他 reason） | `ServiceStatusUnhealthy` |
| `Running` | `status="Unknown"` | `ServiceStatusUnknown` |
| `Running` | 無 Ready condition | `ServiceStatusUnknown` |
| `Pending` | 任意 | `ServiceStatusStarting` |
| `Succeeded` | 任意 | `ServiceStatusStopped` |
| `Failed` | 任意 | `ServiceStatusUnhealthy` |
| `Unknown` | 任意 | `ServiceStatusUnknown` |
| （遺失/空白） | 任意 | `ServiceStatusUnknown` |

### kubectl JSON 結構參考（PodList）

```json
{
  "kind": "PodList",
  "items": [
    {
      "metadata": {
        "name": "supabase-db-0",
        "labels": {
          "app.kubernetes.io/name": "supabase-db"
        }
      },
      "status": {
        "phase": "Running",
        "conditions": [
          {
            "type": "Ready",
            "status": "True"
          }
        ],
        "containerStatuses": [
          {
            "name": "supabase-db",
            "ready": true,
            "state": { "running": {} }
          }
        ]
      }
    }
  ]
}
```

---

## 錯誤處理

| 情境 | 處理方式 | 回應格式 |
|---|---|---|
| 空輸出 | 回傳 `ProjectHealth` 並帶空的 `Services` map | `&ProjectHealth{Services: map[...]{}}` |
| 無效 JSON | 回傳 `ProjectHealth` 並帶空的 `Services` map | `&ProjectHealth{Services: map[...]{}}` |
| 未知的 pod label | 跳過該 pod，不加入 health 結果 | 該服務不出現在 `Services` map 中 |
| 同一服務有多個 pod | 後者覆蓋前者（last one wins） | 正常的 `ServiceHealth` |
| 無 `"Ready"` condition | 視為 `ServiceStatusUnknown` | `ServiceHealth{Status: "unknown"}` |

> **「Last pod wins」策略說明：** Supabase Helm chart 預設每個服務為單一 replica（replicas: 1），因此「last pod wins」等同於「only pod」。未來若需支援多 replica，應改為 worst-case aggregation（任一 pod 不健康則該服務不健康）。此為已知限制。

---

## 測試策略

### 需要測試的行為

- 空輸入 → 空 `Services` map
- 無效 JSON → 空 `Services` map
- 全部服務健康 → 所有 service 為 `ServiceStatusHealthy`
- 混合狀態（healthy、starting、unhealthy）→ 各服務正確對映
- Pending phase pod → `ServiceStatusStarting`
- Failed phase pod → `ServiceStatusUnhealthy`
- 未知 label 的 pod → 被跳過
- 無 Ready condition → `ServiceStatusUnknown`
- Succeeded phase pod → `ServiceStatusStopped`

### 測試類型分配

| 測試類型 | 測試目標 | 層次 |
|---|---|---|
| 單元測試 | `parseK8sPods` 所有狀態組合與邊界條件 | adapter（`k8s` package） |

### 測試案例

| 測試函式 | 輸入 | 預期結果 |
|---|---|---|
| `TestParseK8sPods_Empty` | 空 `[]byte` | 空 `Services` map |
| `TestParseK8sPods_InvalidJSON` | `[]byte("not json")` | 空 `Services` map |
| `TestParseK8sPods_AllHealthy` | 所有服務皆 `Running` + `Ready=True` | 所有 service 為 `ServiceStatusHealthy` |
| `TestParseK8sPods_MixedStatus` | 混合 healthy、starting、unhealthy | 各服務狀態正確對映 |
| `TestParseK8sPods_PendingPod` | phase=`Pending` | `ServiceStatusStarting` |
| `TestParseK8sPods_FailedPod` | phase=`Failed` | `ServiceStatusUnhealthy` |
| `TestParseK8sPods_UnknownLabel` | 未知的 `app.kubernetes.io/name` | 該 pod 被跳過 |
| `TestParseK8sPods_NoReadyCondition` | phase=`Running` 但無 Ready condition | `ServiceStatusUnknown` |
| `TestParseK8sPods_ReadyStatusUnknown` | phase=`Running` + Ready condition `status="Unknown"` | `ServiceStatusUnknown` |
| `TestParseK8sPods_SucceededPhase` | phase=`Succeeded` | `ServiceStatusStopped` |
| `TestParseK8sPods_UnknownPhase` | phase=`Unknown` | `ServiceStatusUnknown` |
| `TestParseK8sPods_EmptyPhase` | phase=`""` 或遺失 | `ServiceStatusUnknown` |
| `TestParseK8sPods_DuplicateService_LastWins` | 同一服務 2 個 pod，不同狀態 | 後者的狀態覆蓋前者（last pod wins） |

### Mock 策略

- 本功能為純函式（pure function），不依賴任何外部服務或 interface
- 使用 test constant 中的 fixture JSON 資料作為輸入
- 無需 mock

### CI 執行方式

- 所有測試在一般 CI 環境中以 `go test` 執行
- 不需要特殊環境（無需 Docker、Kubernetes 或網路存取）

---

## Production Ready 考量

### 錯誤處理

- 任何格式錯誤的輸入均回傳空的 `ProjectHealth`（永不 panic）
- 呼叫端根據空結果自行決定後續處理

### 日誌與可觀測性

- 不適用：本功能為純解析函式，不產生 log
- 呼叫端（K8s adapter）負責記錄 `kubectl` 執行結果與錯誤

### 輸入驗證

- 優雅處理任何輸入：空值、無效 JSON、結構不完整的 JSON 均安全處理
- 不會因為意外輸入而 panic 或回傳 nil

### 安全性

- 不涉及 secret 或敏感資料
- 僅解析 pod 的 metadata 與 status，不接觸 spec 或 secret volume

### 優雅降級

- 空輸出 → 回傳空的 health（呼叫端決定後續行為）
- 部分 pod 無法識別 → 跳過，仍回傳已識別的服務狀態

### 設定管理

- 不需要任何環境變數
- 對映表為硬編碼的 Go map（與 Helm chart 版本綁定）

---

## 參考實作

### Compose Status Parser

`control-plane/internal/adapter/compose/status_parser.go` 解析
`docker compose ps --format json` NDJSON 輸出。本功能採用相同的設計模式：

- 純函式，接收原始 `[]byte`，回傳 `*domain.ProjectHealth`
- 永不回傳 `nil`，永不 panic
- 無法解析的輸入回傳空的 `Services` map

Compose 版本的對映規則：

| Docker Compose 狀態 | → ServiceStatus |
|---|---|
| `State="running"` + `Health="healthy"` | `ServiceStatusHealthy` |
| `State="running"` + `Health="starting"` | `ServiceStatusStarting` |
| `State="running"` + `Health="unhealthy"` 或 `"restarting"` | `ServiceStatusUnhealthy` |
| `State="running"` + `Health=""` | `ServiceStatusHealthy`（無 healthcheck） |
| `State="exited"` | `ServiceStatusStopped` |
| 格式錯誤的行 | 靜默跳過 |

---

## 影響檔案

| 檔案路徑 | 操作 |
|---|---|
| `internal/adapter/k8s/status_parser.go` | 新增 |
| `internal/adapter/k8s/status_parser_test.go` | 新增 |

---

## 待決問題

- 無

---

## 審查

### Reviewer A（架構）

- **狀態：** APPROVED（Round 2）
- **意見：**
  - Round 1：
    1. 對映表缺少 Running + 無 Ready condition 的情境 → 已補充
    2. 「Last pod wins」策略需要說明為何合理 → 已於錯誤處理段落補充已知限制說明
    3. 缺少 phase=Unknown 與 phase="" 的測試案例 → 已補充
  - Round 2：APPROVED

### Reviewer B（實作）

- **狀態：** APPROVED（Round 3）
- **意見：**
  - Round 1：
    1. Running + Ready condition status="Unknown" 未處理 → 已於對映表補充
    2. 缺少 DuplicateService 的測試案例 → 已補充
  - Round 2：缺少 Running + Ready status="Unknown" 的測試案例 → 已補充 TestParseK8sPods_ReadyStatusUnknown
  - Round 3：APPROVED

---

## 任務

<!-- 兩位審查者都回覆 APPROVED 後，根據設計展開為具體可執行的任務。 -->

---

## 程式碼審查

<!-- 所有任務完成後，由 code-review subagent 審查 feature branch 對 main 的完整 diff -->

- **審查結果：** PASS
- **發現問題：** 無
- **修正記錄：** 無
