//go:build windows

package overlay

import (
	"fmt"
	"image"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"screenocr-wails/internal/ocr"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassExW           = user32.NewProc("RegisterClassExW")
	procCreateWindowExW            = user32.NewProc("CreateWindowExW")
	procDefWindowProcW             = user32.NewProc("DefWindowProcW")
	procDestroyWindow              = user32.NewProc("DestroyWindow")
	procShowWindow                 = user32.NewProc("ShowWindow")
	procSetWindowPos               = user32.NewProc("SetWindowPos")
	procGetMessageW                = user32.NewProc("GetMessageW")
	procTranslateMessage           = user32.NewProc("TranslateMessage")
	procDispatchMessageW           = user32.NewProc("DispatchMessageW")
	procPostQuitMessage            = user32.NewProc("PostQuitMessage")
	procBeginPaint                 = user32.NewProc("BeginPaint")
	procEndPaint                   = user32.NewProc("EndPaint")
	procInvalidateRect             = user32.NewProc("InvalidateRect")
	procSetCapture                 = user32.NewProc("SetCapture")
	procReleaseCapture             = user32.NewProc("ReleaseCapture")
	procSetCursor                  = user32.NewProc("SetCursor")
	procLoadCursorW                = user32.NewProc("LoadCursorW")
	procSetForegroundWindow        = user32.NewProc("SetForegroundWindow")
	procSetFocus                   = user32.NewProc("SetFocus")
	procPeekMessageW               = user32.NewProc("PeekMessageW")
	procGetSystemMetrics           = user32.NewProc("GetSystemMetrics")
	procSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	procOpenClipboard              = user32.NewProc("OpenClipboard")
	procCloseClipboard             = user32.NewProc("CloseClipboard")
	procEmptyClipboard             = user32.NewProc("EmptyClipboard")
	procSetClipboardData           = user32.NewProc("SetClipboardData")
	procGlobalAlloc                = kernel32.NewProc("GlobalAlloc")
	procGlobalLock                 = kernel32.NewProc("GlobalLock")
	procGlobalUnlock               = kernel32.NewProc("GlobalUnlock")
	procGetModuleHandleW           = kernel32.NewProc("GetModuleHandleW")

	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procCreateSolidBrush       = gdi32.NewProc("CreateSolidBrush")
	procFillRect               = user32.NewProc("FillRect") // FillRect 在 user32.dll 中
	procGetDpiForSystem        = user32.NewProc("GetDpiForSystem")
)

// 系统 DPI 缓存
var systemDPI int
var dpiInitialized bool

// GetSystemDPI 获取系统 DPI
func GetSystemDPI() int {
	if dpiInitialized {
		return systemDPI
	}
	dpiInitialized = true

	// 方法1：使用 GetDeviceCaps（最可靠）
	hdc, _, _ := procGetDC.Call(0)
	if hdc != 0 {
		dpi, _, _ := procGetDeviceCaps.Call(hdc, 88) // LOGPIXELSX = 88
		procReleaseDC.Call(0, hdc)
		if dpi > 0 {
			systemDPI = int(dpi)
			fmt.Printf("[DPI] 检测到系统 DPI: %d (缩放比例: %d%%)\n", systemDPI, systemDPI*100/96)
			return systemDPI
		}
	}

	// 方法2：尝试使用 GetDpiForSystem (Windows 10 1607+)
	dpi, _, _ := procGetDpiForSystem.Call()
	if dpi > 0 {
		systemDPI = int(dpi)
		fmt.Printf("[DPI] 检测到系统 DPI (GetDpiForSystem): %d\n", systemDPI)
		return systemDPI
	}

	// 默认 96 DPI
	systemDPI = 96
	fmt.Println("[DPI] 使用默认 DPI: 96")
	return systemDPI
}

// ScaleForDPI 根据 DPI 缩放值
func ScaleForDPI(value int) int {
	dpi := GetSystemDPI()
	scaled := value * dpi / 96
	return scaled
}

