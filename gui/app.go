package gui

import (
	appi18n "Cursor_Windsurf_Reset/i18n"
	"context"
	"fmt"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/rs/zerolog"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"Cursor_Windsurf_Reset/cleaner"
	"Cursor_Windsurf_Reset/config"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"image/color"
)

type App struct {
	fyneApp    fyne.App
	mainWindow fyne.Window
	engine     *cleaner.Engine
	config     *config.Config
	logChan    chan string
	bundle     *i18n.Bundle
	localizer  *appi18n.LocalizerWrapper

	guiLogger zerolog.Logger

	appData            []AppInfo
	progressBar        *widget.ProgressBar
	statusLabel        *widget.Label
	logText            *widget.Entry
	logScrollContainer *container.Scroll
	cleanButton        *widget.Button
	discoverButton     *widget.Button
	configButton       *widget.Button
	aboutButton        *widget.Button
	helpButton         *widget.Button
	selectedIndex      int
	mainAreaContainer  fyne.CanvasObject

	selectedApps   map[int]bool
	selectAllCheck *widget.Check
}

type AppInfo struct {
	Name        string
	DisplayName string
	Path        string
	Size        string
	Running     bool
	Found       bool
}

func NewApp() *App {
	fyneApp := app.New()
	fyneApp.SetIcon(theme.ComputerIcon())

	fyneApp.Settings().SetTheme(NewModernDarkTheme())

	bundle, err := appi18n.Init("i18n")
	if err != nil {
		panic(err)
	}

	systemLang := appi18n.DetectSystemLanguage()
	localizer := appi18n.NewLocalizer(bundle, systemLang)

	logChan := make(chan string, 100)
	guiWriter := &config.GuiLogWriter{LogChan: logChan}

	consoleWriter := zerolog.ConsoleWriter{
		Out:             guiWriter,
		NoColor:         true,
		TimeFormat:      "",
		FormatTimestamp: func(i interface{}) string { return "" },
		FormatLevel: func(i interface{}) string {
			if l, ok := i.(string); ok {
				return strings.ToUpper(l) + ":"
			}
			return "INFO:"
		},
		FormatMessage: func(i interface{}) string {
			return fmt.Sprintf(" %s", i)
		},
	}
	guiLogger := zerolog.New(consoleWriter).With().Logger()

	// Load configuration
	cfg, err := config.LoadConfig("")
	if err != nil {
		guiLogger.Error().Err(err).Msg("Failed to load configuration")
		cfg = config.GetDefaultConfig()
	}

	engine := cleaner.NewEngine(cfg, false, false, localizer)

	app := &App{
		fyneApp:       fyneApp,
		engine:        engine,
		config:        cfg,
		logChan:       logChan,
		bundle:        bundle,
		localizer:     localizer,
		guiLogger:     guiLogger,
		selectedApps:  make(map[int]bool),
		selectedIndex: -1,
	}

	app.setupMainWindow()
	go app.listenForLogs()

	go func() {
		time.Sleep(200 * time.Millisecond)
		langName := "English"
		if systemLang == "zh" {
			langName = "中文"
		}
		app.logMessage("INFO", "LogMessage", map[string]interface{}{
			"Message": fmt.Sprintf("检测到系统语言: %s (%s)", langName, systemLang),
		})
	}()

	return app
}

func (app *App) createEnhancedLogWidget() *widget.Entry {
	logText := widget.NewMultiLineEntry()
	logText.Disable()
	logText.SetPlaceHolder(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "LogPlaceholder"}))

	logText.TextStyle = fyne.TextStyle{
		Monospace: true,
	}

	logText.Wrapping = fyne.TextWrapWord

	return logText
}

func (app *App) listenForLogs() {
	for logMsg := range app.logChan {
		// 在UI线程中执行文本更新和滚动操作
		func(msg string) {
			currentText := app.logText.Text
			if len(currentText) > 20000 {
				lines := strings.Split(currentText, "\n")
				if len(lines) > 400 {
					currentText = strings.Join(lines[len(lines)-300:], "\n")
				}
			}
			app.logText.SetText(currentText + msg)
			app.logText.CursorRow = len(strings.Split(app.logText.Text, "\n"))

			// 自动滚动到底部显示最新日志
			if app.logScrollContainer != nil {
				app.logScrollContainer.ScrollToBottom()
			}
		}(logMsg)
	}
}

func (app *App) setupMainWindow() {
	app.mainWindow = app.fyneApp.NewWindow(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "WindowTitle"}))
	app.mainWindow.Resize(fyne.NewSize(800, 600))
	app.mainWindow.CenterOnScreen()
	app.mainWindow.SetIcon(theme.ComputerIcon())
	app.mainWindow.SetMaster()
	app.mainWindow.SetFixedSize(false)

	app.mainWindow.SetContent(app.createContent())

	go func() {
		time.Sleep(100 * time.Millisecond)
		// Initial discovery
		app.performDiscovery()
	}()
}

func ModernButton(text string, icon fyne.Resource, onTapped func()) *widget.Button {
	button := widget.NewButtonWithIcon(text, icon, onTapped)

	button.Importance = widget.MediumImportance

	return button
}

