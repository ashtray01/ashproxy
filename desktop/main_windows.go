package main

import (
	"embed"
	"log"
	"os"
	"path/filepath"

	"github.com/ashtray01/ashproxy/desktop/engine"
	"github.com/ashtray01/ashproxy/desktop/settings"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed frontend/dist
var assets embed.FS

//go:embed assets/app.ico
var appIcon []byte

func main() {
	service := &settings.Startup{}
	exe, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	profile, err := os.UserCacheDir()
	if err != nil {
		log.Fatal(err)
	}
	manager := engine.New(filepath.Join(filepath.Dir(exe), "core", "3proxy.exe"), filepath.Join(profile, "AshProxy"))
	app := application.New(application.Options{
		Name:       "AshProxy",
		Icon:       appIcon,
		Services:   []application.Service{application.NewService(service), application.NewService(manager)},
		OnShutdown: manager.Close,
		Assets:     application.AssetOptions{Handler: application.BundledAssetFileServer(assets)},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "io.github.ashtray01.ashproxy",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if window, ok := application.Get().Window.GetByName("main"); ok {
					window.Show()
					window.Restore()
					window.Focus()
				}
			},
		},
	})
	service.Manager = app.Autostart
	config := manager.Configuration()
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "main", Title: "AshProxy", URL: "/", Width: 500, Height: 480,
		MinWidth: 400, MinHeight: 300,
		Hidden: config.StartHidden,
	})
	show := func() { window.Show(); window.Restore(); window.Focus() }
	tray := app.SystemTray.New()
	tray.SetIcon(appIcon)
	tray.SetTooltip("AshProxy")
	tray.OnClick(show)
	menu := app.NewMenu()
	menu.Add("Открыть AshProxy").OnClick(func(*application.Context) { show() })
	menu.Add("Свернуть в трей").OnClick(func(*application.Context) { window.Hide() })
	menu.Add("Выход").OnClick(func(*application.Context) { app.Quit() })
	tray.SetMenu(menu)
	if config.AutoStart {
		app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
			go func() {
				if err := manager.Start(); err != nil {
					show()
					app.Dialog.Error().SetMessage(err.Error()).Show()
				}
			}()
		})
	}
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