var (
	procGetDC         = user32.NewProc("GetDC")
	procReleaseDC     = user32.NewProc("ReleaseDC")
	procGetDeviceCaps = gdi32.NewProc("GetDeviceCaps")
	procCreatePen              = gdi32.NewProc("CreatePen")
	procRectangle              = gdi32.NewProc("Rectangle")
	procSetBkMode              = gdi32.NewProc("SetBkMode")
	procTextOutW               = gdi32.NewProc("TextOutW")
	procSetTextColor           = gdi32.NewProc("SetTextColor")
	procGetStockObject         = gdi32.NewProc("GetStockObject")
	procCreateFontW            = gdi32.NewProc("CreateFontW")
	procSetDIBitsToDevice      = gdi32.NewProc("SetDIBitsToDevice")
)

const (
	WS_EX_LAYERED    = 0x00080000
	WS_EX_TOPMOST    = 0x00000008
	WS_EX_TOOLWINDOW = 0x00000080
	WS_EX_NOACTIVATE = 0x08000000
	WS_POPUP         = 0x80000000
	WS_VISIBLE       = 0x10000000

	SW_SHOW      = 5
	SW_HIDE      = 0
	HWND_TOPMOST = ^uintptr(0)

	SWP_NOSIZE     = 0x0001
	SWP_NOMOVE     = 0x0002
	SWP_SHOWWINDOW = 0x0040

	WM_DESTROY     = 0x0002
	WM_PAINT       = 0x000F
	WM_CLOSE       = 0x0010
	WM_LBUTTONDOWN = 0x0201
	WM_LBUTTONUP   = 0x0202
	WM_MOUSEMOVE   = 0x0200

	SM_CXSCREEN = 0
	SM_CYSCREEN = 1

	LWA_ALPHA = 0x02

	SRCCOPY        = 0x00CC0020
	TRANSPARENT_BK = 1

	CF_UNICODETEXT = 13
	GMEM_MOVEABLE  = 0x0002

	IDC_ARROW = 32512
	IDC_IBEAM = 32513

	PM_REMOVE = 0x0001

	PS_SOLID = 0

	// 位图常量
	BI_RGB         = 0
	DIB_RGB_COLORS = 0

	// 与 Python 版本保持一致的颜色
	COLOR_HIGHLIGHT      = 0xFF944D // 蓝色高亮 #4D94FF (BGR)
	COLOR_BORDER_WAITING = 0xDB9834 // 蓝色边框 #3498db (BGR)
	COLOR_BORDER_READY   = 0x00FF00 // 绿色边框 #00FF00 (BGR)
	COLOR_OVERLAY_WAIT   = 0x000000 // 等待时的遮罩颜色（黑色）
	COLOR_OVERLAY_READY  = 0xFFFFFF // 就绪时的遮罩颜色（白色）
	
	// 透明度 (与 Python 版本一致)
	ALPHA_WAITING = 180 // 等待时 ~70% 不透明
	ALPHA_READY   = 100 // 完成时 ~39% 不透明
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
	hwnd      uintptr
	hInstance uintptr
	className *uint16
	running   bool
	mu        sync.RWMutex

	// 屏幕尺寸
	screenWidth  int
	screenHeight int

	// 显示状态
	screenshot *image.RGBA   // 截图
	textBlocks []ocr.TextBlock
	isReady    bool // OCR 是否完成

	// 缓存（优化性能：截图+遮罩只计算一次，与 Python 一致）
	cachedBackground []byte // 缓存的截图+遮罩混合结果（BGRA 格式，自底向上）
	cachedReady      bool   // 缓存对应的 isReady 状态
	cacheValid       bool   // 缓存是否有效

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

	// 窗口线程通信
	showChan   chan showRequest
	hideChan   chan struct{}
	closeChan  chan struct{}
	updateChan chan []ocr.TextBlock
}

// showRequest 显示请求
type showRequest struct {
	screenshot image.Image
	textBlocks []ocr.TextBlock
}

// 全局 overlay 实例（用于窗口过程回调）
var globalOverlay *Overlay

// NewOverlay 创建覆盖层
func NewOverlay() *Overlay {
	o := &Overlay{
		showChan:   make(chan showRequest),
		hideChan:   make(chan struct{}, 1), // 带缓冲，避免在 wndProc 中阻塞
		closeChan:  make(chan struct{}, 1),
		updateChan: make(chan []ocr.TextBlock, 1),
	}

	// 启动专用窗口线程
	go o.windowThread()

	return o
}

