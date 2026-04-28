# notify-me 设计文档

- 日期:2026-04-27
- 状态:已确认,待实施
- 项目根目录:`/data/project/aiDev/ntfy-client/`(代码项目目标名 `notify-me`)

## 1. 项目概览

### 1.1 用途

`notify-me` 是一个跨平台桌面通知与确认工具,接收本机 HTTP 推送,弹出置顶对话框,**同步阻塞**等待用户点击按钮,并把结果回写给 HTTP 调用方。

主要服务于 ClaudeCode hook 的阻塞式确认场景:hook 脚本以 `curl` 发起请求,在用户点击"确定/取消"前一直挂起,从而把"盯控制台等待确认"的工作转移到桌面弹框上。

### 1.2 核心特性

- 跨平台:Windows + macOS,单二进制
- HTTP 同步阻塞 API,响应 body 为 `approved` / `denied` / `timeout` / `acknowledged`
- 主窗口(通知历史 + 设置)+ 系统托盘(混合形态,可扩展)
- 串行队列处理并发请求,同时只显示一个置顶弹框
- 三种请求形态:纯文本 body / Header+Query 扩展 / JSON
- 通知历史持久化(SQLite),通知音、开机自启、超时等可配置
- 单实例运行,文件锁防止重复启动

### 1.3 技术栈

| 层 | 选型 |
| --- | --- |
| 语言 | Go |
| GUI 框架 | Wails v2(WebView 前端 + Go 后端) |
| 前端 | Vue 3 + Vite + Pinia(Wails 默认模板) |
| HTTP server | Go 标准库 `net/http` |
| 持久化 | SQLite(`modernc.org/sqlite`,纯 Go,无 cgo) |
| 系统托盘 | Wails 原生 SystemTray API(必要时回退 `getlantern/systray`) |
| 自启 | Win 注册表 `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`;macOS LaunchAgent `~/Library/LaunchAgents/me.notify.plist` |

### 1.4 预期产物

- `notify-me.exe`(Windows,目标 ≤ 20MB)
- `Notify Me.app`(macOS,目标 ≤ 25MB,**不签名公证**,首次需右键"打开"绕过 Gatekeeper)

## 2. 架构

### 2.1 模块视图

```
┌─────────────────────────────────────────────────────────┐
│                  notify-me 单进程                       │
│                                                         │
│  ┌──────────────┐    ┌──────────────┐    ┌───────────┐  │
│  │  HTTP Server │──▶│ Notification │──▶ │  Wails    │  │
│  │  :886 (本机) │    │   Queue      │    │  Frontend │  │
│  └──────────────┘    │  (chan)      │    │  (Vue 3)  │  │
│         ▲            └──────┬───────┘    └─────┬─────┘  │
│         │                   │                  │        │
│         │ HTTP 长连接挂起   │ 事件 emit        │ 用户点击 │
│         │                   ▼                  ▼        │
│         │            ┌──────────────┐    ┌───────────┐  │
│         └────────────│  Dispatcher  │◀──│  Window   │   │
│           response   │ (Go context) │    │  Manager  │  │
│                      └──────┬───────┘    └───────────┘  │
│                             │                           │
│                             ▼                           │
│                      ┌──────────────┐    ┌───────────┐  │
│                      │   SQLite     │    │  Config   │  │
│                      │  (history)   │    │  (json)   │  │
│                      └──────────────┘    └───────────┘  │
│                                                         │
│                      ┌──────────────┐                   │
│                      │  System Tray │                   │
│                      └──────────────┘                   │
└─────────────────────────────────────────────────────────┘
```

### 2.2 模块职责

1. **HTTP Server** (`internal/server`)
   - 监听本机端口,接收 POST 请求
   - 解析三种请求形态(纯文本 / Header+Query / JSON),统一为内部 `Notification` 结构
   - 把请求送进队列,然后阻塞等待结果(`<-ResultChan` 与 `r.Context().Done()` select)
   - 拿到结果后,写 200 + body(`approved` / `denied` / `timeout` / `acknowledged`)