func (app *App) createContent() fyne.CanvasObject {

	app.progressBar = widget.NewProgressBar()
	app.progressBar.Hide()

	app.progressBar.Resize(fyne.NewSize(200, 20))

	app.statusLabel = widget.NewLabel(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "Ready"}))
	app.statusLabel.Hide()

	app.discoverButton = ModernButton(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "DiscoverApps"}), theme.SearchIcon(), app.onDiscover)
	app.cleanButton = ModernButton(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ResetSelected"}), theme.DeleteIcon(), app.onClean)
	app.configButton = ModernButton(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "Settings"}), theme.SettingsIcon(), app.onConfig)

	app.discoverButton.Importance = widget.HighImportance
	app.cleanButton.Importance = widget.DangerImportance
	app.configButton.Importance = widget.MediumImportance

	app.cleanButton.Disable()

	app.helpButton = ModernButton("", theme.HelpIcon(), app.onHelp)
	app.aboutButton = ModernButton("", theme.InfoIcon(), app.onAbout)

	app.logText = app.createEnhancedLogWidget()

	// 初始化全选复选框
	app.selectAllCheck = widget.NewCheck(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "SelectAll"}), func(checked bool) {
		app.logMessage("INFO", "LogSelectAllChanged", map[string]interface{}{"Status": checked})

		// 保存修改前的状态，用于对比找出哪些项需要刷新
		oldSelectedState := make(map[int]bool)
		for id, selected := range app.selectedApps {
			oldSelectedState[id] = selected
		}

		// 更新选中状态
		app.selectedApps = make(map[int]bool)
		for i, appInfo := range app.appData {
			if appInfo.Found && !appInfo.Running {
				app.selectedApps[i] = checked
			}
		}

		// 查找到当前可见的列表
		listObj := app.findAppList()
		if listObj != nil {
			// 只刷新状态发生变化的项
			for i, appInfo := range app.appData {
				if appInfo.Found && !appInfo.Running {
					// 如果状态有变化或是新增状态
					if oldSelectedState[i] != app.selectedApps[i] || !oldSelectedState[i] {
						listObj.RefreshItem(i)
					}
				}
			}
		} else {
			// 如果找不到列表对象，则整体刷新
			app.refreshAppList()
		}

		app.updateCleanButton()
	})

	// 1. 创建头部
	appTitle := widget.NewLabelWithStyle(
		app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "WindowTitle"}),
		fyne.TextAlignCenter,
		fyne.TextStyle{Bold: true})

	header := container.NewVBox(
		container.NewPadded(
			container.NewHBox(
				widget.NewIcon(theme.ComputerIcon()),
				appTitle,
				layout.NewSpacer(),
				app.helpButton,
				app.aboutButton)),
		widget.NewSeparator())

	// 2. 创建应用列表区域
	listLabel := widget.NewLabelWithStyle(
		app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "AppList"}),
		fyne.TextAlignLeading,
		fyne.TextStyle{Bold: true})

	loadingLabel := widget.NewLabelWithStyle(
		app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "LoadingAppList"}),
		fyne.TextAlignCenter,
		fyne.TextStyle{Italic: true})

	appListContainer := container.NewBorder(
		listLabel, nil, nil, nil,
		container.NewPadded(
			container.NewVBox(
				container.NewHBox(
					layout.NewSpacer(),
					widget.NewIcon(theme.ViewRefreshIcon()),
					layout.NewSpacer()),
				container.NewHBox(
					layout.NewSpacer(),
					loadingLabel,
					layout.NewSpacer()))))

	// 3. 创建操作按钮区域
	actionLabel := widget.NewLabelWithStyle(
		app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "Actions"}),
		fyne.TextAlignLeading,
		fyne.TextStyle{Bold: true})

	actionButtons := container.NewGridWithColumns(3,
		app.discoverButton,
		app.cleanButton,
		app.configButton)

	actionButtonsCard := container.NewVBox(
		actionLabel,
		actionButtons)

	// 4. 创建状态区域
	progressLabel := widget.NewLabelWithStyle(
		app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "Progress"}),
		fyne.TextAlignLeading,
		fyne.TextStyle{Bold: true})

	progressContainer := container.NewPadded(app.progressBar)

	statusCard := container.NewBorder(
		progressLabel,
		nil, nil, nil,
		progressContainer)

	// 5. 组合控制区域
	controlsContainer := container.NewVBox(
		actionButtonsCard,
		widget.NewSeparator(),
		statusCard)

	controlsContainer.Resize(fyne.NewSize(0, 150))

	// 6. 创建日志区域
	logLabel := widget.NewLabelWithStyle(
		app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "Log"}),
		fyne.TextAlignLeading,
		fyne.TextStyle{Bold: true})

	clearLogButton := ModernButton(
		app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ClearLog"}),
		theme.ContentClearIcon(),
		func() {
			app.logText.SetText("")
			// 清除日志后滚动到顶部
			if app.logScrollContainer != nil {
				app.logScrollContainer.ScrollToTop()
			}
		})

	collapseLogButton := ModernButton("", theme.MoveDownIcon(), nil)

	app.logScrollContainer = container.NewScroll(app.logText)

	app.logScrollContainer.SetMinSize(fyne.NewSize(0, 100))

	logContentContainer := container.NewBorder(
		widget.NewSeparator(),
		nil, nil, nil,
		app.logScrollContainer)

	isLogCollapsed := false
	collapseLogButton.OnTapped = func() {
		isLogCollapsed = !isLogCollapsed

		if isLogCollapsed {
			collapseLogButton.SetIcon(theme.MoveUpIcon())
			logContentContainer.Hide()
		} else {
			collapseLogButton.SetIcon(theme.MoveDownIcon())
			logContentContainer.Show()
		}
	}

	logTitle := container.NewHBox(
		logLabel,
		layout.NewSpacer(),
		clearLogButton,
		collapseLogButton)

	logContainer := container.NewBorder(
		logTitle, nil, nil, nil,
		logContentContainer)

	// 7. 创建边框效果
	createBorderedContainer := func(content fyne.CanvasObject) *fyne.Container {
		border := canvas.NewRectangle(color.NRGBA{R: 50, G: 55, B: 65, A: 100})
		border.StrokeWidth = 1
		border.StrokeColor = color.NRGBA{R: 60, G: 70, B: 80, A: 150}

		return container.New(
			layout.NewMaxLayout(),
			border,
			content)
	}

	borderedControlsContainer := createBorderedContainer(controlsContainer)
	borderedLogContainer := createBorderedContainer(logContainer)

	controlsAndLogArea := container.NewVSplit(
		borderedControlsContainer, // 上部：控制区域
		borderedLogContainer)      // 下部：日志区域

	// 设置控制区域和日志区域的分割比例
	controlsAndLogArea.Offset = 0.25 // 控制区域占25%，日志区域占75%

	// 第二层分割：应用列表 vs (控制区域+日志区域)
	mainArea := container.NewVSplit(
		appListContainer,   // 上部：应用列表（可调节）
		controlsAndLogArea) // 下部：控制区域+日志区域（可调节）

	mainArea.Offset = 0.3 // 应用列表占30%，其他区域占70%

	app.mainAreaContainer = mainArea
	app.logMessage("INFO", "LogMainAreaCreated", map[string]interface{}{
		"AppListRatio":  mainArea.Offset,
		"ControlsRatio": controlsAndLogArea.Offset,
	})

	// 9. 组合所有区域 - 现在所有区域都在可调节的分割容器中
	mainContent := container.NewBorder(
		header,
		nil,
		nil, nil,
		mainArea)

	return container.NewPadded(mainContent)
}

