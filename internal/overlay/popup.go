//go:build windows

package overlay

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	procCreateWindowExW2 = user32.NewProc("CreateWindowExW")
	procSetWindowTextW   = user32.NewProc("SetWindowTextW")
	procMoveWindow       = user32.NewProc("MoveWindow")
	procSetTimer         = user32.NewProc("SetTimer")
	procKillTimer        = user32.NewProc("KillTimer")
)

const (
	WS_EX_DLGMODALFRAME = 0x00000001
	WS_OVERLAPPED       = 0x00000000
	WS_CAPTION          = 0x00C00000
	WS_SYSMENU          = 0x00080000
	WM_TIMER            = 0x0113
)

// TranslationPopup 翻译弹窗
type TranslationPopup struct {
	hwnd      uintptr
	hInstance uintptr
	className *uint16

	sourceText string
	targetText string
	isLoading  bool

	OnCopy  func(text string)
	OnClose func()
}

// 全局弹窗实例
var globalPopup *TranslationPopup

// NewTranslationPopup 创建翻译弹窗
func NewTranslationPopup() *TranslationPopup {
	return &TranslationPopup{
		isLoading: true,
	}
}

// Show 显示弹窗
func (p *TranslationPopup) Show(sourceText string, x, y int) error {
	globalPopup = p
	p.sourceText = sourceText
	p.targetText = ""
	p.isLoading = true

	if p.hwnd == 0 {
		return p.createWindow(x, y)
	}

	// 更新位置
	procMoveWindow.Call(p.hwnd, uintptr(x+20), uintptr(y+20), 350, 180, 1)
	procShowWindow.Call(p.hwnd, SW_SHOW)
	procInvalidateRect.Call(p.hwnd, 0, 1)
	return nil
}

// UpdateTranslation 更新翻译结果
func (p *TranslationPopup) UpdateTranslation(text string) {
	p.targetText = text
	p.isLoading = false
	if p.hwnd != 0 {
		procInvalidateRect.Call(p.hwnd, 0, 1)
	}
}

// ShowError 显示错误
func (p *TranslationPopup) ShowError(errMsg string) {
	p.targetText = "错误: " + errMsg
	p.isLoading = false
	if p.hwnd != 0 {
		procInvalidateRect.Call(p.hwnd, 0, 1)
	}
}

// Hide 隐藏弹窗
func (p *TranslationPopup) Hide() {
	if p.hwnd != 0 {
		procShowWindow.Call(p.hwnd, SW_HIDE)
	}
}

// Close 关闭弹窗
func (p *TranslationPopup) Close() {
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

	wc := WNDCLASSEXW{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		LpfnWndProc:   syscall.NewCallback(popupWndProc),
		HInstance:     p.hInstance,
		HbrBackground: 16 + 1, // COLOR_BTNFACE + 1
		LpszClassName: className,
	}

	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	// 获取屏幕尺寸以确保不超出边界
	screenWidth, _, _ := procGetSystemMetrics.Call(SM_CXSCREEN)
	screenHeight, _, _ := procGetSystemMetrics.Call(SM_CYSCREEN)

	popupWidth := 350
	popupHeight := 180

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

	// 启动消息循环
	go p.messageLoop()

	return nil
}

// messageLoop 消息循环
func (p *TranslationPopup) messageLoop() {
	var msg MSG
	for {
		ret, _, _ := procGetMessageW.Call(
			uintptr(unsafe.Pointer(&msg)),
			p.hwnd, 0, 0,
		)
		if ret == 0 {
			break
		}

		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
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
		// 点击外部关闭
		p.Hide()
		if p.OnClose != nil {
			p.OnClose()
		}
		return 0

	case WM_KEYDOWN:
		if wParam == VK_ESCAPE {
			p.Hide()
			if p.OnClose != nil {
				p.OnClose()
			}
		}
		return 0

	case WM_CLOSE:
		p.Hide()
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}

// onPaint 绘制弹窗
func (p *TranslationPopup) onPaint(hwnd uintptr) {
	var ps PAINTSTRUCT
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	// 绘制背景
	bgBrush, _, _ := procCreateSolidBrush.Call(0x2E1A1A) // 深色背景 (BGR)
	defer procDeleteObject.Call(bgBrush)

	rect := RECT{0, 0, 350, 180}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rect)), bgBrush)

	// 绘制边框
	borderBrush, _, _ := procCreateSolidBrush.Call(0x6A4A4A)
	defer procDeleteObject.Call(borderBrush)

	// 这里可以进一步绘制文字内容
	// 为简化示例，实际可使用 GDI TextOut 或更复杂的绘制
}

