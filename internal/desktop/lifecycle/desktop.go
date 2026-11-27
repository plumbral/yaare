package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"yaare/internal/adapters/frontendassets"
	"yaare/internal/adapters/sessionlog"
	"yaare/internal/adapters/storage/sqlite"
	"yaare/internal/app"
	"yaare/internal/desktop/ipc"
)

func Run(application *app.Application) error {
	var window *mygo.Window
	startupPending := false
	validationRoot := validationDataDir(os.Args[1:])
	if filepath.IsAbs(validationRoot) {
		mygo.App.SetName(validationSessionName(validationRoot))
	}
	off := mygo.App.OnSecondInstance(func([]string, string) {
		if window == nil || startupPending || window.IsDestroyed() {
			return
		}
		if !window.IsVisible() {
			window.Show()
		}
		window.Restore()
		window.Focus()
	})
	defer off()
	if !mygo.App.RequestSingleInstanceLock() {
		return nil
	}
	mygo.Bind(ipc.NewApplicationService(application))
	root := func() (string, error) {
		if validationRoot != "" {
			return validationRoot, nil
		}
		path, err := os.UserConfigDir()
		return filepath.Join(path, "Yaare"), err
	}
	store := sqlite.New(root)
	defer store.Close()
	protector := ipc.DesktopCredentialProtector(store, root)
	workspace := app.NewWorkspace(store)
	workspaceService := ipc.NewWorkspaceService(workspace, protector)
	mygo.Bind(workspaceService)
	mygo.Bind(ipc.NewSSHConfigService(store))
	mygo.Bind(ipc.NewSessionLayoutService(store))
	mygo.Bind(ipc.NewPreferencesService(store))
	keys := ipc.NewPersistentKeyService(store)
	ipc.ConfigureWorkspaceKeys(workspaceService, keys)
	mygo.Bind(keys)
	identities := ipc.NewIdentityService(store, keys)
	mygo.Bind(identities)
	ssh, shutdownSSH := ipc.NewSSHService(keys, root)
	ipc.ConfigureCredentialProtection(keys, identities, ssh, protector)
	ipc.ConfigureSSHIdentities(ssh, identities)
	ipc.ConfigureSSHWorkspace(ssh, workspace)
	logs := sessionlog.New(root)
	ipc.ConfigureSessionLogs(ssh, logs)
	defer logs.Close()
	mygo.Bind(ssh)

	mygo.Bind(ipc.NewDesktopService(ssh))
	files, shutdownFiles := ipc.NewFileService(ssh)
	ipc.ConfigureTransferRecovery(files, root)
	mygo.Bind(files)

	cloud := ipc.NewCloudService(store, protector)
	mygo.Bind(ipc.NewBackupService(cloud))
	configurationSync := ipc.NewSyncService(cloud)
	mygo.Bind(configurationSync)
	stopSync := func() {}
	defer func() { stopSync() }()
	defer shutdownFiles()
	defer shutdownSSH()
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) { stopSync(); shutdownFiles(); shutdownSSH() })
	var frontendErr error
	mygo.App.WhenReady(func() {
		if !mygo.IsDev() {
			files, err := frontendassets.Load()
			if err != nil {
				frontendErr = fmt.Errorf("load desktop frontend: %w", err)
				mygo.App.Quit()
				return
			}
			mygo.SetFrontend(files)
		}
		_ = store.Initialize(context.Background())
		storedPreferences, preferencesErr := store.Preferences(context.Background())
		appearance := startupAppearance{Mode: "system"}
		if preferencesErr == nil {
			appearance = savedStartupAppearance(storedPreferences)
		}
		stopSync = ipc.StartConfigurationSync(configurationSync)
		ipc.InitializeTransferRecovery(files)
		var pageSession *mygo.Session
		if filepath.IsAbs(validationRoot) {
			pageSession = mygo.NewSession(validationSessionName(validationRoot))
		}
		pageOptions := experiencePageOptions(pageSession)
		pageOptions.PreloadScript = appearance.preload() + pageOptions.PreloadScript
		options := mygo.WindowOptions{
			Title: "Yaare", URL: "/",
			Width: 1280, Height: 820,
			MinWidth: 720, MinHeight: 480,
			TitleBarStyle:  mygo.TitleBarHidden,
			TitleBarHeight: 48,
			Page:           pageOptions,
		}
		if runtime.GOOS == "windows" {
			startupPending = true
			options.Hidden = true
			options.BackgroundColor = appearance.background()
		}
		window = mygo.NewWindow(options)
		window.Page().OnContextMenu(func(event *mygo.ContextMenuEvent) { event.PreventDefault() })
		window.SetWindowButtonsTheme(appearance.captionTheme())
		ipc.GuardTextWindow(window, files)
		ipc.InstallNativeDrop(window, files)
		installExperienceProbe(window)
		if startupPending {
			startupWindow := window
			show := func() {
				if startupPending && !startupWindow.IsDestroyed() {
					startupPending = false
					startupWindow.Show()
				}
			}
			fallback := time.AfterFunc(5*time.Second, func() { mygo.RunOnMain(show) })
			startupWindow.OnReadyToShow(func() {
				fallback.Stop()
				show()
			})
			startupWindow.OnClosed(func() {
				fallback.Stop()
				startupPending = false
			})
		}
	})
	if err := mygo.App.Run(); err != nil {
		return err
	}
	return frontendErr
}

func validationDataDir(args []string) string {
	for _, arg := range args {
		if dir, ok := strings.CutPrefix(arg, "--yaare-data-dir="); ok {
			return dir
		}
	}
	return ""
}

func validationSessionName(root string) string {
	path := filepath.Clean(root)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	sum := sha256.Sum256([]byte(path))
	return "yaare-validation-" + hex.EncodeToString(sum[:16])
}
