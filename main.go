package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"whale-desktop/internal/platform"
)

//go:embed assets frontend
var embedded embed.FS

func main() {
	assetFS, err := fs.Sub(embedded, "assets")
	if err != nil {
		log.Fatal(err)
	}
	frontendFS, err := fs.Sub(embedded, "frontend")
	if err != nil {
		log.Fatal(err)
	}
	secondPet := hasArg("--second-pet")
	if !secondPet && !platform.AcquireSingleInstance("H1kaRU.WhaleDesktop.Go") {
		platform.ShowWindowByClass(windowClassName)
		return
	}
	app := NewApp(assetFS, secondPet)

	appOptions := &options.App{
		Title:             "WhaleDesktop",
		Width:             560,
		Height:            520,
		DisableResize:     true,
		Frameless:         true,
		HideWindowOnClose: true,
		AlwaysOnTop:       app.configSnapshot().AlwaysOnTop,
		BackgroundColour:  options.NewRGBA(0, 0, 0, 0),
		AssetServer: &assetserver.Options{
			Assets: frontendFS,
			Middleware: func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
					response.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
					response.Header().Set("Pragma", "no-cache")
					next.ServeHTTP(response, request)
				})
			},
		},
		OnStartup:       app.startup,
		OnShutdown:      app.shutdown,
		OnBeforeClose:   app.beforeClose,
		Bind:            []any{app},
		CSSDragProperty: "--whale-native-drag",
		CSSDragValue:    "drag",
		Windows: &windows.Options{
			WebviewIsTransparent:              true,
			WindowIsTranslucent:               true,
			DisableFramelessWindowDecorations: true,
			WindowClassName:                   windowClassName,
			WebviewUserDataPath:               filepath.Join(app.paths.Root, "webview2"),
			Theme:                             windows.SystemDefault,
		},
	}
	if err := wails.Run(appOptions); err != nil {
		app.paths.Log("wails run failed:", err)
		log.Fatal(err)
	}
}
