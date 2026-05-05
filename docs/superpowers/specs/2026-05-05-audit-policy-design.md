# 策略审批功能页设计

**日期**: 2026-05-05
**状态**: Draft

## 概述

在主窗口新增"审核"Tab，提供两层免审批机制：全局策略引擎模版和会话级免审批规则。同时在通知弹框中新增"会话授权"按钮，支持一键将当前工具调用添加到会话免审批列表。

## 目标

- 减少重复确认：常用安全命令无需每次弹框
- 保持安全边界：全局策略和会话规则分开管理，可随时撤销
- 零学习成本：从弹框直接授权，规则自动生成

## 页面布局

审核 Tab 采用上下分区 + 表格布局，位于 Tab 栏（首页 | 通知历史 | 审核 | 设置）第三位。

### 上方：策略引擎模版

全局生效的免审批规则，对所有会话有效。表格列：

| 列 | 说明 |
|---|---|
| 启用 | 开关，控制规则是否生效 |
| 工具名 | 匹配的工具名（如 `Bash`、`Edit`、`Write`、`*`） |
| 命令模式 | glob 模式匹配命令内容。对文件类工具（Edit/Write）匹配文件绝对路径；对 Bash 匹配命令文本；`*` 表示匹配所有 |
| 优先级 | 数字越大优先级越高，默认 0 |
| 操作 | 编辑、删除 |

新增规则通过「+ 新增规则」按钮打开编辑对话框。

**匹配逻辑**：当请求进入时，按优先级从高到低遍历已启用的全局规则。匹配条件为 `工具名一致` AND `命令模式匹配`。命中则自动允许，跳过弹框。

### 下方：会话免审批列表

从弹框中添加的会话级规则，持久化存储。表格列：

| 列 | 说明 |
|---|---|
| 会话 ID | 来源会话标识，截断显示 |
| 工具名 | 匹配的工具名 |
| 命令/路径模式 | glob 模式。Bash 匹配命令；Edit/Write 匹配文件绝对路径 |
| 来源 | 固定为"弹框授权" |
| 添加时间 | 规则创建时间 |
| 操作 | 删除 |

提供「清理已结束会话」按钮，批量删除不再活跃的会话规则。活跃判断标准：该 session_id 在 `notifications` 表中有最近 24 小时内的记录。

**匹配逻辑**：全局规则未命中时，检查当前会话 ID 对应的规则。匹配条件同全局规则。命中则自动允许。

## 弹框改动

在现有的拒绝/允许按钮旁新增「会话授权」按钮。仅对 `PreToolUse` 和 `PermissionRequest` 事件显示。

弹框按钮布局：拒绝（左）、允许（中）、会话授权（右，蓝色样式）。会话授权按钮在单按钮模式（Notification/Stop）下不显示。

**点击「会话授权」**：
1. 允许当前请求（等同于点击"允许"）
2. 自动生成一条会话规则并添加到会话免审批列表：
   - 工具名 = 当前请求的工具名
   - 命令/路径模式 = 从工具输入中提取（见下方「模式生成规则」）
   - 会话 ID = 当前请求的 session_id
3. 后续同会话中匹配的请求自动通过，不再弹框

**模式生成规则**（从弹框自动添加时）：
- `Bash`：取命令二进制名 + ` *`。例如 `git commit -m "fix"` → 模式 `git *`；`npm test -- --watch` → 模式 `npm *`
- `Edit` / `Write`：取 file_path 的目录部分 + `/*`。例如 `/Users/x/proj/src/main.go` → 模式 `/Users/x/proj/src/*`
- 其他工具：模式为 `*`（匹配该工具的所有调用）

## 数据模型

### SQLite 新增表：`policy_rules`

```sql
CREATE TABLE policy_rules (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    type        TEXT NOT NULL,           -- 'global' | 'session'
    session_id  TEXT NOT NULL DEFAULT '',-- 仅 type='session' 时有值
    tool_name   TEXT NOT NULL,           -- 工具名或 '*'
    pattern     TEXT NOT NULL DEFAULT '*',-- glob 模式
    enabled     INTEGER NOT NULL DEFAULT 1,
    priority    INTEGER NOT NULL DEFAULT 0,
    source      TEXT NOT NULL DEFAULT '', -- 'manual' | 'popup'
    created_at  INTEGER NOT NULL,        -- Unix ms
    updated_at  INTEGER NOT NULL         -- Unix ms
);
CREATE INDEX idx_policy_rules_type ON policy_rules(type, enabled, priority DESC);
CREATE INDEX idx_policy_rules_session ON policy_rules(session_id);
```

