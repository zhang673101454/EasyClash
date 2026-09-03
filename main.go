package main

import (
	"embed"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"easy-clash/backend"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	writers := []io.Writer{os.Stdout}
	if dir, err := backend.DefaultConfigDir(); err == nil {
		logPath := filepath.Join(dir, "easy-clash.log")
		if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			writers = append(writers, f)
		}
	}
	logger := slog.New(slog.NewTextHandler(io.MultiWriter(writers...), &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)
	if dir, err := backend.DefaultConfigDir(); err == nil {
		slog.Info("日志文件", "path", filepath.Join(dir, "easy-clash.log"))
	}

	app := NewApp()

	err := wails.Run(&options.App{
		Title:            "EasyClash",
		Width:            300,
		Height:           580,
		MinWidth:         300,
		MinHeight:        580,
		DisableResize:    false,
		Frameless:        true,
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 0},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		HideWindowOnClose: true,
		OnStartup:         app.startup,
		OnShutdown:        app.shutdown,
		OnBeforeClose:     app.beforeClose,
		Bind: []interface{}{
			app,
		},
		// 只允许一个实例：再启动时唤醒已有窗口
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "com.easyclash.app.single-instance",
			OnSecondInstanceLaunch: func(_ options.SecondInstanceData) {
				slog.Info("检测到已有实例，唤醒主窗口")
				app.showMainWindow()
			},
		},
		Windows: &windows.Options{
			Theme:                             windows.Dark,
			WebviewIsTransparent:              true,
			WindowIsTranslucent:               true,
			BackdropType:                      windows.None,
			DisableFramelessWindowDecorations: true,
		},
		Mac: &mac.Options{
			TitleBar:   mac.TitleBarHiddenInset(),
			Appearance: mac.NSAppearanceNameDarkAqua,
		},
		Linux: &linux.Options{
			WindowIsTranslucent: false,
		},
	})
	if err != nil {
		slog.Error("应用启动失败", "error", err)
		os.Exit(1)
	}
}