// performDiscovery performs application discovery
func (app *App) performDiscovery() {
	app.logMessage("INFO", "LogDiscoveryStarted", nil)
	if app.statusLabel != nil {
		app.statusLabel.SetText(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "StatusDiscoveringApps"}))
	}

	// 获取和显示所有应用数据路径
	appDataPaths := app.engine.GetAppDataPaths()

	// 打印原始路径数据
	app.logMessage("INFO", "LogOriginalAppPaths", map[string]interface{}{
		"Paths": fmt.Sprintf("%v", appDataPaths),
	})

	// 重置应用数据列表
	app.appData = make([]AppInfo, 0)

	// 调试日志
	app.logMessage("INFO", "LogDiscoveredAppCount", map[string]interface{}{
		"Count": len(appDataPaths),
	})

	// 详细输出所有应用
	for name, path := range appDataPaths {
		app.logMessage("INFO", "LogDiscoveredApp", map[string]interface{}{
			"Name": name,
			"Path": path,
		})
	}

	// 按顺序排列应用，确保顺序一致
	appNames := make([]string, 0, len(appDataPaths))
	for appName := range appDataPaths {
		appNames = append(appNames, appName)
	}
	// 按应用名称排序，保证顺序一致
	sort.Strings(appNames)

	// 按排序后的顺序处理应用
	for _, appName := range appNames {
		appPath := appDataPaths[appName]
		appConfig := app.config.Applications[appName]

		app.logMessage("INFO", "LogProcessingApp", map[string]interface{}{
			"Name":        appName,
			"DisplayName": appConfig.DisplayName,
		})

		appInfo := AppInfo{
			Name:        appName,
			DisplayName: appConfig.DisplayName,
			Path:        appPath,
			Found:       appPath != "",
		}

		if appInfo.Found {
			// 检查应用是否正在运行
			appInfo.Running = app.engine.IsAppRunning(appName)

			// 获取目录大小
			size := app.engine.GetDirectorySize(appPath)
			appInfo.Size = app.engine.FormatSize(size)

			app.logMessage("INFO", "LogFoundAppDetails", map[string]interface{}{
				"DisplayName": appInfo.DisplayName,
				"Path":        appPath,
				"Size":        appInfo.Size,
				"Running":     appInfo.Running,
			})
		} else {
			appInfo.Size = "未知"
			app.logMessage("INFO", "LogAppNotFound", map[string]interface{}{
				"DisplayName": appInfo.DisplayName,
			})
		}

		app.appData = append(app.appData, appInfo)
		app.logMessage("INFO", "LogAppAddedToList", map[string]interface{}{
			"DisplayName": appInfo.DisplayName,
			"Index":       len(app.appData) - 1,
		})
	}

	// 调试日志
	app.logMessage("INFO", "LogTotalAppCount", map[string]interface{}{
		"Count": len(app.appData),
	})

	for i, appInfo := range app.appData {
		app.logMessage("INFO", "LogFinalAppListItem", map[string]interface{}{
			"Index":       i,
			"DisplayName": appInfo.DisplayName,
			"Path":        appInfo.Path,
		})
	}

	// 清空选中状态
	app.selectedApps = make(map[int]bool)

	// 安全地设置全选复选框状态
	if app.selectAllCheck != nil {
		app.selectAllCheck.SetChecked(false)
	}

	// 重新创建并刷新应用列表
	app.refreshAppList()

	// 确保在主UI线程上执行刷新 - 添加空指针检查
	if app.mainWindow != nil && app.mainWindow.Content() != nil {
		if canvas := fyne.CurrentApp().Driver().CanvasForObject(app.mainWindow.Content()); canvas != nil {
			canvas.Refresh(app.mainWindow.Content())
		} else {
			// 如果无法获取Canvas，直接刷新窗口内容
			app.mainWindow.Content().Refresh()
		}
	}

	if app.statusLabel != nil {
		app.statusLabel.SetText(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "StatusDiscoveryComplete"}))
	}
	app.logMessage("INFO", "LogDiscoveryComplete", nil)

	// 计算有效的应用数量（已找到且未运行的应用）
	validAppCount := 0
	for _, appInfo := range app.appData {
		if appInfo.Found && !appInfo.Running {
			validAppCount++
		}
	}

	// 在日志中额外添加摘要信息
	app.logMessage("INFO", "LogDiscoverySummary", map[string]interface{}{
		"Total": len(app.appData),
		"Valid": validAppCount,
	})

	// 更新重置按钮状态
	app.updateCleanButton()
}

