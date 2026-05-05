package policy

const (
	RuleTypeGlobal  = "global"
	RuleTypeSession = "session"
	SourceManual    = "manual"
	SourcePopup     = "popup"
)

type PolicyRule struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	ToolName  string `json:"tool_name"`
	Pattern   string `json:"pattern"`
	Enabled   bool   `json:"enabled"`
	Priority  int    `json:"priority"`
	Source    string `json:"source"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}
