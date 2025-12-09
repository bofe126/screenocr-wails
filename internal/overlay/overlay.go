//go:build windows

package overlay

import (
	"fmt"
	"image"
	"sync"
	"syscall"
	"unsafe"

	"screenocr-wails/internal/ocr"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassExW     = user32.NewProc("RegisterClassExW")
	procCreateWindowExW      = user32.NewProc("CreateWindowExW")
	procDefWindowProcW       = user32.NewProc("DefWindowProcW")
	procDestroyWindow        = user32.NewProc("DestroyWindow")
	procShowWindow           = user32.NewProc("ShowWindow")
	procUpdateWindow         = user32.NewProc("UpdateWindow")
	procSetWindowPos         = user32.NewProc("SetWindowPos")
	procGetMessageW          = user32.NewProc("GetMessageW")
	procTranslateMessage     = user32.NewProc("TranslateMessage")
	procDispatchMessageW     = user32.NewProc("DispatchMessageW")
	procPostQuitMessage      = user32.NewProc("PostQuitMessage")
	procPostMessageW         = user32.NewProc("PostMessageW")
	procGetDC                = user32.NewProc("GetDC")
	procReleaseDC            = user32.NewProc("ReleaseDC")
	procBeginPaint           = user32.NewProc("BeginPaint")
	procEndPaint             = user32.NewProc("EndPaint")
	procInvalidateRect       = user32.NewProc("InvalidateRect")
	procSetCapture           = user32.NewProc("SetCapture")
	procReleaseCapture       = user32.NewProc("ReleaseCapture")
	procGetCursorPos         = user32.NewProc("GetCursorPos")
	procSetCursor            = user32.NewProc("SetCursor")
	procLoadCursorW          = user32.NewProc("LoadCursorW")
	procGetSystemMetrics     = user32.NewProc("GetSystemMetrics")
	procSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	procUpdateLayeredWindow  = user32.NewProc("UpdateLayeredWindow")
	procOpenClipboard        = user32.NewProc("OpenClipboard")
	procCloseClipboard       = user32.NewProc("CloseClipboard")
	procEmptyClipboard       = user32.NewProc("EmptyClipboard")
	procSetClipboardData     = user32.NewProc("SetClipboardData")
	procGlobalAlloc          = kernel32.NewProc("GlobalAlloc")
	procGlobalLock           = kernel32.NewProc("GlobalLock")
	procGlobalUnlock         = kernel32.NewProc("GlobalUnlock")
	procGetModuleHandleW     = kernel32.NewProc("GetModuleHandleW")

	procCreateCompatibleDC      = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap  = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject            = gdi32.NewProc("SelectObject")
	procDeleteDC                = gdi32.NewProc("DeleteDC")
	procDeleteObject            = gdi32.NewProc("DeleteObject")
	procBitBlt                  = gdi32.NewProc("BitBlt")
	procSetDIBitsToDevice       = gdi32.NewProc("SetDIBitsToDevice")
	procCreateSolidBrush        = gdi32.NewProc("CreateSolidBrush")
	procFillRect                = gdi32.NewProc("FillRect")
	procCreatePen               = gdi32.NewProc("CreatePen")
	procRectangle               = gdi32.NewProc("Rectangle")
	procSetBkMode               = gdi32.NewProc("SetBkMode")
)