// onDiscover handles the discover button click
func (app *App) onDiscover() {
	app.logMessage("INFO", "LogUserStartedDiscovery", nil)

	// 禁用扫描按钮，防止重复点击
	if app.discoverButton != nil {
		app.discoverButton.Disable()
		app.discoverButton.SetText(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "Scanning"}))
	}

	// 显示加载状态
	if app.statusLabel != nil {
		app.statusLabel.Show()
		app.statusLabel.SetText(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ScanningApplications"}))
	}
	if app.progressBar != nil {
		app.progressBar.Show()
		app.progressBar.SetValue(0.5) // 中间值，表示处理中
	}

	// 在后台线程中执行扫描
	go func() {
		// 执行发现流程
		app.performDiscovery()

		// 恢复UI状态
		if app.discoverButton != nil {
			app.discoverButton.Enable()
			app.discoverButton.SetText(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "DiscoverApps"}))
		}

		if app.progressBar != nil {
			app.progressBar.Hide()
		}
		if app.statusLabel != nil {
			app.statusLabel.SetText(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "Ready"}))
		}
	}()
}

// updateCleanButton 更新重置按钮状态
func (app *App) updateCleanButton() {
	// 检查是否有选中的应用
	hasSelected := false

	// 调试日志 - 输出所有应用信息
	app.logMessage("INFO", "LogUpdateResetButtonStatus", nil)
	for i, appInfo := range app.appData {
		isSelected := app.selectedApps[i]
		app.logMessage("INFO", "LogAppStatusForButtonUpdate", map[string]interface{}{
			"Index":    i,
			"Name":     appInfo.DisplayName,
			"Found":    appInfo.Found,
			"Running":  appInfo.Running,
			"Selected": isSelected,
		})
	}

	for _, selected := range app.selectedApps {
		if selected {
			hasSelected = true
			break
		}
	}

	// 根据是否有选中的应用启用或禁用重置按钮
	if hasSelected {
		app.cleanButton.Enable()
		// 计算选中的数量
		count := 0
		for _, selected := range app.selectedApps {
			if selected {
				count++
			}
		}
		app.cleanButton.SetText(fmt.Sprintf("%s (%d)", app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ResetSelected"}), count))
		app.logMessage("INFO", "LogResetButtonEnabled", map[string]interface{}{"Count": count})
	} else {
		app.cleanButton.Disable()
		app.cleanButton.SetText(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ResetSelected"}))
		app.logMessage("INFO", "LogResetButtonDisabled", nil)
	}
}

// onClean handles the clean button click
func (app *App) onClean() {
	// 获取所有选中的有效应用
	selectedApps := make([]AppInfo, 0)
	for id, selected := range app.selectedApps {
		if selected && id < len(app.appData) {
			appInfo := app.appData[id]
			if appInfo.Found && !appInfo.Running {
				selectedApps = append(selectedApps, appInfo)
			}
		}
	}

	// 如果没有选中应用，直接返回
	if len(selectedApps) == 0 {
		dialog.ShowInformation(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "InfoTitle"}), app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "SelectAppToReset"}), app.mainWindow)
		return
	}

	// 检查是否有应用正在运行
	for _, appInfo := range selectedApps {
		if appInfo.Running {
			dialog.ShowError(fmt.Errorf(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "CloseAppToReset", TemplateData: map[string]interface{}{"AppName": appInfo.DisplayName}})), app.mainWindow)
			return
		}
	}

	// 创建确认内容
	confirmContent := container.NewVBox(
		widget.NewLabelWithStyle(
			fmt.Sprintf(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ConfirmResetCount"}), len(selectedApps)),
			fyne.TextAlignCenter,
			fyne.TextStyle{Bold: true},
		),
		widget.NewSeparator(),
	)

	// 添加选中的应用名称
	for _, appInfo := range selectedApps {
		confirmContent.Add(widget.NewLabel("• " + appInfo.DisplayName))
	}

	// 添加操作说明
	confirmContent.Add(widget.NewSeparator())
	confirmContent.Add(widget.NewLabel(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ConfirmResetDescription"})))
	confirmContent.Add(widget.NewLabel(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ConfirmResetDeviceID"})))
	confirmContent.Add(widget.NewLabel(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ConfirmResetAccountRecords"})))
	confirmContent.Add(widget.NewLabel(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ConfirmResetCacheData"})))
	confirmContent.Add(widget.NewLabel(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ConfirmResetBackup"})))
	confirmContent.Add(widget.NewSeparator())
	confirmContent.Add(widget.NewLabelWithStyle(
		app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ConfirmBackupLocation"}),
		fyne.TextAlignCenter,
		fyne.TextStyle{Italic: true},
	))

	// 显示确认对话框
	customConfirm := dialog.NewCustomConfirm(
		app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ConfirmResetTitle"}),
		app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ConfirmExecute"}),
		app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "Cancel"}),
		confirmContent,
		func(confirm bool) {
			if confirm {
				// 逐个重置选中的应用
				for _, appInfo := range selectedApps {
					app.performCleanup(appInfo)
				}
			}
		},
		app.mainWindow,
	)

	customConfirm.Show()
}

// performCleanup performs the actual cleanup operation
func (app *App) performCleanup(appInfo AppInfo) {
	app.logMessage("INFO", "LogStartResetting", map[string]interface{}{
		"AppName": appInfo.DisplayName,
	})

	if app.statusLabel != nil {
		app.statusLabel.SetText(app.localizer.MustLocalize(&i18n.LocalizeConfig{
			MessageID: "StatusResetting",
			TemplateData: map[string]interface{}{
				"AppName": appInfo.DisplayName,
			},
		}))
	}
	app.progressBar.Show()
	app.progressBar.SetValue(0)

	// Update engine settings
	app.engine = cleaner.NewEngine(app.config, false, false, app.localizer)

	// Start progress monitoring
	go app.monitorProgress()

	// Perform cleanup in background
	go func() {
		err := app.engine.CleanApplication(context.Background(), appInfo.Name)
		if err != nil {
			app.logMessage("ERROR", "ResetFailed", map[string]interface{}{
				"AppName": appInfo.DisplayName,
				"Error":   err,
			})
		} else {
			app.logMessage("INFO", "ResetComplete", map[string]interface{}{
				"AppName": appInfo.DisplayName,
			})
			// 项目主页和免责声明现在在进度达到100%后通过monitorProgress显示
		}
	}()
}

// monitorProgress monitors cleanup progress
func (app *App) monitorProgress() {
	progressChan := app.engine.GetProgressChannel()
	var completedApps []string // 记录已完成的应用

	for update := range progressChan {
		if app.progressBar != nil {
			app.progressBar.SetValue(update.Progress / 100.0)
		}

		// 状态消息可能已经是国际化的，直接使用
		if app.statusLabel != nil {
			app.statusLabel.SetText(update.Message)
		}

		app.logMessage("INFO", "LogResetProgress", map[string]interface{}{
			"Phase":   update.Phase,
			"Message": update.Message,
			"Percent": int(update.Progress), // 转换为整数，去掉小数点
		})

		// 检查是否达到100%进度
		if update.Progress >= 100.0 && update.AppName != "" {
			// 检查是否已经处理过这个应用
			alreadyProcessed := false
			for _, completedApp := range completedApps {
				if completedApp == update.AppName {
					alreadyProcessed = true
					break
				}
			}

			// 如果没有处理过，则显示项目主页和免责声明
			if !alreadyProcessed {
				completedApps = append(completedApps, update.AppName)
				// 在单独的goroutine中执行，避免阻塞进度监控
				go app.showProjectInfoAfterCompletion()
			}
		}
	}
}

// onConfig handles the config button click
func (app *App) onConfig() {
	// 创建配置对话框
	configForm := &widget.Form{}

	// 备份设置
	backupEnabledCheck := widget.NewCheck("启用备份功能", nil)
	backupEnabledCheck.SetChecked(app.config.BackupOptions.Enabled)

	backupKeepDays := widget.NewEntry()
	backupKeepDays.SetText(fmt.Sprintf("%d", app.config.BackupOptions.RetentionDays))

	// 安全设置
	confirmCheck := widget.NewCheck("操作需要确认", nil)
	confirmCheck.SetChecked(app.config.SafetyOptions.RequireConfirmation)

	// 添加到表单
	configForm.Append(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "EnableBackup"}), backupEnabledCheck)
	configForm.Append(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "RetentionDays"}), backupKeepDays)
	configForm.Append(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "RequireConfirmation"}), confirmCheck)

	// Language selection
	langSelector := widget.NewSelect([]string{"en", "zh"}, func(s string) {
		app.localizer = appi18n.NewLocalizer(app.bundle, s)
		app.recreateUI()
	})
	langSelector.Selected = app.localizer.Locale
	configForm.Append(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "Language"}), langSelector)

	// 创建对话框
	dialog.ShowCustomConfirm(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "AppSettings"}), app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "Save"}), app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "Cancel"}), configForm, func(save bool) {
		if save {
			// 更新配置
			app.config.BackupOptions.Enabled = backupEnabledCheck.Checked
			days, err := strconv.Atoi(backupKeepDays.Text)
			if err == nil && days > 0 {
				app.config.BackupOptions.RetentionDays = days
			}
			app.config.SafetyOptions.RequireConfirmation = confirmCheck.Checked

			err = config.SaveConfig(app.config, "")
			if err != nil {
				dialog.ShowError(fmt.Errorf(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "SaveConfigFailed"}), err), app.mainWindow)
				app.logMessage("ERROR", "SaveConfigFailedLog", map[string]interface{}{
					"Error": err,
				})
			} else {
				app.logMessage("INFO", "ConfigSaved", nil)
			}
		}
	}, app.mainWindow)
}

