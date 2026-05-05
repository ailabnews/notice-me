package policy

import (
	"path"
	"sort"
	"strings"
	"sync"
)

type PolicyStore interface {
	ListPolicyRules() ([]PolicyRule, error)
}

type Engine struct {
	store        PolicyStore
	mu           sync.RWMutex
	globalRules  []PolicyRule
	sessionRules map[string][]PolicyRule
}

func NewEngine(store PolicyStore) *Engine {
	e := &Engine{store: store, sessionRules: map[string][]PolicyRule{}}
	if store != nil {
		e.Reload()
	}
	return e
}

func (e *Engine) Reload() {
	if e.store == nil {
		return
	}
	rules, err := e.store.ListPolicyRules()
	if err != nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.globalRules = nil
	e.sessionRules = map[string][]PolicyRule{}
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		switch r.Type {
		case RuleTypeGlobal:
			e.globalRules = append(e.globalRules, r)
		case RuleTypeSession:
			e.sessionRules[r.SessionID] = append(e.sessionRules[r.SessionID], r)
		}
	}
	sort.Slice(e.globalRules, func(i, j int) bool {
		return e.globalRules[i].Priority > e.globalRules[j].Priority
	})
}

func (e *Engine) Match(toolName, sessionID, content string) (bool, *PolicyRule) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for i := range e.globalRules {
		r := &e.globalRules[i]
		if matchTool(r.ToolName, toolName) && matchPattern(r.ToolName, r.Pattern, content) {
			return true, r
		}
	}
	sessRules := e.sessionRules[sessionID]
	for i := range sessRules {
		r := &sessRules[i]
		if matchTool(r.ToolName, toolName) && matchPattern(r.ToolName, r.Pattern, content) {
			return true, r
		}
	}
	return false, nil
}

func matchTool(ruleTool, reqTool string) bool {
	return ruleTool == "*" || ruleTool == reqTool
}

func matchPattern(toolName, pattern, content string) bool {
	if pattern == "*" {
		return true
	}
	switch toolName {
	case "Edit", "Write":
		matched, _ := path.Match(pattern, content)
		return matched
	default:
		if strings.HasSuffix(pattern, " *") {
			prefix := pattern[:len(pattern)-2]
			return strings.HasPrefix(content, prefix+" ")
		}
		return content == pattern
	}
}

func ExtractContent(toolName string, command, filePath string) string {
	switch toolName {
	case "Bash":
		return command
	case "Edit", "Write":
		return filePath
	default:
		return toolName
	}
}

func DerivePattern(toolName string, command, filePath string) string {
	switch toolName {
	case "Bash":
		cmd := strings.TrimSpace(command)
		if idx := strings.IndexByte(cmd, ' '); idx > 0 {
			return cmd[:idx] + " *"
		}
		return cmd + " *"
	case "Edit", "Write":
		dir := path.Dir(filePath)
		return dir + "/*"
	default:
		return "*"
	}
}

func (e *Engine) SetRules(global []PolicyRule, session map[string][]PolicyRule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.globalRules = global
	e.sessionRules = session
}