const (
	WS_EX_LAYERED     = 0x00080000
	WS_EX_TRANSPARENT = 0x00000020
	WS_EX_TOPMOST     = 0x00000008
	WS_EX_TOOLWINDOW  = 0x00000080
	WS_EX_NOACTIVATE  = 0x08000000
	WS_POPUP          = 0x80000000
	WS_VISIBLE        = 0x10000000

	SW_SHOW    = 5
	SW_HIDE    = 0
	HWND_TOPMOST = ^uintptr(0)

	SWP_NOSIZE     = 0x0001
	SWP_NOMOVE     = 0x0002
	SWP_SHOWWINDOW = 0x0040

	WM_DESTROY     = 0x0002
	WM_PAINT       = 0x000F
	WM_CLOSE       = 0x0010
	WM_KEYDOWN     = 0x0100
	WM_LBUTTONDOWN = 0x0201
	WM_LBUTTONUP   = 0x0202
	WM_MOUSEMOVE   = 0x0200
	WM_USER        = 0x0400

	VK_ESCAPE = 0x1B

	SM_CXSCREEN = 0
	SM_CYSCREEN = 1

	LWA_ALPHA    = 0x02
	LWA_COLORKEY = 0x01

	SRCCOPY = 0x00CC0020
	TRANSPARENT_BK = 1

	CF_UNICODETEXT = 13
	GMEM_MOVEABLE  = 0x0002

	IDC_ARROW = 32512
	IDC_IBEAM = 32513

	PS_SOLID = 0

	COLOR_HIGHLIGHT = 0x4D94FF // 蓝色高亮
	COLOR_BORDER_WAITING = 0xDB9834 // 蓝色边框 (BGR)
	COLOR_BORDER_READY   = 0x00FF00 // 绿色边框 (BGR)
)

// WNDCLASSEXW 窗口类结构
type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

// MSG 消息结构
type MSG struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
}

// POINT 点结构
type POINT struct {
	X, Y int32
}

// RECT 矩形结构
type RECT struct {
	Left, Top, Right, Bottom int32
}

// PAINTSTRUCT 绘制结构
type PAINTSTRUCT struct {
	HDC         uintptr
	FErase      int32
	RcPaint     RECT
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}

// BITMAPINFOHEADER 位图信息头
type BITMAPINFOHEADER struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

// BITMAPINFO 位图信息
type BITMAPINFO struct {
	BmiHeader BITMAPINFOHEADER
	BmiColors [1]uint32
}

// Overlay 覆盖层窗口
type Overlay struct {
	hwnd         uintptr
	hInstance    uintptr
	className    *uint16
	running      bool
	mu           sync.RWMutex

	// 显示状态
	screenshot   image.Image
	textBlocks   []ocr.TextBlock
	isReady      bool // OCR 是否完成

	// 选择状态
	selecting      bool
	selectionStart POINT
	selectionEnd   POINT
	selectedBlocks []int // 选中的文字块索引

	// 回调
	OnTextSelected func(text string, x, y int)
	OnClose        func()

	// 光标
	cursorArrow uintptr
	cursorIBeam uintptr
}

// 全局 overlay 实例（用于窗口过程回调）
var globalOverlay *Overlay

// NewOverlay 创建覆盖层
func NewOverlay() *Overlay {
	o := &Overlay{}
	o.cursorArrow, _, _ = procLoadCursorW.Call(0, IDC_ARROW)
	o.cursorIBeam, _, _ = procLoadCursorW.Call(0, IDC_IBEAM)
	return o
}

// Show 显示覆盖层
func (o *Overlay) Show(screenshot image.Image, textBlocks []ocr.TextBlock) error {
	o.mu.Lock()
	o.screenshot = screenshot
	o.textBlocks = textBlocks
	o.isReady = len(textBlocks) > 0
	o.selectedBlocks = nil
	o.selecting = false
	o.mu.Unlock()

	if o.hwnd == 0 {
		return o.createWindow()
	}

	// 刷新显示
	procInvalidateRect.Call(o.hwnd, 0, 1)
	procShowWindow.Call(o.hwnd, SW_SHOW)
	return nil
}

// ShowWaiting 显示等待状态
func (o *Overlay) ShowWaiting(screenshot image.Image) error {
	return o.Show(screenshot, nil)
}

// UpdateResults 更新 OCR 结果
func (o *Overlay) UpdateResults(textBlocks []ocr.TextBlock) {
	o.mu.Lock()
	o.textBlocks = textBlocks
	o.isReady = true
	o.mu.Unlock()

	if o.hwnd != 0 {
		procInvalidateRect.Call(o.hwnd, 0, 1)
	}
}

// Hide 隐藏覆盖层
func (o *Overlay) Hide() {
	if o.hwnd != 0 {
		procShowWindow.Call(o.hwnd, SW_HIDE)
	}
	o.mu.Lock()
	o.selectedBlocks = nil
	o.selecting = false
	o.mu.Unlock()
}

