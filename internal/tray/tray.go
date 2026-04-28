// Package tray builds and mounts the notify-me system tray icon and its menu.
//
// Wails v3 alpha.78 exposes the tray through app.SystemTray.New(), and menus
// through app.NewMenu() / Menu.Add() / Menu.AddCheckbox() / Menu.AddSeparator().
// MenuItem.OnClick takes a func(*application.Context). Tray.SetIcon /
// SetTooltip / SetMenu return *SystemTray for chaining.
package tray

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// Handlers wires tray menu actions back into the rest of the app. Paused must
// be safe to call from a UI thread (it is read once when building the menu and
// then again every time the user toggles the pause item, so it reflects the
// current dispatcher state).
type Handlers struct {
	OnShow   func()
	OnPause  func()
	OnResume func()
	OnQuit   func()
	Paused   func() bool
}

// Mount creates the system tray, sets its icon, tooltip and menu, and wires
// menu clicks to the handlers. The returned *SystemTray is owned by the Wails
// app — callers don't need to keep a reference unless they want to mutate it
// later. We return it so tests / future code can inspect or destroy it.
//
// Menu layout (Chinese):
//
//	显示主窗口
//	───────────
//	暂停接收 (checkbox; toggles dispatcher pause/resume)
//	───────────
//	退出
func Mount(app *application.App, iconPNG []byte, h Handlers) *application.SystemTray {
	tray := app.SystemTray.New()
	tray.SetIcon(iconPNG)
	tray.SetTooltip("notify-me")

	menu := app.NewMenu()

	menu.Add("显示主窗口").OnClick(func(_ *application.Context) {
		if h.OnShow != nil {
			h.OnShow()
		}
	})

	menu.AddSeparator()

	initiallyPaused := false
	if h.Paused != nil {
		initiallyPaused = h.Paused()
	}
	pauseItem := menu.AddCheckbox("暂停接收", initiallyPaused)
	pauseItem.OnClick(func(_ *application.Context) {
		// Use the live dispatcher state rather than the menu item's own
		// checked flag — the platform impl flips the checkbox before invoking
		// the click callback on some backends, and reading the source of
		// truth keeps us correct regardless.
		paused := false
		if h.Paused != nil {
			paused = h.Paused()
		}
		if paused {
			if h.OnResume != nil {
				h.OnResume()
			}
		} else {
			if h.OnPause != nil {
				h.OnPause()
			}
		}
		// Re-read and reflect the new state on the checkbox.
		newState := false
		if h.Paused != nil {
			newState = h.Paused()
		}
		pauseItem.SetChecked(newState)
	})

	menu.AddSeparator()

	menu.Add("退出").OnClick(func(_ *application.Context) {
		if h.OnQuit != nil {
			h.OnQuit()
		}
	})

	tray.SetMenu(menu)
	return tray
}
