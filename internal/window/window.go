// Package window owns the lifecycle of the persistent main window and the
// per-notification popup window. Wails v3 alpha.78 exposes window creation
// through app.Window.NewWithOptions and emits/listens on event types from the
// github.com/wailsapp/wails/v3/pkg/events package.
package window

import (
	"fmt"
	"net/url"
	"sync"
	"unicode/utf8"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"notify-me/internal/config"
)

type Manager struct {
	app *application.App
	cfg *config.Config

	mu       sync.Mutex
	main     *application.WebviewWindow
	popup    *application.WebviewWindow
	diff     map[int64]*application.WebviewWindow
	feedback *application.WebviewWindow
	about    *application.WebviewWindow
}

func NewManager(app *application.App, cfg *config.Config) *Manager {
	return &Manager{app: app, cfg: cfg, diff: make(map[int64]*application.WebviewWindow)}
}

// MountMain creates the persistent main window. We start hidden — the window
// becomes visible on tray "Show" or the first ShowMain() call. Closing only
// hides the window when the user has opted into "minimize to tray on close";
// otherwise we let the framework destroy it (and trigger app shutdown via the
// last-window-closed semantics).
func (m *Manager) MountMain() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.main != nil {
		return
	}
	m.main = m.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "notify-me",
		Width:            860,
		Height:           480,
		URL:              "/index.html",
		BackgroundColour: application.NewRGB(255, 255, 255),
		Hidden:           false,
	})
	// RegisterHook runs synchronously and supports event.Cancel() to suppress
	// the framework's default close-and-destroy behaviour. OnWindowEvent fires
	// in a goroutine — too late to abort the close.
	m.main.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if m.cfg.Snapshot().Behavior.MinimizeToTrayOnClose {
			e.Cancel()
			// Hide the window; the app keeps running in the tray.
			if m.main != nil {
				m.main.Hide()
			}
		}
	})
}

// ShowMain reveals (and focuses) the main window, mounting it lazily if needed.
func (m *Manager) ShowMain() {
	m.MountMain()
	m.mu.Lock()
	w := m.main
	m.mu.Unlock()
	if w == nil {
		return
	}
	w.Show()
	w.Focus()
}

// OpenPopup creates a fresh always-on-top popup window for one notification.
// Notification data and server address are passed via URL query params so the
// popup frontend can render immediately without needing the Wails JS runtime.
func (m *Manager) OpenPopup(payload map[string]any) {
	m.mu.Lock()
	if m.popup != nil {
		old := m.popup
		m.popup = nil
		m.mu.Unlock()
		old.Close()
		m.mu.Lock()
	}
	snap := m.cfg.Snapshot()
	w := snap.UI.PopupSize.Width
	h := snap.UI.PopupSize.Height
	if w <= 0 {
		w = 480
	}
	if h <= 0 {
		h = 220
	}
	if msg, _ := payload["message"].(string); msg != "" {
		runes := utf8.RuneCountInString(msg)

		// Widen popup for code blocks or long lines that would otherwise
		// be clipped at the default 480 px width.
		if containsCodeBlock(msg) || longestLine(msg) > 60 {
			w = max(w, 640)
		}

		// Grow popup height for long messages.
		if runes > 80 {
			extra := min(runes/30*16, 400) // ~16px per line, cap +400px
			h = max(h, 280)
			h += extra
			if h > 800 {
				h = 800
			}
		}
	}

	// Build popup URL with notification data as query params.
	q := url.Values{}
	for k, v := range payload {
		q.Set(k, fmt.Sprintf("%v", v))
	}
	q.Set("port", fmt.Sprintf("%d", snap.Server.Port))
	q.Set("prefix", snap.Server.EndpointPrefix)

	// Session auth button: show for PreToolUse and PermissionRequest events only.
	hookEvent, _ := payload["hook_event"].(string)
	if hookEvent == "PreToolUse" || hookEvent == "PermissionRequest" {
		q.Set("has_session_auth", "true")
	}

	popupURL := "/popup.html?" + q.Encode()

	winTitle, _ := payload["title"].(string)
	if winTitle == "" {
		winTitle = "notify-me"
	}

	popup := m.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:                 "popup",
		Title:                winTitle,
		Width:                w,
		Height:               h,
		URL:                  popupURL,
		AlwaysOnTop:          true,
		DisableResize:        true,
		Hidden:               false,
		InitialPosition:      application.WindowCentered,
		MinimiseButtonState:  application.ButtonHidden,
		MaximiseButtonState:  application.ButtonHidden,
		Mac: application.MacWindow{
			WindowLevel: application.MacWindowLevelFloating,
			CollectionBehavior: application.MacWindowCollectionBehaviorCanJoinAllSpaces |
				application.MacWindowCollectionBehaviorFullScreenAuxiliary,
		},
		Windows: application.WindowsWindow{
			HiddenOnTaskbar: true,
		},
	})
	m.popup = popup
	m.mu.Unlock()
	popup.Focus()
}

// ClosePopup tears down the active popup window, if any.
func (m *Manager) ClosePopup() {
	m.mu.Lock()
	p := m.popup
	m.popup = nil
	m.mu.Unlock()
	if p != nil {
		p.Close()
	}
}

// containsCodeBlock reports whether s includes a fenced code block (```).
func containsCodeBlock(s string) bool {
	for i := 0; i+2 < len(s); i++ {
		if s[i] == '`' && s[i+1] == '`' && s[i+2] == '`' {
			return true
		}
	}
	return false
}

// longestLine returns the rune count of the longest single line in s.
func longestLine(s string) int {
	max := 0
	cur := 0
	for _, r := range s {
		if r == '\n' {
			if cur > max {
				max = cur
			}
			cur = 0
		} else {
			cur++
		}
	}
	if cur > max {
		max = cur
	}
	return max
}