2. **Queue & Dispatcher** (`internal/dispatcher`)
   - 单 worker goroutine,从 channel 取 Notification
   - 同一时刻只有一个 active 通知,通过 Wails Events 推给前端
   - 监听前端"用户决策"事件,把 Result 回填到对应 ResultChan
   - 处理超时(每个 Notification 带 `context.WithTimeout`)、客户端断开(`r.Context().Done()`)

3. **Frontend** (`frontend/`,Vue 3)
   - 主窗口:左侧标签(通知历史 / 设置),右侧内容
   - 弹框窗口:**独立 Wails Window**,置顶 + 抢焦点,显示 title/message/按钮
   - 通过 Wails Bindings 调用 Go(查历史、改配置、用户决策)
   - 通过 Wails Events 接收新通知

4. **Window Manager** (`internal/window`)
   - 管理两个窗口:主窗口(可隐藏到托盘)、弹框窗口(按需创建/销毁)
   - 弹框创建时设置 `AlwaysOnTop=true`、抢焦点、按配置定位

5. **Storage** (`internal/storage`)
   - SQLite 表 `notifications`,提供查询/分页/清理 API
   - 定期清理超过 `max_records` 或 `retention_days` 的旧记录

6. **Config & Tray** (`internal/config`, `internal/tray`)
   - 配置文件读写、热更新(端口除外)
   - 托盘菜单:显示主窗口 / 暂停接收 / 退出
   - 自启写入(平台分支)由配置开关控制

### 2.3 目录结构

```
notify-me/
├── main.go                  # Wails 入口
├── app.go                   # Wails App,暴露给前端的方法
├── wails.json
├── go.mod
├── internal/
│   ├── server/              # HTTP 服务
│   ├── dispatcher/          # 队列与分发
│   ├── storage/             # SQLite 操作
│   ├── config/              # 配置读写
│   ├── window/              # 窗口管理
│   ├── tray/                # 系统托盘
│   ├── autostart/           # 开机自启(Win/macOS 平台分支)
│   └── sound/               # 通知音(Win/macOS 平台分支)
├── frontend/                # Vue 3 项目
│   ├── src/
│   │   ├── views/
│   │   │   ├── MainWindow.vue
│   │   │   ├── ConfirmDialog.vue
│   │   │   ├── HistoryList.vue
│   │   │   └── Settings.vue
│   │   └── stores/          # Pinia
│   └── ...
└── build/                   # 平台资源(图标、plist 等)
```

## 3. HTTP API 规范

### 3.1 默认端点

| 路径 | 模式 | 默认 OK 文案 | 默认 Cancel 文案 | 默认标题 |
| --- | --- | --- | --- | --- |
| `POST /api/confirm` | two-button | 确定 | 取消 | ClaudeCode 通知 |
| `POST /api/danger` | two-button | 允许 | 拒绝 | ⚠️ 危险操作确认 |
| `POST /api/info` | single-button | 知道了 | (无) | 通知 |

`/api` 前缀可在配置中修改(为空字符串则不带前缀)。`endpoints` 配置可以新增、删除、改文案。

### 3.2 请求三种形态

**形态 1:纯文本**
```bash
curl -d "Claude 想运行 rm -rf,允许吗?" http://127.0.0.1:886/api/confirm
```

**形态 2:Header / Query 扩展**
```bash
curl -d "rm -rf /tmp/foo" \
     -H "X-Title: 危险命令" \
     -H "X-Timeout: 60" \
     -H "X-Ok: 允许" \
     -H "X-Cancel: 拒绝" \
     http://127.0.0.1:886/api/confirm
```

或者:
```bash
curl -d "rm -rf /tmp/foo" \
     "http://127.0.0.1:886/api/confirm?title=危险&timeout=60&ok=允许&cancel=拒绝"
```

