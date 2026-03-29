> **文件語言：繁體中文**
> 本設計文件以**繁體中文**撰寫。程式碼識別名稱與技術術語保留英文。

---

# 設計文件：Watch 模式

## 狀態

done

## Phase

- **Phase：** Phase 5
- **Phase Plan：** docs/designs/phase-5-plan.md

---

## 目的

目前使用者執行 `sbctl project start <slug>` 後，需要反覆手動執行 `sbctl project get <slug>` 來確認專案是否已從 `starting` 轉為 `running`。同樣地，在 `stop`、`reset` 等操作後也需要反覆查詢狀態。

本功能新增 `--watch` 旗標（或 `-w` 短旗標），讓 `sbctl project get`、`sbctl project list` 與 `sbctl status` 命令持續定期更新輸出，直到使用者按 Ctrl+C 或達到 timeout。

---

## 範圍

### 包含

- 新增 `--watch` / `-w` 旗標到 `project get`、`project list`、`status` 命令
- 新增 `--watch-interval` 旗標控制輪詢間隔（預設 2 秒）
- 新增 `--watch-timeout` 旗標控制最大 watch 時間（預設 300 秒，0 表示無限）
- 使用 ANSI clear screen 在每次更新時清除前一次輸出
- Ctrl+C 優雅退出（context cancellation）
- 僅 table 格式支援 watch（JSON/YAML 不支援，因為持續輸出的 JSON 片段無法被管線工具正確解析）

### 不包含

- 事件驅動的即時更新（使用輪詢而非 WebSocket 或 DB LISTEN/NOTIFY）
- 差異顯示（每次刷新重新繪製完整畫面）
- 自動結束條件（例如偵測到 `running` 後自動停止 watch）— 留待未來功能
- MCP 端的 watch 支援

---

## 資料模型

本功能不修改任何資料模型。

---

## 介面合約

### 新增 `cmd/sbctl/watch.go`

```go
package main

// watchConfig holds the configuration for watch mode.
type watchConfig struct {
    enabled  bool
    interval time.Duration
    timeout  time.Duration
}

// runWatch executes the given render function in a loop, clearing the screen
// between iterations. It stops on context cancellation or timeout.
// renderFn should write output to w. If renderFn returns an error, it is
// printed to stderr and watch continues.
// The ctx passed to runWatch must already incorporate signal handling and timeout.
func runWatch(ctx context.Context, w io.Writer, errW io.Writer, cfg watchConfig, renderFn func(ctx context.Context) error) error
```

### 旗標定義

在需要 watch 的命令中註冊旗標：

```go
// addWatchFlags registers --watch, --watch-interval, --watch-timeout on a command.
func addWatchFlags(cmd *cobra.Command, cfg *watchConfig) {
    cmd.Flags().BoolVarP(&cfg.enabled, "watch", "w", false,
        "Watch for changes and refresh output periodically")
    cmd.Flags().DurationVar(&cfg.interval, "watch-interval", 2*time.Second,
        "Interval between refreshes in watch mode")
    cmd.Flags().DurationVar(&cfg.timeout, "watch-timeout", 5*time.Minute,
        "Maximum duration for watch mode (0 for no timeout)")
}
```

### 命令修改

以 `project get` 為例（使用現有函式簽名，若 Feature 1 先合併需加 `*colorer`）：

```go
func buildGetCmd(deps **Deps, output *string) *cobra.Command {
    var watchCfg watchConfig
    cmd := &cobra.Command{
        Use:   "get <slug>",
        Short: "Get project details",
        RunE: func(cmd *cobra.Command, args []string) error {
            slug := args[0]
            renderFn := func(ctx context.Context) error {
                view, err := (*deps).ProjectService.Get(ctx, slug)
                if err != nil { return err }
                return writeProjectView(cmd.OutOrStdout(), *output, view)
            }

            if watchCfg.enabled {
                if *output != "table" {
                    return fmt.Errorf("--watch is only supported with table output format")
                }
                return runWatch(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), watchCfg, renderFn)
            }
            return renderFn(cmd.Context())
        },
    }
    addWatchFlags(cmd, &watchCfg)
    return cmd
}
```

> 注意：`renderFn` 接收 `ctx context.Context` 參數，確保 watch timeout 能傳播到 service 呼叫。

### 支援 watch 的命令清單

| 命令 | renderFn 內容 | 備註 |
|------|-------------|------|
| `project get <slug>` | `Get()` + `writeProjectView()` | — |
| `project list` | `List()` + `writeProjectViews()` | — |
| `status` | `List()` + `writeStatusOverview()` | 定義於 `status-overview.md`（並行設計），需待該功能合併後整合 |

