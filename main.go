package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"Cursor_Windsurf_Reset/cleaner"
	"Cursor_Windsurf_Reset/config"
	"Cursor_Windsurf_Reset/gui"
	appi18n "Cursor_Windsurf_Reset/i18n"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// version will be set at build time using -ldflags
var version = "dev"

func main() {
	os.Setenv("FYNE_FONT", "")
	os.Setenv("FYNE_SCALE", "1.1")
	os.Setenv("FYNE_THEME", "dark")
	var (
		configPath = flag.String("config", "", "Configuration file path")
		discover   = flag.Bool("discover", false, "Discover and report application data locations")
		clean      = flag.String("clean", "", "Clean specific application (cursor/windsurf)")
		cleanAll   = flag.Bool("clean-all", false, "Clean all found applications")
		noConfirm  = flag.Bool("no-confirm", false, "Skip confirmation prompts")
		dryRun     = flag.Bool("dry-run", false, "Preview actions without making changes")
		verbose    = flag.Bool("verbose", false, "Show detailed output")
		cli        = flag.Bool("cli", false, "Use command line interface instead of GUI")
		version    = flag.Bool("version", false, "Show version information")
		testSQLite = flag.String("test-sqlite", "", "Test SQLite database connection (provide database path)")
	)
	flag.Parse()

	if *version {
		fmt.Printf("Cursor & Windsurf Data Cleaner %s (Go)\n", version)
		fmt.Println("Built with Go and Fyne GUI framework")
		return
	}

	logLevel := zerolog.InfoLevel
	if *verbose {
		logLevel = zerolog.DebugLevel
	}
	zerolog.SetGlobalLevel(logLevel)

	consoleWriter := zerolog.ConsoleWriter{
		Out:             os.Stdout,
		NoColor:         false,
		TimeFormat:      "",
		FormatTimestamp: func(i interface{}) string { return "" },
		FormatLevel: func(i interface{}) string {
			if l, ok := i.(string); ok {
				return fmt.Sprintf("[%s]", strings.ToUpper(l))
			}
			return "[INFO]"
		},
		FormatMessage: func(i interface{}) string {
			return fmt.Sprintf("%s", i)
		},
	}
	log.Logger = zerolog.New(consoleWriter).Level(logLevel).With().Logger()

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load configuration")
	}

	bundle, err := appi18n.Init("i18n")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize i18n")
	}

	localizer := appi18n.NewLocalizer(bundle, "en")

	engine := cleaner.NewEngine(cfg, *dryRun, *verbose, localizer)

	if *testSQLite != "" {
		fmt.Printf("Testing SQLite connection to: %s\n", *testSQLite)
		err := engine.TestSQLiteConnection(*testSQLite)
		if err != nil {
			fmt.Printf("❌ SQLite test failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✅ SQLite test successful")
		return
	}

	if *cli || *discover || *clean != "" || *cleanAll {
		runCLI(engine, cfg, discover, clean, cleanAll, noConfirm, dryRun)
		return
	}

	runGUI()
}

