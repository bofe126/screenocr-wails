//go:build windows

package overlay

import (
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	procCreateWindowExW2 = user32.NewProc("CreateWindowExW")
	procSetWindowTextW   = user32.NewProc("SetWindowTextW")
	procMoveWindow       = user32.NewProc("MoveWindow")
	procSetTimer         = user32.NewProc("SetTimer")
	procKillTimer        = user32.NewProc("KillTimer")
	procDrawTextW        = user32.NewProc("DrawTextW")
	// procSetTextColor 和 procGetStockObject 已在 overlay.go 中声明
)

const (
	WS_EX_DLGMODALFRAME = 0x00000001
	WS_OVERLAPPED       = 0x00000000
	WS_CAPTION          = 0x00C00000
	WS_SYSMENU          = 0x00080000
	WM_TIMER            = 0x0113
	WM_ACTIVATE         = 0x0006

	DT_LEFT          = 0x00000000
	DT_WORDBREAK     = 0x00000010
	DT_NOPREFIX      = 0x00000800
	DEFAULT_GUI_FONT = 17
)

// TranslationPopup 翻译弹窗
type TranslationPopup struct {
	hwnd      uintptr
	hInstance uintptr
	className *uint16
	running   bool

	sourceText string
	targetText string
	isLoading  bool

	OnCopy  func(text string)
	OnClose func()

	// 通道
	showChan   chan popupShowRequest
	hideChan   chan struct{}
	updateChan chan string
	closeChan  chan struct{}
}

type popupShowRequest struct {
	sourceText string
	x, y       int
}

// 全局弹窗实例
var globalPopup *TranslationPopup

// NewTranslationPopup 创建翻译弹窗
func NewTranslationPopup() *TranslationPopup {
	p := &TranslationPopup{
		isLoading:  true,
		showChan:   make(chan popupShowRequest),
		hideChan:   make(chan struct{}, 1), // 带缓冲，避免阻塞
		updateChan: make(chan string, 1),
		closeChan:  make(chan struct{}),
	}

	// 启动专用窗口线程
	go p.windowThread()

	return p
}

// Show 显示弹窗
func (p *TranslationPopup) Show(sourceText string, x, y int) error {
	p.showChan <- popupShowRequest{sourceText: sourceText, x: x, y: y}
	return nil
}

// UpdateTranslation 更新翻译结果
func (p *TranslationPopup) UpdateTranslation(text string) {
	select {
	case p.updateChan <- text:
	default:
	}
}

// ShowError 显示错误
func (p *TranslationPopup) ShowError(errMsg string) {
	select {
	case p.updateChan <- "错误: " + errMsg:
	default:
	}
}

// Hide 隐藏弹窗
func (p *TranslationPopup) Hide() {
	select {
	case p.hideChan <- struct{}{}:
	default:
	}
}

// Close 关闭弹窗
func (p *TranslationPopup) Close() {
	select {
	case p.closeChan <- struct{}{}:
	default:
	}
}

