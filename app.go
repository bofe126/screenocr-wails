package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"screenocr-wails/internal/hotkey"
	"screenocr-wails/internal/ocr"
	"screenocr-wails/internal/overlay"
	"screenocr-wails/internal/screenshot"
	"screenocr-wails/internal/translator"
	"screenocr-wails/internal/tray"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Config 应用配置
type Config struct {
	TriggerDelayMs    int    `json:"trigger_delay_ms"`
	Hotkey            string `json:"hotkey"`
	AutoCopy          bool   `json:"auto_copy"`
	ShowDebug         bool   `json:"show_debug"`
	ImagePreprocess   bool   `json:"image_preprocess"`
	OcrEngine         string `json:"ocr_engine"`
	EnableTranslation bool   `json:"enable_translation"`
	TranslationSource string `json:"translation_source"`
	TranslationTarget string `json:"translation_target"`
	TencentSecretId   string `json:"tencent_secret_id"`
	TencentSecretKey  string `json:"tencent_secret_key"`
	FirstRun          bool   `json:"first_run"`
	ShowWelcome       bool   `json:"show_welcome"`
	ShowStartupNotify bool   `json:"show_startup_notification"`
}

// App 应用结构
type App struct {
	ctx        context.Context
	config     Config
	configPath string
	mu         sync.RWMutex
	enabled    bool
	quitting   bool // 标记是否正在退出程序

	// 组件
	ocrEngine   ocr.Engine
	screenshoot *screenshot.Capturer
	hotkeyMgr   *hotkey.Manager
	trayIcon    *tray.SystemTray
	translator  *translator.TencentTranslator
	overlay     *overlay.Overlay
	popup       *overlay.TranslationPopup
	welcome     *overlay.WelcomePage
}

// NewApp 创建新应用实例
func NewApp() *App {
	return &App{
		enabled: true,
		config:  defaultConfig(),
	}
}

// getConfigDir 获取配置文件目录
func getConfigDir() string {
	// 优先使用 APPDATA 环境变量
	appData := os.Getenv("APPDATA")
	if appData != "" {
		return filepath.Join(appData, "ScreenOCR")
	}
	// 回退到用户主目录
	home, err := os.UserHomeDir()
	if err == nil {
		return filepath.Join(home, ".screenocr")
	}
	// 最后回退到可执行文件目录
	exe, _ := os.Executable()
	return filepath.Dir(exe)
}

// defaultConfig 默认配置
func defaultConfig() Config {
	return Config{
		TriggerDelayMs:    300,
		Hotkey:            "alt",
		AutoCopy:          true,
		ShowDebug:         false,
		ImagePreprocess:   false,
		OcrEngine:         "windows",
		EnableTranslation: true,
		TranslationSource: "auto",
		TranslationTarget: "zh",
		TencentSecretId:   "",
		TencentSecretKey:  "",
		FirstRun:          true,
		ShowWelcome:       true,
		ShowStartupNotify: true,
	}
}

// startup 应用启动时调用
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// 获取配置文件路径（使用 %APPDATA%\ScreenOCR 目录）
	configDir := getConfigDir()
	os.MkdirAll(configDir, 0755) // 确保目录存在
	a.configPath = filepath.Join(configDir, "config.json")

	// 加载配置
	a.loadConfig()

	// 初始化 OCR 引擎
	a.initOCREngine()

	// 初始化截图器
	a.screenshoot = screenshot.NewCapturer()

	// 初始化翻译器
	a.translator = translator.NewTencentTranslator(
		a.config.TencentSecretId,
		a.config.TencentSecretKey,
	)

	// 初始化覆盖层
	a.overlay = overlay.NewOverlay()
	a.overlay.OnTextSelected = a.onTextSelected
	a.overlay.OnClose = a.onOverlayClose

	// 初始化翻译弹窗
	a.popup = overlay.NewTranslationPopup()

	// 初始化热键管理器
	a.hotkeyMgr = hotkey.NewManager(a.config.Hotkey, a.config.TriggerDelayMs)
	a.hotkeyMgr.OnTrigger = a.onHotkeyTriggered
	a.hotkeyMgr.OnKeyRelease = a.onHotkeyReleased // 按键松开时关闭覆盖层
	a.hotkeyMgr.OnEscape = a.onEscapePressed      // 全局 ESC 键关闭（与 Python 一致）

	// 初始化系统托盘
	a.initTray()

	// 启动热键监听
	go a.hotkeyMgr.Start()

	// 处理首次启动引导
	a.handleStartupGuide()

	fmt.Println("✓ ScreenOCR 启动完成")
}