// onHelp handles the help button click
func (app *App) onHelp() {
	helpContent := container.NewVBox(
		widget.NewLabelWithStyle(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "HelpTitle"}), fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabel(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "HelpStep1"})),
		widget.NewLabel(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "HelpStep2"})),
		widget.NewLabel(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "HelpStep3"})),
		widget.NewLabel(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "HelpStep4"})),
		widget.NewSeparator(),
		widget.NewLabelWithStyle(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ResetContent"}), fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabel(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ResetDeviceID"})),
		widget.NewLabel(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ResetAccountRecords"})),
		widget.NewLabel(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ResetCacheData"})),
		widget.NewLabel(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ResetNote"})),
	)

	dialog.ShowCustom(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "HelpInfo"}), app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "Close"}), helpContent, app.mainWindow)
}

// onAbout handles the about button click
func (app *App) onAbout() {
	// Create project homepage hyperlink
	projectURL, _ := url.Parse("https://github.com/whispin/Cursor_Windsurf_Reset")
	projectLink := widget.NewHyperlink(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ProjectHomepage"}), projectURL)
	projectLink.Alignment = fyne.TextAlignCenter

	aboutContent := container.NewVBox(
		widget.NewLabelWithStyle(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "AboutTitle"}), fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabel(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "Version"})),
		widget.NewLabel(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "DevelopedBy"})),
		projectLink,
		widget.NewSeparator(),
		widget.NewLabel(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "AboutDescription"})),
		widget.NewLabel(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "AboutIncludes"})),
		widget.NewSeparator(),
		widget.NewLabelWithStyle(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "AboutNote"}), fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabel(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "AboutBackupNote"})),
		widget.NewSeparator(),
		widget.NewLabelWithStyle(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "AboutDisclaimer"}), fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabel(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "Disclaimer3"})),
	)

	dialog.ShowCustom(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "AboutTitle"}), app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "Close"}), aboutContent, app.mainWindow)
}

