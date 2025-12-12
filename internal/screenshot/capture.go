//go:build windows

package screenshot

import (
	"fmt"
	"image"
	"syscall"
	"unsafe"
)

var (
	user32 = syscall.NewLazyDLL("user32.dll")
	gdi32  = syscall.NewLazyDLL("gdi32.dll")
	shcore = syscall.NewLazyDLL("shcore.dll")

	procGetDC                  = user32.NewProc("GetDC")
	procReleaseDC              = user32.NewProc("ReleaseDC")
	procGetSystemMetrics       = user32.NewProc("GetSystemMetrics")
	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection       = gdi32.NewProc("CreateDIBSection")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procGdiFlush               = gdi32.NewProc("GdiFlush")
	procSetProcessDpiAwareness = shcore.NewProc("SetProcessDpiAwareness")
)

const (
	SM_CXSCREEN        = 0
	SM_CYSCREEN        = 1
	SM_XVIRTUALSCREEN  = 76
	SM_YVIRTUALSCREEN  = 77
	SM_CXVIRTUALSCREEN = 78
	SM_CYVIRTUALSCREEN = 79
	SRCCOPY            = 0x00CC0020
	BI_RGB             = 0
)

// BITMAPINFOHEADER Windows 位图信息头
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

// BITMAPINFO Windows 位图信息
type BITMAPINFO struct {
	BmiHeader BITMAPINFOHEADER
	BmiColors [1]uint32
}

// Capturer 屏幕截图器
type Capturer struct {
	dpiAware bool
}

// NewCapturer 创建截图器
func NewCapturer() *Capturer {
	c := &Capturer{}
	c.setDPIAware()
	return c
}

// setDPIAware 设置 DPI 感知
func (c *Capturer) setDPIAware() {
	ret, _, _ := procSetProcessDpiAwareness.Call(2) // PROCESS_PER_MONITOR_DPI_AWARE
	c.dpiAware = ret == 0
}

// CaptureScreen 捕获整个屏幕
func (c *Capturer) CaptureScreen() (image.Image, error) {
	// 获取虚拟屏幕尺寸（支持多显示器）
	x, _, _ := procGetSystemMetrics.Call(SM_XVIRTUALSCREEN)
	y, _, _ := procGetSystemMetrics.Call(SM_YVIRTUALSCREEN)
	width, _, _ := procGetSystemMetrics.Call(SM_CXVIRTUALSCREEN)
	height, _, _ := procGetSystemMetrics.Call(SM_CYVIRTUALSCREEN)

	if width == 0 || height == 0 {
		// 回退到主屏幕
		width, _, _ = procGetSystemMetrics.Call(SM_CXSCREEN)
		height, _, _ = procGetSystemMetrics.Call(SM_CYSCREEN)
		x, y = 0, 0
	}

	return c.captureRect(int(x), int(y), int(width), int(height))
}

// CaptureRect 捕获指定区域（使用 CreateDIBSection，兼容 Win10/Win11）
func (c *Capturer) captureRect(x, y, width, height int) (image.Image, error) {
	// 获取屏幕 DC
	hdc, _, _ := procGetDC.Call(0)
	if hdc == 0 {
		return nil, fmt.Errorf("获取屏幕 DC 失败")
	}

	// 创建兼容 DC
	hdcMem, _, _ := procCreateCompatibleDC.Call(hdc)
	if hdcMem == 0 {
		procReleaseDC.Call(0, hdc)
		return nil, fmt.Errorf("创建兼容 DC 失败")
	}

	// 准备位图信息（使用负高度，从上到下，避免翻转）
	bi := BITMAPINFO{
		BmiHeader: BITMAPINFOHEADER{
			BiSize:        uint32(unsafe.Sizeof(BITMAPINFOHEADER{})),
			BiWidth:       int32(width),
			BiHeight:      -int32(height), // 负值 = 从上到下
			BiPlanes:      1,
			BiBitCount:    32,
			BiCompression: BI_RGB,
		},
	}

	// 创建 DIB Section（直接获取像素指针）
	var pBits uintptr
	hBitmap, _, _ := procCreateDIBSection.Call(
		hdc,
		uintptr(unsafe.Pointer(&bi)),
		0, // DIB_RGB_COLORS
		uintptr(unsafe.Pointer(&pBits)),
		0,
		0,
	)
	if hBitmap == 0 || pBits == 0 {
		procDeleteDC.Call(hdcMem)
		procReleaseDC.Call(0, hdc)
		return nil, fmt.Errorf("创建 DIB Section 失败")
	}

	// 选择位图到内存 DC
	oldBitmap, _, _ := procSelectObject.Call(hdcMem, hBitmap)

	// 复制屏幕内容到 DIB
	ret, _, _ := procBitBlt.Call(
		hdcMem, 0, 0, uintptr(width), uintptr(height),
		hdc, uintptr(x), uintptr(y),
		SRCCOPY,
	)
	if ret == 0 {
		procSelectObject.Call(hdcMem, oldBitmap)
		procDeleteObject.Call(hBitmap)
		procDeleteDC.Call(hdcMem)
		procReleaseDC.Call(0, hdc)
		return nil, fmt.Errorf("BitBlt 失败")
	}

	// 确保 GDI 操作完成，数据写入 pBits
	procGdiFlush.Call()

	// 直接从 pBits 读取像素数据
	dataSize := width * height * 4
	data := unsafe.Slice((*byte)(unsafe.Pointer(pBits)), dataSize)

	// 创建 RGBA 图像并复制数据 (BGRA -> RGBA)
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < width*height; i++ {
		offset := i * 4
		img.Pix[offset+0] = data[offset+2] // R
		img.Pix[offset+1] = data[offset+1] // G
		img.Pix[offset+2] = data[offset+0] // B
		img.Pix[offset+3] = 255            // A
	}

	// 清理资源
	procSelectObject.Call(hdcMem, oldBitmap)
	procDeleteObject.Call(hBitmap)
	procDeleteDC.Call(hdcMem)
	procReleaseDC.Call(0, hdc)

	return img, nil
}

// GetScreenSize 获取屏幕尺寸
func (c *Capturer) GetScreenSize() (width, height int) {
	w, _, _ := procGetSystemMetrics.Call(SM_CXSCREEN)
	h, _, _ := procGetSystemMetrics.Call(SM_CYSCREEN)
	return int(w), int(h)
}