// windowThread 专用窗口线程
func (p *TranslationPopup) windowThread() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	p.running = true

	for p.running {
		// 处理 Windows 消息
		var msg MSG
		ret, _, _ := procPeekMessageW.Call(
			uintptr(unsafe.Pointer(&msg)),
			0, 0, 0, PM_REMOVE,
		)
		if ret != 0 {
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
		}

		// 检查通道（不自动隐藏，只在用户交互时关闭）
		select {
		case req := <-p.showChan:
			p.handleShow(req)
		case text := <-p.updateChan:
			p.handleUpdate(text)
		case <-p.hideChan:
			p.handleHide()
		case <-p.closeChan:
			p.handleClose()
			return
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func (p *TranslationPopup) handleShow(req popupShowRequest) {
	globalPopup = p
	p.sourceText = req.sourceText
	p.targetText = ""
	p.isLoading = true

	if p.hwnd == 0 {
		p.createWindow(req.x, req.y)
	} else {
		procMoveWindow.Call(p.hwnd, uintptr(req.x+20), uintptr(req.y+20), 420, 220, 1)
		procShowWindow.Call(p.hwnd, SW_SHOW)
		procInvalidateRect.Call(p.hwnd, 0, 1)
	}

	// 获取焦点（与 Python focus_force() 一致）
	// 这样点击窗口外部时 WM_ACTIVATE 才会触发
	if p.hwnd != 0 {
		procSetForegroundWindow.Call(p.hwnd)
		procSetFocus.Call(p.hwnd)
	}
	fmt.Println("[Popup] 显示")
}

func (p *TranslationPopup) handleUpdate(text string) {
	p.targetText = text
	p.isLoading = false
	if p.hwnd != 0 {
		procInvalidateRect.Call(p.hwnd, 0, 1)
	}
	fmt.Printf("[Popup] 更新翻译: %s\n", text)
}

func (p *TranslationPopup) handleHide() {
	if p.hwnd != 0 {
		procShowWindow.Call(p.hwnd, SW_HIDE)
	}
	fmt.Println("[Popup] 隐藏")
}

func (p *TranslationPopup) handleClose() {
	p.running = false
	if p.hwnd != 0 {
		procDestroyWindow.Call(p.hwnd)
		p.hwnd = 0
	}
}

// createWindow 创建弹窗
func (p *TranslationPopup) createWindow(x, y int) error {
	p.hInstance, _, _ = procGetModuleHandleW.Call(0)

	// 注册窗口类
	className, _ := syscall.UTF16PtrFromString("ScreenOCRPopup")
	p.className = className

	// 加载箭头光标
	hCursor, _, _ := procLoadCursorW.Call(0, IDC_ARROW)

	wc := WNDCLASSEXW{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		LpfnWndProc:   syscall.NewCallback(popupWndProc),
		HInstance:     p.hInstance,
		HCursor:       hCursor, // 设置默认光标为箭头
		HbrBackground: 0,
		LpszClassName: className,
	}

	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	// 获取屏幕尺寸以确保不超出边界
	screenWidth, _, _ := procGetSystemMetrics.Call(SM_CXSCREEN)
	screenHeight, _, _ := procGetSystemMetrics.Call(SM_CYSCREEN)

	popupWidth := 420
	popupHeight := 220

	// 调整位置
	if x+20+popupWidth > int(screenWidth) {
		x = int(screenWidth) - popupWidth - 20
	}
	if y+20+popupHeight > int(screenHeight) {
		y = int(screenHeight) - popupHeight - 60
	}
	if x < 20 {
		x = 20
	}
	if y < 20 {
		y = 20
	}

	// 创建窗口
	hwnd, _, err := procCreateWindowExW2.Call(
		WS_EX_TOPMOST|WS_EX_TOOLWINDOW,
		uintptr(unsafe.Pointer(className)),
		0,
		WS_POPUP|WS_VISIBLE,
		uintptr(x+20), uintptr(y+20),
		uintptr(popupWidth), uintptr(popupHeight),
		0, 0,
		p.hInstance,
		0,
	)

	if hwnd == 0 {
		return fmt.Errorf("创建弹窗失败: %v", err)
	}

	p.hwnd = hwnd

	// 获取焦点（与 Python focus_force() 一致）
	procSetForegroundWindow.Call(hwnd)
	procSetFocus.Call(hwnd)

	fmt.Println("[Popup] 窗口创建成功")
	return nil
}

// popupWndProc 弹窗窗口过程
func popupWndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	p := globalPopup
	if p == nil {
		ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
		return ret
	}

	switch msg {
	case WM_PAINT:
		p.onPaint(hwnd)
		return 0

	case WM_LBUTTONDOWN:
		// 点击窗口内部不关闭（与 Python 一致）
		// 用户可能在点击复制按钮等
		return 0

	// ESC 键由全局键盘钩子处理（与 Python 一致）

	case WM_ACTIVATE:
		// WA_INACTIVE = 0，窗口被停用（用户点击了其他窗口）
		if (wParam & 0xFFFF) == 0 {
			fmt.Println("[Popup] 失去焦点，关闭")
			p.handleHide() // 直接调用（与 Overlay 一致）
			if p.OnClose != nil {
				go p.OnClose() // 异步调用避免阻塞
			}
		}
		return 0

	case WM_CLOSE:
		p.handleHide()
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}

// onPaint 绘制弹窗 - 现代深色主题设计
func (p *TranslationPopup) onPaint(hwnd uintptr) {
	var ps PAINTSTRUCT
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	width := 420
	height := 220

	// 现代深色主题配色 (Catppuccin Mocha 风格, BGR 格式)
	colorBase := uint32(0x2E1E1E)        // #1e1e2e 主背景
	colorSurface := uint32(0x443131)     // #313144 卡片背景
	colorText := uint32(0xF4D6CD)        // #cdd6f4 主文字
	colorSubtext := uint32(0x86706C)     // #6c7086 次要文字
	colorBlue := uint32(0xFAB489)        // #89b4fa 蓝色强调
	colorGreen := uint32(0xA1E3A6)       // #a6e3a1 绿色强调
	colorYellow := uint32(0x96E9F9)      // #f9e996 黄色（加载中）
	colorAccent := uint32(0xD4A6F5)      // #f5a6d4 粉色强调边框

	// 绘制主背景
	bgBrush, _, _ := procCreateSolidBrush.Call(uintptr(colorBase))
	rect := RECT{0, 0, int32(width), int32(height)}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rect)), bgBrush)
	procDeleteObject.Call(bgBrush)

	// 绘制顶部强调边框 (粉色渐变效果)
	accentBrush, _, _ := procCreateSolidBrush.Call(uintptr(colorAccent))
	accentRect := RECT{0, 0, int32(width), 3}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&accentRect)), accentBrush)
	procDeleteObject.Call(accentBrush)

	// 绘制原文卡片背景
	cardBrush, _, _ := procCreateSolidBrush.Call(uintptr(colorSurface))
	sourceCardRect := RECT{16, 20, int32(width - 16), 85}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&sourceCardRect)), cardBrush)
	procDeleteObject.Call(cardBrush)

	// 绘制译文卡片背景
	cardBrush2, _, _ := procCreateSolidBrush.Call(uintptr(colorSurface))
	targetCardRect := RECT{16, 95, int32(width - 16), 175}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&targetCardRect)), cardBrush2)
	procDeleteObject.Call(cardBrush2)

	procSetBkMode.Call(hdc, TRANSPARENT_BK)

	// 创建主字体 - Segoe UI (现代 Windows 风格)
	fontName, _ := syscall.UTF16PtrFromString("Segoe UI")
	hFont, _, _ := procCreateFontW.Call(
		uintptr(15), 0, 0, 0,
		400, 0, 0, 0,
		1, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(fontName)),
	)
	oldFont, _, _ := procSelectObject.Call(hdc, hFont)
	defer func() {
		procSelectObject.Call(hdc, oldFont)
		procDeleteObject.Call(hFont)
	}()

	// 创建小号字体（用于标签）
	smallFontName, _ := syscall.UTF16PtrFromString("Segoe UI")
	hSmallFont, _, _ := procCreateFontW.Call(
		uintptr(12), 0, 0, 0,
		600, 0, 0, 0, // 粗体
		1, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(smallFontName)),
	)
	defer procDeleteObject.Call(hSmallFont)

	// 绘制原文标签 (蓝色)
	procSelectObject.Call(hdc, hSmallFont)
	procSetTextColor.Call(hdc, uintptr(colorBlue))
	sourceLabelRect := RECT{24, 26, int32(width - 24), 42}
	sourceLabel := "SOURCE"
	sourceLabelUTF16, _ := syscall.UTF16FromString(sourceLabel)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&sourceLabelUTF16[0])), uintptr(len(sourceLabelUTF16)-1),
		uintptr(unsafe.Pointer(&sourceLabelRect)), DT_LEFT|DT_NOPREFIX)

	// 绘制原文内容 (主文字色)
	procSelectObject.Call(hdc, hFont)
	procSetTextColor.Call(hdc, uintptr(colorText))
	sourceRect := RECT{24, 44, int32(width - 24), 80}
	sourceText := p.sourceText
	if len([]rune(sourceText)) > 50 {
		sourceText = string([]rune(sourceText)[:50]) + "..."
	}
	sourceUTF16, _ := syscall.UTF16FromString(sourceText)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&sourceUTF16[0])), uintptr(len(sourceUTF16)-1),
		uintptr(unsafe.Pointer(&sourceRect)), DT_LEFT|DT_WORDBREAK|DT_NOPREFIX)

	// 绘制译文标签 (绿色)
	procSelectObject.Call(hdc, hSmallFont)
	procSetTextColor.Call(hdc, uintptr(colorGreen))
	targetLabelRect := RECT{24, 101, int32(width - 24), 117}
	targetLabel := "TRANSLATION"
	targetLabelUTF16, _ := syscall.UTF16FromString(targetLabel)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&targetLabelUTF16[0])), uintptr(len(targetLabelUTF16)-1),
		uintptr(unsafe.Pointer(&targetLabelRect)), DT_LEFT|DT_NOPREFIX)

	// 绘制译文内容
	procSelectObject.Call(hdc, hFont)
	resultRect := RECT{24, 119, int32(width - 24), 170}

	var resultText string
	if p.isLoading {
		// 加载中 - 黄色闪烁效果
		procSetTextColor.Call(hdc, uintptr(colorYellow))
		resultText = "● Translating..."
	} else if strings.HasPrefix(p.targetText, "错误") {
		// 错误 - 红色
		procSetTextColor.Call(hdc, 0x6060FF) // 红色
		resultText = p.targetText
	} else {
		// 成功 - 绿色
		procSetTextColor.Call(hdc, uintptr(colorGreen))
		resultText = p.targetText
	}
	resultUTF16, _ := syscall.UTF16FromString(resultText)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&resultUTF16[0])), uintptr(len(resultUTF16)-1),
		uintptr(unsafe.Pointer(&resultRect)), DT_LEFT|DT_WORDBREAK|DT_NOPREFIX)

	// 绘制底部提示 (次要文字色)
	procSetTextColor.Call(hdc, uintptr(colorSubtext))
	hintRect := RECT{16, int32(height - 30), int32(width - 16), int32(height - 10)}
	hintText := "ESC or click outside to close"
	hintUTF16, _ := syscall.UTF16FromString(hintText)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&hintUTF16[0])), uintptr(len(hintUTF16)-1),
		uintptr(unsafe.Pointer(&hintRect)), DT_LEFT|DT_NOPREFIX)
}