---

## 執行流程

### Watch 模式流程

1. 使用者執行 `sbctl project get my-app --watch`
2. CLI 驗證 `--output` 為 `table`（否則回傳錯誤）
3. 建立 watch context（使用 `signal.NotifyContext` 取代手動 goroutine）：
   ```go
   // Signal 處理：使用 signal.NotifyContext 避免 goroutine 洩漏
   ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
   defer stop()

   // Timeout：若 --watch-timeout > 0，在 signal context 之上疊加 timeout
   if watchCfg.timeout > 0 {
       var cancel context.CancelFunc
       ctx, cancel = context.WithTimeout(ctx, watchCfg.timeout)
       defer cancel()
   }
   ```
4. 進入 `runWatch` 迴圈：
   a. 清除畫面（ANSI `\033[2J\033[H`）
   b. 執行 `renderFn(ctx)` 取得最新資料並輸出
   c. 在輸出底部顯示 `Last updated: <timestamp> | Ctrl+C to exit`
   d. 使用 `select` 等待下次迭代（確保可即時回應取消）：
      ```go
      select {
      case <-ctx.Done():
          return nil // 優雅退出
      case <-time.After(cfg.interval):
          // 繼續下次迭代
      }
      ```
   e. 重複步驟 a-d

### ANSI Clear Screen 策略

```go
// clearScreen writes ANSI escape sequences to clear the screen and move cursor to top-left.
func clearScreen(w io.Writer) {
    fmt.Fprint(w, "\033[2J\033[H")
}
```

- `\033[2J` — 清除整個畫面
- `\033[H` — 移動游標到左上角

### Signal 處理

使用 `signal.NotifyContext`（Go 1.16+）取代手動 goroutine + channel，避免 goroutine 洩漏：

```go
ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
defer stop()
```

`signal.NotifyContext` 會在收到信號時自動取消 context，且 `stop()` 會正確清理信號監聽，無資源洩漏風險。

---

## 錯誤處理

| 情境 | 處理方式 | 回應格式 |
|---|---|---|
| `--watch` 搭配非 table 格式 | 回傳錯誤，不進入 watch 模式 | `--watch is only supported with table output format` |
| `renderFn()` 回傳錯誤 | 印出錯誤到 stderr，watch 繼續下一次迭代 | `Error: <message>` |
| `--watch-timeout` 到期 | context 取消，`select` 立即回應，靜默結束，exit code 0 | — |
| Ctrl+C（SIGINT） | `signal.NotifyContext` 取消 context，`select` 立即回應，exit code 0 | — |
| `--watch-interval` 小於 500ms | 限制最小值為 500ms，避免過度查詢 | — |

---

## 測試策略

### 需要測試的行為

- `runWatch` 在 context 取消時透過 `select` 立即停止（不等待 interval）
- `runWatch` 在 timeout 後透過 context 停止迴圈
- `runWatch` 在 `renderFn` 失敗時印出 error 到 errW 並繼續下一次迭代
- `runWatch` 在每次迭代前執行 clear screen
- `runWatch` 在每次迭代後顯示 timestamp
- `addWatchFlags` 正確註冊三個旗標（watch、watch-interval、watch-timeout）
- `--watch` 搭配 `--output json` 回傳錯誤
- `--watch` 搭配 `--output yaml` 回傳錯誤
- `--watch-interval` 最小值限制（500ms）
- `project get --watch` 在無 watch 時的行為與既有一致

### 測試類型分配

| 測試類型 | 測試目標 | 層次 |
|---|---|---|
| 單元測試 | `runWatch` 迴圈邏輯（context cancellation 立即回應、timeout、error 續行） | `cmd/sbctl` |
| 單元測試 | `addWatchFlags` 旗標註冊 | `cmd/sbctl` |
| 單元測試 | 非 table 格式的 watch 拒絕 | `cmd/sbctl` |
| 單元測試 | interval 最小值限制 | `cmd/sbctl` |

### Mock 策略

- `renderFn` — 使用計數器追蹤呼叫次數，控制何時回傳 error
- 使用短 interval（10ms）與短 timeout（50ms）加速測試
- Context cancellation 測試：取消 context 後驗證 `runWatch` 立即返回（不等待 interval）

### CI 執行方式

- 所有測試在一般 CI 中執行
- 測試不依賴真實終端機（ANSI 碼寫入 `bytes.Buffer`）