Go 常量定义（在 `internal/policy` 包中）：
```go
const (
    RuleTypeGlobal  = "global"
    RuleTypeSession = "session"
    SourceManual    = "manual"
    SourcePopup     = "popup"
)
```

### 通知记录审计

策略匹配自动批准的请求**仍会创建通知记录**，状态为 `auto_approved`。`notifications` 表新增 `resolved_by_rule_id` 列（INTEGER, nullable），记录匹配的规则 ID，提供审计追踪。

```sql
ALTER TABLE notifications ADD COLUMN resolved_by_rule_id INTEGER DEFAULT NULL;
```

**`Record` struct 和查询适配**：`storage.Record` 新增 `ResolvedByRuleID int64` 字段（json tag `"-"`）。所有 `Scan` 调用（`Get`、`List`、`RecentBySession` 等）增加该列。`UpdateStatus` 方法签名扩展为 `UpdateStatus(ctx, id, status, resolvedAt, ruleID)`，`ruleID` 传 `0` 表示无规则匹配。

**决策统计适配**：`DecisionStats()` 中 `auto_approved` 归入 `Approved` 计数，不单独展示。

### 匹配算法

策略规则使用内存缓存，启动时从 DB 加载，规则变更时（CRUD）刷新缓存。每次 hook 调用直接查内存，不走 DB。

```
func (e *Engine) Match(toolName, sessionID, content string) (matched bool, rule *PolicyRule) {
    // 1. 遍历全局规则（按 priority DESC）
    for _, r := range e.globalRules {
        if !r.Enabled { continue }
        if matchTool(r.ToolName, toolName) && matchPattern(r.Pattern, content) {
            return true, &r
        }
    }
    // 2. 遍历当前 session 的规则
    for _, r := range e.sessionRules[sessionID] {
        if !r.Enabled { continue }
        if matchTool(r.ToolName, toolName) && matchPattern(r.Pattern, content) {
            return true, &r
        }
    }
    return false, nil
}
```

`content` 的提取规则：
- `Bash` → `tool_input.command`
- `Edit` / `Write` → `tool_input.file_path`（绝对路径）
- 其他工具 → `tool_name` 本身

**模式匹配语义**：区分两种匹配场景：
- **文件路径匹配**（Edit/Write）：使用 `path.Match`，`*` 不跨越 `/`。模式 `/Users/x/proj/src/*` 匹配 `/Users/x/proj/src/main.go` 但不匹配 `/Users/x/proj/src/sub/util.go`。若需跨目录匹配，使用 `**` 后缀（实现时展开为逐段匹配）。
- **命令文本匹配**（Bash 等）：使用 `strings.Contains` + 前缀匹配。模式 `npm *` 匹配以 `npm ` 开头的所有命令。模式 `*` 匹配任意字符串。

### 会话授权元数据传递

`_resolve` 端点需要通知的 hook 元数据来生成策略规则。方案：Dispatcher 新增 `GetInFlight(id int64) *Notification` 方法，暴露 in-flight notification 查找。`_resolve` handler 通过 `s.disp.GetInFlight(id)` 获取 `SessionID` 和 `ToolContent`，避免在 Server 中维护重复的 in-flight map。

`dispatcher.Notification` 新增字段：
```go
type Notification struct {
    // ... existing fields ...
    SessionID    string // 从 claudeHookRequest 传入
    ToolContent  string // 提取后的匹配内容（command 或 file_path）
}
```

**自动批准的代码路径**：在 `processPreToolUse` / `processPermissionRequest` 中，先调用 `Engine.Match()`。若命中规则，直接插入通知记录（status=`auto_approved`，`resolved_by_rule_id`=rule.ID），立即返回允许结果，**绕过 `blockingPopup`**。未命中时，走原有 `blockingPopup` 流程。伪代码：

