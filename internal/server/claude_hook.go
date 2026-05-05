package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"notify-me/internal/diff"
	"notify-me/internal/dispatcher"
	"notify-me/internal/policy"
	"notify-me/internal/storage"
)

// claudeHookRequest represents the JSON input sent by Claude Code hooks.
// See: https://code.claude.com/docs/zh-CN/hooks
type claudeHookRequest struct {
	SessionID        string          `json:"session_id"`
	TranscriptPath   string          `json:"transcript_path"`
	Cwd              string          `json:"cwd"`
	PermissionMode   string          `json:"permission_mode,omitempty"`
	HookEventName    string          `json:"hook_event_name"`
	ToolName         string          `json:"tool_name,omitempty"`
	ToolInput        json.RawMessage `json:"tool_input,omitempty"`
	ToolUseID        string          `json:"tool_use_id,omitempty"`
	Message          string          `json:"message,omitempty"`
	Title            string          `json:"title,omitempty"`
	NotificationType string          `json:"notification_type,omitempty"`
	Error            string          `json:"error,omitempty"`
	ErrorDetails     string          `json:"error_details,omitempty"`
	LastAssistantMsg string          `json:"last_assistant_message,omitempty"`
	StopHookActive   bool            `json:"stop_hook_active,omitempty"`
	Reason           string          `json:"reason,omitempty"`
	Source           string          `json:"source,omitempty"`
}

// claudeHookOutput is the top-level JSON output returned to Claude Code.
type claudeHookOutput struct {
	Decision           string              `json:"decision,omitempty"`
	Reason             string              `json:"reason,omitempty"`
	HookSpecificOutput *hookSpecificOutput `json:"hookSpecificOutput,omitempty"`
}

type hookSpecificOutput struct {
	HookEventName         string               `json:"hookEventName"`
	PermissionDecision    string               `json:"permissionDecision,omitempty"`
	PermissionDecisionRsn string               `json:"permissionDecisionReason,omitempty"`
	Decision              *permissionDecision  `json:"decision,omitempty"`
	Retry                 bool                 `json:"retry,omitempty"`
}

type permissionDecision struct {
	Behavior string `json:"behavior"`
	Message  string `json:"message,omitempty"`
}