---

## Production Ready 考量

### 錯誤處理

`renderFn` 失敗不中斷 watch，避免暫時性 DB 問題導致 watch 退出。使用者可透過 Ctrl+C 手動退出。

### 日誌與可觀測性

不需日誌。展示層功能。

### 輸入驗證

- `--watch-interval` ≥ 500ms（防止過度查詢）
- `--watch-timeout` ≥ 0（0 表示無限）
- `--watch` 僅與 `--output table` 搭配

### 安全性

無安全影響。

### 優雅降級

- DB 暫時不可用時，`renderFn` 回傳 error，watch 繼續等待並在下次迭代重試
- 非 TTY 環境中 ANSI clear screen 可能不被正確解讀，但不影響功能正確性（內容仍被輸出）
- 建議：未來可加入 TTY 偵測（使用 Feature 1 的 `term.IsTerminal`），非 TTY 時跳過 ANSI clear（僅用空行分隔）

### 設定管理

| 設定 | 型別 | 預設值 | 來源 |
|------|------|--------|------|
| `--watch` | bool | `false` | CLI 旗標 |
| `--watch-interval` | duration | `2s` | CLI 旗標 |
| `--watch-timeout` | duration | `5m` | CLI 旗標 |

---

## 待決問題

- 無

---

## 審查

### Reviewer A（架構）

- **狀態：** 🔁 REVISE
- **意見：**

整體架構方向正確，但有 4 項需要修正。

**✅ 正確決策**

1. **Flag-based 方式正確。** `--watch` 掛在既有命令上（而非獨立 `watch` 命令）符合 kubectl 慣例，也避免命令重複。使用者心智模型自然：「我要 get，但持續 get」。
2. **僅限 table 格式正確。** 持續輸出 JSON 片段無法被 `jq` 等工具正確解析。NDJSON 是另一種語意（streaming），不屬於本次範圍，留到未來再議即可。
3. **`runWatch` 抽象層次恰當。** `renderFn func() error` 作為注入點，讓 watch 邏輯與具體命令解耦，且可獨立單元測試。
4. **ANSI clear screen 可接受。** `\033[2J\033[H` 作為 v1 方案務實。未來若需減少閃爍可改用 `\033[H` + 覆寫 + `\033[J`（cursor home → overwrite → clear below），但不阻擋本次。

**🔁 必須修正**

1. **Signal 處理必須使用 `signal.NotifyContext` 並清理。** 目前設計使用手動 goroutine 模式：

   ```go
   sigCh := make(chan os.Signal, 1)
   signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
   go func() { <-sigCh; cancel() }()
   ```

   問題：(a) 此 goroutine 在正常 timeout 結束時會洩漏（永遠阻塞在 `<-sigCh`）；(b) 未呼叫 `signal.Stop(sigCh)` 清理。建議改用 Go 1.16+ 的 `signal.NotifyContext`：

   ```go
   ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
   defer stop()
   if cfg.timeout > 0 {
       var cancel context.CancelFunc
       ctx, cancel = context.WithTimeout(ctx, cfg.timeout)
       defer cancel()
   }
   ```

   這樣 context 取消時自動清理 signal registration，無 goroutine 洩漏。

2. **迴圈等待必須用 `select`，不可用 `time.Sleep`。** 設計文件第 144 行寫「`time.Sleep(interval)` 或 `select`」，但 `time.Sleep` 會阻塞整個 goroutine，無法即時回應 `ctx.Done()`。必須明確規定使用 `select`：

   ```go
   select {
   case <-time.After(cfg.interval):
       // 繼續下一次迭代
   case <-ctx.Done():
       return nil // 或 ctx.Err()
   }
   ```

   請將設計文件中的「`time.Sleep(interval)` 或 `select`」改為僅 `select`。

3. **程式碼範例中 `colorOut *colorer` 參數不存在。** 設計文件第 95 行的 `buildGetCmd` 範例簽名為：

   ```go
   func buildGetCmd(deps **Deps, output *string, colorOut *colorer) *cobra.Command
   ```

   但實際程式碼簽名為 `func buildGetCmd(deps **Deps, output *string)`，且 `writeProjectView` 也無 `colorer` 參數。範例程式碼需與現有 codebase 對齊，否則實作時會產生混淆。

4. **Timeout context 的巢狀順序需明確。** 設計文件分別提到 `context.WithTimeout` 和 signal cancellation，但未明確說明兩者的組合方式。建議在「執行流程」章節明確指出：signal context 為外層，timeout context 為內層（如上述第 1 點的程式碼所示）。任一觸發皆可正確取消。