// handleStartupGuide 处理启动引导
func (a *App) handleStartupGuide() {
	a.mu.RLock()
	showWelcome := a.config.ShowWelcome
	showNotify := a.config.ShowStartupNotify
	hotkey := a.config.Hotkey
	a.mu.RUnlock()

	if showWelcome {
		// 显示欢迎页面
		a.welcome = overlay.NewWelcomePage()
		a.welcome.OnClose = a.onWelcomeClose
		a.welcome.Show(hotkey)
	} else if showNotify {
		// 显示 Toast 通知
		toast := overlay.NewStartupToast()
		toast.Show(hotkey)
	}
}

// onWelcomeClose 欢迎页面关闭回调
func (a *App) onWelcomeClose(dontShowAgain bool, openSettings bool) {
	fmt.Printf("[Welcome] 关闭，不再显示=%v，打开设置=%v\n", dontShowAgain, openSettings)

	// 更新配置
	if dontShowAgain {
		a.mu.Lock()
		a.config.ShowWelcome = false
		a.mu.Unlock()
		a.saveConfig()
	}

	// 如果用户点击了"详细设置"，显示主窗口
	if openSettings {
		runtime.WindowShow(a.ctx)
	}
}

// shutdown 应用关闭时调用
func (a *App) shutdown(ctx context.Context) {
	// 停止热键监听
	if a.hotkeyMgr != nil {
		a.hotkeyMgr.Stop()
	}

	// 关闭覆盖层
	if a.overlay != nil {
		a.overlay.Close()
	}

	// 关闭弹窗
	if a.popup != nil {
		a.popup.Close()
	}

	// 关闭托盘图标
	if a.trayIcon != nil {
		a.trayIcon.Close()
	}

	// 保存配置
	a.saveConfig()

	fmt.Println("ScreenOCR 已关闭")
}

// domReady DOM 准备就绪时调用
func (a *App) domReady(ctx context.Context) {
	// 可以在这里执行需要 DOM 就绪后的操作
}

// beforeClose 窗口关闭前调用 - 隐藏到托盘而不是退出
func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	// 如果是从托盘点击"退出"，允许程序退出
	if a.quitting {
		return false
	}
	// 否则只是隐藏窗口到托盘
	runtime.WindowHide(ctx)
	fmt.Println("设置窗口已隐藏到托盘")
	return true
}

// initOCREngine 初始化 OCR 引擎
func (a *App) initOCREngine() {
	switch a.config.OcrEngine {
	case "wechat":
		a.ocrEngine = ocr.NewWeChatOCR()
	default:
		a.ocrEngine = ocr.NewWindowsOCR()
	}

	if a.ocrEngine != nil && a.ocrEngine.IsAvailable() {
		fmt.Printf("✓ OCR 引擎 (%s) 初始化成功\n", a.config.OcrEngine)
	} else {
		fmt.Printf("⚠ OCR 引擎 (%s) 不可用\n", a.config.OcrEngine)
	}
}

// initTray 初始化系统托盘
func (a *App) initTray() {
	a.trayIcon = tray.NewSystemTray()
	a.trayIcon.OnSettings = func() {
		runtime.WindowShow(a.ctx)
	}
	a.trayIcon.OnToggle = func(enabled bool) {
		a.mu.Lock()
		a.enabled = enabled
		a.mu.Unlock()
	}
	a.trayIcon.OnQuit = func() {
		a.quitting = true // 标记正在退出
		runtime.Quit(a.ctx)
	}
	go a.trayIcon.Run()
}

// onHotkeyTriggered 热键触发时的回调
func (a *App) onHotkeyTriggered() {
	a.mu.RLock()
	enabled := a.enabled
	a.mu.RUnlock()

	if !enabled {
		return
	}

	fmt.Println("热键触发，开始截图...")

	// 截图
	img, err := a.screenshoot.CaptureScreen()
	if err != nil {
		fmt.Println("截图失败:", err)
		return
	}

	// 显示等待状态的覆盖层
	if err := a.overlay.ShowWaiting(img); err != nil {
		fmt.Println("显示覆盖层失败:", err)
		return
	}

	// OCR 识别
	if a.ocrEngine == nil || !a.ocrEngine.IsAvailable() {
		fmt.Println("OCR 引擎不可用")
		a.overlay.Hide()
		return
	}

	// 异步执行 OCR
	go func() {
		fmt.Println("开始 OCR 识别...")
		results, err := a.ocrEngine.Recognize(img, a.config.ImagePreprocess)
		if err != nil {
			fmt.Println("OCR 识别失败:", err)
			a.overlay.Hide()
			return
		}

		fmt.Printf("识别到 %d 个文本块\n", len(results))

		// 更新覆盖层显示结果
		a.overlay.UpdateResults(results)
	}()
}

