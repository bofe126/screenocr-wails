//go:build windows && cgo

package ocr

/*
#cgo windows CFLAGS: -I.

#include <stdlib.h>
#include <string.h>
#include <windows.h>

// 动态加载 DLL 并调用 wechat_ocr 函数
static const char* call_wechat_ocr(const char* dll_path, const char* wechatocr_path, const char* wechat_path, const char* image_path) {
    HMODULE hModule = LoadLibraryA(dll_path);
    if (hModule == NULL) {
        return NULL;
    }
    
    // 获取函数地址
    typedef const char* (*WeChatOCRFunc)(const char*, const char*, const char*);
    WeChatOCRFunc wechat_ocr = (WeChatOCRFunc)GetProcAddress(hModule, "wechat_ocr");
    
    if (wechat_ocr == NULL) {
        FreeLibrary(hModule);
        return NULL;
    }
    
    // 调用函数
    const char* result = wechat_ocr(wechatocr_path, wechat_path, image_path);
    
    // 注意：不要立即释放 DLL，因为返回的字符串可能指向 DLL 内部的内存
    // 在实际使用中，应该复制返回的字符串
    // FreeLibrary(hModule);
    
    return result;
}

// 释放 DLL
static void free_wechat_dll(const char* dll_path) {
    HMODULE hModule = GetModuleHandleA(dll_path);
    if (hModule != NULL) {
        FreeLibrary(hModule);
    }
}
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"unsafe"
)

// WeChatOCRCGO 微信 OCR 引擎（使用 CGO 调用）
type WeChatOCRCGO struct {
	available     bool
	errorMsg      string
	dllPath       string // wcocr.dll 路径
	wechatOCRPath string // WeChatOCR.exe 或 wxocr.dll 路径
	wechatPath    string // 微信运行时目录
}

// NewWeChatOCRCGO 创建 WeChatOCR 实例（CGO 版本）
func NewWeChatOCRCGO() *WeChatOCRCGO {
	ocr := &WeChatOCRCGO{}
	ocr.init()
	return ocr
}

// findWcocrDLL 查找 wcocr.dll
func (w *WeChatOCRCGO) findWcocrDLL() string {
	// 方法1: 程序目录
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		dllPath := filepath.Join(exeDir, "wcocr.dll")
		if _, err := os.Stat(dllPath); err == nil {
			return dllPath
		}
	}

	// 方法2: 当前工作目录
	if wd, err := os.Getwd(); err == nil {
		dllPath := filepath.Join(wd, "wcocr.dll")
		if _, err := os.Stat(dllPath); err == nil {
			return dllPath
		}
	}

	return ""
}

// findWeChatOCRBinary 查找微信 OCR 组件（复用 syscall 版本的逻辑）
func (w *WeChatOCRCGO) findWeChatOCRBinary() (string, string) {
	// 复用 syscall 版本的查找逻辑
	// 这里简化处理，直接调用 syscall 版本的函数
	// 或者可以复制查找逻辑
	appdata := os.Getenv("APPDATA")
	if appdata == "" {
		return "", ""
	}

	// 微信 3.x 基础路径
	wechat3BasePaths := []string{
		filepath.Join(appdata, "Tencent", "WeChat", "XPlugin", "Plugins", "WeChatOCR"),
	}

	// 微信 4.0 基础路径
	wechat4BasePaths := []string{
		filepath.Join(appdata, "Tencent", "xwechat", "XPlugin", "plugins", "WeChatOcr"),
		filepath.Join(appdata, "Tencent", "WeChat", "XPlugin", "Plugins", "WeChatOcr"),
	}

	// 检查微信 3.x
	for _, basePath := range wechat3BasePaths {
		if ocrPath, runtimePath := w.searchWeChatOCR3(basePath); ocrPath != "" {
			return ocrPath, runtimePath
		}
	}

	// 检查微信 4.0
	for _, basePath := range wechat4BasePaths {
		if ocrPath, runtimePath := w.searchWeChatOCR4(basePath); ocrPath != "" {
			return ocrPath, runtimePath
		}
	}

	return "", ""
}

// findWeChatRuntimePath 查找微信运行时目录
func (w *WeChatOCRCGO) findWeChatRuntimePath(version string) string {
	if version == "4.0" {
		paths := []string{
			`C:\Program Files\Tencent\Weixin`,
			`C:\Program Files (x86)\Tencent\Weixin`,
		}
		for _, basePath := range paths {
			entries, err := os.ReadDir(basePath)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if entry.IsDir() && len(entry.Name()) > 0 {
					fullPath := filepath.Join(basePath, entry.Name())
					if _, err := os.Stat(filepath.Join(fullPath, "WeChatApp.exe")); err == nil {
						return fullPath
					}
				}
			}
		}
	} else {
		paths := []string{
			`C:\Program Files\Tencent\WeChat`,
			`C:\Program Files (x86)\Tencent\WeChat`,
		}
		for _, basePath := range paths {
			entries, err := os.ReadDir(basePath)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if entry.IsDir() && len(entry.Name()) > 0 {
					fullPath := filepath.Join(basePath, entry.Name())
					if _, err := os.Stat(filepath.Join(fullPath, "WeChat.exe")); err == nil {
						return fullPath
					}
				}
			}
		}
	}
	return ""
}

// searchWeChatOCR3 递归搜索微信 3.x OCR 组件
func (w *WeChatOCRCGO) searchWeChatOCR3(basePath string) (string, string) {
	if _, err := os.Stat(basePath); err != nil {
		return "", ""
	}

	entries, err := os.ReadDir(basePath)
	if err != nil {
		return "", ""
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		extractedPath := filepath.Join(basePath, entry.Name(), "extracted", "WeChatOCR.exe")
		if _, err := os.Stat(extractedPath); err == nil {
			runtimePath := w.findWeChatRuntimePath("3.x")
			return extractedPath, runtimePath
		}
	}

	return "", ""
}

// searchWeChatOCR4 递归搜索微信 4.0 OCR 组件
func (w *WeChatOCRCGO) searchWeChatOCR4(basePath string) (string, string) {
	if _, err := os.Stat(basePath); err != nil {
		return "", ""
	}

	entries, err := os.ReadDir(basePath)
	if err != nil {
		return "", ""
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		extractedPath := filepath.Join(basePath, entry.Name(), "extracted", "wxocr.dll")
		if _, err := os.Stat(extractedPath); err == nil {
			runtimePath := w.findWeChatRuntimePath("4.0")
			return extractedPath, runtimePath
		}
	}

	return "", ""
}

// init 初始化
func (w *WeChatOCRCGO) init() {
	// 1. 查找 wcocr.dll
	dllPath := w.findWcocrDLL()
	if dllPath == "" {
		w.errorMsg = "未找到 wcocr.dll，请从 https://github.com/swigger/wechat-ocr 下载并编译，将 wcocr.dll 放在程序目录"
		fmt.Println("⚠ WeChatOCR (CGO): " + w.errorMsg)
		return
	}

	// 2. 查找微信 OCR 组件和运行时目录
	ocrPath, runtimePath := w.findWeChatOCRBinary()
	if ocrPath == "" {
		w.errorMsg = "未找到微信 OCR 组件，请确保已安装微信"
		fmt.Println("⚠ WeChatOCR (CGO): " + w.errorMsg)
		return
	}

	w.dllPath = dllPath
	w.wechatOCRPath = ocrPath
	w.wechatPath = runtimePath

	w.available = true
	fmt.Printf("✓ WeChat OCR (CGO) 初始化完成\n")
	fmt.Printf("  wcocr.dll: %s\n", dllPath)
	fmt.Printf("  微信 OCR 组件: %s\n", ocrPath)
	fmt.Printf("  微信运行时目录: %s\n", runtimePath)
}

// IsAvailable 检查是否可用
func (w *WeChatOCRCGO) IsAvailable() bool {
	return w.available
}

// Recognize 识别图片
func (w *WeChatOCRCGO) Recognize(img image.Image, preprocess bool) ([]TextBlock, error) {
	if !w.available {
		return nil, fmt.Errorf("WeChatOCR 不可用: %s", w.errorMsg)
	}

	// 预处理
	if preprocess {
		img = preprocessImage(img)
	}

	// 保存临时文件
	tmpFile, err := os.CreateTemp("", "wechat_ocr_*.png")
	if err != nil {
		return nil, fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if err := png.Encode(tmpFile, img); err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("保存图片失败: %w", err)
	}
	tmpFile.Close()

	// 调用 OCR
	return w.recognizeImage(tmpPath)
}

// recognizeImage 识别图片（使用 CGO 调用）
func (w *WeChatOCRCGO) recognizeImage(imagePath string) ([]TextBlock, error) {
	// 确保图片文件存在
	if _, err := os.Stat(imagePath); err != nil {
		return nil, fmt.Errorf("图片文件不存在: %w", err)
	}

	// 转换为绝对路径
	absWeChatOCRPath, _ := filepath.Abs(w.wechatOCRPath)
	absWeChatPath, _ := filepath.Abs(w.wechatPath)
	absImagePath, _ := filepath.Abs(imagePath)

	// 转换为 C 字符串
	cDllPath := C.CString(w.dllPath)
	cWeChatOCRPath := C.CString(absWeChatOCRPath)
	cWeChatPath := C.CString(absWeChatPath)
	cImagePath := C.CString(absImagePath)
	defer C.free(unsafe.Pointer(cDllPath))
	defer C.free(unsafe.Pointer(cWeChatOCRPath))
	defer C.free(unsafe.Pointer(cWeChatPath))
	defer C.free(unsafe.Pointer(cImagePath))

	fmt.Printf("调用 wechat_ocr (CGO):\n")
	fmt.Printf("  dll_path: %s\n", w.dllPath)
	fmt.Printf("  wechatocr_path: %s\n", absWeChatOCRPath)
	fmt.Printf("  wechat_path: %s\n", absWeChatPath)
	fmt.Printf("  image_path: %s\n", absImagePath)

	// 调用 C 函数（动态加载 DLL）
	resultPtr := C.call_wechat_ocr(cDllPath, cWeChatOCRPath, cWeChatPath, cImagePath)

	if resultPtr == nil {
		return nil, fmt.Errorf("wechat_ocr 返回空指针（可能是 DLL 加载失败或函数调用失败）")
	}

	// 将 C 字符串转换为 Go 字符串（需要复制，因为 DLL 可能被释放）
	resultStr := C.GoString(resultPtr)

	fmt.Printf("OCR 结果长度: %d 字节\n", len(resultStr))
	if len(resultStr) < 200 {
		fmt.Printf("OCR 结果预览: %s\n", resultStr)
	}

	if resultStr == "" {
		return nil, fmt.Errorf("wechat_ocr 返回空字符串")
	}

	// 解析 JSON 结果
	var blocks []struct {
		Text   string `json:"text"`
		X      int    `json:"x"`
		Y      int    `json:"y"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}

	if err := json.Unmarshal([]byte(resultStr), &blocks); err != nil {
		return nil, fmt.Errorf("解析 OCR 结果失败: %w, 原始结果: %s", err, resultStr)
	}

	// 转换为 TextBlock
	textBlocks := make([]TextBlock, 0, len(blocks))
	for _, block := range blocks {
		textBlocks = append(textBlocks, TextBlock{
			Text:   block.Text,
			X:      block.X,
			Y:      block.Y,
			Width:  block.Width,
			Height: block.Height,
		})
	}

	return textBlocks, nil
}

// GetError 获取错误信息
func (w *WeChatOCRCGO) GetError() string {
	return w.errorMsg
}

// Close 关闭
func (w *WeChatOCRCGO) Close() {
	// CGO 版本不需要特殊清理
}