**💡 建議（非阻擋）**

- `renderFn` 內的 API 呼叫使用 `cmd.Context()`，但進入 `runWatch` 後 context 已被包裝（加了 timeout / signal）。確認 `renderFn` 閉包中的 `cmd.Context()` 是否應改為使用 `runWatch` 傳入的 `ctx`，以確保 timeout 和 signal 能正確傳播到 API 呼叫層。目前設計中 `renderFn` 捕獲的是原始 `cmd.Context()`，不受 watch timeout 控制。

### Reviewer B（實作）

- **狀態：** 🔁 REVISE
- **意見：**

#### 1. `time.Sleep` 必須改為 `select` + `time.After`（阻擋性問題）

設計在步驟 4d 列出 `time.Sleep(interval)` 作為等待方式。`time.Sleep` **不可被取消**——使用者按 Ctrl+C 時，goroutine 需等到 sleep 結束才會回應。以 2 秒 interval 為例，最差情況下使用者需等 2 秒才能退出。

建議改為：

```go
select {
case <-ctx.Done():
    return ctx.Err()
case <-time.After(cfg.interval):
    // 繼續下一次迭代
}
```

或使用 `time.NewTicker`：

```go
ticker := time.NewTicker(cfg.interval)
defer ticker.Stop()
for {
    renderFn()
    select {
    case <-ctx.Done():
        return nil
    case <-ticker.C:
    }
}
```

`time.NewTicker` 的 drift 較小，但對 2 秒 interval 差異可忽略。兩者皆可。關鍵是必須用 `select` 讓 context cancellation 能立即中斷等待。

#### 2. Signal 處理缺少清理（goroutine 與 channel 洩漏）

目前設計：

```go
sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
go func() {
    <-sigCh
    cancel()
}()
```

問題：
- 若 watch 因 timeout 正常結束而非 signal，該 goroutine **永遠阻塞**在 `<-sigCh`，造成 goroutine 洩漏。
- 未呼叫 `signal.Stop(sigCh)` 清除 signal 註冊。

建議改用 `signal.NotifyContext`（Go 1.16+），更簡潔且自動處理清理：

```go
ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
defer stop()
```

若堅持手動寫法，goroutine 需改為：

```go
go func() {
    select {
    case <-sigCh:
        cancel()
    case <-ctx.Done():
    }
    signal.Stop(sigCh)
}()
```

#### 3. `buildGetCmd` 簽名與現有程式碼不符

設計中的簽名：
```go
func buildGetCmd(deps **Deps, output *string, colorOut *colorer) *cobra.Command
```

實際程式碼（`project.go:79`）：
```go
func buildGetCmd(deps **Deps, output *string) *cobra.Command
```

程式碼庫中**不存在 `colorer` 型別**。`writeProjectView` 也沒有 `colorOut` 參數。設計需移除 `colorOut *colorer` 以符合現狀，或標註這是新增項目。

#### 4. `writeStatusOverview` 與 `status` 命令不存在

設計在「支援 watch 的命令清單」中列出：

| 命令 | renderFn 內容 |
|------|-------------|
| `status` | `List()` + `writeStatusOverview()` |

但程式碼庫中**不存在** `writeStatusOverview` 函式，也沒有 `status` 子命令。此處應釐清：這是本設計要新增的，還是引用錯誤？若為新增，需補充 `status` 命令的設計細節。

#### 5. ANSI clear screen 應加入 TTY 偵測

設計承認「非 TTY 環境中 ANSI clear screen 可能不被正確解讀」，但未提供保護措施。雖然 `--watch` + JSON/YAML 已被拒絕，但 table 輸出仍可能被管線使用（例如 `sbctl project get --watch slug | tee log.txt`）。

建議：在 `runWatch` 入口偵測 stdout 是否為 TTY，若否則拒絕進入 watch 模式或至少跳過 ANSI 序列。可用 `golang.org/x/term.IsTerminal(int(os.Stdout.Fd()))` 偵測。

此為建議項目，非必須修正。

#### 6. 測試策略可行但需補充一點

測試策略整體合理：
- 使用 `bytes.Buffer` 捕獲輸出 ✅
- 使用 mock `renderFn` 計數器 ✅
- 短 interval/timeout 加速測試 ✅

**但**：若迴圈改用 `select` + context（第 1 點），測試可精確控制取消時機，不再依賴 timing。建議補充一項測試：
- 驗證 `runWatch` 在 context 取消後**立即**停止（不等待下一個 interval 結束）

