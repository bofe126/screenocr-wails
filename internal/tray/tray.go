//go:build windows

package tray

import (
	_ "embed"
	"fmt"

	"github.com/energye/systray"
)

//go:embed icon.ico
var iconData []byte

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

	// 设置左键点击回调（与右键菜单"设置"相同功能）
	systray.SetOnClick(func(menu systray.IMenu) {
		if s.OnSettings != nil {
			s.OnSettings()
		}
	})

	// 设置右键点击回调（显示菜单）
	systray.SetOnRClick(func(menu systray.IMenu) {
		menu.ShowMenu()
	})

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

// getIconData 获取图标数据（使用嵌入的 icon.ico）
func getIconData() []byte {
	return iconData
}
