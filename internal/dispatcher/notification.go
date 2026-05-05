// internal/dispatcher/notification.go
package dispatcher

import (
	"sync"
	"time"
)

type Mode string

const (
	ModeTwoButton    Mode = "two-button"
	ModeSingleButton Mode = "single-button"
)

type Result struct {
	Decision string // "approved" | "denied" | "acknowledged" | "timeout" | "cancelled"
	Reason   string
}

type Notification struct {
	ID         int64
	Endpoint   string
	Title      string
	Message    string
	OkText     string
	CancelText string
	Mode       Mode
	SourceIP   string
	SourceHdr  string
	Timeout    time.Duration
	TimeoutAct string // "timeout" | "denied"
	CreatedAt  time.Time

	// Hook detail fields, populated by claude_hook.go for dashboard.
	ToolName         string
	ToolInputSummary string
	HookEvent        string
	TranscriptPath   string

	// HasDiff indicates this notification carries diff data (Edit/Write tools)
	// and the popup should show a "View Diff" button.
	HasDiff bool

	// Policy engine fields.
	SessionID   string
	ToolContent string

	// ResultCh delivers exactly one Result to the HTTP handler. Cap 1.
	ResultCh chan Result
	// Done is closed exactly once when Resolve is invoked. Used by the
	// dispatcher worker to release its active slot without competing with
	// the HTTP handler for ResultCh.
	Done chan struct{}

	once sync.Once
}

// NewNotification builds a Notification with both channels initialised.
// Callers should prefer this over zero-value construction.
func NewNotification() *Notification {
	return &Notification{
		ResultCh: make(chan Result, 1),
		Done:     make(chan struct{}),
	}
}

// Resolve writes r to ResultCh (non-blocking) and closes Done. Idempotent.
func (n *Notification) Resolve(r Result) {
	n.once.Do(func() {
		if n.Done != nil {
			close(n.Done)
		}
		if n.ResultCh != nil {
			select {
			case n.ResultCh <- r:
			default:
			}
		}
	})
}