// log logs a message with a specific level
func (app *App) log(level, message string) {
	logLevel := parseLevel(level)
	app.guiLogger.WithLevel(logLevel).Msg(message)
}

// logMessage 使用国际化键和模板数据记录消息
func (app *App) logMessage(level string, messageID string, templateData map[string]interface{}) {
	logLevel := parseLevel(level)

	// 使用国际化配置获取本地化消息
	message := app.localizer.MustLocalize(&i18n.LocalizeConfig{
		MessageID:    messageID,
		TemplateData: templateData,
	})

	// 记录本地化后的消息
	app.guiLogger.WithLevel(logLevel).Msg(message)
}

func (app *App) showProjectInfoAfterCompletion() {
	time.Sleep(500 * time.Millisecond)

	app.logMessage("INFO", "LogMessage", map[string]interface{}{
		"Message": config.GetConfOne(),
	})
	app.logMessage("INFO", "LogMessage", map[string]interface{}{
		"Message": config.GetConfTwo(),
	})
}

// parseLevel parses a string level to a zerolog.Level
func parseLevel(level string) zerolog.Level {
	switch strings.ToUpper(level) {
	case "DEBUG":
		return zerolog.DebugLevel
	case "INFO":
		return zerolog.InfoLevel
	case "WARN":
		return zerolog.WarnLevel
	case "ERROR":
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}

// Run starts the GUI application
func (app *App) Run() {
	app.mainWindow.ShowAndRun()
}

// GetMainWindow returns the main window of the application
func (app *App) GetMainWindow() fyne.Window {
	return app.mainWindow
}

// createAppListArea creates the container for the application list
func (app *App) createAppListArea() *fyne.Container {
	// 动态计算有效应用数量
	validAppCount := 0
	for _, appInfo := range app.appData {
		if appInfo.Found {
			validAppCount++
		}
	}
	statusText := fmt.Sprintf(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "AppFound"}), validAppCount)
	if validAppCount == 0 {
		statusText = app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "NoAppsFound"})
	}

	// 创建应用列表
	list := widget.NewList(
		func() int {
			return len(app.appData)
		},
		func() fyne.CanvasObject {
			// 创建模板项，使用垂直布局展示两行信息

			// 第一行：选择框、应用名称和大小
			nameLabel := widget.NewLabel("AppName")
			nameLabel.Alignment = fyne.TextAlignLeading
			nameLabel.TextStyle = fyne.TextStyle{Bold: true}

			sizeLabel := widget.NewLabel("Size")
			sizeLabel.Alignment = fyne.TextAlignTrailing

			statusIcon := widget.NewIcon(theme.ConfirmIcon())
			selectCheck := widget.NewCheck("", nil)

			// 将选择框移至左侧
			topRow := container.NewHBox(
				selectCheck, // 选择框位于最左侧
				nameLabel,
				layout.NewSpacer(),
				sizeLabel,
				statusIcon,
			)

			// 第二行：路径显示
			pathLabel := widget.NewLabel("Path")
			pathLabel.Alignment = fyne.TextAlignLeading
			pathLabel.TextStyle = fyne.TextStyle{Italic: true, Monospace: true}

			// 创建浅色文本的自定义文本，使文字变浅
			pathText := canvas.NewText("Path", color.NRGBA{R: 140, G: 140, B: 150, A: 160}) // 更浅的灰色，更透明
			pathText.TextStyle = fyne.TextStyle{Italic: true, Monospace: true}
			pathText.TextSize = 11 // 更小的字体大小

			// 创建半透明的文件夹图标
			pathIcon := widget.NewIcon(theme.FolderIcon())
			pathIcon.Resource = theme.FolderOpenIcon() // 使用打开的文件夹图标

			// 组合路径图标和标签为一行
			pathRow := container.NewHBox(
				pathIcon,
				container.NewPadded(pathText),
			)

			// 组合两行为一个垂直布局
			return container.NewVBox(
				topRow,
				pathRow,
			)
		},
		func(id widget.ListItemID, item fyne.CanvasObject) {
			if id >= len(app.appData) {
				return // 安全检查
			}

			appInfo := app.appData[id]

			// 转换为VBox容器
			vbox, ok := item.(*fyne.Container)
			if !ok {
				app.logMessage("ERROR", "LogItemTypeError", nil)
				return
			}

			// 确保VBox有足够的子元素
			if len(vbox.Objects) < 2 {
				app.logMessage("ERROR", "LogVBoxChildrenError", nil)
				return
			}

			// 获取顶部行(HBox)
			topRow, ok := vbox.Objects[0].(*fyne.Container)
			if !ok {
				app.logMessage("ERROR", "LogTopRowTypeError", nil)
				return
			}

			// 获取路径标签
			pathRow, ok := vbox.Objects[1].(*fyne.Container)
			if !ok {
				app.logMessage("ERROR", "LogPathRowTypeError", nil)
				return
			}

			// 确保路径行有足够的子元素
			if len(pathRow.Objects) < 2 {
				app.logMessage("ERROR", "LogPathRowChildrenError", nil)
				return
			}

			// 获取路径图标
			pathIcon, ok := pathRow.Objects[0].(*widget.Icon)
			if !ok {
				app.logMessage("ERROR", "LogPathIconTypeError", nil)
				return
			}

			// 如果应用未找到，使用灰色文件夹图标
			if !appInfo.Found {
				pathIcon.SetResource(theme.FolderIcon())
			} else {
				// 使用默认的打开文件夹图标，区分状态
				if appInfo.Running {
					// 运行中的应用使用不同图标
					pathIcon.SetResource(theme.FolderOpenIcon())
				} else {
					// 正常可用的应用使用标准图标
					pathIcon.SetResource(theme.FolderIcon())
				}
			}

			// 获取路径行中的路径标签（位于内部Container中）
			pathContainer, ok := pathRow.Objects[1].(*fyne.Container)
			if !ok {
				app.logMessage("ERROR", "LogPathContainerTypeError", nil)
				return
			}

			// 获取实际的路径文本
			if len(pathContainer.Objects) < 1 {
				app.logMessage("ERROR", "LogPathContainerEmptyError", nil)
				return
			}

			pathText, ok := pathContainer.Objects[0].(*canvas.Text)
			if !ok {
				app.logMessage("ERROR", "LogPathTextTypeError", nil)
				return
			}

			// 确保顶部行有足够的子元素
			if len(topRow.Objects) < 5 {
				app.logMessage("ERROR", "LogTopRowChildrenError", nil)
				return
			}

			// 获取UI元素 - 注意索引已变更
			selectCheck, ok := topRow.Objects[0].(*widget.Check)
			if !ok {
				app.logMessage("ERROR", "LogCheckboxTypeError", nil)
				return
			}

			nameLabel, ok := topRow.Objects[1].(*widget.Label)
			if !ok {
				app.logMessage("ERROR", "LogNameLabelTypeError", nil)
				return
			}

			sizeLabel, ok := topRow.Objects[3].(*widget.Label)
			if !ok {
				app.logMessage("ERROR", "LogSizeLabelTypeError", nil)
				return
			}

			statusIcon, ok := topRow.Objects[4].(*widget.Icon)
			if !ok {
				app.logMessage("ERROR", "LogStatusIconTypeError", nil)
				return
			}

			// 设置应用名称
			nameLabel.SetText(appInfo.DisplayName)

			// 设置大小
			sizeLabel.SetText(appInfo.Size)

			// 设置路径 - 使用自定义文本对象
			pathText.Text = appInfo.Path

			// 根据应用状态添加"可清理"或"不可清理"状态信息
			var statusMsg string
			if !appInfo.Found {
				// 未找到的应用
				statusMsg = app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "NotFoundStatus"})
			} else if appInfo.Running {
				// 运行中的应用，显示"不可清理"
				statusMsg = app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "NotCleanableStatus"})
			} else {
				// 未运行的应用，显示"可清理"
				statusMsg = app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "CleanableStatus"})
			}
			// 在路径后添加状态信息（括号包围）
			pathText.Text = fmt.Sprintf("%s   (%s)", appInfo.Path, statusMsg)

			// 设置路径图标的透明度
			// Fyne没有直接设置图标透明度的API，这里可以通过颜色设置来实现
			// 在此处只能使用替代方案，例如使用不同的图标

			// 根据应用状态设置图标
			if appInfo.Running {
				statusIcon.SetResource(theme.CancelIcon())
				selectCheck.Disable()
			} else if !appInfo.Found {
				statusIcon.SetResource(theme.QuestionIcon())
				selectCheck.Disable()
			} else {
				statusIcon.SetResource(theme.ConfirmIcon())
				selectCheck.Enable()
			}

			// 设置复选框状态和回调
			selectCheck.SetChecked(app.selectedApps[id])
			selectCheck.OnChanged = func(checked bool) {
				app.selectedApps[id] = checked
				app.updateCleanButton()
			}
		},
	)

	// 修改OnSelected回调，点击条目时勾选复选框
	list.OnSelected = func(id widget.ListItemID) {
		app.selectedIndex = id
		app.logMessage("INFO", "LogSelectedItem", map[string]interface{}{"ID": id})

		// 只处理可用的应用
		if id < len(app.appData) {
			appInfo := app.appData[id]
			if appInfo.Found && !appInfo.Running {
				// 切换选中状态
				isSelected := app.selectedApps[id]
				app.selectedApps[id] = !isSelected

				// 只刷新被点击的这一项，而不是整个列表
				list.RefreshItem(id)

				// 更新重置按钮状态
				app.updateCleanButton()

				app.logMessage("INFO", "LogItemSelectionToggled", map[string]interface{}{
					"Name":   appInfo.DisplayName,
					"Status": !isSelected,
				})
			}
		}

		// 取消选中，避免高亮显示
		list.UnselectAll()
	}

	listScroll := container.NewScroll(list)

	// 列表标题
	listHeader := container.NewHBox(
		widget.NewLabelWithStyle(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "AppList"}), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		layout.NewSpacer(),
		widget.NewLabelWithStyle(statusText, fyne.TextAlignTrailing, fyne.TextStyle{Italic: true}),
		app.selectAllCheck,
	)

	// 最终的应用列表容器，使用Border布局
	return container.NewBorder(listHeader, nil, nil, nil, listScroll)
}

