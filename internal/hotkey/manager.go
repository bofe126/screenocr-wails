//go:build windows

package hotkey

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32              = syscall.NewLazyDLL("user32.dll")
	kernel32            = syscall.NewLazyDLL("kernel32.dll")
	procSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procPeekMessageW        = user32.NewProc("PeekMessageW")
	procGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
)

const (
	WH_KEYBOARD_LL = 13
	WM_KEYDOWN     = 0x0100
	WM_KEYUP       = 0x0101
	WM_SYSKEYDOWN  = 0x0104
	WM_SYSKEYUP    = 0x0105
	PM_REMOVE      = 0x0001
)

// 虚拟键码映射
var keyCodeMap = map[string][]uint32{
	"ctrl":  {162, 163}, // 左右 CTRL
	"alt":   {164, 165}, // 左右 ALT
	"shift": {160, 161}, // 左右 SHIFT
	"win":   {91, 92},   // 左右 WIN
	"f1":    {112}, "f2": {113}, "f3": {114}, "f4": {115},
	"f5": {116}, "f6": {117}, "f7": {118}, "f8": {119},
	"f9": {120}, "f10": {121}, "f11": {122}, "f12": {123},
	"space": {32}, "tab": {9}, "enter": {13}, "esc": {27},
	// 字母键
	"a": {65}, "b": {66}, "c": {67}, "d": {68}, "e": {69},
	"f": {70}, "g": {71}, "h": {72}, "i": {73}, "j": {74},
	"k": {75}, "l": {76}, "m": {77}, "n": {78}, "o": {79},
	"p": {80}, "q": {81}, "r": {82}, "s": {83}, "t": {84},
	"u": {85}, "v": {86}, "w": {87}, "x": {88}, "y": {89}, "z": {90},
}

// 键码到名称的反向映射
var keyNameMap = map[uint32]string{
	162: "LCTRL", 163: "RCTRL",
	164: "LALT", 165: "RALT",
	160: "LSHIFT", 161: "RSHIFT",
	91: "LWIN", 92: "RWIN",
}

// KBDLLHOOKSTRUCT 键盘钩子结构
type KBDLLHOOKSTRUCT struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

// MSG Windows 消息结构
type MSG struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

// Manager 热键管理器
type Manager struct {
	hotkey       string
	delayMs      int
	hookID       uintptr
	running      bool
	mu           sync.RWMutex
	pressedKeys  map[uint32]bool
	pressTime    time.Time
	triggered    bool

	OnTrigger    func() // 触发回调
	OnKeyRelease func() // 按键松开回调（用于关闭覆盖层）
	OnEscape     func() // ESC 键回调（全局，与 Python 一致）
}

// NewManager 创建热键管理器
func NewManager(hotkey string, delayMs int) *Manager {
	fmt.Printf("[热键] 创建管理器: hotkey=%s, delay=%dms\n", hotkey, delayMs)
	return &Manager{
		hotkey:      strings.ToLower(hotkey),
		delayMs:     delayMs,
		pressedKeys: make(map[uint32]bool),
	}
}

// Start 启动热键监听
func (m *Manager) Start() error {
	// 锁定到当前 OS 线程，Win32 钩子必须在同一线程处理消息
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		fmt.Println("[热键] 已经在运行中")
		return nil
	}
	m.running = true
	m.mu.Unlock()

	fmt.Println("[热键] 开始启动...")

	// 获取模块句柄
	moduleHandle, _, _ := procGetModuleHandleW.Call(0)
	fmt.Printf("[热键] 模块句柄: %d\n", moduleHandle)

	// 设置键盘钩子
	hookProc := syscall.NewCallback(m.keyboardProc)
	fmt.Println("[热键] 设置键盘钩子...")
	
	hookID, _, err := procSetWindowsHookExW.Call(
		WH_KEYBOARD_LL,
		hookProc,
		moduleHandle,
		0,
	)

	if hookID == 0 {
		fmt.Printf("[热键] ❌ 设置键盘钩子失败: %v\n", err)
		return fmt.Errorf("设置键盘钩子失败: %v", err)
	}

	m.hookID = hookID
	fmt.Printf("[热键] ✓ 键盘钩子已安装, hookID=%d\n", hookID)
	fmt.Printf("[热键] 监听热键: %s (延迟 %dms)\n", m.hotkey, m.delayMs)
	fmt.Printf("[热键] 目标键码: %v\n", m.getHotkeyCodes())

	// 消息循环
	fmt.Println("[热键] 进入消息循环...")
	var msg MSG
	for m.running {
		// 使用 PeekMessage 非阻塞检查消息
		ret, _, _ := procPeekMessageW.Call(
			uintptr(unsafe.Pointer(&msg)),
			0, 0, 0,
			PM_REMOVE,
		)
		
		if ret != 0 {
			// 有消息，处理它
		}

		// 检查是否需要触发
		m.checkTrigger()
		
		// 短暂休眠避免 CPU 占用过高
		time.Sleep(10 * time.Millisecond)
	}

	fmt.Println("[热键] 消息循环结束")
	return nil
}

// Stop 停止热键监听
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	fmt.Println("[热键] 停止监听...")
	m.running = false
	if m.hookID != 0 {
		ret, _, _ := procUnhookWindowsHookEx.Call(m.hookID)
		fmt.Printf("[热键] 卸载钩子结果: %d\n", ret)
		m.hookID = 0
		fmt.Println("[热键] ✓ 键盘钩子已卸载")
	}
}