func runCLI(engine *cleaner.Engine, cfg *config.Config,
	discover *bool, clean *string, cleanAll *bool, noConfirm *bool, dryRun *bool) {

	fmt.Printf("🧹 Cursor & Windsurf Data Cleaner %s (Go)\n", version)
	fmt.Println(strings.Repeat("=", 55))
	fmt.Println("⚠️  IMPORTANT: This tool will modify application data.")
	fmt.Println("   Always backup your important work before proceeding.")
	fmt.Println("   Use this tool responsibly and in accordance with application ToS.")
	fmt.Println()

	if *discover {
		performDiscovery(engine, cfg)
		return
	}

	appDataPaths := engine.GetAppDataPaths()
	availableApps := make([]string, 0)
	for appName, appPath := range appDataPaths {
		if appPath != "" {
			availableApps = append(availableApps, appName)
		}
	}

	if len(availableApps) == 0 {
		fmt.Println("❌ No supported applications found.")
		os.Exit(1)
	}

	var appsToClean []string
	if *clean != "" {
		found := false
		for _, app := range availableApps {
			if app == *clean {
				appsToClean = []string{app}
				found = true
				break
			}
		}
		if !found {
			fmt.Printf("❌ Application '%s' not found or not supported.\n", *clean)
			os.Exit(1)
		}
	} else if *cleanAll {
		appsToClean = availableApps
	} else {
		performDiscovery(engine, cfg)
		fmt.Println("\nAvailable applications to clean:")
		for i, app := range availableApps {
			appConfig := cfg.Applications[app]
			displayName := appConfig.DisplayName
			fmt.Printf("  %d. %s\n", i+1, displayName)
		}
		fmt.Println("  0. Exit")

		fmt.Print("\nSelect application to clean (number): ")
		var choice int
		fmt.Scanf("%d", &choice)

		if choice == 0 {
			return
		}

		if choice > 0 && choice <= len(availableApps) {
			appsToClean = []string{availableApps[choice-1]}
		} else {
			fmt.Println("❌ Invalid choice.")
			os.Exit(1)
		}
	}

	if !*noConfirm {
		safetyOptions := cfg.SafetyOptions
		if safetyOptions.RequireConfirmation {
			fmt.Printf("\n⚠️  You are about to clean data for: %s\n", appsToClean[0])
			fmt.Println("This will:")
			fmt.Println("  • Reset machine/device IDs")
			fmt.Println("  • Clear account-specific database records")
			fmt.Println("  • Remove cached workspace data")
			fmt.Println("  • Create backups of all modified files")

			fmt.Print("\nAre you sure you want to proceed? (type 'yes' to confirm): ")
			var confirm string
			fmt.Scanf("%s", &confirm)
			if confirm != "yes" {
				fmt.Println("Operation cancelled.")
				return
			}
		}
	}

	overallSuccess := true
	for _, appName := range appsToClean {
		fmt.Printf("\n🧹 Starting cleanup for %s...\n", appName)

		if engine.IsAppRunning(appName) {
			fmt.Printf("❌ %s is currently running. Please close it first.\n", appName)
			overallSuccess = false
			continue
		}

		err := engine.CleanApplication(context.Background(), appName)
		if err != nil {
			fmt.Printf("❌ Failed to clean %s: %v\n", appName, err)
			overallSuccess = false
		} else {
			fmt.Printf("✅ Successfully cleaned %s\n", appName)
		}
	}

	fmt.Println("\n===== Cleaning Summary =====")
	if overallSuccess {
		fmt.Printf("✅ Successfully cleaned data for: %s\n", appsToClean[0])
		fmt.Printf("📁 Backups saved to: %s\n", engine.GetBackupDirectory())
		fmt.Println("\nYou can now launch the applications and log in with different accounts.")
	} else {
		fmt.Println("⚠️  Cleanup completed with some errors. Check the log for details.")
		fmt.Printf("📁 Backups saved to: %s\n", engine.GetBackupDirectory())
	}
}

func performDiscovery(engine *cleaner.Engine, cfg *config.Config) {
	fmt.Println("=== Application Data Discovery ===")

	appDataPaths := engine.GetAppDataPaths()
	for appName, appPath := range appDataPaths {
		appConfig := cfg.Applications[appName]
		displayName := appConfig.DisplayName

		if appPath != "" {
			fmt.Printf("%s: Found at %s\n", displayName, appPath)

			if engine.IsAppRunning(appName) {
				fmt.Printf("  %s is currently running\n", displayName)
			} else {
				fmt.Printf("  %s is not running\n", displayName)
			}

			size := engine.GetDirectorySize(appPath)
			fmt.Printf("  💾 Size: %s\n", engine.FormatSize(size))
		} else {
			fmt.Printf("%s: Not found\n", displayName)
		}
	}

	fmt.Printf("📁 Backup directory: %s\n", engine.GetBackupDirectory())
}

func runGUI() {
	app := gui.NewApp(version)
	app.Run()
}