// Close 关闭覆盖层
func (o *Overlay) Close() {
	o.mu.Lock()
	o.running = false
	o.mu.Unlock()

	if o.hwnd != 0 {
		procDestroyWindow.Call(o.hwnd)
		o.hwnd = 0
	}
}

// createWindow 创建窗口
func (o *Overlay) createWindow() error {
	globalOverlay = o

	// 获取模块句柄
	o.hInstance, _, _ = procGetModuleHandleW.Call(0)

	// 注册窗口类
	className, _ := syscall.UTF16PtrFromString("ScreenOCROverlay")
	o.className = className

	wc := WNDCLASSEXW{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		LpfnWndProc:   syscall.NewCallback(wndProc),
		HInstance:     o.hInstance,
		HCursor:       o.cursorArrow,
		LpszClassName: className,
	}

	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	// 获取屏幕尺寸
	screenWidth, _, _ := procGetSystemMetrics.Call(SM_CXSCREEN)
	screenHeight, _, _ := procGetSystemMetrics.Call(SM_CYSCREEN)

	// 创建窗口
	hwnd, _, err := procCreateWindowExW.Call(
		WS_EX_LAYERED|WS_EX_TOPMOST|WS_EX_TOOLWINDOW,
		uintptr(unsafe.Pointer(className)),
		0,
		WS_POPUP|WS_VISIBLE,
		0, 0,
		screenWidth, screenHeight,
		0, 0,
		o.hInstance,
		0,
	)

	if hwnd == 0 {
		return fmt.Errorf("创建窗口失败: %v", err)
	}

	o.hwnd = hwnd

	// 设置窗口透明度
	procSetLayeredWindowAttributes.Call(hwnd, 0, 255, LWA_ALPHA)

	// 置顶显示
	procSetWindowPos.Call(hwnd, HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_SHOWWINDOW)

	o.running = true

	// 启动消息循环
	go o.messageLoop()

	return nil
}

// messageLoop 消息循环
func (o *Overlay) messageLoop() {
	var msg MSG
	for o.running {
		ret, _, _ := procGetMessageW.Call(
			uintptr(unsafe.Pointer(&msg)),
			0, 0, 0,
		)
		if ret == 0 || !o.running {
			break
		}

		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

// wndProc 窗口过程
func wndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	o := globalOverlay
	if o == nil {
		ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
		return ret
	}

	switch msg {
	case WM_PAINT:
		o.onPaint(hwnd)
		return 0

	case WM_LBUTTONDOWN:
		o.onMouseDown(lParam)
		return 0

	case WM_MOUSEMOVE:
		o.onMouseMove(lParam)
		return 0

	case WM_LBUTTONUP:
		o.onMouseUp(lParam)
		return 0

	case WM_KEYDOWN:
		if wParam == VK_ESCAPE {
			o.Hide()
			if o.OnClose != nil {
				o.OnClose()
			}
		}
		return 0

	case WM_CLOSE:
		o.Hide()
		return 0

	case WM_DESTROY:
		procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}

// onPaint 绘制
func (o *Overlay) onPaint(hwnd uintptr) {
	var ps PAINTSTRUCT
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	o.mu.RLock()
	screenshot := o.screenshot
	isReady := o.isReady
	textBlocks := o.textBlocks
	selectedBlocks := o.selectedBlocks
	o.mu.RUnlock()

	if screenshot == nil {
		return
	}

	bounds := screenshot.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// 创建内存 DC
	memDC, _, _ := procCreateCompatibleDC.Call(hdc)
	defer procDeleteDC.Call(memDC)

	hBitmap, _, _ := procCreateCompatibleBitmap.Call(hdc, uintptr(width), uintptr(height))
	defer procDeleteObject.Call(hBitmap)

	procSelectObject.Call(memDC, hBitmap)

	// 绘制截图
	o.drawScreenshot(memDC, screenshot)

	// 绘制遮罩
	o.drawOverlay(memDC, width, height, isReady)

	// 绘制选中高亮
	if len(selectedBlocks) > 0 {
		o.drawSelection(memDC, textBlocks, selectedBlocks)
	}

	// 绘制边框
	o.drawBorder(memDC, width, height, isReady)

	// 复制到屏幕
	procBitBlt.Call(hdc, 0, 0, uintptr(width), uintptr(height), memDC, 0, 0, SRCCOPY)
}

// drawScreenshot 绘制截图
func (o *Overlay) drawScreenshot(hdc uintptr, img image.Image) {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// 准备像素数据 (BGRA)
	pixels := make([]byte, width*height*4)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			offset := (y*width + x) * 4
			pixels[offset+0] = byte(b >> 8) // B
			pixels[offset+1] = byte(g >> 8) // G
			pixels[offset+2] = byte(r >> 8) // R
			pixels[offset+3] = byte(a >> 8) // A
		}
	}

	// 设置位图信息
	bi := BITMAPINFO{
		BmiHeader: BITMAPINFOHEADER{
			BiSize:        uint32(unsafe.Sizeof(BITMAPINFOHEADER{})),
			BiWidth:       int32(width),
			BiHeight:      -int32(height), // 负值表示从上到下
			BiPlanes:      1,
			BiBitCount:    32,
			BiCompression: 0,
		},
	}

	procSetDIBitsToDevice.Call(
		hdc,
		0, 0,
		uintptr(width), uintptr(height),
		0, 0, 0, uintptr(height),
		uintptr(unsafe.Pointer(&pixels[0])),
		uintptr(unsafe.Pointer(&bi)),
		0,
	)
}

