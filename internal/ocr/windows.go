//go:build windows

package ocr

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// WindowsOCR Windows OCR 引擎（通过 Python 调用）
type WindowsOCR struct {
	available  bool
	errorMsg   string
	pythonPath string
	scriptPath string
}

// NewWindowsOCR 创建 Windows OCR 实例
func NewWindowsOCR() *WindowsOCR {
	ocr := &WindowsOCR{}
	ocr.init()
	return ocr
}

// init 初始化
func (w *WindowsOCR) init() {
	// 查找 Python
	pythonPaths := []string{"python", "python3", "py"}
	for _, p := range pythonPaths {
		if path, err := exec.LookPath(p); err == nil {
			w.pythonPath = path
			break
		}
	}

	if w.pythonPath == "" {
		w.errorMsg = "未找到 Python，请安装 Python 3.x"
		fmt.Println("❌ " + w.errorMsg)
		return
	}

	// 创建内嵌脚本
	scriptPath, err := w.createScript()
	if err != nil {
		w.errorMsg = fmt.Sprintf("创建 OCR 脚本失败: %v", err)
		fmt.Println("❌ " + w.errorMsg)
		return
	}
	w.scriptPath = scriptPath

	// 检查 winrt 模块是否可用
	checkCmd := exec.Command(w.pythonPath, "-c", "import winrt.windows.media.ocr")
	checkCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := checkCmd.Run(); err != nil {
		fmt.Println("⚠ Python winrt 模块未安装")
		fmt.Println("  请运行: pip install winrt-Windows.Media.Ocr winrt-Windows.Graphics.Imaging winrt-Windows.Storage winrt-Windows.Globalization")
	}

	w.available = true
	fmt.Printf("✓ Windows OCR 初始化完成 (Python: %s)\n", w.pythonPath)
}

// createScript 创建 OCR 脚本
func (w *WindowsOCR) createScript() (string, error) {
	// 简洁的 OCR 脚本，只输出 JSON
	script := `# -*- coding: utf-8 -*-
import sys
import json
import asyncio
import os

# 禁用所有日志和警告
import logging
logging.disable(logging.CRITICAL)
import warnings
warnings.filterwarnings('ignore')

# 设置输出编码
if sys.platform == 'win32':
    import io
    sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding='utf-8', errors='replace')
    sys.stderr = io.TextIOWrapper(sys.stderr.buffer, encoding='utf-8', errors='replace')

def output_error(msg):
    print(json.dumps({"error": str(msg)}, ensure_ascii=False))
    sys.exit(1)

def output_result(data):
    print(json.dumps(data, ensure_ascii=False))
    sys.exit(0)

if len(sys.argv) < 2:
    output_error("Usage: python ocr.py <image_path>")

image_path = sys.argv[1]

if not os.path.exists(image_path):
    output_error(f"File not found: {image_path}")

try:
    from winrt.windows.media.ocr import OcrEngine
    from winrt.windows.graphics.imaging import BitmapDecoder
    from winrt.windows.storage import StorageFile
    from winrt.windows.globalization import Language
except ImportError as e:
    output_error(f"winrt not installed. Run: pip install winrt-Windows.Media.Ocr winrt-Windows.Graphics.Imaging winrt-Windows.Storage winrt-Windows.Globalization")

async def do_ocr():
    # 创建 OCR 引擎
    engine = None
    for lang in ['zh-CN', 'zh-Hans-CN', 'en-US']:
        try:
            engine = OcrEngine.try_create_from_language(Language(lang))
            if engine:
                break
        except:
            pass
    
    if not engine:
        engine = OcrEngine.try_create_from_user_profile_languages()
    
    if not engine:
        return {"error": "Cannot create OCR engine. Install language pack."}
    
    # 加载图片
    storage_file = await StorageFile.get_file_from_path_async(image_path)
    stream = await storage_file.open_async(1)
    
    # 解码
    decoder = await BitmapDecoder.create_async(stream)
    bitmap = await decoder.get_software_bitmap_async()
    
    # OCR
    result = await engine.recognize_async(bitmap)
    
    # 解析结果
    blocks = []
    for line in result.lines:
        for word in line.words:
            rect = word.bounding_rect
            blocks.append({
                'text': word.text,
                'x': int(rect.x),
                'y': int(rect.y),
                'width': int(rect.width),
                'height': int(rect.height)
            })
    
    return blocks

try:
    loop = asyncio.new_event_loop()
    asyncio.set_event_loop(loop)
    result = loop.run_until_complete(do_ocr())
    loop.close()
    
    if isinstance(result, dict) and 'error' in result:
        output_error(result['error'])
    else:
        output_result(result)
except Exception as e:
    output_error(str(e))
`

	// 保存到临时目录
	tmpDir := os.TempDir()
	scriptPath := filepath.Join(tmpDir, "screenocr_ocr.py")

	if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
		return "", err
	}

	return scriptPath, nil
}

