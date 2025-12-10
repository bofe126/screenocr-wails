//go:build windows

package overlay

import (
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

// StartupToast 启动通知
type StartupToast struct {
	hwnd      uintptr
	hInstance uintptr
	className *uint16

	hotkey   string
	title    string // 自定义标题
	message  string // 自定义消息
	isWarn   bool   // 是否为警告样式
	duration time.Duration
}

// 全局实例
var globalToast *StartupToast

// NewStartupToast 创建启动通知
func NewStartupToast() *StartupToast {
	return &StartupToast{
		duration: 3 * time.Second,
	}
}

// Show 显示通知
func (t *StartupToast) Show(hotkey string) {
	t.hotkey = hotkey
	t.title = "✓ Screen OCR 已启动"
	t.message = fmt.Sprintf("按住 %s 键开始识别文字", hotkey)
	t.isWarn = false

	// 在新的 goroutine 中运行，避免阻塞
	go t.run()
}

// ShowMessage 显示自定义消息
func (t *StartupToast) ShowMessage(title, message string) {
	t.title = "⚠ " + title
	t.message = message
	t.isWarn = true

	// 在新的 goroutine 中运行，避免阻塞
	go t.run()
}

func (t *StartupToast) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	globalToast = t
	t.hInstance, _, _ = procGetModuleHandleW.Call(0)

	className, _ := syscall.UTF16PtrFromString("ScreenOCRToast")
	t.className = className

	// 加载光标
	hCursor, _, _ := procLoadCursorW.Call(0, IDC_ARROW)

	wc := WNDCLASSEXW{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		LpfnWndProc:   syscall.NewCallback(toastWndProc),
		HInstance:     t.hInstance,
		HCursor:       hCursor,
		HbrBackground: 0,
		LpszClassName: className,
	}

	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	// 窗口尺寸
	windowWidth := 320
	windowHeight := 80

	// 右下角显示（留出任务栏空间）
	screenWidth, _, _ := procGetSystemMetrics.Call(SM_CXSCREEN)
	screenHeight, _, _ := procGetSystemMetrics.Call(SM_CYSCREEN)
	x := int(screenWidth) - windowWidth - 20
	y := int(screenHeight) - windowHeight - 60

	// 创建窗口
	hwnd, _, _ := procCreateWindowExW.Call(
		WS_EX_TOPMOST|WS_EX_TOOLWINDOW|WS_EX_NOACTIVATE,
		uintptr(unsafe.Pointer(className)),
		0,
		WS_POPUP|WS_VISIBLE,
		uintptr(x), uintptr(y),
		uintptr(windowWidth), uintptr(windowHeight),
		0, 0,
		t.hInstance,
		0,
	)

	if hwnd == 0 {
		fmt.Println("[Toast] 创建通知窗口失败")
		return
	}

	t.hwnd = hwnd
	fmt.Println("[Toast] 启动通知已显示")

	// 设置定时器自动关闭
	startTime := time.Now()

	// 消息循环
	for {
		var msg MSG
		ret, _, _ := procPeekMessageW.Call(
			uintptr(unsafe.Pointer(&msg)),
			0, 0, 0, PM_REMOVE,
		)
		if ret != 0 {
			if msg.Message == 0x0012 { // WM_QUIT
				break
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
		}

		// 检查是否超时
		if time.Since(startTime) >= t.duration {
			break
		}

		time.Sleep(50 * time.Millisecond)
	}

	// 销毁窗口
	if t.hwnd != 0 {
		procDestroyWindow.Call(t.hwnd)
		t.hwnd = 0
	}
	fmt.Println("[Toast] 启动通知已关闭")
}

// toastWndProc 窗口过程
func toastWndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	t := globalToast
	if t == nil {
		ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
		return ret
	}

	switch msg {
	case WM_PAINT:
		t.onPaint(hwnd)
		return 0

	case WM_LBUTTONDOWN:
		// 点击关闭
		procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}

// onPaint 绘制
func (t *StartupToast) onPaint(hwnd uintptr) {
	var ps PAINTSTRUCT
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	width := 320
	height := 80

	// 颜色 (BGR)
	colorBg := uint32(0x2E1E1E)       // #1e1e2e
	colorAccentGreen := uint32(0xA1E3A6) // #a6e3a1 绿色
	colorAccentOrange := uint32(0x5CA0FF) // #ffa05c 橙色
	colorTitle := uint32(0xF4D6CD)    // #cdd6f4
	colorText := uint32(0x86706C)     // #6c7086

	// 根据类型选择强调色
	colorAccent := colorAccentGreen
	if t.isWarn {
		colorAccent = colorAccentOrange
	}

	// 绘制背景
	bgBrush, _, _ := procCreateSolidBrush.Call(uintptr(colorBg))
	rect := RECT{0, 0, int32(width), int32(height)}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rect)), bgBrush)
	procDeleteObject.Call(bgBrush)

	// 左侧强调线
	accentBrush, _, _ := procCreateSolidBrush.Call(uintptr(colorAccent))
	accentRect := RECT{0, 0, 4, int32(height)}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&accentRect)), accentBrush)
	procDeleteObject.Call(accentBrush)

	procSetBkMode.Call(hdc, TRANSPARENT_BK)

	// 创建标题字体
	titleFontName, _ := syscall.UTF16PtrFromString("Microsoft YaHei UI")
	hTitleFont, _, _ := procCreateFontW.Call(
		uintptr(20), 0, 0, 0, // 标题 20px
		600, 0, 0, 0,
		1, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(titleFontName)),
	)
	defer procDeleteObject.Call(hTitleFont)

	// 创建正文字体
	hTextFont, _, _ := procCreateFontW.Call(
		uintptr(17), 0, 0, 0, // 正文 17px
		400, 0, 0, 0,
		1, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(titleFontName)),
	)
	defer procDeleteObject.Call(hTextFont)

	// 绘制标题
	procSelectObject.Call(hdc, hTitleFont)
	procSetTextColor.Call(hdc, uintptr(colorTitle))
	titleRect := RECT{20, 18, int32(width - 20), 40}
	titleUTF16, _ := syscall.UTF16FromString(t.title)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&titleUTF16[0])), uintptr(len(titleUTF16)-1),
		uintptr(unsafe.Pointer(&titleRect)), DT_LEFT|DT_NOPREFIX)

	// 绘制提示文本
	procSelectObject.Call(hdc, hTextFont)
	procSetTextColor.Call(hdc, uintptr(colorText))
	tipRect := RECT{20, 45, int32(width - 20), 70}
	tipUTF16, _ := syscall.UTF16FromString(t.message)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&tipUTF16[0])), uintptr(len(tipUTF16)-1),
		uintptr(unsafe.Pointer(&tipRect)), DT_LEFT|DT_NOPREFIX)
}

