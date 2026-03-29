> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# Phase Plan：Phase 5 — 改善 CLI 使用體驗與 AI Agent 整合

## 來源

`docs/CONTROL_PLANE.md` — Phase 5

---

## 目標

Phase 5 完成後，`sbctl` CLI 應提供更直覺、更豐富的操作體驗，AI agent 透過 MCP 呼叫時能更精準地選擇工具：

1. `sbctl completion bash/zsh/fish` 產生 shell 補全腳本，使用者可用 Tab 鍵補全命令、slug 與旗標
2. `sbctl project create my-app -n "My App"` 執行後，立即顯示 Studio URL、API URL、Postgres DSN 與 API keys
3. `sbctl project list` 表格中的狀態欄位以色彩標示（running=🟢 綠、stopped=⚪ 灰、error=🔴 紅）
4. `sbctl project get my-app --watch` 持續輪詢狀態直到 running 或 error
5. `sbctl status` 提供所有專案的聚合總覽
6. MCP tool 說明文字經精細調整，AI agent 能更準確判斷何時使用哪個工具

---

## 進入條件

- ✅ Phase 3 完成：
  - `sbctl` CLI 已實作（8 個 project 子命令 + mcp serve）
  - `ProjectService` Use-case 層已實作
  - 輸出格式化（table/json/yaml）已實作
  - MCP Server 已實作（7 個 tools）
- ✅ Phase 6 完成：
  - 多 Runtime 支援（Docker Compose + K8s）
  - `--runtime` 旗標已實作

---

## 功能拆解

| # | 功能名稱 | 設計文件路徑 | 狀態 | 說明 |
|---|----------|-------------|------|------|
| 1 | CLI 彩色輸出 | `docs/designs/phase_5/cli-colored-output.md` | 未開始 | 狀態欄位彩色標示（ANSI）、`--no-color` 旗標、`NO_COLOR` 環境變數支援 |
| 2 | 建立專案連線資訊 | `docs/designs/phase_5/create-connection-info.md` | 未開始 | `project create` 與 `project get` 輸出增加 Studio URL、API URL、Postgres DSN、API keys |
| 3 | 全專案狀態總覽 | `docs/designs/phase_5/status-overview.md` | 未開始 | `sbctl status` 新命令，顯示所有專案聚合狀態（含 runtime 類型、服務健康計數） |
| 4 | Watch 模式 | `docs/designs/phase_5/watch-mode.md` | 未開始 | `--watch` 旗標：輪詢專案狀態直到 running/error，含 timeout 控制 |
| 5 | Shell 補全 | `docs/designs/phase_5/shell-completion.md` | 未開始 | `sbctl completion bash/zsh/fish` 產生補全腳本，支援命令、slug、旗標補全 |
| 6 | MCP Tool 說明精細化 | `docs/designs/phase_5/mcp-tool-refinement.md` | 未開始 | 重新撰寫 MCP tool descriptions，提升 AI agent 決策品質 |

---

## 依賴關係

```
功能 1：cli-colored-output（無依賴）
功能 5：shell-completion（無依賴）
功能 6：mcp-tool-refinement（無依賴）
  │
功能 1 完成後
  ↓
功能 2：create-connection-info（依賴功能 1 — 使用彩色輸出元件）
功能 3：status-overview（依賴功能 1 — 使用彩色狀態顯示）
功能 4：watch-mode（依賴功能 1 — watch 輸出含彩色狀態）
```

| 功能 | 依賴於 | 原因 |
|------|--------|------|
| 2 | 1 | 建立後輸出的狀態欄位使用彩色輸出元件 |
| 3 | 1 | 總覽表格的狀態欄位使用彩色輸出元件 |
| 4 | 1 | Watch 輸出的狀態顯示使用彩色輸出元件 |
| 5 | — | Cobra 內建 shell completion 功能，獨立於輸出格式 |
| 6 | — | 純文字修改，不依賴其他功能 |

功能 2、3、4 之間**無互相依賴**，可在功能 1 完成後平行進行。
功能 5、6 可與所有功能平行進行。

---

## 建議實作順序