// UpdateHotkey 更新热键配置
func (m *Manager) UpdateHotkey(hotkey string, delayMs int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	fmt.Printf("[热键] 更新配置: %s -> %s, delay=%dms\n", m.hotkey, hotkey, delayMs)
	m.hotkey = strings.ToLower(hotkey)
	m.delayMs = delayMs
	m.pressedKeys = make(map[uint32]bool)
	m.triggered = false
}

const VK_ESCAPE = 0x1B // ESC 键码

// keyboardProc 键盘钩子回调
func (m *Manager) keyboardProc(nCode int, wParam uintptr, lParam uintptr) uintptr {
	if nCode >= 0 {
		kb := (*KBDLLHOOKSTRUCT)(unsafe.Pointer(lParam))
		vkCode := kb.VkCode

		// 获取键名用于日志
		keyName := keyNameMap[vkCode]
		if keyName == "" {
			keyName = fmt.Sprintf("0x%X", vkCode)
		}

		// ⭐ 全局 ESC 键处理（与 Python 一致）
		if vkCode == VK_ESCAPE && wParam == WM_KEYDOWN {
			fmt.Println("[热键] ESC 键按下，触发全局关闭")
			m.mu.Lock()
			m.pressTime = time.Time{}
			m.triggered = false
			m.mu.Unlock()
			if m.OnEscape != nil {
				go m.OnEscape()
			}
		}

		// 获取配置的热键键码
		targetCodes := m.getHotkeyCodes()

		// 检查是否是目标键
		isTargetKey := false
		for _, code := range targetCodes {
			if vkCode == code {
				isTargetKey = true
				break
			}
		}

		switch wParam {
		case WM_KEYDOWN, WM_SYSKEYDOWN:
			if isTargetKey {
				m.mu.Lock()
				wasPressed := m.pressedKeys[vkCode]
				if !wasPressed {
					m.pressedKeys[vkCode] = true
					fmt.Printf("[热键] 按下: %s (vk=%d) [目标键]\n", keyName, vkCode)
					
					// 检查是否所有键都按下
					allPressed := m.allKeysPressed(targetCodes)
					fmt.Printf("[热键] 已按下的键: %v, 全部按下: %v\n", m.pressedKeys, allPressed)
					
					if allPressed && m.pressTime.IsZero() {
						m.pressTime = time.Now()
						m.triggered = false
						fmt.Printf("[热键] ⏱ 开始计时，延迟 %dms 后触发\n", m.delayMs)
					}
				}
				m.mu.Unlock()
			}

		case WM_KEYUP, WM_SYSKEYUP:
			if isTargetKey {
				m.mu.Lock()
				wasPressed := m.pressedKeys[vkCode]
				wasTriggered := m.triggered
				if wasPressed {
					delete(m.pressedKeys, vkCode)
					fmt.Printf("[热键] 释放: %s (vk=%d)\n", keyName, vkCode)
					
					// 重置计时
					if !m.pressTime.IsZero() {
						elapsed := time.Since(m.pressTime).Milliseconds()
						fmt.Printf("[热键] 按住时间: %dms, 已触发: %v\n", elapsed, m.triggered)
					}
					m.pressTime = time.Time{}
					m.triggered = false
				}
				m.mu.Unlock()

				// ⭐ 关键：如果之前已触发，按键松开时关闭覆盖层
				if wasTriggered && m.OnKeyRelease != nil {
					fmt.Println("[热键] 📤 按键松开，触发关闭覆盖层")
					go m.OnKeyRelease()
				}
			}
		}
	}

	ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
	return ret
}

// getHotkeyCodes 获取热键对应的键码
func (m *Manager) getHotkeyCodes() []uint32 {
	var codes []uint32
	parts := strings.Split(m.hotkey, "+")
	for _, part := range parts {
		part = strings.TrimSpace(strings.ToLower(part))
		if keyCodes, ok := keyCodeMap[part]; ok {
			codes = append(codes, keyCodes...)
		}
	}
	return codes
}

// allKeysPressed 检查是否所有键都按下
func (m *Manager) allKeysPressed(targetCodes []uint32) bool {
	parts := strings.Split(m.hotkey, "+")
	for _, part := range parts {
		part = strings.TrimSpace(strings.ToLower(part))
		if keyCodes, ok := keyCodeMap[part]; ok {
			pressed := false
			for _, code := range keyCodes {
				if m.pressedKeys[code] {
					pressed = true
					break
				}
			}
			if !pressed {
				return false
			}
		}
	}
	return true
}

// checkTrigger 检查是否触发
func (m *Manager) checkTrigger() {
	m.mu.RLock()
	pressTime := m.pressTime
	triggered := m.triggered
	delayMs := m.delayMs
	m.mu.RUnlock()

	if pressTime.IsZero() || triggered {
		return
	}

	// 检查是否达到延迟时间
	elapsed := time.Since(pressTime).Milliseconds()
	if elapsed >= int64(delayMs) {
		m.mu.Lock()
		if !m.triggered { // 再次检查避免重复触发
			m.triggered = true
			m.mu.Unlock()

			fmt.Printf("[热键] 🎯 触发! 延迟 %dms 已到\n", elapsed)

			// 触发回调
			if m.OnTrigger != nil {
				go m.OnTrigger()
			}
		} else {
			m.mu.Unlock()
		}
	}
}
