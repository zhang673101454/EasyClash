package main

import (
	"log/slog"
	"sync"

	"github.com/energye/systray"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

var (
	trayToggleItem *systray.MenuItem
	trayToggleMu   sync.Mutex
)

func setTrayToggleItem(item *systray.MenuItem) {
	trayToggleMu.Lock()
	trayToggleItem = item
	trayToggleMu.Unlock()
}

func updateTrayProxyMenu(connected bool) {
	trayToggleMu.Lock()
	item := trayToggleItem
	trayToggleMu.Unlock()
	if item == nil {
		return
	}
	if connected {
		item.SetTitle("关闭代理")
		item.SetTooltip("代理已开启")
		item.Check()
	} else {
		item.SetTitle("开启代理")
		item.SetTooltip("代理已关闭")
		item.Uncheck()
	}
}

func (a *App) refreshTrayProxyMenu() {
	a.mu.Lock()
	running := a.manager != nil && a.manager.Running()
	a.mu.Unlock()
	updateTrayProxyMenu(running)
}

func (a *App) startTray() {
	go systray.Run(a.onTrayReady, func() {
		slog.Info("系统托盘已退出")
	})
}

func (a *App) onTrayReady() {
	systray.SetIcon(trayIcon)
	systray.SetTitle("EasyClash")
	systray.SetTooltip("EasyClash")

	showItem := systray.AddMenuItem("显示主窗口", "显示主窗口")
	toggleItem := systray.AddMenuItem("开启代理", "代理已关闭")
	setTrayToggleItem(toggleItem)
	a.refreshTrayProxyMenu()
	systray.AddSeparator()
	quitItem := systray.AddMenuItem("退出", "退出 EasyClash")

	showItem.Click(func() {
		a.showMainWindow()
	})
	toggleItem.Click(func() {
		go func() {
			status, err := a.ToggleProxy()
			if err != nil {
				slog.Error("托盘切换代理失败", "error", err)
				if a.ctx != nil {
					runtime.EventsEmit(a.ctx, "proxy:error", err.Error())
				}
				return
			}
			a.emitStatus(status)
		}()
	})
	quitItem.Click(func() {
		go a.requestQuit()
	})

	systray.SetOnClick(func(menu systray.IMenu) {
		a.showMainWindow()
	})
	systray.SetOnRClick(func(menu systray.IMenu) {
		a.refreshTrayProxyMenu()
		if err := menu.ShowMenu(); err != nil {
			slog.Warn("显示托盘菜单失败", "error", err)
		}
	})
	slog.Info("系统托盘已就绪")
}

func (a *App) showMainWindow() {
	if a.ctx == nil {
		return
	}
	if a.compact.Load() {
		if err := a.SetCompactMode(false); err != nil {
			slog.Warn("从托盘恢复主窗口尺寸失败", "error", err)
		}
		runtime.EventsEmit(a.ctx, "window:compact", false)
	}
	runtime.WindowShow(a.ctx)
	runtime.WindowUnminimise(a.ctx)
}

func (a *App) requestQuit() {
	a.quitting.Store(true)
	a.mu.Lock()
	if _, err := a.disableLocked(); err != nil {
		slog.Error("退出前关闭代理失败", "error", err)
	}
	a.mu.Unlock()
	systray.Quit()
	if a.ctx != nil {
		runtime.Quit(a.ctx)
	}
}