**形态 3:JSON**
```bash
curl -X POST -H "Content-Type: application/json" -d '{
  "title": "危险命令",
  "message": "rm -rf /tmp/foo",
  "ok_text": "允许",
  "cancel_text": "拒绝",
  "timeout": 60
}' http://127.0.0.1:886/api/confirm
```

### 3.3 字段优先级

JSON body > Header > Query > 端点默认 > 全局默认

可识别字段:`title`, `message`, `ok_text`, `cancel_text`, `timeout`(秒)。

### 3.4 响应

| 场景 | HTTP | Body |
| --- | --- | --- |
| 用户点 OK(two-button) | 200 | `approved` |
| 用户点 Cancel / 关弹框窗口 | 200 | `denied` |
| 用户点单按钮(`/api/info`) | 200 | `acknowledged` |
| 超时无操作 | 200 | `timeout`(可配置 `timeout_action` 为 `denied`) |
| 服务暂停接收(托盘菜单暂停) | 503 | `paused` |
| 队列已满(超过 `max_queue_size`) | 503 | `queue full` |
| 路径不存在 | 404 | `not found` |
| 解析失败 | 400 | 错误消息 |
| 客户端 TCP 断开 | (无响应) | SQLite 标记为 `cancelled`,详见 3.6 |

### 3.5 完整请求生命周期

1. `curl` 发送 POST → HTTP Server 解析 → 生成 `Notification{ID, Endpoint, Title, Message, Timeout, ResultChan}`
2. 写入 SQLite(状态 `pending`)
3. 推入 `dispatcher.queue`(`chan *Notification`)
4. HTTP handler 阻塞:
   ```go
   select {
   case res := <-n.ResultChan: write res
   case <-ctx.Done():          // 超时或客户端断开
       dispatcher.Cancel(n.ID)
       return
   }
   ```
5. Dispatcher 单 worker 取出 → emit `notification:new` 事件给前端
6. 前端打开/复用置顶弹框窗口,填充内容,播放通知音(若开启)
7. 用户点按钮 → 前端调 `app.Resolve(id, "approved" | "denied" | "acknowledged")`
8. Dispatcher 把结果写回 `ResultChan`,关闭弹框窗口
9. HTTP handler 拿到结果 → 写 SQLite(状态、`resolved_at`、`duration_ms`)→ 返回 200 + body
10. Dispatcher 取下一个排队的 Notification(若有)

### 3.6 客户端断开自动撤销

HTTP handler 在等待结果时同时监听 `r.Context().Done()`。客户端 TCP 断开会触发,handler 调 `dispatcher.Cancel(id)`:

- 若 Notification 还在队列里:从队列移除,标记 `cancelled`
- 若正在弹框:关闭弹框窗口,标记 `cancelled`,dispatcher 立即处理下一个

避免"调用方早就死了,弹框还在等用户"的僵尸状态。

### 3.7 ClaudeCode hook 用法

`.claude/hooks/preToolUse.sh`:

```bash
#!/bin/bash
CMD="$CLAUDE_TOOL_INPUT"

result=$(curl -s -m 200 -d "$CMD" http://127.0.0.1:886/api/confirm)

case "$result" in
  approved) exit 0 ;;
  *)        echo "用户拒绝执行"; exit 2 ;;
esac
```

`-m 200` 客户端超时略大于服务端 `timeout=180` 秒,留 buffer。

## 4. 错误处理与边界

### 4.1 启动期