```go
func (s *Server) processPreToolUse(ctx, req) claudeHookOutput {
    toolInput := parseToolInput(req.ToolInput)
    content := extractContent(req.ToolName, toolInput)
    if rule, ok := s.policy.Match(req.ToolName, req.SessionID, content); ok {
        // 自动批准路径：插入记录，直接返回
        id, _ := s.db.Insert(ctx, Record{
            Status: "auto_approved", ToolName: req.ToolName,
            SessionID: req.SessionID, /* ... */
        })
        s.db.UpdateStatus(ctx, id, "auto_approved", time.Now().UnixMilli(), rule.ID)
        return claudeHookOutput{ /* allow */ }
    }
    // 原有弹框路径
    return s.blockingPopup(ctx, ...)
}
```

`auto_session` decision 安全约束：
- 只能创建 `type='session'` 规则，绝不能创建 `type='global'` 规则
- 验证通知 ID 存在于 in-flight map 中
- 生成的规则 `enabled=1`，`source='popup'`

## 前端数据访问

### 策略规则 CRUD：Wails 服务绑定

遵循现有 Settings 模式，通过 Wails `Call.ByName` 访问后端方法（而非 HTTP API）：

| 方法 | 说明 |
|---|---|
| `App.GetPolicyRules()` | 获取所有规则（全局 + 会话） |
| `App.AddPolicyRule(rule)` | 新增规则 |
| `App.UpdatePolicyRule(rule)` | 更新规则 |
| `App.DeletePolicyRule(id)` | 删除规则 |
| `App.CleanupSessionRules()` | 清理不活跃会话规则 |

### 弹框授权扩展

`/_resolve` 端点（HTTP，不变）新增 `auto_session` decision 值。当 decision 为 `auto_session` 时：
1. 从 in-flight notification 中获取 SessionID 和 ToolContent
2. 生成会话规则（自动推导 pattern）并插入 `policy_rules`
3. 刷新策略引擎缓存
4. 返回 "approved"

## 前端组件

### 新增文件

- `frontend/src/views/AuditView.vue` — 审核 Tab 视图
- `frontend/src/stores/audit.ts` — Pinia store，管理规则数据

### 修改文件

- `frontend/src/App.vue` — 新增审核 Tab 按钮 + 组件路由
- `frontend/src/PopupApp.vue` — 新增「会话授权」按钮（仅 two-button 模式，仅 PreToolUse/PermissionRequest 事件）
- `frontend/src/style.css` — 弹框三按钮样式

## 后端模块

### 新增

- `internal/policy/policy.go` — 规则匹配引擎（Engine struct、Match、matchPattern、matchTool、内存缓存、缓存刷新）
- `internal/policy/storage.go` — PolicyStore 接口 + 基于 storage 层的 CRUD 实现
- `internal/policy/types.go` — PolicyRule struct、常量定义

### 修改

- `internal/dispatcher/dispatcher.go` — 新增 `GetInFlight(id int64) *Notification` 方法，暴露 in-flight 查找
- `internal/dispatcher/notification.go` — Notification 新增 SessionID、ToolContent 字段
- `internal/storage/schema.go` — 新增 `policy_rules` 表迁移 + `notifications.resolved_by_rule_id` 列
- `internal/storage/storage.go` — 新增规则相关查询方法；`Record` struct 新增 `ResolvedByRuleID`；所有 Scan 调用适配新列；`UpdateStatus` 扩展签名支持 ruleID；`DecisionStats` 归入 `auto_approved` 到 Approved
- `internal/server/server.go` — 注入 PolicyEngine，注册策略 Wails 绑定
- `internal/server/claude_hook.go` — 新增 `extractContent()` 工具函数；`processPreToolUse` / `processPermissionRequest` 先调用 `Engine.Match()`，命中时走自动批准路径（插入记录 + 立即返回），未命中走原有 `blockingPopup`；`blockingPopup` 传递 SessionID/ToolContent；`_resolve` handler 通过 `disp.GetInFlight()` 获取元数据并处理 `auto_session`
- `internal/window/window.go` — 弹框传递 `has_session_auth` 参数，控制是否显示会话授权按钮
- `app.go` — 暴露策略相关 Wails 方法（GetPolicyRules、AddPolicyRule、UpdatePolicyRule、DeletePolicyRule、CleanupSessionRules）