// Show 显示覆盖层
func (o *Overlay) Show(screenshot image.Image, textBlocks []ocr.TextBlock) error {
	o.showChan <- showRequest{screenshot: screenshot, textBlocks: textBlocks}
	return nil
}

// ShowWaiting 显示等待状态
func (o *Overlay) ShowWaiting(screenshot image.Image) error {
	return o.Show(screenshot, nil)
}

// UpdateResults 更新 OCR 结果
func (o *Overlay) UpdateResults(textBlocks []ocr.TextBlock) {
	o.updateChan <- textBlocks
}

// Hide 隐藏覆盖层
func (o *Overlay) Hide() {
	select {
	case o.hideChan <- struct{}{}:
	default:
	}
}

// Close 关闭覆盖层
func (o *Overlay) Close() {
	select {
	case o.closeChan <- struct{}{}:
	default:
	}
}

// windowThread 专用窗口线程
func (o *Overlay) windowThread() {
	// 锁定到当前 OS 线程，Win32 窗口必须在同一线程处理消息
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// 在窗口线程中加载光标
	o.cursorArrow, _, _ = procLoadCursorW.Call(0, IDC_ARROW)
	o.cursorIBeam, _, _ = procLoadCursorW.Call(0, IDC_IBEAM)

	o.running = true

	for o.running {
		// 非阻塞检查消息
		var msg MSG
		ret, _, _ := procPeekMessageW.Call(
			uintptr(unsafe.Pointer(&msg)),
			0, 0, 0, PM_REMOVE,
		)

		if ret != 0 {
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
		}

		// 检查通道
		select {
		case req := <-o.showChan:
			o.handleShow(req.screenshot, req.textBlocks)
		case <-o.hideChan:
			o.handleHide()
		case blocks := <-o.updateChan:
			o.handleUpdate(blocks)
		case <-o.closeChan:
			o.handleClose()
			return
		default:
			// 短暂休眠避免 CPU 占用过高
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// handleShow 在窗口线程中处理显示
func (o *Overlay) handleShow(screenshot image.Image, textBlocks []ocr.TextBlock) {
	o.mu.Lock()
	// 保存截图为 RGBA 格式
	if screenshot != nil {
		bounds := screenshot.Bounds()
		o.screenshot = image.NewRGBA(bounds)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				o.screenshot.Set(x, y, screenshot.At(x, y))
			}
		}
	}
	o.textBlocks = textBlocks
	o.isReady = len(textBlocks) > 0
	o.selectedBlocks = nil
	o.selecting = false
	o.cacheValid = false // 清除缓存，需要重新计算背景
	o.mu.Unlock()

	if o.hwnd == 0 {
		o.createWindow()
	} else {
		procInvalidateRect.Call(o.hwnd, 0, 1)
		procShowWindow.Call(o.hwnd, SW_SHOW)
		procSetForegroundWindow.Call(o.hwnd)
		procSetFocus.Call(o.hwnd)
	}
}

// handleHide 在窗口线程中处理隐藏
func (o *Overlay) handleHide() {
	if o.hwnd != 0 {
		procShowWindow.Call(o.hwnd, SW_HIDE)
	}
	o.mu.Lock()
	o.selectedBlocks = nil
	o.selecting = false
	o.mu.Unlock()
	fmt.Println("[Overlay] 隐藏")
}

// handleUpdate 在窗口线程中处理更新
func (o *Overlay) handleUpdate(textBlocks []ocr.TextBlock) {
	// 智能拆分文本块（与 Python 版本一致）
	splitBlocks := ocr.SplitTextBlocks(textBlocks)

	o.mu.Lock()
	o.textBlocks = splitBlocks
	o.isReady = true
	o.cacheValid = false // isReady 状态变化，需要重新计算背景（遮罩颜色不同）
	o.mu.Unlock()

	if o.hwnd != 0 {
		procInvalidateRect.Call(o.hwnd, 0, 1)
	}

	fmt.Printf("[Overlay] 更新结果，原始 %d 个文本块，拆分后 %d 个\n", len(textBlocks), len(splitBlocks))
}

// handleClose 在窗口线程中处理关闭
func (o *Overlay) handleClose() {
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

	// 获取屏幕尺寸
	sw, _, _ := procGetSystemMetrics.Call(SM_CXSCREEN)
	sh, _, _ := procGetSystemMetrics.Call(SM_CYSCREEN)
	o.screenWidth = int(sw)
	o.screenHeight = int(sh)

	// 获取模块句柄
	o.hInstance, _, _ = procGetModuleHandleW.Call(0)

	// 注册窗口类
	className, _ := syscall.UTF16PtrFromString("ScreenOCROverlay")
	o.className = className

	wc := WNDCLASSEXW{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		Style:         0x0008, // CS_DBLCLKS - 接收双击消息
		LpfnWndProc:   syscall.NewCallback(wndProc),
		HInstance:     o.hInstance,
		HCursor:       o.cursorArrow,
		LpszClassName: className,
	}

	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	// 创建窗口 - 使用半透明窗口（移除 WS_EX_NOACTIVATE 以接收鼠标事件）
	hwnd, _, err := procCreateWindowExW.Call(
		WS_EX_LAYERED|WS_EX_TOPMOST|WS_EX_TOOLWINDOW,
		uintptr(unsafe.Pointer(className)),
		0,
		WS_POPUP|WS_VISIBLE,
		0, 0,
		sw, sh,
		0, 0,
		o.hInstance,
		0,
	)

	if hwnd == 0 {
		return fmt.Errorf("创建窗口失败: %v", err)
	}

	o.hwnd = hwnd

	// 设置窗口透明度 - 初始为等待状态（黑色遮罩 70% 不透明）
	procSetLayeredWindowAttributes.Call(hwnd, 0, ALPHA_WAITING, LWA_ALPHA)

	// 置顶显示
	procSetWindowPos.Call(hwnd, HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_SHOWWINDOW)

	// 激活窗口，确保能接收鼠标事件
	procSetForegroundWindow.Call(hwnd)
	procSetFocus.Call(hwnd)

	fmt.Println("[Overlay] 窗口创建成功")
	return nil
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

	// ESC 键和关闭由全局键盘钩子处理（与 Python 一致）

	case WM_CLOSE:
		o.handleHide()
		return 0

	case WM_DESTROY:
		procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}

// onPaint 绘制 - 与 Python 版本保持一致（显示截图 + 遮罩）
func (o *Overlay) onPaint(hwnd uintptr) {
	var ps PAINTSTRUCT
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	o.mu.RLock()
	isReady := o.isReady
	textBlocks := o.textBlocks
	selectedBlocks := o.selectedBlocks
	screenshot := o.screenshot
	o.mu.RUnlock()

	width := o.screenWidth
	height := o.screenHeight

	// 创建内存 DC
	memDC, _, _ := procCreateCompatibleDC.Call(hdc)
	defer procDeleteDC.Call(memDC)

	hBitmap, _, _ := procCreateCompatibleBitmap.Call(hdc, uintptr(width), uintptr(height))
	defer procDeleteObject.Call(hBitmap)

	procSelectObject.Call(memDC, hBitmap)

	// 绘制截图 + 遮罩 + 高亮混合后的图像（与 Python alpha_composite 一致）
	if screenshot != nil {
		o.drawScreenshotWithOverlay(memDC, screenshot, width, height, isReady, textBlocks, selectedBlocks)
	} else {
		// 如果没有截图，绘制纯色背景
		var bgColor uint32
		if isReady {
			bgColor = COLOR_OVERLAY_READY
		} else {
			bgColor = COLOR_OVERLAY_WAIT
		}
		brush, _, _ := procCreateSolidBrush.Call(uintptr(bgColor))
		rect := RECT{0, 0, int32(width), int32(height)}
		procFillRect.Call(memDC, uintptr(unsafe.Pointer(&rect)), brush)
		procDeleteObject.Call(brush)
	}

	// 等待状态显示 "识别中，请稍后..." 文字
	if !isReady {
		o.drawWaitingText(memDC, width, height)
	}

	// 高亮已在 drawScreenshotWithOverlay 中通过像素混合完成

	// 绘制边框 - 与 Python 版本一致的颜色
	o.drawBorder(memDC, width, height, isReady)

	// 设置窗口完全不透明（因为图像已经包含遮罩效果）
	procSetLayeredWindowAttributes.Call(hwnd, 0, 255, LWA_ALPHA)

	// 复制到屏幕
	procBitBlt.Call(hdc, 0, 0, uintptr(width), uintptr(height), memDC, 0, 0, SRCCOPY)
}

// drawScreenshotWithOverlay 绘制截图并叠加遮罩层和高亮层（与 Python alpha_composite 一致）
// 优化：截图+遮罩只计算一次并缓存，高亮只处理选中区域的像素
func (o *Overlay) drawScreenshotWithOverlay(hdc uintptr, screenshot *image.RGBA, width, height int, isReady bool, textBlocks []ocr.TextBlock, selectedBlocks []int) {
	bounds := screenshot.Bounds()
	imgWidth := bounds.Dx()
	imgHeight := bounds.Dy()
	dataSize := imgWidth * imgHeight * 4

	// 检查是否需要重新计算背景缓存
	o.mu.Lock()
	needRecalc := !o.cacheValid || o.cachedReady != isReady || len(o.cachedBackground) != dataSize
	o.mu.Unlock()

	if needRecalc {
		// 计算截图+遮罩混合（只在状态变化时计算一次）
		o.calculateBackground(screenshot, imgWidth, imgHeight, isReady)
	}

	// 复制缓存的背景
	o.mu.RLock()
	pixelData := make([]byte, dataSize)
	copy(pixelData, o.cachedBackground)
	o.mu.RUnlock()

	// 只对高亮区域的像素进行额外混合（与 Python 一致：高亮是单独的小图层）
	if len(selectedBlocks) > 0 {
		o.applyHighlight(pixelData, imgWidth, imgHeight, textBlocks, selectedBlocks)
	}

	// 准备位图信息
	bi := BITMAPINFO{
		BmiHeader: BITMAPINFOHEADER{
			BiSize:        uint32(unsafe.Sizeof(BITMAPINFOHEADER{})),
			BiWidth:       int32(imgWidth),
			BiHeight:      int32(imgHeight), // 正值表示自底向上
			BiPlanes:      1,
			BiBitCount:    32,
			BiCompression: BI_RGB,
		},
	}

	// 绘制到 DC
	procSetDIBitsToDevice.Call(
		hdc,
		0, 0,                              // 目标位置
		uintptr(imgWidth), uintptr(imgHeight), // 尺寸
		0, 0,                              // 源位置
		0, uintptr(imgHeight),             // 起始行，行数
		uintptr(unsafe.Pointer(&pixelData[0])),
		uintptr(unsafe.Pointer(&bi)),
		DIB_RGB_COLORS,
	)
}

// calculateBackground 计算截图+遮罩混合结果并缓存（只在状态变化时调用一次）
func (o *Overlay) calculateBackground(screenshot *image.RGBA, imgWidth, imgHeight int, isReady bool) {
	dataSize := imgWidth * imgHeight * 4
	background := make([]byte, dataSize)

	// 遮罩参数 - 与 Python 保持一致
	var overlayR, overlayG, overlayB, overlayA uint8
	if isReady {
		overlayR, overlayG, overlayB, overlayA = 255, 255, 255, 100 // 白色遮罩 39%
	} else {
		overlayR, overlayG, overlayB, overlayA = 0, 0, 0, 180 // 黑色遮罩 70%
	}

	alpha := float64(overlayA) / 255.0
	invAlpha := 1.0 - alpha

	for y := 0; y < imgHeight; y++ {
		for x := 0; x < imgWidth; x++ {
			srcIdx := (y*imgWidth + x) * 4
			srcR := screenshot.Pix[srcIdx+0]
			srcG := screenshot.Pix[srcIdx+1]
			srcB := screenshot.Pix[srcIdx+2]

			r := uint8(float64(srcR)*invAlpha + float64(overlayR)*alpha)
			g := uint8(float64(srcG)*invAlpha + float64(overlayG)*alpha)
			b := uint8(float64(srcB)*invAlpha + float64(overlayB)*alpha)

			// BGRA 格式，自底向上
			dstY := imgHeight - 1 - y
			dstIdx := (dstY*imgWidth + x) * 4
			background[dstIdx+0] = b
			background[dstIdx+1] = g
			background[dstIdx+2] = r
			background[dstIdx+3] = 255
		}
	}

	o.mu.Lock()
	o.cachedBackground = background
	o.cachedReady = isReady
	o.cacheValid = true
	o.mu.Unlock()

	fmt.Printf("[Overlay] 背景缓存已更新 (isReady=%v)\n", isReady)
}

// applyHighlight 只对高亮区域的像素进行混合（高效：只处理选中区域）
func (o *Overlay) applyHighlight(pixelData []byte, imgWidth, imgHeight int, textBlocks []ocr.TextBlock, selectedBlocks []int) {
	// 高亮参数 - 与 Python 保持一致: (77, 148, 255, 77) = #4D94FF with 30% opacity
	hlR, hlG, hlB := uint8(77), uint8(148), uint8(255)
	hlAlpha := float64(77) / 255.0
	hlInvAlpha := 1.0 - hlAlpha

	for _, idx := range selectedBlocks {
		if idx < 0 || idx >= len(textBlocks) {
			continue
		}
		block := textBlocks[idx]

		// 高亮区域（带2像素边距）
		x1, y1 := block.X-2, block.Y-2
		x2, y2 := block.X+block.Width+2, block.Y+block.Height+2

		// 裁剪到图像边界
		if x1 < 0 {
			x1 = 0
		}
		if y1 < 0 {
			y1 = 0
		}
		if x2 > imgWidth {
			x2 = imgWidth
		}
		if y2 > imgHeight {
			y2 = imgHeight
		}

		// 只处理这个矩形区域的像素
		for y := y1; y < y2; y++ {
			dstY := imgHeight - 1 - y // BGRA 自底向上
			for x := x1; x < x2; x++ {
				dstIdx := (dstY*imgWidth + x) * 4

				// 读取当前像素 (BGR)
				b := pixelData[dstIdx+0]
				g := pixelData[dstIdx+1]
				r := pixelData[dstIdx+2]

				// Alpha 混合高亮
				pixelData[dstIdx+0] = uint8(float64(b)*hlInvAlpha + float64(hlB)*hlAlpha)
				pixelData[dstIdx+1] = uint8(float64(g)*hlInvAlpha + float64(hlG)*hlAlpha)
				pixelData[dstIdx+2] = uint8(float64(r)*hlInvAlpha + float64(hlR)*hlAlpha)
			}
		}
	}
}

// drawWaitingText 绘制等待文字 "识别中，请稍后..."
func (o *Overlay) drawWaitingText(hdc uintptr, width, height int) {
	// 创建字体
	fontName, _ := syscall.UTF16PtrFromString("Microsoft YaHei UI")
	hFont, _, _ := procCreateFontW.Call(
		uintptr(40), 0, 0, 0, // 等待文字 40px
		400, 0, 0, 0, // 正常粗细
		1,    // DEFAULT_CHARSET
		0, 0, // OUT_DEFAULT_PRECIS, CLIP_DEFAULT_PRECIS
		0,    // DEFAULT_QUALITY
		0,    // DEFAULT_PITCH
		uintptr(unsafe.Pointer(fontName)),
	)
	defer procDeleteObject.Call(hFont)

	oldFont, _, _ := procSelectObject.Call(hdc, hFont)
	defer procSelectObject.Call(hdc, oldFont)

	// 设置文字颜色为白色，背景透明
	procSetTextColor.Call(hdc, 0xFFFFFF)
	procSetBkMode.Call(hdc, TRANSPARENT_BK)

	// 计算文字位置（屏幕中心）
	text := "识别中，请稍后..."
	textUTF16, _ := syscall.UTF16FromString(text)

	// 计算大致中心位置
	centerX := width / 2 - 100 // 大约文字宽度的一半
	centerY := height / 2 - 16 // 大约文字高度的一半

	procTextOutW.Call(hdc,
		uintptr(centerX),
		uintptr(centerY),
		uintptr(unsafe.Pointer(&textUTF16[0])),
		uintptr(len(textUTF16)-1),
	)
}

// drawTextBlockBorders 绘制文字块边框（调试）
func (o *Overlay) drawTextBlockBorders(hdc uintptr, textBlocks []ocr.TextBlock) {
	pen, _, _ := procCreatePen.Call(PS_SOLID, 1, 0x00FF00) // 绿色边框
	defer procDeleteObject.Call(pen)

	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	defer procSelectObject.Call(hdc, oldPen)

	// 设置透明背景
	procSetBkMode.Call(hdc, TRANSPARENT_BK)

	for _, block := range textBlocks {
		procRectangle.Call(hdc,
			uintptr(block.X),
			uintptr(block.Y),
			uintptr(block.X+block.Width),
			uintptr(block.Y+block.Height),
		)
	}
}

// drawBorder 绘制边框 - 与 Python 版本一致，使用 4 条实心矩形
func (o *Overlay) drawBorder(hdc uintptr, width, height int, isReady bool) {
	var color uint32
	if isReady {
		color = COLOR_BORDER_READY // 完成时绿色
	} else {
		color = COLOR_BORDER_WAITING // 等待时蓝色
	}

	brush, _, _ := procCreateSolidBrush.Call(uintptr(color))
	defer procDeleteObject.Call(brush)

	borderWidth := int32(6) // 边框宽度

	// 上边框
	topRect := RECT{0, 0, int32(width), borderWidth}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&topRect)), brush)

	// 下边框
	bottomRect := RECT{0, int32(height) - borderWidth, int32(width), int32(height)}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&bottomRect)), brush)

	// 左边框
	leftRect := RECT{0, 0, borderWidth, int32(height)}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&leftRect)), brush)

	// 右边框
	rightRect := RECT{int32(width) - borderWidth, 0, int32(width), int32(height)}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rightRect)), brush)
}

