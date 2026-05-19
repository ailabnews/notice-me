package tray

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// Handlers wires tray menu actions back into the rest of the app.
type Handlers struct {
	OnShow     func()
	OnFeedback func()
	OnAbout    func()
	OnQuit     func()
}

// Menu layout:
//
//	显示主窗口
//	───────────
//	问题反馈
//	关于我们
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

	menu.Add("问题反馈").OnClick(func(_ *application.Context) {
		if h.OnFeedback != nil {
			h.OnFeedback()
		}
	})

	menu.Add("关于我们").OnClick(func(_ *application.Context) {
		if h.OnAbout != nil {
			h.OnAbout()
		}
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