// drawOverlay 绘制遮罩层
func (o *Overlay) drawOverlay(hdc uintptr, width, height int, isReady bool) {
	// 创建半透明遮罩
	var color uint32
	if isReady {
		color = 0x64FFFFFF // 白色半透明 (ABGR)
	} else {
		color = 0xB4000000 // 黑色半透明
	}

	brush, _, _ := procCreateSolidBrush.Call(uintptr(color & 0xFFFFFF))
	defer procDeleteObject.Call(brush)

	rect := RECT{0, 0, int32(width), int32(height)}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rect)), brush)
}

// drawSelection 绘制选中高亮
func (o *Overlay) drawSelection(hdc uintptr, textBlocks []ocr.TextBlock, selected []int) {
	brush, _, _ := procCreateSolidBrush.Call(uintptr(COLOR_HIGHLIGHT))
	defer procDeleteObject.Call(brush)

	procSetBkMode.Call(hdc, TRANSPARENT_BK)

	for _, idx := range selected {
		if idx >= 0 && idx < len(textBlocks) {
			block := textBlocks[idx]
			rect := RECT{
				Left:   int32(block.X),
				Top:    int32(block.Y),
				Right:  int32(block.X + block.Width),
				Bottom: int32(block.Y + block.Height),
			}
			procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rect)), brush)
		}
	}
}

// drawBorder 绘制边框
func (o *Overlay) drawBorder(hdc uintptr, width, height int, isReady bool) {
	var color uint32
	if isReady {
		color = COLOR_BORDER_READY
	} else {
		color = COLOR_BORDER_WAITING
	}

	pen, _, _ := procCreatePen.Call(PS_SOLID, 6, uintptr(color))
	defer procDeleteObject.Call(pen)

	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	defer procSelectObject.Call(hdc, oldPen)

	// 绘制边框矩形
	procRectangle.Call(hdc, 0, 0, uintptr(width), uintptr(height))
}

// onMouseDown 鼠标按下
func (o *Overlay) onMouseDown(lParam uintptr) {
	o.mu.Lock()
	if !o.isReady {
		o.mu.Unlock()
		return
	}

	x := int32(lParam & 0xFFFF)
	y := int32((lParam >> 16) & 0xFFFF)

	o.selecting = true
	o.selectionStart = POINT{x, y}
	o.selectionEnd = POINT{x, y}
	o.selectedBlocks = nil
	o.mu.Unlock()

	procSetCapture.Call(o.hwnd)
}