// IsAvailable 检查是否可用
func (w *WindowsOCR) IsAvailable() bool {
	return w.available
}

// Recognize 识别图片
func (w *WindowsOCR) Recognize(img image.Image, preprocess bool) ([]TextBlock, error) {
	if !w.available {
		return nil, fmt.Errorf("Windows OCR 不可用: %s", w.errorMsg)
	}

	// 预处理
	if preprocess {
		img = preprocessImage(img)
	}

	// 保存临时文件
	tmpFile, err := os.CreateTemp("", "ocr_*.png")
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

	fmt.Printf("[OCR] 临时文件: %s\n", tmpPath)

	// 调用 Python 脚本
	return w.runPythonOCR(tmpPath)
}

// runPythonOCR 调用 Python OCR
func (w *WindowsOCR) runPythonOCR(imagePath string) ([]TextBlock, error) {
	absPath, _ := filepath.Abs(imagePath)
	fmt.Printf("[OCR] 调用 Python: %s %s\n", w.scriptPath, absPath)

	cmd := exec.Command(w.pythonPath, w.scriptPath, absPath)
	// 设置环境变量确保 UTF-8 输出
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8")
	// 隐藏控制台窗口
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	output, err := cmd.Output() // 只获取 stdout，忽略 stderr
	outputStr := strings.TrimSpace(string(output))

	fmt.Printf("[OCR] 输出: %s\n", outputStr)

	if outputStr == "" {
		if err != nil {
			return nil, fmt.Errorf("Python 执行失败: %w", err)
		}
		return []TextBlock{}, nil
	}

	return w.parseResult(outputStr)
}

// parseResult 解析 JSON 结果
func (w *WindowsOCR) parseResult(jsonStr string) ([]TextBlock, error) {
	jsonStr = strings.TrimSpace(jsonStr)

	if jsonStr == "" || jsonStr == "null" || jsonStr == "[]" {
		fmt.Println("[OCR] 未识别到文字")
		return []TextBlock{}, nil
	}

	// 检查错误
	if strings.HasPrefix(jsonStr, `{"error"`) {
		var errResp struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal([]byte(jsonStr), &errResp); err == nil && errResp.Error != "" {
			return nil, fmt.Errorf("OCR 错误: %s", errResp.Error)
		}
	}

	var blocks []TextBlock
	if err := json.Unmarshal([]byte(jsonStr), &blocks); err != nil {
		var block TextBlock
		if err2 := json.Unmarshal([]byte(jsonStr), &block); err2 == nil {
			return []TextBlock{block}, nil
		}
		return nil, fmt.Errorf("解析失败: %w, 原始: %s", err, jsonStr)
	}

	fmt.Printf("[OCR] 识别成功，共 %d 个文本块\n", len(blocks))
	return blocks, nil
}

// GetError 获取错误信息
func (w *WindowsOCR) GetError() string {
	return w.errorMsg
}

// Close 关闭
func (w *WindowsOCR) Close() {}
