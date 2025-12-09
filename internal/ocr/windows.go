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
)

// WindowsOCR Windows 内置 OCR 引擎
type WindowsOCR struct {
	available bool
	errorMsg  string
}

// NewWindowsOCR 创建 Windows OCR 实例
func NewWindowsOCR() *WindowsOCR {
	ocr := &WindowsOCR{available: true}
	fmt.Println("✓ Windows OCR 初始化完成")
	return ocr
}

// IsAvailable 检查是否可用
func (w *WindowsOCR) IsAvailable() bool {
	return w.available
}

// Recognize 识别图片
func (w *WindowsOCR) Recognize(img image.Image, preprocess bool) ([]TextBlock, error) {
	if !w.available {
		return nil, fmt.Errorf("Windows OCR 不可用")
	}

	// 预处理图片
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

	fmt.Printf("[OCR] 保存临时文件: %s\n", tmpPath)

	if err := png.Encode(tmpFile, img); err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("保存图片失败: %w", err)
	}
	tmpFile.Close()

	// 使用 PowerShell 调用 Windows OCR
	return w.runOCR(tmpPath)
}

// runOCR 执行 OCR
func (w *WindowsOCR) runOCR(imagePath string) ([]TextBlock, error) {
	absPath, _ := filepath.Abs(imagePath)
	fmt.Printf("[OCR] 开始识别: %s\n", absPath)

	// 转义路径中的反斜杠
	escapedPath := strings.ReplaceAll(absPath, `\`, `\\`)

	// PowerShell 脚本
	script := `
$ErrorActionPreference = 'Stop'

try {
    Add-Type -AssemblyName System.Runtime.WindowsRuntime
    
    $null = [Windows.Media.Ocr.OcrEngine, Windows.Foundation, ContentType = WindowsRuntime]
    $null = [Windows.Graphics.Imaging.BitmapDecoder, Windows.Foundation, ContentType = WindowsRuntime]
    $null = [Windows.Storage.StorageFile, Windows.Foundation, ContentType = WindowsRuntime]
    $null = [Windows.Globalization.Language, Windows.Foundation, ContentType = WindowsRuntime]

    $asTaskGeneric = ([System.WindowsRuntimeSystemExtensions].GetMethods() | 
        Where-Object { $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 -and !$_.GetParameters()[0].ParameterType.IsArray })[0]

    Function Await($WinRtTask, $ResultType) {
        $asTask = $asTaskGeneric.MakeGenericMethod($ResultType)
        $netTask = $asTask.Invoke($null, @($WinRtTask))
        $netTask.Wait(-1) | Out-Null
        $netTask.Result
    }

    $imagePath = '` + escapedPath + `'
    
    $getFileMethod = [Windows.Storage.StorageFile].GetMethod('GetFileFromPathAsync')
    $fileTask = $getFileMethod.Invoke($null, @($imagePath))
    $storageFile = Await $fileTask ([Windows.Storage.StorageFile])

    $openTask = $storageFile.OpenAsync([Windows.Storage.FileAccessMode]::Read)
    $stream = Await $openTask ([Windows.Storage.Streams.IRandomAccessStream])

    $decoderTask = [Windows.Graphics.Imaging.BitmapDecoder]::CreateAsync($stream)
    $decoder = Await $decoderTask ([Windows.Graphics.Imaging.BitmapDecoder])

    $bitmapTask = $decoder.GetSoftwareBitmapAsync()
    $bitmap = Await $bitmapTask ([Windows.Graphics.Imaging.SoftwareBitmap])

    $ocrEngine = $null
    try {
        $lang = [Windows.Globalization.Language]::new('zh-Hans-CN')
        $ocrEngine = [Windows.Media.Ocr.OcrEngine]::TryCreateFromLanguage($lang)
    } catch {}
    
    if ($null -eq $ocrEngine) {
        try {
            $lang = [Windows.Globalization.Language]::new('zh-CN')
            $ocrEngine = [Windows.Media.Ocr.OcrEngine]::TryCreateFromLanguage($lang)
        } catch {}
    }
    
    if ($null -eq $ocrEngine) {
        $ocrEngine = [Windows.Media.Ocr.OcrEngine]::TryCreateFromUserProfileLanguages()
    }

    if ($null -eq $ocrEngine) {
        Write-Output '{"error":"OCR engine not available"}'
        exit 0
    }

    $ocrTask = $ocrEngine.RecognizeAsync($bitmap)
    $ocrResult = Await $ocrTask ([Windows.Media.Ocr.OcrResult])

    $blocks = @()
    foreach ($line in $ocrResult.Lines) {
        foreach ($word in $line.Words) {
            $rect = $word.BoundingRect
            $blocks += @{
                text = $word.Text
                x = [int]$rect.X
                y = [int]$rect.Y
                width = [int]$rect.Width
                height = [int]$rect.Height
            }
        }
    }

    if ($blocks.Count -eq 0) {
        Write-Output '[]'
    } elseif ($blocks.Count -eq 1) {
        Write-Output ('[' + ($blocks[0] | ConvertTo-Json -Compress) + ']')
    } else {
        Write-Output ($blocks | ConvertTo-Json -Compress)
    }

} catch {
    $errMsg = $_.Exception.Message -replace '"', "'"
    Write-Output ('{"error":"' + $errMsg + '"}')
}
`

	// 将脚本写入临时文件
	scriptFile, err := os.CreateTemp("", "ocr_script_*.ps1")
	if err != nil {
		return nil, fmt.Errorf("创建脚本文件失败: %w", err)
	}
	scriptPath := scriptFile.Name()
	defer os.Remove(scriptPath)

	// 写入 UTF-8 BOM + 脚本内容
	scriptFile.Write([]byte{0xEF, 0xBB, 0xBF}) // UTF-8 BOM
	scriptFile.WriteString(script)
	scriptFile.Close()

	fmt.Printf("[OCR] 执行 PowerShell 脚本...\n")

	// 执行 PowerShell
	cmd := exec.Command("powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-File", scriptPath,
	)

	output, err := cmd.CombinedOutput()
	outputStr := strings.TrimSpace(string(output))

	fmt.Printf("[OCR] PowerShell 输出: %s\n", outputStr)

	if err != nil {
		fmt.Printf("[OCR] PowerShell 错误: %v\n", err)
		if outputStr != "" {
			return nil, fmt.Errorf("OCR 执行失败: %s", outputStr)
		}
		return nil, fmt.Errorf("PowerShell 执行失败: %w", err)
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

	// 检查是否是错误响应
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
		// 尝试解析单个对象
		var block TextBlock
		if err2 := json.Unmarshal([]byte(jsonStr), &block); err2 == nil {
			return []TextBlock{block}, nil
		}
		fmt.Printf("[OCR] 解析失败: %v, 原始数据: %s\n", err, jsonStr)
		return nil, fmt.Errorf("解析失败: %w", err)
	}

	fmt.Printf("[OCR] 识别成功，共 %d 个文本块\n", len(blocks))
	return blocks, nil
}

// GetError 获取错误信息
func (w *WindowsOCR) GetError() string {
	return w.errorMsg
}

// Close 关闭
func (w *WindowsOCR) Close() {
	// 无需清理
}
