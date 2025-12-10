package main

import (
	"embed"
	"fmt"
	"log"
	"net"
	"syscall"
	"unsafe"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

// 单实例相关常量
const (
	mutexName = "ScreenOCR_SingleInstance_Mutex"
	ipcPort   = "127.0.0.1:19527" // 本地 IPC 端口
)

// Windows API
var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutexW = kernel32.NewProc("CreateMutexW")
)

// checkSingleInstance 检查是否已有实例运行
func checkSingleInstance() (uintptr, bool) {
	name, _ := syscall.UTF16PtrFromString(mutexName)
	handle, _, err := procCreateMutexW.Call(
		0,
		1, // bInitialOwner = TRUE
		uintptr(unsafe.Pointer(name)),
	)

	// ERROR_ALREADY_EXISTS = 183
	if err.(syscall.Errno) == 183 {
		return handle, false // 已有实例运行
	}

	return handle, true // 这是第一个实例
}

// notifyRunningInstance 通知已运行的实例
func notifyRunningInstance() {
	conn, err := net.Dial("tcp", ipcPort)
	if err != nil {
		fmt.Println("无法连接到已运行的实例")
		return
	}
	defer conn.Close()
	conn.Write([]byte("SHOW_TOAST"))
	fmt.Println("已通知运行中的实例")
}

// 全局变量，用于 IPC 回调
var globalApp *App

// startIPCServer 启动 IPC 服务器监听
func startIPCServer() {
	listener, err := net.Listen("tcp", ipcPort)
	if err != nil {
		fmt.Println("IPC 服务器启动失败:", err)
		return
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				continue
			}

			buf := make([]byte, 64)
			n, _ := conn.Read(buf)
			if n > 0 && string(buf[:n]) == "SHOW_TOAST" {
				// 显示 Toast 提示
				if globalApp != nil {
					globalApp.showDuplicateToast()
				}
			}
			conn.Close()
		}
	}()

	fmt.Println("✓ IPC 服务器已启动")
}

func main() {
	// 检查单实例
	mutexHandle, isFirst := checkSingleInstance()
	if !isFirst {
		fmt.Println("ScreenOCR 已在运行中，通知已运行的实例...")
		notifyRunningInstance()
		// 关闭句柄
		if mutexHandle != 0 {
			syscall.CloseHandle(syscall.Handle(mutexHandle))
		}
		return
	}
	// 保持互斥锁句柄直到程序退出
	defer func() {
		if mutexHandle != 0 {
			syscall.CloseHandle(syscall.Handle(mutexHandle))
		}
	}()

	// 启动 IPC 服务器
	startIPCServer()

	// 创建应用实例
	app := NewApp()
	globalApp = app // 保存全局引用

	// 创建 Wails 应用
	err := wails.Run(&options.App{
		Title:     "ScreenOCR",
		Width:     480,
		Height:    600,
		MinWidth:  400,
		MinHeight: 500,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		OnDomReady:       app.domReady,
		OnBeforeClose:    app.beforeClose, // 拦截关闭事件，隐藏到托盘
		Bind: []interface{}{
			app,
		},
		// Windows 特定选项
		Windows: &windows.Options{
			WebviewIsTransparent:              false,
			WindowIsTranslucent:               false,
			DisableWindowIcon:                 false,
			DisableFramelessWindowDecorations: false,
			WebviewUserDataPath:               "",
			ZoomFactor:                        1.0,
		},
		// 启动时隐藏窗口（使用系统托盘）
		StartHidden: true,
	})

	if err != nil {
		log.Fatal("启动应用失败:", err)
	}
}