// onTextSelected 文字选中回调
func (a *App) onTextSelected(text string, x, y int) {
	fmt.Printf("选中文字: %s\n", text)

	// 获取配置（加锁读取）
	a.mu.RLock()
	enableTranslation := a.config.EnableTranslation
	translationSource := a.config.TranslationSource
	translationTarget := a.config.TranslationTarget
	a.mu.RUnlock()

	if !enableTranslation || a.translator == nil {
		return
	}

	// 显示翻译弹窗
	if a.popup != nil {
		a.popup.Show(text, x, y)

		// 异步翻译（使用本地变量副本，避免并发问题）
		go func() {
			result, err := a.translator.Translate(
				text,
				translationSource,
				translationTarget,
			)
			if err != nil {
				a.popup.ShowError(err.Error())
				return
			}
			a.popup.UpdateTranslation(result)
		}()
	}
}

// onOverlayClose 覆盖层关闭回调
func (a *App) onOverlayClose() {
	fmt.Println("覆盖层已关闭")
	if a.popup != nil {
		a.popup.Hide()
	}
}

// onHotkeyReleased 热键松开回调 - 关闭覆盖层和翻译窗口（与 Python cleanup_windows 一致）
func (a *App) onHotkeyReleased() {
	fmt.Println("热键松开，关闭覆盖层和翻译窗口")
	if a.overlay != nil {
		a.overlay.Hide()
	}
	// 与 Python 版本的 cleanup_windows 一致：同时关闭翻译窗口
	if a.popup != nil {
		a.popup.Hide()
	}
}

// onEscapePressed 全局 ESC 键回调（与 Python cleanup_windows 一致）
func (a *App) onEscapePressed() {
	fmt.Println("ESC 键按下，关闭覆盖层和翻译窗口")
	if a.overlay != nil {
		a.overlay.Hide()
	}
	if a.popup != nil {
		a.popup.Hide()
	}
}

// loadConfig 加载配置文件
func (a *App) loadConfig() {
	data, err := os.ReadFile(a.configPath)
	if err != nil {
		fmt.Println("配置文件不存在，使用默认配置")
		return
	}

	if err := json.Unmarshal(data, &a.config); err != nil {
		fmt.Println("解析配置文件失败:", err)
	}
}

// saveConfig 保存配置文件
func (a *App) saveConfig() error {
	a.mu.RLock()
	defer a.mu.RUnlock()

	data, err := json.MarshalIndent(a.config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(a.configPath, data, 0644)
}

// ======== 暴露给前端的方法 ========

// GetConfig 获取配置
func (a *App) GetConfig() Config {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.config
}

// SaveConfig 保存配置
func (a *App) SaveConfig(cfg Config) error {
	a.mu.Lock()
	oldEngine := a.config.OcrEngine
	a.config = cfg
	a.mu.Unlock()

	// 更新翻译器凭证
	if a.translator != nil {
		a.translator.SetCredentials(cfg.TencentSecretId, cfg.TencentSecretKey)
	}

	// 更新热键
	if a.hotkeyMgr != nil {
		a.hotkeyMgr.UpdateHotkey(cfg.Hotkey, cfg.TriggerDelayMs)
	}

	// 更新 OCR 引擎
	if oldEngine != cfg.OcrEngine {
		a.initOCREngine()
	}

	return a.saveConfig()
}

// Translate 翻译文本
func (a *App) Translate(text string) (string, error) {
	if a.translator == nil {
		return "", fmt.Errorf("翻译器未初始化")
	}

	return a.translator.Translate(
		text,
		a.config.TranslationSource,
		a.config.TranslationTarget,
	)
}

// IsEnabled 获取服务状态
func (a *App) IsEnabled() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.enabled
}

// SetEnabled 设置服务状态
func (a *App) SetEnabled(enabled bool) {
	a.mu.Lock()
	a.enabled = enabled
	a.mu.Unlock()

	if a.trayIcon != nil {
		a.trayIcon.SetEnabled(enabled)
	}
}

// ShowWindow 显示主窗口
func (a *App) ShowWindow() {
	runtime.WindowShow(a.ctx)
}

// HideWindow 隐藏主窗口
func (a *App) HideWindow() {
	runtime.WindowHide(a.ctx)
}

// showDuplicateToast 显示重复运行提示
func (a *App) showDuplicateToast() {
	fmt.Println("收到重复运行通知，显示 Toast")
	toast := overlay.NewStartupToast()
	toast.ShowMessage("ScreenOCR 已在运行中", "程序已在托盘运行，请勿重复启动")
}
