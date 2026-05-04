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

	mu    sync.Mutex
	main  *application.WebviewWindow
	popup *application.WebviewWindow
}

func NewManager(app *application.App, cfg *config.Config) *Manager {
	return &Manager{app: app, cfg: cfg}
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
		Width:            720,
		Height:           480,
		URL:              "/index.html",
		BackgroundColour: application.NewRGB(255, 255, 255),
		Hidden:           true,
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
	// Grow popup height for long messages (e.g. Claude hook tool details).
	// Use rune count so Chinese text sizing is accurate.
	if msg, _ := payload["message"].(string); utf8.RuneCountInString(msg) > 80 {
		chars := utf8.RuneCountInString(msg)
		extra := min(chars/30*16, 400) // ~16px per line, cap +400px
		h = max(h, 280)
		h += extra
		if h > 800 {
			h = 800
		}
	}

	// Build popup URL with notification data as query params.
	q := url.Values{}
	for k, v := range payload {
		q.Set(k, fmt.Sprintf("%v", v))
	}
	q.Set("port", fmt.Sprintf("%d", snap.Server.Port))
	q.Set("prefix", snap.Server.EndpointPrefix)
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
