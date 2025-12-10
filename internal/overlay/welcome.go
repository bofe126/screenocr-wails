//go:build windows

package overlay

import (
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

// WelcomePage 欢迎页面
type WelcomePage struct {
	hwnd      uintptr
	hInstance uintptr
	className *uint16
	running   bool

	hotkey string

	// 回调
	OnClose func(dontShowAgain bool, openSettings bool)

	// 通道
	showChan  chan string // 传入 hotkey
	closeChan chan struct{}

	// 自定义复选框状态
	checkboxChecked bool
}

// 全局实例
var globalWelcome *WelcomePage

// 常量
const (
	WM_KEYDOWN       = 0x0100
	WM_NCLBUTTONDOWN = 0x00A1
	HTCAPTION        = 2
	DT_CENTER        = 0x00000001
)

var procPostMessageW = user32.NewProc("PostMessageW")

// NewWelcomePage 创建欢迎页面
func NewWelcomePage() *WelcomePage {
	w := &WelcomePage{
		showChan:  make(chan string),
		closeChan: make(chan struct{}),
	}

	go w.windowThread()

	return w
}

// Show 显示欢迎页面
func (w *WelcomePage) Show(hotkey string) {
	w.showChan <- hotkey
}

// Close 关闭
func (w *WelcomePage) Close() {
	select {
	case w.closeChan <- struct{}{}:
	default:
	}
}

// windowThread 窗口线程
func (w *WelcomePage) windowThread() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	w.running = true

	for w.running {
		var msg MSG
		ret, _, _ := procPeekMessageW.Call(
			uintptr(unsafe.Pointer(&msg)),
			0, 0, 0, PM_REMOVE,
		)
		if ret != 0 {
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
		}

		select {
		case hotkey := <-w.showChan:
			w.handleShow(hotkey)
		case <-w.closeChan:
			w.handleClose()
			return
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func (w *WelcomePage) handleShow(hotkey string) {
	globalWelcome = w
	w.hotkey = hotkey
	w.checkboxChecked = false

	if w.hwnd == 0 {
		w.createWindow()
	} else {
		procShowWindow.Call(w.hwnd, SW_SHOW)
		procSetForegroundWindow.Call(w.hwnd)
	}
}

func (w *WelcomePage) handleHide(openSettings bool) {
	dontShow := w.checkboxChecked
	fmt.Printf("[Welcome] 关闭，不再显示=%v，打开设置=%v\n", dontShow, openSettings)

	if w.hwnd != 0 {
		procShowWindow.Call(w.hwnd, SW_HIDE)
	}

	// 使用局部变量传递给回调，避免并发问题
	if w.OnClose != nil {
		go w.OnClose(dontShow, openSettings)
	}
}

func (w *WelcomePage) handleClose() {
	w.running = false
	if w.hwnd != 0 {
		procDestroyWindow.Call(w.hwnd)
		w.hwnd = 0
	}
}

func (w *WelcomePage) createWindow() error {
	w.hInstance, _, _ = procGetModuleHandleW.Call(0)

	className, _ := syscall.UTF16PtrFromString("ScreenOCRWelcome")
	w.className = className

	// 加载光标
	hCursor, _, _ := procLoadCursorW.Call(0, IDC_ARROW)

	wc := WNDCLASSEXW{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		LpfnWndProc:   syscall.NewCallback(welcomeWndProc),
		HInstance:     w.hInstance,
		HCursor:       hCursor,
		HbrBackground: 0,
		LpszClassName: className,
	}

	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	// 窗口尺寸
	windowWidth := 480
	windowHeight := 420

	// 居中显示
	screenWidth, _, _ := procGetSystemMetrics.Call(SM_CXSCREEN)
	screenHeight, _, _ := procGetSystemMetrics.Call(SM_CYSCREEN)
	x := (int(screenWidth) - windowWidth) / 2
	y := (int(screenHeight) - windowHeight) / 2

	// 创建窗口
	hwnd, _, _ := procCreateWindowExW.Call(
		WS_EX_TOPMOST|WS_EX_TOOLWINDOW,
		uintptr(unsafe.Pointer(className)),
		0,
		WS_POPUP|WS_VISIBLE,
		uintptr(x), uintptr(y),
		uintptr(windowWidth), uintptr(windowHeight),
		0, 0,
		w.hInstance,
		0,
	)

	if hwnd == 0 {
		return fmt.Errorf("创建欢迎窗口失败")
	}

	w.hwnd = hwnd

	procSetForegroundWindow.Call(hwnd)
	procSetFocus.Call(hwnd)

	fmt.Println("[Welcome] 欢迎页面已显示")
	return nil
}

// welcomeWndProc 窗口过程
func welcomeWndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	w := globalWelcome
	if w == nil {
		ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
		return ret
	}

	switch msg {
	case WM_PAINT:
		w.onPaint(hwnd)
		return 0

	case WM_LBUTTONDOWN:
		// 检查点击位置
		x := int32(lParam & 0xFFFF)
		y := int32((lParam >> 16) & 0xFFFF)

		// 关闭按钮区域 (窗口右上角，450-480, 0-30)
		if x >= 450 && x <= 480 && y >= 0 && y <= 30 {
			w.handleHide(false)
			return 0
		}

		// 标题栏区域 (0-450, 0-30) - 支持拖动
		if y >= 0 && y <= 30 && x < 450 {
			procReleaseCapture.Call()
			procPostMessageW.Call(hwnd, WM_NCLBUTTONDOWN, HTCAPTION, 0)
			return 0
		}

		// "开始使用" 按钮区域 (30, 340, 220, 378)
		if x >= 30 && x <= 220 && y >= 340 && y <= 378 {
			w.handleHide(false)
			return 0
		}

		// "详细设置" 按钮区域 (250, 340, 440, 378)
		if x >= 250 && x <= 440 && y >= 340 && y <= 378 {
			w.handleHide(true)
			return 0
		}

		// 复选框区域 (30, 390, 250, 415)
		if x >= 30 && x <= 250 && y >= 390 && y <= 415 {
			w.checkboxChecked = !w.checkboxChecked
			fmt.Printf("[Welcome] 复选框点击，新状态=%v\n", w.checkboxChecked)
			// 重绘窗口
			procInvalidateRect.Call(hwnd, 0, 1)
			// 选中后立即关闭
			if w.checkboxChecked {
				w.handleHide(false)
			}
			return 0
		}
		return 0

	case WM_KEYDOWN:
		if wParam == 0x0D { // Enter
			w.handleHide(false)
			return 0
		}
		if wParam == 0x1B { // ESC
			w.handleHide(false)
			return 0
		}

	case WM_CLOSE:
		w.handleHide(false)
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}

// onPaint 绘制
func (w *WelcomePage) onPaint(hwnd uintptr) {
	var ps PAINTSTRUCT
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	width := 480
	height := 420

	// 颜色 (BGR) - 更高对比度的配色
	colorBg := uint32(0x1A1A1A)          // #1a1a1a 更深的背景
	colorAccent := uint32(0xFF7B5C)      // #5c7bff 蓝紫色
	colorTitle := uint32(0xFFFFFF)       // #ffffff 白色标题
	colorText := uint32(0xE0E0E0)        // #e0e0e0 亮灰色文字
	colorSubtext := uint32(0xA0A0A0)     // #a0a0a0 次要文字
	colorHighlight := uint32(0x7BFF5C)   // #5cff7b 亮绿色
	colorOrange := uint32(0x5CA0FF)      // #ffa05c 橙色（翻译功能）
	colorBtnBg := uint32(0xFF7B5C)       // #5c7bff 按钮背景
	colorBtnText := uint32(0xFFFFFF)     // #ffffff 按钮文字
	colorCardBg := uint32(0x2A2A2A)      // #2a2a2a 卡片背景

	// 绘制背景
	bgBrush, _, _ := procCreateSolidBrush.Call(uintptr(colorBg))
	rect := RECT{0, 0, int32(width), int32(height)}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rect)), bgBrush)
	procDeleteObject.Call(bgBrush)

	// ========== 标题栏区域 ==========
	titleBarBrush, _, _ := procCreateSolidBrush.Call(uintptr(0x252525)) // 稍深的标题栏
	titleBarRect := RECT{0, 0, int32(width), 30}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&titleBarRect)), titleBarBrush)
	procDeleteObject.Call(titleBarBrush)

	// 顶部强调线
	accentBrush, _, _ := procCreateSolidBrush.Call(uintptr(colorAccent))
	accentRect := RECT{0, 0, int32(width), 3}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&accentRect)), accentBrush)
	procDeleteObject.Call(accentBrush)

	// 关闭按钮 (X)
	closeBtnBrush, _, _ := procCreateSolidBrush.Call(uintptr(0x4040FF)) // 红色背景
	closeBtnRect := RECT{int32(width - 30), 0, int32(width), 30}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&closeBtnRect)), closeBtnBrush)
	procDeleteObject.Call(closeBtnBrush)

	procSetBkMode.Call(hdc, TRANSPARENT_BK)

	// 创建大标题字体
	titleFontName, _ := syscall.UTF16PtrFromString("Microsoft YaHei UI")
	hTitleFont, _, _ := procCreateFontW.Call(
		uintptr(36), 0, 0, 0, // 大标题 36px
		700, 0, 0, 0,
		1, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(titleFontName)),
	)
	defer procDeleteObject.Call(hTitleFont)

	// 创建正文字体
	hTextFont, _, _ := procCreateFontW.Call(
		uintptr(20), 0, 0, 0, // 正文 20px
		400, 0, 0, 0,
		1, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(titleFontName)),
	)
	defer procDeleteObject.Call(hTextFont)

	// 创建粗体小标题字体
	hBoldFont, _, _ := procCreateFontW.Call(
		uintptr(20), 0, 0, 0, // 小标题 20px
		700, 0, 0, 0,
		1, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(titleFontName)),
	)
	defer procDeleteObject.Call(hBoldFont)

	// 创建小字体
	hSmallFont, _, _ := procCreateFontW.Call(
		uintptr(17), 0, 0, 0, // 小字 17px
		400, 0, 0, 0,
		1, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(titleFontName)),
	)
	defer procDeleteObject.Call(hSmallFont)

	// ========== 绘制标题栏文字 ==========
	procSelectObject.Call(hdc, hSmallFont)
	procSetTextColor.Call(hdc, uintptr(colorSubtext))
	titleBarTextRect := RECT{10, 7, int32(width - 40), 25}
	titleBarText := "Screen OCR"
	titleBarUTF16, _ := syscall.UTF16FromString(titleBarText)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&titleBarUTF16[0])), uintptr(len(titleBarUTF16)-1),
		uintptr(unsafe.Pointer(&titleBarTextRect)), DT_LEFT|DT_NOPREFIX)

	// 关闭按钮 X
	procSetTextColor.Call(hdc, uintptr(colorTitle))
	closeBtnTextRect := RECT{int32(width - 30), 5, int32(width), 28}
	closeText := "×"
	closeUTF16, _ := syscall.UTF16FromString(closeText)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&closeUTF16[0])), uintptr(len(closeUTF16)-1),
		uintptr(unsafe.Pointer(&closeBtnTextRect)), DT_CENTER|DT_NOPREFIX)

	// ========== 绘制主标题 ==========
	procSelectObject.Call(hdc, hTitleFont)
	procSetTextColor.Call(hdc, uintptr(colorTitle))
	titleRect := RECT{30, 45, int32(width - 30), 80}
	titleText := "欢迎使用 Screen OCR"
	titleUTF16, _ := syscall.UTF16FromString(titleText)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&titleUTF16[0])), uintptr(len(titleUTF16)-1),
		uintptr(unsafe.Pointer(&titleRect)), DT_LEFT|DT_NOPREFIX)

	// 绘制副标题
	procSelectObject.Call(hdc, hSmallFont)
	procSetTextColor.Call(hdc, uintptr(colorSubtext))
	subtitleRect := RECT{30, 77, int32(width - 30), 97}
	subtitleText := "快速识别屏幕文字，支持翻译功能"
	subtitleUTF16, _ := syscall.UTF16FromString(subtitleText)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&subtitleUTF16[0])), uintptr(len(subtitleUTF16)-1),
		uintptr(unsafe.Pointer(&subtitleRect)), DT_LEFT|DT_NOPREFIX)

	// ========== OCR 使用步骤卡片 ==========
	cardBrush, _, _ := procCreateSolidBrush.Call(uintptr(colorCardBg))
	ocrCardRect := RECT{20, 105, int32(width - 20), 215}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&ocrCardRect)), cardBrush)

	// OCR 标题
	procSelectObject.Call(hdc, hBoldFont)
	procSetTextColor.Call(hdc, uintptr(colorHighlight))
	ocrTitleRect := RECT{35, 112, int32(width - 35), 132}
	ocrTitleText := "▶ 文字识别"
	ocrTitleUTF16, _ := syscall.UTF16FromString(ocrTitleText)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&ocrTitleUTF16[0])), uintptr(len(ocrTitleUTF16)-1),
		uintptr(unsafe.Pointer(&ocrTitleRect)), DT_LEFT|DT_NOPREFIX)

	// OCR 步骤
	procSelectObject.Call(hdc, hTextFont)
	procSetTextColor.Call(hdc, uintptr(colorText))
	ocrSteps := []string{
		fmt.Sprintf("1. 按住 %s 键，等待屏幕边框变绿", w.hotkey),
		"2. 拖动鼠标选择文字区域",
		"3. 松开快捷键，文字自动复制到剪贴板",
	}
	y := int32(135)
	for _, step := range ocrSteps {
		stepRect := RECT{40, y, int32(width - 40), y + 24}
		stepUTF16, _ := syscall.UTF16FromString(step)
		procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&stepUTF16[0])), uintptr(len(stepUTF16)-1),
			uintptr(unsafe.Pointer(&stepRect)), DT_LEFT|DT_NOPREFIX)
		y += 26
	}

	// ========== 翻译功能卡片 ==========
	transCardRect := RECT{20, 225, int32(width - 20), 320}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&transCardRect)), cardBrush)
	procDeleteObject.Call(cardBrush)

	// 翻译标题
	procSelectObject.Call(hdc, hBoldFont)
	procSetTextColor.Call(hdc, uintptr(colorOrange))
	transTitleRect := RECT{35, 232, int32(width - 35), 252}
	transTitleText := "▶ 翻译功能"
	transTitleUTF16, _ := syscall.UTF16FromString(transTitleText)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&transTitleUTF16[0])), uintptr(len(transTitleUTF16)-1),
		uintptr(unsafe.Pointer(&transTitleRect)), DT_LEFT|DT_NOPREFIX)

	// 翻译说明
	procSelectObject.Call(hdc, hTextFont)
	procSetTextColor.Call(hdc, uintptr(colorText))
	transSteps := []string{
		"• 选中文字后自动弹出翻译窗口",
		"• 需在设置中配置腾讯云 API 密钥",
		"• 支持中英日韩法德等多语言互译",
	}
	y = int32(255)
	for _, step := range transSteps {
		stepRect := RECT{40, y, int32(width - 40), y + 20}
		stepUTF16, _ := syscall.UTF16FromString(step)
		procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&stepUTF16[0])), uintptr(len(stepUTF16)-1),
			uintptr(unsafe.Pointer(&stepRect)), DT_LEFT|DT_NOPREFIX)
		y += 21
	}

	// ========== 按钮区域 ==========
	// "开始使用" 按钮
	btnBrush, _, _ := procCreateSolidBrush.Call(uintptr(colorBtnBg))
	btnRect := RECT{30, 340, 220, 378}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&btnRect)), btnBrush)
	procDeleteObject.Call(btnBrush)

	procSelectObject.Call(hdc, hBoldFont)
	procSetTextColor.Call(hdc, uintptr(colorBtnText))
	btnTextRect := RECT{30, 350, 220, 378}
	btnText := "开始使用"
	btnUTF16, _ := syscall.UTF16FromString(btnText)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&btnUTF16[0])), uintptr(len(btnUTF16)-1),
		uintptr(unsafe.Pointer(&btnTextRect)), DT_CENTER|DT_NOPREFIX)

	// "详细设置" 按钮（边框样式）
	borderBrush, _, _ := procCreateSolidBrush.Call(uintptr(colorHighlight))
	// 上下左右边框
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&RECT{250, 340, 440, 342})), borderBrush)
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&RECT{250, 376, 440, 378})), borderBrush)
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&RECT{250, 340, 252, 378})), borderBrush)
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&RECT{438, 340, 440, 378})), borderBrush)
	procDeleteObject.Call(borderBrush)

	procSetTextColor.Call(hdc, uintptr(colorHighlight))
	btn2TextRect := RECT{250, 350, 440, 378}
	btn2Text := "详细设置"
	btn2UTF16, _ := syscall.UTF16FromString(btn2Text)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&btn2UTF16[0])), uintptr(len(btn2UTF16)-1),
		uintptr(unsafe.Pointer(&btn2TextRect)), DT_CENTER|DT_NOPREFIX)

	// ========== 自定义复选框 ==========
	// 复选框边框
	checkboxBorderBrush, _, _ := procCreateSolidBrush.Call(uintptr(colorSubtext))
	// 外框
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&RECT{30, 393, 48, 395})), checkboxBorderBrush) // 上
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&RECT{30, 408, 48, 410})), checkboxBorderBrush) // 下
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&RECT{30, 393, 32, 410})), checkboxBorderBrush) // 左
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&RECT{46, 393, 48, 410})), checkboxBorderBrush) // 右
	procDeleteObject.Call(checkboxBorderBrush)

	// 如果选中，绘制勾选标记
	if w.checkboxChecked {
		checkBrush, _, _ := procCreateSolidBrush.Call(uintptr(colorHighlight))
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&RECT{34, 397, 44, 406})), checkBrush)
		procDeleteObject.Call(checkBrush)
	}

	// 复选框文字
	procSetTextColor.Call(hdc, uintptr(colorText))
	procSelectObject.Call(hdc, hSmallFont)
	checkTextRect := RECT{55, 392, int32(width - 30), 412}
	checkText := "不再显示此欢迎页面"
	checkUTF16, _ := syscall.UTF16FromString(checkText)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&checkUTF16[0])), uintptr(len(checkUTF16)-1),
		uintptr(unsafe.Pointer(&checkTextRect)), DT_LEFT|DT_NOPREFIX)
}

