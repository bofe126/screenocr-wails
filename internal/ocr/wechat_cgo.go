//go:build windows && cgo

package ocr

/*
#cgo windows CFLAGS: -I.
#cgo windows LDFLAGS: -L${SRCDIR}/../../assets/libs -lwcocr -lprotobuf-lite -lstdc++ -lws2_32 -lole32 -loleaut32 -luuid -lshlwapi -ladvapi32

#include <stdlib.h>
#include <string.h>
#include <windows.h>

// wcocr.lib 导出的函数声明
extern int wechat_ocr(const wchar_t* ocr_exe, const wchar_t* wechat_dir, const char* imgfn, void (*callback)(const char*));
extern void stop_ocr();

// 存储 OCR 结果
static char* g_ocr_result = NULL;
static int g_ocr_done = 0;
static DWORD g_last_error = 0;

// OCR 结果回调
static void ocr_result_callback(const char* result) {
    if (g_ocr_result != NULL) {
        free(g_ocr_result);
        g_ocr_result = NULL;
    }
    if (result != NULL) {
        size_t len = strlen(result);
        g_ocr_result = (char*)malloc(len + 1);
        if (g_ocr_result != NULL) {
            strcpy(g_ocr_result, result);
        }
    }
    g_ocr_done = 1;
}

// UTF-8 转 UTF-16 (宽字符)
static wchar_t* utf8_to_wchar(const char* utf8) {
    if (utf8 == NULL) return NULL;
    int len = MultiByteToWideChar(CP_UTF8, 0, utf8, -1, NULL, 0);
    if (len == 0) return NULL;
    wchar_t* wstr = (wchar_t*)malloc(len * sizeof(wchar_t));
    if (wstr == NULL) return NULL;
    MultiByteToWideChar(CP_UTF8, 0, utf8, -1, wstr, len);
    return wstr;
}

// 调用 wechat_ocr（静态链接版本）
static const char* call_wechat_ocr_static(const char* ocr_exe_path, const char* wechat_dir, const char* image_path) {
    // 重置状态
    g_ocr_done = 0;
    if (g_ocr_result != NULL) {
        free(g_ocr_result);
        g_ocr_result = NULL;
    }

    // 转换路径为宽字符（Windows 需要）
    wchar_t* w_ocr_exe = utf8_to_wchar(ocr_exe_path);
    wchar_t* w_wechat_dir = utf8_to_wchar(wechat_dir);

    if (w_ocr_exe == NULL || w_wechat_dir == NULL) {
        if (w_ocr_exe) free(w_ocr_exe);
        if (w_wechat_dir) free(w_wechat_dir);
        g_last_error = ERROR_OUTOFMEMORY;
        return NULL;
    }

    // 调用静态链接的 wechat_ocr
    int success = wechat_ocr(w_ocr_exe, w_wechat_dir, image_path, ocr_result_callback);

    free(w_ocr_exe);
    free(w_wechat_dir);

    if (!success) {
        g_last_error = GetLastError();
        return NULL;
    }

    // 等待回调完成（最多等待 30 秒）
    int wait_count = 0;
    while (!g_ocr_done && wait_count < 300) {
        Sleep(100);
        wait_count++;
    }

    if (!g_ocr_done) {
        g_last_error = ERROR_TIMEOUT;
        return NULL;
    }

    return g_ocr_result;
}

// 获取错误码
static DWORD get_last_wcocr_error() {
    return g_last_error;
}

// 停止 OCR
static void wcocr_stop() {
    stop_ocr();
    if (g_ocr_result != NULL) {
        free(g_ocr_result);
        g_ocr_result = NULL;
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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

// WeChatOCRCGO 微信 OCR 引擎（使用 CGO 静态链接）
type WeChatOCRCGO struct {
	available     bool
	initialized   bool
	errorMsg      string
	wechatOCRPath string // WeChatOCR.exe 或 wxocr.dll 路径
	wechatPath    string // 微信运行时目录
	isWeChat4     bool   // 是否是微信 4.0
}

// ocrCandidate OCR 组件候选
type ocrCandidate struct {
	path    string
	version int
}

// NewWeChatOCRCGO 创建 WeChatOCR 实例（CGO 版本）
func NewWeChatOCRCGO() *WeChatOCRCGO {
	ocr := &WeChatOCRCGO{}
	ocr.init()
	return ocr
}

// getWeChatFromRegistry 从注册表获取微信安装路径
func (w *WeChatOCRCGO) getWeChatFromRegistry() []string {
	var paths []string

	registryKeys := []struct {
		root   registry.Key
		subkey string
	}{
		{registry.CURRENT_USER, `Software\Tencent\WeChat`},
		{registry.LOCAL_MACHINE, `Software\Tencent\WeChat`},
		{registry.LOCAL_MACHINE, `Software\WOW6432Node\Tencent\WeChat`},
	}

	for _, rk := range registryKeys {
		key, err := registry.OpenKey(rk.root, rk.subkey, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		installPath, _, err := key.GetStringValue("InstallPath")
		key.Close()
		if err == nil && installPath != "" {
			found := false
			for _, p := range paths {
				if strings.EqualFold(p, installPath) {
					found = true
					break
				}
			}
			if !found {
				paths = append(paths, installPath)
			}
		}
	}

	return paths
}

// parseVersion 解析版本号字符串为数字
func (w *WeChatOCRCGO) parseVersion(versionStr string) int {
	// 纯数字版本号
	if v, err := strconv.Atoi(versionStr); err == nil {
		return v
	}

	// 带点的版本号（如 3.9.10.19）
	versionPattern := regexp.MustCompile(`^\d+(\.\d+)*$`)
	if versionPattern.MatchString(versionStr) {
		parts := strings.Split(versionStr, ".")
		result := 0
		for i, p := range parts {
			if i >= 4 {
				break
			}
			v, _ := strconv.Atoi(p)
			result = result*1000 + v
		}
		return result
	}

	return -1
}

// scanOCRDirectory 扫描 OCR 目录，返回所有找到的 OCR 文件
func (w *WeChatOCRCGO) scanOCRDirectory(basePath string) []ocrCandidate {
	var candidates []ocrCandidate

	if _, err := os.Stat(basePath); err != nil {
		return candidates
	}

	entries, err := os.ReadDir(basePath)
	if err != nil {
		return candidates
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		versionStr := entry.Name()
		versionNum := w.parseVersion(versionStr)
		if versionNum < 0 {
			continue
		}

		// 检查可能的文件位置
		possibleFiles := []string{
			filepath.Join(basePath, versionStr, "extracted", "WeChatOCR.exe"),
			filepath.Join(basePath, versionStr, "WeChatOCR.exe"),
			filepath.Join(basePath, versionStr, "extracted", "wxocr.dll"),
			filepath.Join(basePath, versionStr, "wxocr.dll"),
		}

		for _, filePath := range possibleFiles {
			if _, err := os.Stat(filePath); err == nil {
				candidates = append(candidates, ocrCandidate{path: filePath, version: versionNum})
				break
			}
		}
	}

	return candidates
}

// selectBestCandidate 从候选列表中选择最佳的（版本号最大的）
func (w *WeChatOCRCGO) selectBestCandidate(candidates []ocrCandidate) string {
	if len(candidates) == 0 {
		return ""
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].version > candidates[j].version
	})

	return candidates[0].path
}

// findWeChatOCRExe 查找 WeChatOCR.exe 或 wxocr.dll
func (w *WeChatOCRCGO) findWeChatOCRExe() string {
	var allCandidates []ocrCandidate

	// 策略1: APPDATA 路径（最快，最常见）
	appdata := os.Getenv("APPDATA")
	if appdata != "" {
		appdataPaths := []string{
			// 微信 3.x
			filepath.Join(appdata, "Tencent", "WeChat", "XPlugin", "Plugins", "WeChatOCR"),
			// 微信 4.x
			filepath.Join(appdata, "Tencent", "xwechat", "XPlugin", "plugins", "WeChatOcr"),
		}
		for _, basePath := range appdataPaths {
			allCandidates = append(allCandidates, w.scanOCRDirectory(basePath)...)
		}
	}

	// 策略2: 注册表路径
	registryPaths := w.getWeChatFromRegistry()
	for _, regPath := range registryPaths {
		pluginPaths := []string{
			filepath.Join(regPath, "XPlugin", "Plugins", "WeChatOCR"),
			filepath.Join(regPath, "XPlugin", "plugins", "WeChatOcr"),
		}
		for _, pluginPath := range pluginPaths {
			allCandidates = append(allCandidates, w.scanOCRDirectory(pluginPath)...)
		}
	}

	// 策略3: 常见安装位置（仅在前两种方法失败时使用）
	if len(allCandidates) == 0 {
		for _, drive := range []string{"C", "D", "E"} {
			commonBases := []string{
				filepath.Join(drive+":", "Program Files", "Tencent", "WeChat"),
				filepath.Join(drive+":", "Program Files (x86)", "Tencent", "WeChat"),
			}
			for _, base := range commonBases {
				if _, err := os.Stat(base); err == nil {
					pluginPaths := []string{
						filepath.Join(base, "XPlugin", "Plugins", "WeChatOCR"),
						filepath.Join(base, "XPlugin", "plugins", "WeChatOcr"),
					}
					for _, pluginPath := range pluginPaths {
						allCandidates = append(allCandidates, w.scanOCRDirectory(pluginPath)...)
					}
				}
			}
		}
	}

	return w.selectBestCandidate(allCandidates)
}

// findWeChatDir 查找微信运行时目录
func (w *WeChatOCRCGO) findWeChatDir() string {
	// 判断是否是微信 4.0（根据 OCR 组件路径判断）
	isWeChat4 := w.wechatOCRPath != "" && strings.Contains(strings.ToLower(w.wechatOCRPath), "wxocr.dll")
	w.isWeChat4 = isWeChat4

	// 方法1: 从注册表查找
	registryPaths := w.getWeChatFromRegistry()
	for _, installPath := range registryPaths {
		if isWeChat4 {
			// 微信 4.0: 需要查找 Weixin\x.x.x.x 目录
			parent := filepath.Dir(installPath) // Tencent 目录

			// 尝试多个可能的 Weixin 位置
			possibleParents := []string{parent, filepath.Dir(parent)}
			for _, p := range possibleParents {
				weixinBase := filepath.Join(p, "Weixin")
				if dir := w.findVersionDirSimple(weixinBase, true); dir != "" {
					return dir
				}
			}
		} else {
			// 微信 3.x: 直接返回注册表路径
			if _, err := os.Stat(installPath); err == nil {
				return installPath
			}
		}
	}

	// 方法2: 在常见安装位置查找
	// 获取所有存在的驱动器
	drives := []string{}
	for _, letter := range "CDEFGHIJKLMNOPQRSTUVWXYZ" {
		drive := string(letter) + ":\\"
		if _, err := os.Stat(drive); err == nil {
			drives = append(drives, string(letter))
		}
	}

	for _, drive := range drives {
		driveRoot := drive + ":\\"
		if isWeChat4 {
			// 微信 4.0: 查找 Weixin\x.x.x.x
			weixinPaths := []string{
				filepath.Join(driveRoot, "Program Files", "Tencent", "Weixin"),
				filepath.Join(driveRoot, "Program Files (x86)", "Tencent", "Weixin"),
				filepath.Join(driveRoot, "Weixin"),
				filepath.Join(driveRoot, "Tencent", "Weixin"),
			}
			for _, weixinBase := range weixinPaths {
				if dir := w.findVersionDirSimple(weixinBase, true); dir != "" {
					return dir
				}
				// 如果没有版本号子目录，检查是否直接是运行目录
				for _, exe := range []string{"WeChat.exe", "WeChatApp.exe", "WeChatAppEx.exe"} {
					if fileExists(filepath.Join(weixinBase, exe)) {
						return weixinBase
					}
				}
			}
		} else {
			// 微信 3.x: 查找 WeChat 目录
			commonPaths := []string{
				filepath.Join(driveRoot, "Program Files", "Tencent", "WeChat"),
				filepath.Join(driveRoot, "Program Files (x86)", "Tencent", "WeChat"),
				filepath.Join(driveRoot, "WeChat"),
				filepath.Join(driveRoot, "Tencent", "WeChat"),
			}
			for _, basePath := range commonPaths {
				if dir := w.findVersionDirSimple(basePath, false); dir != "" {
					return dir
				}
				// 如果没有版本号目录，直接返回基础路径
				if _, err := os.Stat(basePath); err == nil {
					return basePath
				}
			}
		}
	}

	return ""
}

// findVersionDirSimple 简化版本：查找版本号目录（不检查文件是否存在）
func (w *WeChatOCRCGO) findVersionDirSimple(basePath string, isWeChat4 bool) string {
	if _, err := os.Stat(basePath); err != nil {
		return ""
	}

	entries, err := os.ReadDir(basePath)
	if err != nil {
		return ""
	}

	// 版本号目录模式
	var versionPattern *regexp.Regexp
	if isWeChat4 {
		// 微信 4.0: 4.1.2.17 格式
		versionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+\.\d+$`)
	} else {
		// 微信 3.x: [3.9.12.51] 或 3.9.12.51 格式
		versionPattern = regexp.MustCompile(`^\[?\d+(\.\d+)*\]?$`)
	}

	type versionDir struct {
		path    string
		version int
	}
	var versionDirs []versionDir

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !versionPattern.MatchString(name) {
			continue
		}

		fullPath := filepath.Join(basePath, name)
		cleanName := strings.Trim(name, "[]")
		v := w.parseVersion(cleanName)
		versionDirs = append(versionDirs, versionDir{path: fullPath, version: v})
	}

	if len(versionDirs) == 0 {
		return ""
	}

	// 选择版本号最大的
	sort.Slice(versionDirs, func(i, j int) bool {
		return versionDirs[i].version > versionDirs[j].version
	})

	return versionDirs[0].path
}

// fileExists 检查文件是否存在
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// init 初始化
func (w *WeChatOCRCGO) init() {
	// 1. 查找微信 OCR 组件
	ocrPath := w.findWeChatOCRExe()
	if ocrPath == "" {
		w.errorMsg = "未找到微信 OCR 组件，请确保已安装微信并使用过'提取图中文字'功能"
		fmt.Println("⚠ WeChatOCR (CGO): " + w.errorMsg)
		return
	}
	w.wechatOCRPath = ocrPath

	// 2. 查找微信运行时目录
	wechatDir := w.findWeChatDir()
	if wechatDir == "" {
		w.errorMsg = "未找到微信运行时目录"
		fmt.Println("⚠ WeChatOCR (CGO): " + w.errorMsg)
		return
	}
	w.wechatPath = wechatDir

	fmt.Printf("✓ WeChat OCR (CGO) 初始化完成\n")
	fmt.Printf("  OCR 组件: %s\n", ocrPath)
	fmt.Printf("  微信目录: %s\n", wechatDir)

	w.available = true
	w.initialized = true
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
	absImagePath, _ := filepath.Abs(imagePath)
	absOcrPath, _ := filepath.Abs(w.wechatOCRPath)
	absWechatDir, _ := filepath.Abs(w.wechatPath)

	// 转换为 C 字符串
	cOcrPath := C.CString(absOcrPath)
	cWechatDir := C.CString(absWechatDir)
	cImagePath := C.CString(absImagePath)
	defer C.free(unsafe.Pointer(cOcrPath))
	defer C.free(unsafe.Pointer(cWechatDir))
	defer C.free(unsafe.Pointer(cImagePath))

	// 调用 wechat_ocr（静态链接）
	resultPtr := C.call_wechat_ocr_static(cOcrPath, cWechatDir, cImagePath)

	if resultPtr == nil {
		errCode := C.get_last_wcocr_error()
		return nil, fmt.Errorf("wechat_ocr 调用失败（错误码: %d）", errCode)
	}

	// 将 C 字符串转换为 Go 字符串
	resultStr := C.GoString(resultPtr)

	if resultStr == "" {
		return nil, fmt.Errorf("wechat_ocr 返回空字符串")
	}

	// 解析结果 - wcocr 返回的格式可能是 dict 或 list
	return w.parseOCRResult(resultStr)
}

// parseOCRResult 解析 OCR 结果
func (w *WeChatOCRCGO) parseOCRResult(resultStr string) ([]TextBlock, error) {
	var textBlocks []TextBlock

	// wcocr 返回格式: {"errcode":0,"imgpath":"...","width":1920,"height":1080,"ocr_response":[...]}
	// ocr_response 中的坐标是浮点数
	var dictResult struct {
		Errcode     int `json:"errcode"`
		OcrResponse []struct {
			Text   string  `json:"text"`
			Left   float64 `json:"left"`
			Top    float64 `json:"top"`
			Right  float64 `json:"right"`
			Bottom float64 `json:"bottom"`
			Rate   float64 `json:"rate"`
		} `json:"ocr_response"`
	}

	if err := json.Unmarshal([]byte(resultStr), &dictResult); err == nil {
		if dictResult.Errcode != 0 {
			return nil, fmt.Errorf("OCR 错误码: %d", dictResult.Errcode)
		}

		for _, item := range dictResult.OcrResponse {
			if item.Text == "" {
				continue
			}

			x := int(item.Left)
			y := int(item.Top)
			width := int(item.Right - item.Left)
			height := int(item.Bottom - item.Top)

			if width > 0 && height > 0 {
				textBlocks = append(textBlocks, TextBlock{
					Text:   item.Text,
					X:      x,
					Y:      y,
					Width:  width,
					Height: height,
				})
			}
		}

		fmt.Printf("✓ OCR 识别到 %d 个文本块\n", len(textBlocks))
		return textBlocks, nil
	}

	// 尝试解析为 list 格式 [{"text": ..., ...}, ...]
	var listResult []struct {
		Text   string `json:"text"`
		Word   string `json:"word"`
		Left   int    `json:"left"`
		Top    int    `json:"top"`
		Right  int    `json:"right"`
		Bottom int    `json:"bottom"`
		X      int    `json:"x"`
		Y      int    `json:"y"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}

	if err := json.Unmarshal([]byte(resultStr), &listResult); err == nil {
		for _, item := range listResult {
			text := item.Text
			if text == "" {
				text = item.Word
			}
			if text == "" {
				continue
			}

			var x, y, width, height int
			if item.Right > 0 || item.Bottom > 0 {
				x = item.Left
				y = item.Top
				width = item.Right - item.Left
				height = item.Bottom - item.Top
			} else {
				x = item.X
				y = item.Y
				width = item.Width
				height = item.Height
			}

			if width > 0 && height > 0 {
				textBlocks = append(textBlocks, TextBlock{
					Text:   text,
					X:      x,
					Y:      y,
					Width:  width,
					Height: height,
				})
			}
		}
		return textBlocks, nil
	}

	return nil, fmt.Errorf("无法解析 OCR 结果: %s", resultStr)
}

// GetError 获取错误信息
func (w *WeChatOCRCGO) GetError() string {
	return w.errorMsg
}

// Close 关闭
func (w *WeChatOCRCGO) Close() {
	if w.initialized {
		C.wcocr_stop()
		w.initialized = false
		w.available = false
	}
}