| 情况 | 处理 |
| --- | --- |
| 端口被占用 | 启动失败 → 用平台原生对话框(Win `MessageBoxW` / macOS `osascript display dialog`)显示错误并提示改端口 → 退出码 1 |
| 已有实例运行 | 文件锁 `<config_dir>/.lock`(配合 `flock`/`LockFileEx`)检测 → 不启动新实例,通过 IPC(本机 socket 文件 `<config_dir>/.sock` 或在已有实例的 HTTP server 上加一个内部 `/_internal/show-main` 端点)激活已有实例的主窗口后退出 |
| 配置文件损坏(JSON 解析失败) | 重命名为 `config.json.broken-<时间戳>` → 用默认配置启动 → 主窗口顶部横幅提示 |
| SQLite 损坏(打开失败) | 重命名为 `notifications.db.broken-<时间戳>` → 新建 → 主窗口顶部横幅提示 |

### 4.2 运行期

| 情况 | 处理 |
| --- | --- |
| 队列堆积超过 `max_queue_size`(100) | 新请求 503 + body=`queue full`,主窗口红色徽章 |
| 用户已在弹框时收到新请求 | 新请求排队,主窗口"待处理"徽章 +1 |
| 用户从托盘暂停 | 新请求立即 503 `paused`;已在队列的继续处理 |
| 客户端 TCP 断开 | 撤销该 Notification(详见 3.6) |
| 超时无操作 | 关闭弹框,写 `timeout`,返回 `timeout`(默认),或按 `timeout_action` 返回 `denied` |
| 关闭主窗口(X) | 最小化到托盘,不退出。首次提示气泡说明 |
| 用户从托盘"退出" | 优雅关停:关 HTTP server、取消所有排队请求(各返回 503)、关 SQLite |

### 4.3 安全

- **默认仅监听 `127.0.0.1`**,不暴露公网
- **可选 Token 鉴权**(默认关):配置 `auth_token` 非空时,请求必须带 `X-Token: <值>`,否则 401

### 4.4 弹框窗口行为

- `AlwaysOnTop=true`,Show 后 SetFocus,Win 平台必要时调 SetForegroundWindow
- 位置(配置):`center`(默认)、`cursor`、`bottom-right`
- 键盘:Enter = OK,Esc = Cancel,Tab 切换按钮焦点
- 通知音(配置开关,默认开):Win `SystemDefault`、macOS `Glass`,不超过 1 秒
- 大小:480 × 220,不可调整,正文超长时正文区滚动
- 图标:Dock/任务栏 + 托盘图标统一

### 4.5 日志

- 文件:`<config_dir>/logs/notify-me.log`
- 滚动:单文件 5MB,保留 3 个
- 级别:`info`(默认),配置可切 `debug`
- 内容:启动停止、每次请求(路径 / 来源 IP / 结果 / 耗时)、错误堆栈
- **不记录请求 body**(隐私)

## 5. 配置与持久化

### 5.1 配置文件 `config.json`

完整结构:

```json
{
  "server": {
    "host": "127.0.0.1",
    "port": 886,
    "endpoint_prefix": "/api",
    "auth_token": "",
    "max_queue_size": 100
  },
  "endpoints": [
    {
      "path": "confirm",
      "title": "ClaudeCode 通知",
      "ok_text": "确定",
      "cancel_text": "取消",
      "mode": "two-button"
    },
    {
      "path": "danger",
      "title": "⚠️ 危险操作确认",
      "ok_text": "允许",
      "cancel_text": "拒绝",
      "mode": "two-button"
    },
    {
      "path": "info",
      "title": "通知",
      "ok_text": "知道了",
      "cancel_text": "",
      "mode": "single-button"
    }
  ],
  "ui": {
    "popup_position": "center",
    "popup_size": { "width": 480, "height": 220 },
    "theme": "system"
  },
  "behavior": {
    "default_timeout_seconds": 180,
    "timeout_action": "timeout",
    "sound_enabled": true,
    "autostart": false,
    "minimize_to_tray_on_close": true
  },
  "history": {
    "max_records": 1000,
    "retention_days": 30
  },
  "log": {
    "level": "info",
    "max_size_mb": 5,
    "max_backups": 3
  }
}
```

- 首次启动:不存在则写入默认值
- 热更新:在主窗口设置内保存后立即生效。**修改 `server.host` / `server.port` 需重启**(主窗口提示"重启生效")