// onMouseDown 鼠标按下
func (o *Overlay) onMouseDown(lParam uintptr) {
	x := int32(lParam & 0xFFFF)
	y := int32((lParam >> 16) & 0xFFFF)

	o.mu.Lock()
	isReady := o.isReady
	blockCount := len(o.textBlocks)
	fmt.Printf("[Overlay] 鼠标按下: (%d, %d), isReady=%v, blocks=%d\n", x, y, isReady, blockCount)

	if !isReady {
		o.mu.Unlock()
		return
	}

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

// onMouseUp 鼠标释放 - 与 Python 版本保持一致
func (o *Overlay) onMouseUp(lParam uintptr) {
	procReleaseCapture.Call()

	x := int32(lParam & 0xFFFF)
	y := int32((lParam >> 16) & 0xFFFF)

	o.mu.Lock()
	selecting := o.selecting
	o.selecting = false
	selectedBlocks := o.selectedBlocks
	textBlocks := o.textBlocks
	o.mu.Unlock()

	fmt.Printf("[Overlay] 鼠标释放: (%d, %d), selecting=%v, selected=%d\n", x, y, selecting, len(selectedBlocks))

	if !selecting {
		return
	}

	// 如果没有选中任何文字块，不做任何事（与 Python 一致）
	if len(selectedBlocks) == 0 {
		fmt.Println("[Overlay] 没有选中文字")
		// 不关闭 overlay，用户可以继续选择
		return
	}

	// 合并选中的文字
	text := o.mergeSelectedText(textBlocks, selectedBlocks)
	if text == "" {
		return
	}

	// 复制到剪贴板
	o.copyToClipboard(text)

	// 与 Python 版本一致：选中后不关闭 overlay，只触发翻译
	// overlay 只在快捷键松开或按 ESC 时关闭
	fmt.Println("[Overlay] 已复制，触发翻译回调")

	// 触发回调（异步，避免阻塞窗口线程）
	if o.OnTextSelected != nil {
		go o.OnTextSelected(text, int(x), int(y))
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

	fmt.Printf("[Overlay] 已复制到剪贴板: %s\n", text)
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
