package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"notify-me/internal/dispatcher"
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

	switch req.HookEventName {
	case "PreToolUse":
		s.handlePreToolUse(w, r, &req)
	case "PermissionRequest":
		s.handlePermissionRequest(w, r, &req)
	case "PermissionDenied":
		s.handlePermissionDenied(w, r, &req)
	case "Notification":
		s.handleNotification(w, r, &req)
	case "Stop":
		s.handleStop(w, r, &req)
	case "StopFailure":
		s.handleStopFailure(w, r, &req)
	default:
		// Unknown / unsupported event — acknowledge and move on.
		w.WriteHeader(http.StatusOK)
	}
}

// --------------- Blocking handlers ---------------

// handlePreToolUse shows a confirmation popup and returns a permissionDecision
// (allow/deny) based on the user's response.
func (s *Server) handlePreToolUse(w http.ResponseWriter, r *http.Request, req *claudeHookRequest) {
	toolInput := parseToolInput(req.ToolInput)
	title, message, okText, cancelText, mode := buildPreToolUsePopup(req, toolInput)

	result, err := s.blockingPopup(r.Context(), title, message, okText, cancelText, mode)
	if err != nil {
		// Queue full or server error — deny to be safe.
		writeJSON(w, http.StatusOK, claudeHookOutput{
			HookSpecificOutput: &hookSpecificOutput{
				HookEventName:         "PreToolUse",
				PermissionDecision:    "deny",
				PermissionDecisionRsn: "notify-me: " + err.Error(),
			},
		})
		return
	}

	decision := "allow"
	reason := "用户通过 notify-me 批准"
	if result != "approved" {
		decision = "deny"
		reason = "用户通过 notify-me 拒绝 (" + result + ")"
	}

	writeJSON(w, http.StatusOK, claudeHookOutput{
		HookSpecificOutput: &hookSpecificOutput{
			HookEventName:         "PreToolUse",
			PermissionDecision:    decision,
			PermissionDecisionRsn: reason,
		},
	})
}