// OpenDiffWindow creates a resizable always-on-top window for viewing a diff.
// The notification ID is passed as a query param so the frontend can fetch the
// diff data from the server.
func (m *Manager) OpenDiffWindow(id int64) {
	m.mu.Lock()
	if m.diff[id] != nil {
		m.mu.Unlock()
		return // already open
	}
	snap := m.cfg.Snapshot()
	q := url.Values{}
	q.Set("id", fmt.Sprintf("%d", id))
	q.Set("port", fmt.Sprintf("%d", snap.Server.Port))
	q.Set("prefix", snap.Server.EndpointPrefix)
	diffURL := "/diff.html?" + q.Encode()

	win := m.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:                 fmt.Sprintf("diff-%d", id),
		Title:                "Diff 查看器",
		Width:                900,
		Height:               600,
		URL:                  diffURL,
		AlwaysOnTop:          true,
		DisableResize:        false,
		Hidden:               false,
		InitialPosition:      application.WindowCentered,
		MinimiseButtonState:  application.ButtonHidden,
		MaximiseButtonState:  application.ButtonHidden,
		Mac: application.MacWindow{
			WindowLevel: application.MacWindowLevelFloating,
			CollectionBehavior: application.MacWindowCollectionBehaviorCanJoinAllSpaces |
				application.MacWindowCollectionBehaviorFullScreenAuxiliary,
		},
		Windows: application.WindowsWindow{
			HiddenOnTaskbar: true,
		},
	})
	// Clean up the map entry when the diff window is closed by the user,
	// so it can be opened again later.
	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		m.mu.Lock()
		delete(m.diff, id)
		m.mu.Unlock()
	})
	m.diff[id] = win
	m.mu.Unlock()
}

// CloseDiffWindow closes the diff window for the given notification, if any.
func (m *Manager) CloseDiffWindow(id int64) {
	m.mu.Lock()
	w := m.diff[id]
	delete(m.diff, id)
	m.mu.Unlock()
	if w != nil {
		w.Close()
	}
}

// CloseAllDiff closes all open diff windows. Called during shutdown.
func (m *Manager) CloseAllDiff() {
	m.mu.Lock()
	windows := make([]*application.WebviewWindow, 0, len(m.diff))
	for _, w := range m.diff {
		windows = append(windows, w)
	}
	m.diff = make(map[int64]*application.WebviewWindow)
	m.mu.Unlock()
	for _, w := range windows {
		w.Close()
	}
}

// OpenFeedbackWindow creates an always-on-top window for user feedback.
func (m *Manager) OpenFeedbackWindow() {
	m.mu.Lock()
	if m.feedback != nil {
		w := m.feedback
		m.mu.Unlock()
		w.Focus()
		return
	}
	win := m.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:                 "feedback",
		Title:                "问题反馈",
		Width:                480,
		Height:               460,
		URL:                  "/feedback.html",
		AlwaysOnTop:          true,
		DisableResize:        false,
		Hidden:               false,
		InitialPosition:      application.WindowCentered,
		MinimiseButtonState:  application.ButtonHidden,
		MaximiseButtonState:  application.ButtonHidden,
		BackgroundColour:     application.NewRGB(255, 255, 255),
		Mac: application.MacWindow{
			WindowLevel: application.MacWindowLevelFloating,
			CollectionBehavior: application.MacWindowCollectionBehaviorCanJoinAllSpaces |
				application.MacWindowCollectionBehaviorFullScreenAuxiliary,
		},
		Windows: application.WindowsWindow{
			HiddenOnTaskbar: true,
		},
	})
	// Clean up reference when the window is closed.
	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		m.mu.Lock()
		m.feedback = nil
		m.mu.Unlock()
	})
	m.feedback = win
	m.mu.Unlock()
	win.Focus()
}

// CloseFeedbackWindow closes the feedback window if open.
func (m *Manager) CloseFeedbackWindow() {
	m.mu.Lock()
	w := m.feedback
	m.feedback = nil
	m.mu.Unlock()
	if w != nil {
		w.Close()
	}
}

// OpenAboutWindow creates a centered window showing the about page.
func (m *Manager) OpenAboutWindow() {
	m.mu.Lock()
	if m.about != nil {
		w := m.about
		m.mu.Unlock()
		w.Focus()
		return
	}
	win := m.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:                 "about",
		Title:                "关于我们",
		Width:                400,
		Height:               520,
		URL:                  "/about.html",
		AlwaysOnTop:          true,
		DisableResize:        true,
		Hidden:               false,
		InitialPosition:      application.WindowCentered,
		MinimiseButtonState:  application.ButtonHidden,
		MaximiseButtonState:  application.ButtonHidden,
		BackgroundColour:     application.NewRGB(255, 255, 255),
		Mac: application.MacWindow{
			WindowLevel: application.MacWindowLevelFloating,
			CollectionBehavior: application.MacWindowCollectionBehaviorCanJoinAllSpaces |
				application.MacWindowCollectionBehaviorFullScreenAuxiliary,
		},
		Windows: application.WindowsWindow{
			HiddenOnTaskbar: true,
		},
	})
	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		m.mu.Lock()
		m.about = nil
		m.mu.Unlock()
	})
	m.about = win
	m.mu.Unlock()
	win.Focus()
}

// CloseAboutWindow closes the about window if open.
func (m *Manager) CloseAboutWindow() {
	m.mu.Lock()
	w := m.about
	m.about = nil
	m.mu.Unlock()
	if w != nil {
		w.Close()
	}
}