// claudeToolInput holds the parsed tool_input fields we care about.
type claudeToolInput struct {
	Command     string `json:"command,omitempty"`
	Description string `json:"description,omitempty"`
	FilePath    string `json:"file_path,omitempty"`
	OldString   string `json:"old_string,omitempty"`
	NewString   string `json:"new_string,omitempty"`
	Content     string `json:"content,omitempty"`
	Pattern     string `json:"pattern,omitempty"`
	Query       string `json:"query,omitempty"`
	URL         string `json:"url,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
}

// claudeHookHandler receives Claude Code hook events and shows appropriate
// popups via the dispatcher. Blocking events (PreToolUse, PermissionRequest)
// wait for the user's decision; non-blocking events return immediately.
func (s *Server) claudeHookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Context().Done() != nil {
		// Respect client disconnect.
	}

	r.Body = http.MaxBytesReader(w, r.Body, 256<<10) // 256KB
	var req claudeHookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}

	s.log.Debug().
		Str("event", req.HookEventName).
		Str("tool", req.ToolName).
		Msg("claude hook received")

	out := s.routeHookEvent(r.Context(), &req)
	writeJSON(w, http.StatusOK, out)
}

// --------------- IPC entry point ---------------

// ProcessHookIPC processes a Claude Code hook request received via the IPC
// socket (non-HTTP path). body is the raw JSON from the hook. Returns the
// response JSON for the caller to write back to the IPC client.
func (s *Server) ProcessHookIPC(ctx context.Context, body []byte) ([]byte, error) {
	var req claudeHookRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("invalid json: %w", err)
	}

	s.log.Debug().
		Str("event", req.HookEventName).
		Str("tool", req.ToolName).
		Str("source", "ipc").
		Msg("claude hook received")

	out := s.routeHookEvent(ctx, &req)
	return json.Marshal(out)
}

// routeHookEvent dispatches a hook request to the appropriate processor.
func (s *Server) routeHookEvent(ctx context.Context, req *claudeHookRequest) claudeHookOutput {
	switch req.HookEventName {
	case "PreToolUse":
		if isInteractiveTool(req.ToolName) {
			s.processInteractiveTool(req)
			return claudeHookOutput{
				HookSpecificOutput: &hookSpecificOutput{
					HookEventName:         "PreToolUse",
					PermissionDecision:    "allow",
					PermissionDecisionRsn: "interactive tool, auto-allowed",
				},
			}
		}
		return s.processPreToolUse(ctx, req)
	case "PermissionRequest":
		if isInteractiveTool(req.ToolName) {
			s.processInteractiveTool(req)
			return claudeHookOutput{
				HookSpecificOutput: &hookSpecificOutput{
					HookEventName:         "PermissionRequest",
					PermissionDecision:    "allow",
					PermissionDecisionRsn: "interactive tool, auto-allowed",
				},
			}
		}
		return s.processPermissionRequest(ctx, req)
	case "PermissionDenied":
		return s.processPermissionDenied()
	case "Notification":
		s.processNotification(req)
		return claudeHookOutput{}
	case "Stop":
		// When stop hook toggle is off, return success immediately without popup or history.
		if !s.cfg.Snapshot().Behavior.StopHookEnabled {
			return claudeHookOutput{}
		}
		s.processStop(req)
		return claudeHookOutput{}
	case "StopFailure":
		// StopFailure (errors) always shows a popup regardless of the toggle.
		s.processStopFailure(req)
		return claudeHookOutput{}
	default:
		return claudeHookOutput{}
	}
}

// --------------- Core logic (HTTP-independent) ---------------

func (s *Server) processPreToolUse(ctx context.Context, req *claudeHookRequest) claudeHookOutput {
	toolInput := parseToolInput(req.ToolInput)

	// Policy engine check: if a matching rule exists, auto-approve without popup.
	if s.policy != nil {
		content := extractContent(req.ToolName, toolInput)
		if matched, rule := s.policy.Match(req.ToolName, req.SessionID, content); matched {
			s.autoApprove(ctx, req, toolInput, rule.ID)
			return claudeHookOutput{
				HookSpecificOutput: &hookSpecificOutput{
					HookEventName:         "PreToolUse",
					PermissionDecision:    "allow",
					PermissionDecisionRsn: "notify-me: auto-approved by policy rule",
				},
			}
		}
	}

	title, message, okText, cancelText, mode := buildPreToolUsePopup(req, toolInput)

	// Prepare diff data for Edit/Write tools.
	var diffPayload *diff.DiffPayload
	isDiffTool := req.ToolName == "Edit" || req.ToolName == "Write"
	if isDiffTool {
		dp := diff.DiffPayload{
			ToolName:  req.ToolName,
			FilePath:  toolInput.FilePath,
			OldString: toolInput.OldString,
			NewString: toolInput.NewString,
		}
		if req.ToolName == "Write" {
			dp.NewString = toolInput.Content
			// Read current file content for Write diff context.
			if data, err := os.ReadFile(toolInput.FilePath); err == nil {
				dp.OldString = string(data)
			}
		}
		dp.Hunks = diff.ComputeHunks(dp.OldString, dp.NewString)
		diffPayload = &dp
	}

	hi := &hookInfo{
		ToolName:         req.ToolName,
		ToolInputSummary: truncate(formatToolMessage(req.ToolName, toolInput), 200),
		HookEvent:        req.HookEventName,
		TranscriptPath:   req.TranscriptPath,
	}

	toolContent := extractContent(req.ToolName, toolInput)
	result, err := s.blockingPopup(ctx, req.SessionID, title, message, okText, cancelText, mode, diffPayload, hi, toolContent)
	if err != nil {
		return claudeHookOutput{
			HookSpecificOutput: &hookSpecificOutput{
				HookEventName:         "PreToolUse",
				PermissionDecision:    "deny",
				PermissionDecisionRsn: "notify-me: " + err.Error(),
			},
		}
	}

	decision := "allow"
	reason := "用户通过 notify-me 批准"
	if result != "approved" {
		decision = "deny"
		reason = "用户通过 notify-me 拒绝 (" + result + ")"
	}

	return claudeHookOutput{
		HookSpecificOutput: &hookSpecificOutput{
			HookEventName:         "PreToolUse",
			PermissionDecision:    decision,
			PermissionDecisionRsn: reason,
		},
	}
}

func (s *Server) processPermissionRequest(ctx context.Context, req *claudeHookRequest) claudeHookOutput {
	toolInput := parseToolInput(req.ToolInput)

	// Policy engine check: if a matching rule exists, auto-approve without popup.
	if s.policy != nil {
		content := extractContent(req.ToolName, toolInput)
		if matched, rule := s.policy.Match(req.ToolName, req.SessionID, content); matched {
			s.autoApprove(ctx, req, toolInput, rule.ID)
			return claudeHookOutput{
				HookSpecificOutput: &hookSpecificOutput{
					HookEventName: "PermissionRequest",
					Decision: &permissionDecision{
						Behavior: "allow",
					},
				},
			}
		}
	}

	title := "权限请求: " + req.ToolName
	message := formatToolMessage(req.ToolName, toolInput)
	if message == "" {
		message = req.ToolName + " 请求权限"
	}

	hi := &hookInfo{
		ToolName:         req.ToolName,
		ToolInputSummary: truncate(formatToolMessage(req.ToolName, toolInput), 200),
		HookEvent:        req.HookEventName,
		TranscriptPath:   req.TranscriptPath,
	}

	toolContent := extractContent(req.ToolName, toolInput)
	result, err := s.blockingPopup(ctx, req.SessionID, title, message, "允许", "拒绝", dispatcher.ModeTwoButton, nil, hi, toolContent)
	if err != nil {
		return claudeHookOutput{
			HookSpecificOutput: &hookSpecificOutput{
				HookEventName: "PermissionRequest",
				Decision: &permissionDecision{
					Behavior: "deny",
					Message:  "notify-me: " + err.Error(),
				},
			},
		}
	}

	behavior := "allow"
	if result != "approved" {
		behavior = "deny"
	}

	out := claudeHookOutput{
		HookSpecificOutput: &hookSpecificOutput{
			HookEventName: "PermissionRequest",
			Decision: &permissionDecision{
				Behavior: behavior,
			},
		},
	}
	if behavior == "deny" {
		out.HookSpecificOutput.Decision.Message = "用户通过 notify-me 拒绝"
	}
	return out
}

func (s *Server) processPermissionDenied() claudeHookOutput {
	return claudeHookOutput{
		HookSpecificOutput: &hookSpecificOutput{
			HookEventName: "PermissionDenied",
			Retry:         true,
		},
	}
}

func (s *Server) processNotification(req *claudeHookRequest) {
	title := "Claude Code"
	if req.Title != "" {
		title = req.Title
	}
	message := req.Message
	if message == "" {
		message = "通知 (" + req.NotificationType + ")"
	}
	hi := &hookInfo{
		HookEvent:      req.HookEventName,
		TranscriptPath: req.TranscriptPath,
	}
	go func() {
		_, _ = s.blockingPopup(context.Background(), req.SessionID, title, message, "知道了", "", dispatcher.ModeSingleButton, nil, hi, "")
	}()
}

func (s *Server) processStop(req *claudeHookRequest) {
	title := "Claude 已完成"
	message := "Claude 已停止响应"
	if req.LastAssistantMsg != "" {
		msg := req.LastAssistantMsg
		if utf8.RuneCountInString(msg) > 2000 {
			runes := []rune(msg)
			msg = string(runes[:1997]) + "..."
		}
		message = msg
	}
	hi := &hookInfo{
		HookEvent:      req.HookEventName,
		TranscriptPath: req.TranscriptPath,
	}
	go func() {
		_, _ = s.blockingPopup(context.Background(), req.SessionID, title, message, "知道了", "", dispatcher.ModeSingleButton, nil, hi, "")
	}()
}

func (s *Server) processStopFailure(req *claudeHookRequest) {
	title := "Claude 出错了"
	message := req.Error
	if req.ErrorDetails != "" {
		message += ": " + req.ErrorDetails
	}
	if message == "" {
		message = "未知错误"
	}
	hi := &hookInfo{
		HookEvent:      req.HookEventName,
		TranscriptPath: req.TranscriptPath,
	}
	go func() {
		_, _ = s.blockingPopup(context.Background(), req.SessionID, title, message, "知道了", "", dispatcher.ModeSingleButton, nil, hi, "")
	}()
}

// isInteractiveTool returns true for tools that present interactive choices
// in the terminal. These are auto-allowed with an info popup so the terminal
// remains free for the user to make selections.
func isInteractiveTool(toolName string) bool {
	switch toolName {
	case "AskUserQuestion":
		return true
	default:
		return false
	}
}

// processInteractiveTool shows a non-blocking info popup for interactive tools
// (like AskUserQuestion) that need the user to select options in the terminal.
func (s *Server) processInteractiveTool(req *claudeHookRequest) {
	toolInput := parseToolInput(req.ToolInput)
	title := "💬 交互选择"
	message := "Claude 需要您的选择，请到终端操作"
	if toolInput.Description != "" {
		message = toolInput.Description + "\n\n请到终端进行选择"
	}

	hi := &hookInfo{
		ToolName:         req.ToolName,
		ToolInputSummary: truncate(formatToolMessage(req.ToolName, toolInput), 200),
		HookEvent:        req.HookEventName,
		TranscriptPath:   req.TranscriptPath,
	}

	go func() {
		_, _ = s.blockingPopup(context.Background(), req.SessionID, title, message, "知道了", "", dispatcher.ModeSingleButton, nil, hi, "")
	}()
}

// --------------- Helpers ---------------

// hookInfo carries Claude Code hook metadata for dashboard tracking.
type hookInfo struct {
	ToolName         string
	ToolInputSummary string
	HookEvent        string
	TranscriptPath   string
}

// blockingPopup enqueues a notification, blocks until the user responds or
// the request context is cancelled, then returns a canonical decision:
// "approved", "denied", "acknowledged", "timeout", or "cancelled".
// The popup sends the button label as the decision; we normalize it here
// so callers always get a stable value regardless of locale/button text.
func (s *Server) blockingPopup(ctx context.Context, sessionID, title, message, okText, cancelText string, mode dispatcher.Mode, diffPayload *diff.DiffPayload, hi *hookInfo, toolContent string) (string, error) {
	if s.disp.IsPaused() {
		return "", fmt.Errorf("server paused")
	}

	n := dispatcher.NewNotification()
	n.Endpoint = "claude/hook"
	n.Title = title
	n.Message = message
	n.OkText = okText
	n.CancelText = cancelText
	n.Mode = mode
	n.Timeout = 0 // No timeout — wait for manual user action.
	n.TimeoutAct = ""
	n.SourceIP = "127.0.0.1"
	n.SourceHdr = "claude-hook"

	if hi != nil {
		n.ToolName = hi.ToolName
		n.ToolInputSummary = hi.ToolInputSummary
		n.HookEvent = hi.HookEvent
		n.TranscriptPath = hi.TranscriptPath
	}

	n.SessionID = sessionID
	n.ToolContent = toolContent

	if diffPayload != nil {
		n.HasDiff = true
	}

	id, err := s.db.Insert(ctx, storage.Record{
		Endpoint:         n.Endpoint,
		Title:            n.Title,
		Message:          n.Message,
		SourceIP:         n.SourceIP,
		SourceHeader:     n.SourceHdr,
		SessionID:        sessionID,
		ToolName:         n.ToolName,
		ToolInputSummary: n.ToolInputSummary,
		HookEvent:        n.HookEvent,
		TranscriptPath:   n.TranscriptPath,
		Status:           "pending",
		CreatedAt:        time.Now().UnixMilli(),
	})
	if err != nil {
		return "", fmt.Errorf("storage: %w", err)
	}
	n.ID = id

	// Store diff data for the diff viewer to fetch.
	if diffPayload != nil {
		s.DiffStore.Set(id, *diffPayload)
	}

	if err := s.disp.Enqueue(n); err != nil {
		return "", fmt.Errorf("enqueue: %w", err)
	}

	var raw string
	select {
	case res := <-n.ResultCh:
		_ = s.db.UpdateStatus(ctx, id, res.Decision, time.Now().UnixMilli(), 0)
		raw = res.Decision
	case <-ctx.Done():
		s.disp.Cancel(n.ID)
		s.DiffStore.Delete(n.ID)
		if s.OnCancel != nil {
			s.OnCancel(n.ID)
		}
		res := <-n.ResultCh
		_ = s.db.UpdateStatus(context.Background(), id, res.Decision, time.Now().UnixMilli(), 0)
		raw = res.Decision
	}

	return normalizeDecision(raw, okText, cancelText, mode), nil
}

// normalizeDecision maps the popup's raw decision (button label or
// dispatcher canonical value) to one of: approved, denied, acknowledged,
// timeout, cancelled.
func normalizeDecision(raw, okText, cancelText string, mode dispatcher.Mode) string {
	// Dispatcher-produced canonical values pass through as-is.
	switch raw {
	case "timeout", "cancelled":
		return raw
	}
	// Button click: raw is the button label text.
	if raw == okText {
		if mode == dispatcher.ModeSingleButton {
			return "acknowledged"
		}
		return "approved"
	}
	if raw == cancelText {
		return "denied"
	}
	// Timeout action mapped to "denied" by dispatcher, or other fallback.
	if raw == "denied" {
		return "denied"
	}
	// Unknown — treat as denied for safety.
	return "denied"
}


// parseToolInput safely parses the raw tool_input JSON.
func parseToolInput(raw json.RawMessage) claudeToolInput {
	var ti claudeToolInput
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &ti)
	}
	return ti
}

// buildPreToolUsePopup returns the popup title, message, button labels and mode
// for a PreToolUse event. Dangerous tools get the danger style.
// Message is formatted in Markdown for rich display in the popup.
func buildPreToolUsePopup(req *claudeHookRequest, ti claudeToolInput) (string, string, string, string, dispatcher.Mode) {
	message := formatToolMessageMd(req.ToolName, ti)

	if isDangerousTool(req.ToolName, ti) {
		return "⚠️ 危险操作: " + req.ToolName, message, "允许", "拒绝", dispatcher.ModeTwoButton
	}

	return "确认执行: " + req.ToolName, message, "允许", "拒绝", dispatcher.ModeTwoButton
}

// formatToolMessageMd produces a Markdown-formatted description of what the tool will do.
func formatToolMessageMd(toolName string, ti claudeToolInput) string {
	var b strings.Builder
	b.WriteString("**工具**: `" + toolName + "`\n\n")

	switch toolName {
	case "Bash":
		if ti.Description != "" {
			b.WriteString("**说明**: " + ti.Description + "\n\n")
		}
		b.WriteString("**命令**:\n\n```\n" + ti.Command + "\n```")
	case "Write":
		b.WriteString("**文件**: `" + ti.FilePath + "`\n\n")
		b.WriteString("**内容长度**: " + fmt.Sprintf("%d", len(ti.Content)) + " 字符")
	case "Edit":
		b.WriteString("**文件**: `" + ti.FilePath + "`\n\n")
		if ti.OldString != "" {
			old := ti.OldString
			if len(old) > 300 {
				old = old[:297] + "..."
			}
			b.WriteString("**替换**:\n\n```\n" + old + "\n```\n\n")
		}
		if ti.NewString != "" {
			newStr := ti.NewString
			if len(newStr) > 300 {
				newStr = newStr[:297] + "..."
			}
			b.WriteString("**替换为**:\n\n```\n" + newStr + "\n```")
		}
		if ti.OldString == "" && ti.NewString == "" {
			b.WriteString("*(空操作)*")
		}
	case "Read":
		b.WriteString("**文件**: `" + ti.FilePath + "`")
	case "Glob":
		b.WriteString("**模式**: `" + ti.Pattern + "`")
	case "Grep":
		b.WriteString("**搜索**: `" + ti.Pattern + "`")
	default:
		if ti.Command != "" {
			b.WriteString("**命令**:\n\n```\n" + ti.Command + "\n```")
		} else if ti.FilePath != "" {
			b.WriteString("**路径**: `" + ti.FilePath + "`")
		} else if ti.Query != "" {
			b.WriteString("**查询**: " + ti.Query)
		}
	}

	return b.String()
}

// formatToolMessage produces a human-readable description of what the tool will do.
func formatToolMessage(toolName string, ti claudeToolInput) string {
	switch toolName {
	case "Bash":
		msg := ti.Command
		if msg == "" {
			msg = "(no command)"
		}
		if ti.Description != "" {
			msg = ti.Description + "\n\n" + ti.Command
		}
		return msg
	case "Write":
		return "写入文件: " + ti.FilePath + "\n内容长度: " + fmt.Sprintf("%d", len(ti.Content)) + " 字符"
	case "Edit":
		msg := "编辑文件: " + ti.FilePath
		if ti.OldString != "" {
			if len(ti.OldString) > 200 {
				msg += "\n替换: " + ti.OldString[:197] + "..."
			} else {
				msg += "\n替换: " + ti.OldString
			}
		}
		return msg
	case "Read":
		return "读取文件: " + ti.FilePath
	case "Glob":
		return "搜索文件: " + ti.Pattern
	case "Grep":
		return "搜索内容: " + ti.Pattern
	case "WebFetch":
		return "获取 URL: " + ti.URL
	case "WebSearch":
		return "搜索: " + ti.Query
	case "Agent":
		return "启动子代理\n" + truncate(ti.Prompt, 300)
	default:
		if ti.Command != "" {
			return ti.Command
		}
		if ti.FilePath != "" {
			return toolName + ": " + ti.FilePath
		}
		if ti.Query != "" {
			return toolName + ": " + ti.Query
		}
		return ""
	}
}

// isDangerousTool returns true for tool invocations that look destructive.
func isDangerousTool(toolName string, ti claudeToolInput) bool {
	if toolName == "Bash" {
		cmd := strings.ToLower(ti.Command)
		dangerPatterns := []string{
			"rm -rf", "rm -r", "rmdir",
			"git push --force", "git push -f", "force-push",
			"git reset --hard",
			"git clean",
			"drop table", "drop database",
			"truncate table",
			":(){ :|:& };:",
			"dd if=",
			"mkfs.",
			"> /dev/sd",
			"chmod -r 777",
		}
		for _, p := range dangerPatterns {
			if strings.Contains(cmd, p) {
				return true
			}
		}
	}
	return false
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// extractContent extracts the policy-relevant content from tool input fields.
func extractContent(toolName string, ti claudeToolInput) string {
	return policy.ExtractContent(toolName, ti.Command, ti.FilePath)
}

// autoApprove inserts a notification record marked as auto_approved with the
// matching rule ID, bypassing the popup entirely.
func (s *Server) autoApprove(ctx context.Context, req *claudeHookRequest, ti claudeToolInput, ruleID int64) {
	now := time.Now().UnixMilli()
	id, err := s.db.Insert(ctx, storage.Record{
		Endpoint:         "claude/hook",
		Title:            "确认执行: " + req.ToolName,
		Message:          formatToolMessage(req.ToolName, ti),
		SourceIP:         "127.0.0.1",
		SourceHeader:     "claude-hook",
		SessionID:        req.SessionID,
		ToolName:         req.ToolName,
		ToolInputSummary: truncate(formatToolMessage(req.ToolName, ti), 200),
		HookEvent:        req.HookEventName,
		TranscriptPath:   req.TranscriptPath,
		Status:           "auto_approved",
		CreatedAt:        now,
	})
	if err != nil {
		return
	}
	_ = s.db.UpdateStatus(ctx, id, "auto_approved", now, ruleID)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