// onMouseMove 鼠标移动
func (o *Overlay) onMouseMove(lParam uintptr) {
	o.mu.Lock()
	isReady := o.isReady
	selecting := o.selecting
	o.mu.Unlock()

	x := int32(lParam & 0xFFFF)
	y := int32((lParam >> 16) & 0xFFFF)

	// 更新光标
	if isReady && o.isOverText(int(x), int(y)) {
		procSetCursor.Call(o.cursorIBeam)
	} else {
		procSetCursor.Call(o.cursorArrow)
	}

	if !selecting {
		return
	}

	o.mu.Lock()
	o.selectionEnd = POINT{x, y}
	o.updateSelection()
	o.mu.Unlock()

	procInvalidateRect.Call(o.hwnd, 0, 0)
}

// onMouseUp 鼠标释放
func (o *Overlay) onMouseUp(lParam uintptr) {
	procReleaseCapture.Call()

	o.mu.Lock()
	selecting := o.selecting
	o.selecting = false
	selectedBlocks := o.selectedBlocks
	textBlocks := o.textBlocks
	o.mu.Unlock()

	if !selecting || len(selectedBlocks) == 0 {
		return
	}

	// 合并选中的文字
	text := o.mergeSelectedText(textBlocks, selectedBlocks)
	if text == "" {
		return
	}

	// 复制到剪贴板
	o.copyToClipboard(text)

	// 触发回调
	x := int32(lParam & 0xFFFF)
	y := int32((lParam >> 16) & 0xFFFF)
	if o.OnTextSelected != nil {
		o.OnTextSelected(text, int(x), int(y))
	}
}

// isOverText 检查是否在文字上
func (o *Overlay) isOverText(x, y int) bool {
	o.mu.RLock()
	defer o.mu.RUnlock()

	for _, block := range o.textBlocks {
		if x >= block.X && x <= block.X+block.Width &&
			y >= block.Y && y <= block.Y+block.Height {
			return true
		}
	}
	return false
}

// updateSelection 更新选区
func (o *Overlay) updateSelection() {
	minX := min(o.selectionStart.X, o.selectionEnd.X)
	maxX := max(o.selectionStart.X, o.selectionEnd.X)
	minY := min(o.selectionStart.Y, o.selectionEnd.Y)
	maxY := max(o.selectionStart.Y, o.selectionEnd.Y)

	o.selectedBlocks = nil
	for i, block := range o.textBlocks {
		bx1, by1 := block.X, block.Y
		bx2, by2 := block.X+block.Width, block.Y+block.Height

		// 检查是否相交
		if !(int(maxX) < bx1 || int(minX) > bx2 || int(maxY) < by1 || int(minY) > by2) {
			o.selectedBlocks = append(o.selectedBlocks, i)
		}
	}
}

// mergeSelectedText 合并选中文字
func (o *Overlay) mergeSelectedText(textBlocks []ocr.TextBlock, selected []int) string {
	if len(selected) == 0 {
		return ""
	}

	// 按位置排序并合并文字
	// 简化版本：直接连接
	result := ""
	for _, idx := range selected {
		if idx >= 0 && idx < len(textBlocks) {
			if result != "" {
				result += " "
			}
			result += textBlocks[idx].Text
		}
	}
	return result
}

// copyToClipboard 复制到剪贴板
func (o *Overlay) copyToClipboard(text string) {
	ret, _, _ := procOpenClipboard.Call(o.hwnd)
	if ret == 0 {
		return
	}
	defer procCloseClipboard.Call()

	procEmptyClipboard.Call()

	// 转换为 UTF-16
	utf16, _ := syscall.UTF16FromString(text)
	size := len(utf16) * 2

	hMem, _, _ := procGlobalAlloc.Call(GMEM_MOVEABLE, uintptr(size))
	if hMem == 0 {
		return
	}

	pMem, _, _ := procGlobalLock.Call(hMem)
	if pMem == 0 {
		return
	}

	// 复制数据
	for i, c := range utf16 {
		*(*uint16)(unsafe.Pointer(pMem + uintptr(i*2))) = c
	}

	procGlobalUnlock.Call(hMem)
	procSetClipboardData.Call(CF_UNICODETEXT, hMem)

	fmt.Printf("已复制到剪贴板: %s\n", text)
}

func min(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

func max(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

