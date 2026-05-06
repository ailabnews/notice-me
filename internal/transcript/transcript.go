// Package transcript parses Claude Code JSONL transcript files for
// conversation replay in the dashboard.
package transcript

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// Message represents a single message in a Claude Code transcript.
type Message struct {
	Type      string `json:"type"`      // "user", "assistant"
	Role      string `json:"role"`      // "user", "assistant"
	Content   string `json:"content"`   // text content
	Timestamp int64  `json:"timestamp"` // Unix milliseconds
	ToolName  string `json:"tool_name"` // set for tool_use blocks
}

// Parse reads a JSONL transcript file and returns up to 500 messages.
func Parse(path string) ([]Message, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open transcript: %w", err)
	}
	defer f.Close()

	var msgs []Message
	reader := bufio.NewReaderSize(f, 4*1024*1024) // 4MB internal buffer

	for {
		if len(msgs) >= 500 {
			break
		}
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("read transcript: %w", err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			if err == io.EOF {
				break
			}
			continue
		}

		// Truncate extremely long lines before JSON parsing to avoid
		// excessive memory usage from giant content blocks.
		if len(line) > 2*1024*1024 {
			line = line[:2*1024*1024]
		}

		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			if err == io.EOF {
				break
			}
			continue
		}

		// Skip non-conversation entries.
		msgType, _ := raw["type"].(string)
		switch msgType {
		case "progress", "system", "file-history-snapshot", "summary":
			continue
		}

		msg := Message{Type: msgType}

		// Extract timestamp.
		switch ts := raw["timestamp"].(type) {
		case float64:
			msg.Timestamp = int64(ts)
		case string:
			if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
				msg.Timestamp = t.UnixMilli()
			}
		}

		// The interesting content is nested inside "message" for user/assistant types.
		messageRaw, _ := raw["message"].(map[string]any)
		if messageRaw == nil {
			messageRaw = raw
		}

		// Extract role.
		if r, ok := messageRaw["role"].(string); ok {
			msg.Role = r
		}

		// Extract content.
		msg.Content = extractContent(messageRaw)

		// Extract tool name for tool_use blocks.
		if name, ok := raw["name"].(string); ok {
			msg.ToolName = name
		}

		// Skip empty messages.
		if strings.TrimSpace(msg.Content) == "" && msg.ToolName == "" {
			continue
		}

		// Truncate content for display.
		if len(msg.Content) > 4000 {
			msg.Content = msg.Content[:3997] + "..."
		}

		// Normalize type.
		switch {
		case msg.Role == "user" || msg.Type == "user":
			msg.Type = "user"
		case msg.Role == "assistant" || msg.Type == "assistant":
			msg.Type = "assistant"
		}

		msgs = append(msgs, msg)

		if err == io.EOF {
			break
		}
	}

	return msgs, nil
}

// extractContent pulls text content from a raw message, handling both
// string content and array-of-blocks content formats.
func extractContent(raw map[string]any) string {
	switch c := raw["content"].(type) {
	case string:
		return c
	case []any:
		var sb strings.Builder
		for _, item := range c {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			switch block["type"] {
			case "text":
				if t, ok := block["text"].(string); ok {
					sb.WriteString(t)
					sb.WriteByte('\n')
				}
			case "tool_use":
				if name, ok := block["name"].(string); ok {
					fmt.Fprintf(&sb, "[Tool: %s] ", name)
					if input, ok := block["input"].(map[string]any); ok {
						if cmd, ok := input["command"].(string); ok {
							sb.WriteString(trunc(cmd, 200))
						} else if fp, ok := input["file_path"].(string); ok {
							sb.WriteString(fp)
						} else if desc, ok := input["description"].(string); ok {
							sb.WriteString(desc)
						}
					}
					sb.WriteByte('\n')
				}
			case "tool_result":
				if t, ok := block["content"].(string); ok {
					sb.WriteString(trunc(t, 500))
					sb.WriteByte('\n')
				}
			case "thinking":
				// Skip thinking blocks.
			}
			if sb.Len() > 8000 {
				break
			}
		}
		return strings.TrimSpace(sb.String())
	default:
		return ""
	}
}

func trunc(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
