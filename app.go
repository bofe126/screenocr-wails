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

	// 组件
	ocrEngine   ocr.Engine
	screenshoot *screenshot.Capturer
	hotkeyMgr   *hotkey.Manager
	trayIcon    *tray.SystemTray
	translator  *translator.TencentTranslator
	overlay     *overlay.Overlay
	popup       *overlay.TranslationPopup
}

// NewApp 创建新应用实例
func NewApp() *App {
	return &App{
		enabled: true,
		config:  defaultConfig(),
	}
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

	// 获取配置文件路径
	exe, _ := os.Executable()
	a.configPath = filepath.Join(filepath.Dir(exe), "config.json")

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

	// 初始化系统托盘
	a.initTray()

	// 启动热键监听
	go a.hotkeyMgr.Start()

	fmt.Println("✓ ScreenOCR 启动完成")
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

	// 检查是否启用翻译
	a.mu.RLock()
	enableTranslation := a.config.EnableTranslation
	a.mu.RUnlock()

	if !enableTranslation || a.translator == nil {
		return
	}

	// 显示翻译弹窗
	if a.popup != nil {
		a.popup.Show(text, x, y)

		// 异步翻译
		go func() {
			result, err := a.translator.Translate(
				text,
				a.config.TranslationSource,
				a.config.TranslationTarget,
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