// handlePermissionRequest shows a confirmation popup and returns a
// permission decision (allow/deny).
func (s *Server) handlePermissionRequest(w http.ResponseWriter, r *http.Request, req *claudeHookRequest) {
	toolInput := parseToolInput(req.ToolInput)
	title := "权限请求: " + req.ToolName
	message := formatToolMessage(req.ToolName, toolInput)
	if message == "" {
		message = req.ToolName + " 请求权限"
	}

	result, err := s.blockingPopup(r.Context(), title, message, "允许", "拒绝", dispatcher.ModeTwoButton)
	if err != nil {
		writeJSON(w, http.StatusOK, claudeHookOutput{
			HookSpecificOutput: &hookSpecificOutput{
				HookEventName: "PermissionRequest",
				Decision: &permissionDecision{
					Behavior: "deny",
					Message:  "notify-me: " + err.Error(),
				},
			},
		})
		return
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
	writeJSON(w, http.StatusOK, out)
}

// handlePermissionDenied returns a retry suggestion when auto mode denies a tool.
func (s *Server) handlePermissionRequestDenied(w http.ResponseWriter, r *http.Request, req *claudeHookRequest) {
	writeJSON(w, http.StatusOK, claudeHookOutput{
		HookSpecificOutput: &hookSpecificOutput{
			HookEventName: "PermissionDenied",
			Retry:         true,
		},
	})
}

// handlePermissionDenied is the real handler for the PermissionDenied event.
func (s *Server) handlePermissionDenied(w http.ResponseWriter, r *http.Request, req *claudeHookRequest) {
	writeJSON(w, http.StatusOK, claudeHookOutput{
		HookSpecificOutput: &hookSpecificOutput{
			HookEventName: "PermissionDenied",
			Retry:         true,
		},
	})
}

// --------------- Non-blocking handlers ---------------

// handleNotification shows a transient info popup and returns immediately.
func (s *Server) handleNotification(w http.ResponseWriter, r *http.Request, req *claudeHookRequest) {
	title := "Claude Code"
	if req.Title != "" {
		title = req.Title
	}
	message := req.Message
	if message == "" {
		message = "通知 (" + req.NotificationType + ")"
	}

	// Fire-and-forget: enqueue popup, return 200 immediately.
	go func() {
		_, _ = s.blockingPopup(context.Background(), title, message, "知道了", "", dispatcher.ModeSingleButton)
	}()

	w.WriteHeader(http.StatusOK)
}

// handleStop shows a notification popup when Claude finishes.
func (s *Server) handleStop(w http.ResponseWriter, r *http.Request, req *claudeHookRequest) {
	title := "Claude 已完成"
	message := "Claude 已停止响应"
	if req.LastAssistantMsg != "" {
		// Truncate long messages for popup display.
		// Use rune count so Chinese text isn't cut too aggressively.
		msg := req.LastAssistantMsg
		if utf8.RuneCountInString(msg) > 2000 {
			runes := []rune(msg)
			msg = string(runes[:1997]) + "..."
		}
		message = msg
	}

	go func() {
		_, _ = s.blockingPopup(context.Background(), title, message, "知道了", "", dispatcher.ModeSingleButton)
	}()

	w.WriteHeader(http.StatusOK)
}

// handleStopFailure shows a notification popup when Claude errors out.
func (s *Server) handleStopFailure(w http.ResponseWriter, r *http.Request, req *claudeHookRequest) {
	title := "Claude 出错了"
	message := req.Error
	if req.ErrorDetails != "" {
		message += ": " + req.ErrorDetails
	}
	if message == "" {
		message = "未知错误"
	}

	go func() {
		_, _ = s.blockingPopup(context.Background(), title, message, "知道了", "", dispatcher.ModeSingleButton)
	}()

	w.WriteHeader(http.StatusOK)
}

// --------------- Helpers ---------------

// blockingPopup enqueues a notification, blocks until the user responds or
// the request context is cancelled, then returns a canonical decision:
// "approved", "denied", "acknowledged", "timeout", or "cancelled".
// The popup sends the button label as the decision; we normalize it here
// so callers always get a stable value regardless of locale/button text.
func (s *Server) blockingPopup(ctx context.Context, title, message, okText, cancelText string, mode dispatcher.Mode) (string, error) {
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

	id, err := s.db.Insert(ctx, storage.Record{
		Endpoint:     n.Endpoint,
		Title:        n.Title,
		Message:      n.Message,
		SourceIP:     n.SourceIP,
		SourceHeader: n.SourceHdr,
		Status:       "pending",
		CreatedAt:    time.Now().UnixMilli(),
	})
	if err != nil {
		return "", fmt.Errorf("storage: %w", err)
	}
	n.ID = id

	if err := s.disp.Enqueue(n); err != nil {
		return "", fmt.Errorf("enqueue: %w", err)
	}

	var raw string
	select {
	case res := <-n.ResultCh:
		_ = s.db.UpdateStatus(ctx, id, res.Decision, time.Now().UnixMilli())
		raw = res.Decision
	case <-ctx.Done():
		s.disp.Cancel(n.ID)
		res := <-n.ResultCh
		_ = s.db.UpdateStatus(context.Background(), id, res.Decision, time.Now().UnixMilli())
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
	b.WriteString("**工具**: `" + toolName + "`\n")

	switch toolName {
	case "Bash":
		if ti.Description != "" {
			b.WriteString("**说明**: " + ti.Description + "\n")
		}
		b.WriteString("**命令**:\n```\n" + ti.Command + "\n```")
	case "Write":
		b.WriteString("**文件**: `" + ti.FilePath + "`\n")
		b.WriteString("**内容长度**: " + fmt.Sprintf("%d", len(ti.Content)) + " 字符")
	case "Edit":
		b.WriteString("**文件**: `" + ti.FilePath + "`\n")
		if ti.OldString != "" {
			old := ti.OldString
			if len(old) > 300 {
				old = old[:297] + "..."
			}
			b.WriteString("**替换**:\n```\n" + old + "\n```\n")
		}
		if ti.NewString != "" {
			newStr := ti.NewString
			if len(newStr) > 300 {
				newStr = newStr[:297] + "..."
			}
			b.WriteString("**替换为**:\n```\n" + newStr + "\n```")
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
			b.WriteString("**命令**:\n```\n" + ti.Command + "\n```")
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
