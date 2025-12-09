//go:build windows

package screenshot

import (
	"fmt"
	"image"
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	shcore   = syscall.NewLazyDLL("shcore.dll")

	procGetDC             = user32.NewProc("GetDC")
	procReleaseDC         = user32.NewProc("ReleaseDC")
	procGetSystemMetrics  = user32.NewProc("GetSystemMetrics")
	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject      = gdi32.NewProc("SelectObject")
	procBitBlt            = gdi32.NewProc("BitBlt")
	procDeleteDC          = gdi32.NewProc("DeleteDC")
	procDeleteObject      = gdi32.NewProc("DeleteObject")
	procGetDIBits         = gdi32.NewProc("GetDIBits")
	procSetProcessDpiAwareness = shcore.NewProc("SetProcessDpiAwareness")
)

const (
	SM_CXSCREEN = 0
	SM_CYSCREEN = 1
	SM_XVIRTUALSCREEN = 76
	SM_YVIRTUALSCREEN = 77
	SM_CXVIRTUALSCREEN = 78
	SM_CYVIRTUALSCREEN = 79
	SRCCOPY = 0x00CC0020
	BI_RGB  = 0
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

// CaptureRect 捕获指定区域
func (c *Capturer) captureRect(x, y, width, height int) (image.Image, error) {
	// 获取屏幕 DC
	hdc, _, _ := procGetDC.Call(0)
	if hdc == 0 {
		return nil, fmt.Errorf("获取屏幕 DC 失败")
	}
	defer procReleaseDC.Call(0, hdc)

	// 创建兼容 DC
	hdcMem, _, _ := procCreateCompatibleDC.Call(hdc)
	if hdcMem == 0 {
		return nil, fmt.Errorf("创建兼容 DC 失败")
	}
	defer procDeleteDC.Call(hdcMem)

	// 创建兼容位图
	hBitmap, _, _ := procCreateCompatibleBitmap.Call(hdc, uintptr(width), uintptr(height))
	if hBitmap == 0 {
		return nil, fmt.Errorf("创建兼容位图失败")
	}
	defer procDeleteObject.Call(hBitmap)

	// 选择位图到 DC
	procSelectObject.Call(hdcMem, hBitmap)

	// 复制屏幕内容
	ret, _, _ := procBitBlt.Call(
		hdcMem, 0, 0, uintptr(width), uintptr(height),
		hdc, uintptr(x), uintptr(y),
		SRCCOPY,
	)
	if ret == 0 {
		return nil, fmt.Errorf("BitBlt 失败")
	}

	// 获取位图数据
	return c.bitmapToImage(hdc, hBitmap, width, height)
}

// bitmapToImage 将 Windows 位图转换为 Go image
func (c *Capturer) bitmapToImage(hdc, hBitmap uintptr, width, height int) (image.Image, error) {
	// 准备位图信息
	bi := BITMAPINFO{
		BmiHeader: BITMAPINFOHEADER{
			BiSize:        uint32(unsafe.Sizeof(BITMAPINFOHEADER{})),
			BiWidth:       int32(width),
			BiHeight:      -int32(height), // 负值表示从上到下
			BiPlanes:      1,
			BiBitCount:    32,
			BiCompression: BI_RGB,
		},
	}

	// 分配缓冲区
	dataSize := width * height * 4
	data := make([]byte, dataSize)

	// 获取位图数据
	ret, _, _ := procGetDIBits.Call(
		hdc,
		hBitmap,
		0,
		uintptr(height),
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(unsafe.Pointer(&bi)),
		0, // DIB_RGB_COLORS
	)
	if ret == 0 {
		return nil, fmt.Errorf("GetDIBits 失败")
	}

	// 创建 RGBA 图像
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// 复制数据 (BGRA -> RGBA)
	for i := 0; i < width*height; i++ {
		offset := i * 4
		img.Pix[offset+0] = data[offset+2] // R
		img.Pix[offset+1] = data[offset+1] // G
		img.Pix[offset+2] = data[offset+0] // B
		img.Pix[offset+3] = 255            // A
	}

	return img, nil
}

// GetScreenSize 获取屏幕尺寸
func (c *Capturer) GetScreenSize() (width, height int) {
	w, _, _ := procGetSystemMetrics.Call(SM_CXSCREEN)
	h, _, _ := procGetSystemMetrics.Call(SM_CYSCREEN)
	return int(w), int(h)
}