// refreshAppList refreshes the application list area
func (app *App) refreshAppList() {
	startTime := time.Now()
	app.logMessage("INFO", "LogAppListRefreshStarted", nil)
	app.logMessage("INFO", "LogAppListData", map[string]interface{}{"Count": len(app.appData)})

	// 打印每个应用的详细信息
	for i, appInfo := range app.appData {
		app.logMessage("INFO", "LogAppDetails", map[string]interface{}{
			"Index":    i,
			"Name":     appInfo.DisplayName,
			"Path":     appInfo.Path,
			"Running":  appInfo.Running,
			"Found":    appInfo.Found,
			"Selected": app.selectedApps[i],
		})
	}

	// 重新创建应用列表区域
	newAppListArea := app.createAppListArea()

	// 设置主区域的实际宽高
	if app.mainAreaContainer != nil {
		app.logMessage("INFO", "LogMainAreaSize", map[string]interface{}{
			"Size": app.mainAreaContainer.Size(),
		})
	}

	// 检查主区域是否是VSplit布局
	if app.mainAreaContainer != nil {
		if vSplit, ok := app.mainAreaContainer.(*container.Split); ok {
			app.logMessage("INFO", "LogFoundVSplit", nil)

			// 获取当前分割比例
			currentOffset := vSplit.Offset

			// 只更新上半部分（应用列表）
			vSplit.Leading = newAppListArea

			// 保持原有分割比例
			vSplit.Offset = currentOffset
			app.logMessage("INFO", "LogMaintainingSplitRatio", map[string]interface{}{
				"Ratio": currentOffset,
			})

			// 刷新UI
			vSplit.Refresh()
			app.logMessage("INFO", "LogVSplitRefreshed", nil)
		} else {
			app.logMessage("ERROR", "LogMainAreaNotVSplit", nil)
		}
	}

	elapsedTime := time.Since(startTime)
	app.logMessage("INFO", "LogAppListRefreshComplete", map[string]interface{}{
		"Duration": elapsedTime,
	})
}