#### 總結

| # | 問題 | 嚴重度 | 類型 |
|---|------|--------|------|
| 1 | `time.Sleep` 不可取消 | 🔴 高 | 必須修正 |
| 2 | Signal goroutine 洩漏、缺少 `signal.Stop` | 🔴 高 | 必須修正 |
| 3 | `buildGetCmd` 簽名不符現有程式碼 | 🟡 中 | 必須修正 |
| 4 | `writeStatusOverview` / `status` 命令不存在 | 🟡 中 | 需釐清 |
| 5 | 非 TTY 環境 ANSI 輸出 | 🔵 低 | 建議改善 |
| 6 | 測試補充即時取消驗證 | 🔵 低 | 建議改善 |

**判定：🔁 REVISE** — 第 1、2 點涉及 goroutine 洩漏與使用者體驗的正確性問題，需修正後重新審查。第 3、4 點為事實性錯誤需更正。

---

## 任務

### T-F4-1: 建立 watch 核心邏輯（`f4-watch-core`）

**建立/修改檔案：**
- 新增 `cmd/sbctl/watch.go` — watchConfig、addWatchFlags、runWatch、clearScreen
- 新增 `cmd/sbctl/watch_test.go` — watch 核心測試

**實作內容：**
1. `watch.go`：
   - `type watchConfig struct { enabled bool; interval time.Duration; timeout time.Duration }`
   - `func addWatchFlags(cmd *cobra.Command, cfg *watchConfig)` — 註冊 `--watch`/`-w`、`--watch-interval`（預設 2s）、`--watch-timeout`（預設 5m）
   - `func runWatch(ctx context.Context, w, errW io.Writer, cfg watchConfig, renderFn func(ctx context.Context) error) error`
     - 用 `signal.NotifyContext` 處理 SIGINT/SIGTERM
     - timeout 用 `context.WithTimeout` 包裝
     - 迴圈：clearScreen → renderFn(ctx) → 印時戳 → `select { case <-ctx.Done(): return nil; case <-time.After(cfg.interval): }`
     - renderFn 失敗時印 error 到 errW，繼續下一輪
   - `func clearScreen(w io.Writer)` — `\033[2J\033[H`
2. `watch_test.go`：
   - context 取消時立即停止（不等 interval）
   - timeout 到達時停止
   - renderFn 失敗時繼續迴圈（error 印到 errW）
   - clearScreen 在每次 render 前呼叫
   - 時戳行顯示於每次 render 後
   - `addWatchFlags` 正確註冊三個 flag
   - `--watch-interval` 最小值 500ms 驗證

**驗收標準：**
- `go build ./...` 通過
- `go test -race ./...` 通過

---

### T-F4-2: 整合 watch 至 project 與 status 命令（`f4-watch-integration`）

**依賴：** T-F4-1、T-F1-2、T-F3-1

**建立/修改檔案：**
- 修改 `cmd/sbctl/project.go` — project get 與 list 加入 watch 支援
- 修改 `cmd/sbctl/status.go` — status 命令加入 watch 支援
- 修改 `cmd/sbctl/main_test.go` 或新增測試檔 — watch 整合測試

**實作內容：**
1. `project.go`：
   - `project get`：加入 `addWatchFlags`，RunE 中若 `watchCfg.enabled`：
     - 檢查 `output != "table"` 回傳 error
     - 建構 `renderFn` 呼叫 `Get()` + `writeProjectView()`
     - 呼叫 `runWatch(ctx, w, errW, watchCfg, renderFn)`
   - `project list`：同上，renderFn 呼叫 `List()` + `writeProjectViews()`
2. `status.go`：
   - `status`：加入 `addWatchFlags`，RunE 中若 `watchCfg.enabled`，同上模式
3. 測試：
   - `--watch` + `--output json` 回傳 error
   - `--watch` 模式正確呼叫 renderFn
   - 非 watch 模式行為不變

**驗收標準：**
- `go build ./...` 通過
- `go test -race ./...` 通過
- `sbctl project list --watch` 進入 watch 模式
- `sbctl project list --watch -o json` 回傳 error

---

## 程式碼審查

- **審查結果：** ✅ APPROVED（第二輪）
- **發現問題：** R1: watch-timeout 未套用 context、gofmt 格式問題、writeCreateSummary 錯誤忽略
- **修正記錄：** 7f163fc fix watch-timeout & gofmt; cd2480d fix writeCreateSummary error handling