1. **功能 1（cli-colored-output）** — 最先完成；定義彩色輸出基礎設施供後續功能使用
2. **功能 5（shell-completion）** 與 **功能 6（mcp-tool-refinement）** — 與功能 1 平行進行設計與審查
3. **功能 2（create-connection-info）**、**功能 3（status-overview）**、**功能 4（watch-mode）** — 功能 1 審查通過後，平行進行設計與審查
4. 所有功能設計通過後，依上述順序實作

---

## 退出標準

### 設計階段

- [ ] 所有功能的設計文件狀態為 `approved`（通過兩位 reviewer 審查）
  - [ ] `cli-colored-output.md` — approved
  - [ ] `create-connection-info.md` — approved
  - [ ] `status-overview.md` — approved
  - [ ] `watch-mode.md` — approved
  - [ ] `shell-completion.md` — approved
  - [ ] `mcp-tool-refinement.md` — approved

### 實作階段

| 任務 ID | 說明 | 狀態 |
|---------|------|------|
| cli-colored-output | ANSI 彩色狀態 + `--no-color` + `NO_COLOR` env | 未開始 |
| create-connection-info | create/get 輸出增加連線資訊 | 未開始 |
| status-overview | `sbctl status` 聚合總覽命令 | 未開始 |
| watch-mode | `--watch` 輪詢旗標 | 未開始 |
| shell-completion | `sbctl completion bash/zsh/fish` | 未開始 |
| mcp-tool-refinement | MCP tool descriptions 精細化 | 未開始 |

### Phase 整合驗證

```bash
cd control-plane

# 編譯
go build ./...

# 靜態分析
go vet ./...

# Linter
golangci-lint run ./...

# 完整測試套件（含 race condition 檢查）
go test -race -count=1 ./...

# Smoke test — 彩色輸出
sbctl project list                          # 狀態欄位應有色彩
sbctl project list --no-color               # 無色彩
NO_COLOR=1 sbctl project list               # 無色彩

# Smoke test — 建立專案連線資訊
sbctl project create smoke-test -n "Smoke"  # 應顯示 Studio URL、API URL、DSN
sbctl project get smoke-test                # 應顯示連線資訊

# Smoke test — 全專案狀態總覽
sbctl status                                # 聚合總覽

# Smoke test — Watch 模式
sbctl project get smoke-test --watch        # 應輪詢至 running 或 error

# Smoke test — Shell 補全
sbctl completion bash > /dev/null           # 應產生 bash 補全腳本
sbctl completion zsh > /dev/null            # 應產生 zsh 補全腳本
sbctl completion fish > /dev/null           # 應產生 fish 補全腳本

# Smoke test — MCP
sbctl mcp serve --help                      # 確認 MCP 仍可正常啟動

# 清理
sbctl project delete smoke-test --yes
```

---

## 風險與待決事項

| 風險 / 待決事項 | 影響範圍 | 處理方式 |
|----------------|---------|---------|
| ANSI 色彩在 Windows Terminal 的兼容性 | 功能 1 | 依賴 `NO_COLOR` 環境變數與 `--no-color` 旗標作為 fallback；遵循 [no-color.org](https://no-color.org/) 標準 |
| JSON/YAML 輸出不可含 ANSI 色碼 | 功能 1 | 彩色輸出僅套用於 table 格式；JSON/YAML 輸出保持純文字 |
| `--watch` 的預設 timeout 值 | 功能 4 | 設計文件中定義合理預設值（建議 300 秒），並提供 `--timeout` 覆寫 |
| `--watch` 在 CI/非互動環境的行為 | 功能 4 | 偵測 `!isatty(stdout)` 時仍可使用，但不清除前一行（逐行印出） |
| Shell completion 中 project slug 的動態補全需要 DB 連線 | 功能 5 | 設計文件中評估是否支援動態 slug 補全或僅補全靜態命令/旗標 |
| Cobra 內建 completion 是否支援 `ValidArgsFunction` 動態補全 | 功能 5 | Cobra v1.8+ 支援；需確認目前使用的 Cobra 版本 |
| MCP tool description 的最佳長度與格式 | 功能 6 | 研究 Claude Desktop / Copilot CLI 對 tool description 的處理方式 |

---

## 變更記錄

| 日期 | 變更內容 | 原因 |
|------|---------|------|
| 2026-03-28 | 初稿建立 | Phase 5 規劃，拆解為 6 個功能 |
