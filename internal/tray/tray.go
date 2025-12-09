//go:build windows

package tray

import (
	"fmt"

	"github.com/energye/systray"
)

// SystemTray 系统托盘
type SystemTray struct {
	enabled bool

	// 回调
	OnSettings func()
	OnToggle   func(enabled bool)
	OnQuit     func()

	// 菜单项
	mToggle *systray.MenuItem
}

// NewSystemTray 创建系统托盘
func NewSystemTray() *SystemTray {
	return &SystemTray{
		enabled: true,
	}
}

// Run 运行系统托盘
func (s *SystemTray) Run() {
	systray.Run(s.onReady, s.onExit)
}

// Close 关闭系统托盘
func (s *SystemTray) Close() {
	systray.Quit()
}

// SetEnabled 设置启用状态
func (s *SystemTray) SetEnabled(enabled bool) {
	s.enabled = enabled
	if s.mToggle != nil {
		if enabled {
			s.mToggle.Check()
		} else {
			s.mToggle.Uncheck()
		}
	}
}

// onReady 托盘就绪回调
func (s *SystemTray) onReady() {
	// 设置图标
	systray.SetIcon(getIconData())
	systray.SetTitle("ScreenOCR")
	systray.SetTooltip("ScreenOCR - 屏幕文字识别")

	// 添加菜单项
	mSettings := systray.AddMenuItem("设置", "打开设置窗口")
	s.mToggle = systray.AddMenuItemCheckbox("启用服务", "启用/禁用 OCR 服务", s.enabled)
	systray.AddSeparator()
	mHelp := systray.AddMenuItem("帮助", "查看帮助信息")
	mQuit := systray.AddMenuItem("退出", "退出程序")

	// 使用 SetOnClick 设置点击回调
	mSettings.Click(func() {
		if s.OnSettings != nil {
			s.OnSettings()
		}
	})

	s.mToggle.Click(func() {
		s.enabled = !s.enabled
		if s.enabled {
			s.mToggle.Check()
		} else {
			s.mToggle.Uncheck()
		}
		if s.OnToggle != nil {
			s.OnToggle(s.enabled)
		}
	})

	mHelp.Click(func() {
		fmt.Println("ScreenOCR 使用帮助:")
		fmt.Println("1. 按住快捷键（默认 ALT）等待屏幕出现边框")
		fmt.Println("2. 拖动鼠标选择需要识别的文字区域")
		fmt.Println("3. 松开快捷键完成识别")
	})

	mQuit.Click(func() {
		if s.OnQuit != nil {
			s.OnQuit()
		}
		systray.Quit()
	})

	fmt.Println("✓ 系统托盘已启动")
}

// onExit 托盘退出回调
func (s *SystemTray) onExit() {
	fmt.Println("系统托盘已关闭")
}

// getIconData 获取图标数据（16x16 ICO）
func getIconData() []byte {
	// 简化的蓝色方块图标 (ICO 格式头 + 16x16 32位位图)
	// 实际使用时建议用 embed 嵌入真实的 .ico 文件
	iconData := make([]byte, 0, 1150)

	// ICO 头 (6 bytes)
	iconData = append(iconData, 0, 0) // Reserved
	iconData = append(iconData, 1, 0) // Type: ICO
	iconData = append(iconData, 1, 0) // Count: 1 image

	// ICONDIRENTRY (16 bytes)
	iconData = append(iconData, 16)  // Width
	iconData = append(iconData, 16)  // Height
	iconData = append(iconData, 0)   // Colors (0 = > 256)
	iconData = append(iconData, 0)   // Reserved
	iconData = append(iconData, 1, 0) // Color planes
	iconData = append(iconData, 32, 0) // Bits per pixel
	// Size of image data (will be 1128 = 40 header + 1024 pixels + 64 mask)
	iconData = append(iconData, 0x68, 0x04, 0x00, 0x00)
	// Offset to image data (22 bytes from start)
	iconData = append(iconData, 0x16, 0x00, 0x00, 0x00)

	// BITMAPINFOHEADER (40 bytes)
	iconData = append(iconData, 40, 0, 0, 0) // Header size
	iconData = append(iconData, 16, 0, 0, 0) // Width
	iconData = append(iconData, 32, 0, 0, 0) // Height (2x for mask)
	iconData = append(iconData, 1, 0)        // Planes
	iconData = append(iconData, 32, 0)       // Bits per pixel
	iconData = append(iconData, 0, 0, 0, 0)  // Compression
	iconData = append(iconData, 0, 4, 0, 0)  // Image size
	iconData = append(iconData, 0, 0, 0, 0)  // X pixels per meter
	iconData = append(iconData, 0, 0, 0, 0)  // Y pixels per meter
	iconData = append(iconData, 0, 0, 0, 0)  // Colors used
	iconData = append(iconData, 0, 0, 0, 0)  // Important colors

	// 像素数据 (16x16 BGRA, 从下到上)
	// 蓝色: B=238, G=97, R=67 (#4361EE)
	for i := 0; i < 256; i++ {
		iconData = append(iconData, 238, 97, 67, 255) // BGRA
	}

	// AND mask (16x16 位, 64 bytes) - 全透明
	for i := 0; i < 64; i++ {
		iconData = append(iconData, 0)
	}

	return iconData
}