### 5.2 SQLite 数据库 `notifications.db`

```sql
CREATE TABLE notifications (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  endpoint      TEXT NOT NULL,
  title         TEXT NOT NULL,
  message       TEXT NOT NULL,
  source_ip     TEXT,
  source_header TEXT,
  status        TEXT NOT NULL,
  created_at    INTEGER NOT NULL,
  resolved_at   INTEGER,
  duration_ms   INTEGER
);
CREATE INDEX idx_created ON notifications(created_at DESC);
CREATE INDEX idx_status  ON notifications(status);
```

`status` 取值:`pending` / `approved` / `denied` / `acknowledged` / `timeout` / `cancelled`。

清理(每小时一次):
1. 删除 `created_at < now - retention_days * 86400000` 的记录
2. 若总条数仍超 `max_records`,按 `created_at` 升序删除最旧的

### 5.3 文件位置

| 平台 | 路径 |
| --- | --- |
| macOS | `~/Library/Application Support/notify-me/` |
| Windows | `%APPDATA%\notify-me\` |

目录下:

- `config.json`
- `notifications.db`
- `.lock`
- `logs/notify-me.log`(及滚动文件)

## 6. 测试策略

### 6.1 单元测试

- `internal/server`:三种请求形态解析、字段优先级合并、Token 校验
- `internal/dispatcher`:串行队列、超时取消、客户端断开取消、暂停模式
- `internal/storage`:CRUD、清理策略、损坏恢复
- `internal/config`:加载、写入、默认值、损坏恢复
- `internal/autostart`:Win/macOS 各一个测试(mock 文件系统)

目标核心逻辑覆盖率 ≥ 80%。

### 6.2 集成测试

启动真实 HTTP server + 内存 SQLite,模拟前端事件:

- `TestSyncBlocking`:curl 发请求,模拟前端点击,验证 200 + body
- `TestTimeout`:不点击,等到超时,验证 `timeout`
- `TestClientDisconnect`:中途 ctx cancel,验证排队项被撤销
- `TestSerialQueue`:并发 5 个请求,验证按顺序处理
- `TestPaused`:暂停后新请求 503

### 6.3 手动 / E2E

**Windows**

- [ ] 双击 exe → 主窗口 + 托盘图标出现
- [ ] curl 触发 → 弹框置顶覆盖其他全屏程序
- [ ] OK / Cancel → curl 拿到对应 body
- [ ] Esc / Enter / Tab 键盘操作
- [ ] 关闭主窗口 → 最小化到托盘
- [ ] 第二次启动 → 激活已有窗口
- [ ] 配置开机自启 → 重启后自动运行
- [ ] 客户端断开 → 弹框消失

**macOS**:同上 + 首次右键打开绕过 Gatekeeper

### 6.4 性能基准(选做)

- HTTP server 处理 100 个并发请求(全部排队)的内存占用
- 1 万条历史记录下,主窗口"历史"分页查询 < 50ms

## 7. 里程碑

| Phase | 内容 | 预估 |
| --- | --- | --- |
| M1 骨架 | Wails 项目脚手架、Vue 主窗口空壳、Go 模块结构、SQLite 接入 | 1 天 |
| M2 HTTP + 队列 | 三种请求解析、串行 dispatcher、阻塞响应、超时、客户端断开 | 2 天 |
| M3 弹框窗口 | 独立 Wails Window、置顶、抢焦点、键盘交互、播音 | 1 天 |
| M4 主窗口 UI | 历史列表 + 分页、设置表单、暂停开关、Pinia store | 2 天 |
| M5 系统集成 | 托盘菜单、单实例锁、自启、关闭即最小化、平台分支 | 1 天 |
| M6 打磨与测试 | 单元/集成测试、Win + Mac 实测、构建脚本、README | 1 天 |
| 合计 | | 约 8 天 |
