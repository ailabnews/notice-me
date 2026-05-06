package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

    "notify-me/internal/singleton"
)

// hookEvent carries the fields we need to display and build responses.
type hookEvent struct {
	HookEventName string          `json:"hook_event_name"`
	ToolName      string          `json:"tool_name,omitempty"`
	ToolInput     json.RawMessage `json:"tool_input,omitempty"`
	Message       string          `json:"message,omitempty"`
}

// toolInput holds parsed tool_input for display.
type toolInput struct {
	Command     string `json:"command,omitempty"`
	Description string `json:"description,omitempty"`
	FilePath    string `json:"file_path,omitempty"`
	Content     string `json:"content,omitempty"`
	OldString   string `json:"old_string,omitempty"`
	NewString   string `json:"new_string,omitempty"`
	Pattern     string `json:"pattern,omitempty"`
	Query       string `json:"query,omitempty"`
	URL         string `json:"url,omitempty"`
}

// isInteractiveTool returns true for tools that present interactive choices
// to the user in the terminal. For these tools, we show an info popup
// instead of a blocking decision popup, and don't consume terminal input
// so the user can select options in the terminal.
func isInteractiveTool(toolName string) bool {
	switch toolName {
	case "AskUserQuestion":
		return true
	default:
		return false
	}
}

// runHook is the "notify-me hook" subcommand. It reads a Claude Code hook
// request from stdin, forwards it to the running notify-me daemon via IPC,
// and writes the response to stdout.
//
// For blocking events (PreToolUse, PermissionRequest), it also:
//   - displays event info on stderr
//   - accepts terminal input (/dev/tty or CONIN$) concurrently
//   - the first response (GUI popup or terminal) wins
//
// Interactive tools (AskUserQuestion) are auto-allowed with an info popup
// so the terminal remains free for the user to make selections.
func runHook() {
	body, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "notify-me hook: read stdin: %v\n", err)
		os.Exit(1)
	}

	var evt hookEvent
	_ = json.Unmarshal(body, &evt)

	// Display event info to stderr so the user can see what's happening.
	displayEvent(&evt)

	blocking := evt.HookEventName == "PreToolUse" || evt.HookEventName == "PermissionRequest"
	if !blocking {
		// Non-blocking events (Notification, Stop, StopFailure): forward to
		// daemon, return immediately. No terminal prompt needed.
		resp, err := singleton.HookIPC(body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "notify-me hook: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(resp))
		return
	}

	// Interactive tools: show info popup, auto-allow, don't consume terminal input.
	// This frees /dev/tty so Claude Code can present options for the user to select.
	if isInteractiveTool(evt.ToolName) {
		resp, err := singleton.HookIPC(body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "notify-me hook: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(resp))
		return
	}

	// Blocking event: race terminal input vs GUI popup.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type result struct {
		data []byte
		err  error
	}
	ipcCh := make(chan result, 1)
	ttyCh := make(chan result, 1)

	// Goroutine 1: IPC to daemon (shows GUI popup).
	go func() {
		resp, err := singleton.HookIPCCancel(ctx, body)
		ipcCh <- result{data: resp, err: err}
	}()

	// Goroutine 2: read from the real terminal.
	go func() {
		resp, err := readTerminalInput(&evt)
		ttyCh <- result{data: resp, err: err}
	}()

	// First response wins.
	select {
	case r := <-ipcCh:
		cancel() // stop terminal reader
		if r.err != nil {
			fmt.Fprintf(os.Stderr, "notify-me hook: %v\n", r.err)
			os.Exit(1)
		}
		fmt.Println(string(r.data))
	case r := <-ttyCh:
		cancel() // stop IPC (closes connection, daemon cancels popup)
		if r.err != nil {
			// Terminal read failed (no TTY?), fall back to IPC.
			r2 := <-ipcCh
			if r2.err != nil {
				fmt.Fprintf(os.Stderr, "notify-me hook: %v\n", r2.err)
				os.Exit(1)
			}
			fmt.Println(string(r2.data))
			return
		}
		fmt.Println(string(r.data))
	}
}

// displayEvent prints hook event info to stderr.
func displayEvent(evt *hookEvent) {
	var ti toolInput
	_ = json.Unmarshal(evt.ToolInput, &ti)

	switch evt.HookEventName {
	case "PreToolUse":
		fmt.Fprintf(os.Stderr, "\n[notify-me] 🔧 工具: %s\n", evt.ToolName)
		if ti.Description != "" {
			fmt.Fprintf(os.Stderr, "[notify-me]    说明: %s\n", ti.Description)
		}
		if ti.Command != "" {
			fmt.Fprintf(os.Stderr, "[notify-me]    命令: %s\n", ti.Command)
		}
		if ti.FilePath != "" {
			fmt.Fprintf(os.Stderr, "[notify-me]    文件: %s\n", ti.FilePath)
		}
		if isInteractiveTool(evt.ToolName) {
			fmt.Fprintf(os.Stderr, "[notify-me] 💬 请到终端进行选择\n")
		} else {
			fmt.Fprintf(os.Stderr, "[notify-me] 允许执行? [Y/n] ")
		}
	case "PermissionRequest":
		fmt.Fprintf(os.Stderr, "\n[notify-me] 🔑 权限请求: %s\n", evt.ToolName)
		if ti.Command != "" {
			fmt.Fprintf(os.Stderr, "[notify-me]    命令: %s\n", ti.Command)
		}
		if ti.FilePath != "" {
			fmt.Fprintf(os.Stderr, "[notify-me]    文件: %s\n", ti.FilePath)
		}
		fmt.Fprintf(os.Stderr, "[notify-me] 允许? [Y/n] ")
	case "Notification":
		fmt.Fprintf(os.Stderr, "\n[notify-me] 📢 %s\n", evt.Message)
	case "Stop":
		fmt.Fprintf(os.Stderr, "\n[notify-me] ✅ Claude 已完成\n")
	case "StopFailure":
		fmt.Fprintf(os.Stderr, "\n[notify-me] ❌ Claude 出错\n")
	}
}

// readTerminalInput opens the real terminal (/dev/tty on Unix, CONIN$ on
// Windows) and reads a y/n response, returning the appropriate hook JSON.
func readTerminalInput(evt *hookEvent) ([]byte, error) {
	tty, err := openTTY()
	if err != nil {
		return nil, err
	}
	defer tty.Close()

	scanner := bufio.NewScanner(tty)
	if !scanner.Scan() {
		return nil, fmt.Errorf("no terminal input")
	}
	line := strings.TrimSpace(strings.ToLower(scanner.Text()))

	allow := line == "" || line == "y" || line == "yes"

	switch evt.HookEventName {
	case "PreToolUse":
		decision := "allow"
		reason := "用户通过终端批准"
		if !allow {
			decision = "deny"
			reason = "用户通过终端拒绝"
		}
		return json.Marshal(map[string]any{
			"hookSpecificOutput": map[string]any{
				"hookEventName":         "PreToolUse",
				"permissionDecision":    decision,
				"permissionDecisionReason": reason,
			},
		})
	case "PermissionRequest":
		behavior := "allow"
		if !allow {
			behavior = "deny"
		}
		out := map[string]any{
			"hookSpecificOutput": map[string]any{
				"hookEventName": "PermissionRequest",
				"decision": map[string]any{
					"behavior": behavior,
				},
			},
		}
		if !allow {
			out["hookSpecificOutput"].(map[string]any)["decision"].(map[string]any)["message"] = "用户通过终端拒绝"
		}
		return json.Marshal(out)
	}
	return nil, fmt.Errorf("unsupported event: %s", evt.HookEventName)
}