func (app *App) recreateUI() {
	app.mainWindow.SetTitle(app.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "WindowTitle"}))
	app.mainWindow.SetContent(app.createContent())
	// Re-run discovery to populate the list with the correct language
	go func() {
		time.Sleep(100 * time.Millisecond)
		app.performDiscovery()
	}()
}

// findAppList 尝试查找并返回当前应用列表控件
func (app *App) findAppList() *widget.List {
	// 如果主区域容器不存在，直接返回nil
	if app.mainAreaContainer == nil {
		return nil
	}

	mainSplit, ok := app.mainAreaContainer.(*container.Split)
	if !ok {
		app.logMessage("ERROR", "LogMainAreaNotVSplit", nil)
		return nil
	}

	appListContainer := mainSplit.Leading
	if appListContainer == nil {
		app.logMessage("ERROR", "LogSplitLeadingEmpty", nil)
		return nil
	}

	border, ok := appListContainer.(*fyne.Container)
	if !ok {
		app.logMessage("ERROR", "LogAppListAreaNotContainer", nil)
		return nil
	}

	if len(border.Objects) < 1 {
		app.logMessage("ERROR", "LogBorderContainerEmpty", nil)
		return nil
	}

	var content fyne.CanvasObject
	// 查找非Label的组件
	for _, obj := range border.Objects {
		if _, isLabel := obj.(*widget.Label); !isLabel {
			content = obj
			break
		}
	}

	if content == nil {
		app.logMessage("ERROR", "LogBorderContentNotFound", nil)
		return nil
	}

	scroll, ok := content.(*container.Scroll)
	if !ok {
		nestedContainer, isContainer := content.(*fyne.Container)
		if !isContainer || len(nestedContainer.Objects) == 0 {
			app.logMessage("ERROR", "LogContentNotScrollOrContainer", nil)
			return nil
		}

		scroll, ok = nestedContainer.Objects[0].(*container.Scroll)
		if !ok {
			app.logMessage("ERROR", "LogAppListNotScroll", nil)
			return nil
		}
	}

	list, ok := scroll.Content.(*widget.List)
	if !ok {
		app.logMessage("ERROR", "LogScrollContentNotList", nil)
		return nil
	}

	return list
}
