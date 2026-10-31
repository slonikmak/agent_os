package main

import (
	"context"

	"errors"
	"log/slog"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"

	"github.com/wailsapp/wails/v3/pkg/application"
	"personaldesk/internal/adapters/diagnostics"
	"personaldesk/internal/adapters/wailsread"
	"personaldesk/internal/application/queries"
	"personaldesk/internal/bootstrap"
)

func init() { application.RegisterEvent[int64]("workspace.changed") }
func main() {
	if len(os.Args) > 1 {
		if os.Args[1] != "maintenance" {
			_, _ = os.Stderr.WriteString("Invalid arguments\n")
			os.Exit(1)
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		code := bootstrap.RunMaintenance(ctx, os.Getenv("PERSONAL_DESK_WORKSPACE"), os.Args[2:], os.Stdout)
		stop()
		os.Exit(code)
	}
	diag := diagnostics.New()
	defer diag.Close()
	workspace, err := bootstrap.StartSession(context.Background(), bootstrap.Config{Root: os.Getenv("PERSONAL_DESK_WORKSPACE"), Address: os.Getenv("PERSONAL_DESK_ADDRESS")}, diag)
	var reader queries.Reader
	startupCode := ""
	startupErrorID := ""
	if workspace != nil {
		defer workspace.Close()
	}
	if err == nil {
		reader = workspace
	} else {
		startupCode = "service_unavailable"
		var startup *bootstrap.StartupError
		if errors.As(err, &startup) {
			startupCode = startup.Code
			startupErrorID = startup.ErrorID
		}
	}
	facade := wailsread.NewStartup(reader, diag, startupCode, startupErrorID)
	var app *application.App
	maintenance := bootstrap.NewDiagnosticMaintenance(diag, os.Getenv("PERSONAL_DESK_WORKSPACE"), workspace, diagnosticSaveFixture(func() (string, error) {
		return app.Dialog.SaveFile().SetFilename("personal-desk-diagnostics.zip").SetMessage("Export private diagnostics to a new ZIP file").AddFilter("ZIP archive", "*.zip").AllowsOtherFileTypes(false).PromptForSingleSelection()
	}))
	defer maintenance.Close()
	diagnosticFacade := wailsread.NewDiagnostics(diag, maintenance.ExportDiagnostics)
	var openBrowser func(string) error
	navigation := wailsread.NewNavigationDiagnostics(func(value string) error { return openBrowser(value) }, diag)
	app = application.New(application.Options{
		Name: "Personal Desk", Description: "A private saved workspace",
		Logger: slog.New(diagnostics.Handler{Buffer: diag}),
		PanicHandler: func(_ *application.PanicDetails) {
			diag.Fault("framework", "framework", diagnostics.ID(), "panic")
			diag.RequireRestart()
			if workspace != nil {
				workspace.Halt()
			}
		},
		Services: []application.Service{application.NewService(facade), application.NewService(navigation), application.NewService(diagnosticFacade)},
		Assets:   application.AssetOptions{Handler: application.AssetFileServerFS(assets)},
		Mac:      application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
		OnShutdown: func() {
			maintenance.Close()
			if workspace != nil {
				_ = workspace.Close()
			}
		},
	})
	openBrowser = app.Browser.OpenURL
	menu := application.DefaultApplicationMenu()
	help := menu.FindByRole(application.HelpMenu)
	if help == nil {
		help = menu.FindByLabel("Help")
	}
	if help != nil {
		submenu := help.GetSubmenu()
		submenu.Clear()
		var nativeExportActive atomic.Bool
		submenu.Add("Export diagnostics…").OnClick(func(_ *application.Context) {
			if !nativeExportActive.CompareAndSwap(false, true) {
				return
			}
			defer nativeExportActive.Store(false)
			result := maintenance.ExportDiagnostics()
			if result.Status == "cancelled" {
				return
			}
			message := "Diagnostics exported. Inspect the archive before sharing it."
			if result.Status == "exported" && !result.Coverage.Complete {
				message = "Diagnostics exported with incomplete coverage. Inspect the archive before sharing it."
			}
			if result.Status == "failed" {
				message = "Diagnostics could not be exported. Choose a new ZIP file outside Personal Desk folders. Error code: " + *result.ErrorID
			}
			app.Dialog.Info().SetTitle("Export diagnostics").SetMessage(message).Show()
		})
	}
	app.Menu.Set(menu)
	runDiagnosticExportFixture(diag, maintenance.ExportDiagnostics)
	if workspace != nil {
		unsubscribe := workspace.SubscribeChanges(func(revision int64) { app.Event.Emit("workspace.changed", revision) })
		defer unsubscribe()
	}
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "Personal Desk", Width: 1280, Height: 720, MinWidth: 640, MinHeight: 600,
		Mac:              application.MacWindow{InvisibleTitleBarHeight: 36, Backdrop: application.MacBackdropNormal, TitleBar: application.MacTitleBarHiddenInset},
		BackgroundColour: application.NewRGB(252, 252, 250), URL: diagnosticFixtureURL(),
	})
	if err := app.Run(); err != nil {
		diag.Fault("bootstrap", "startup", diagnostics.ID(), "internal")
	}
}
