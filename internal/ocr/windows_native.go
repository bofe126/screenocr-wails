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

// WindowsOCRNative Windows OCR 引擎（通过 PowerShell 直接调用，无需 Python）
type WindowsOCRNative struct {
	available     bool
	errorMsg      string
	scriptPath    string
	powershellPath string // 缓存 PowerShell 路径，避免重复查找
}

// NewWindowsOCRNative 创建 Windows OCR 实例（原生版本，使用 PowerShell）
func NewWindowsOCRNative() *WindowsOCRNative {
	ocr := &WindowsOCRNative{}
	ocr.init()
	return ocr
}

// init 初始化
func (w *WindowsOCRNative) init() {
	// 创建 PowerShell 脚本
	scriptPath, err := w.createScript()
	if err != nil {
		w.errorMsg = fmt.Sprintf("创建 OCR 脚本失败: %v", err)
		fmt.Println("❌ " + w.errorMsg)
		return
	}
	w.scriptPath = scriptPath

	// 检查 PowerShell 是否可用（Windows 自带，应该总是可用）
	powershellPath := w.findPowerShell()
	if powershellPath == "" {
		w.errorMsg = "未找到 PowerShell"
		fmt.Println("❌ " + w.errorMsg)
		return
	}
	w.powershellPath = powershellPath // 缓存路径

	// 测试脚本是否可用
	if err := w.testScript(powershellPath); err != nil {
		w.errorMsg = fmt.Sprintf("PowerShell OCR 脚本测试失败: %v", err)
		fmt.Println("⚠ " + w.errorMsg)
		// 不阻止初始化，可能只是语言包未安装
	}

	w.available = true
	fmt.Println("✓ Windows OCR 初始化完成 (PowerShell 直接调用)")
}

// findPowerShell 查找 PowerShell 路径
func (w *WindowsOCRNative) findPowerShell() string {
	// 尝试不同的 PowerShell 路径
	paths := []string{
		"powershell.exe",
		"C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe",
		"C:\\Windows\\SysWOW64\\WindowsPowerShell\\v1.0\\powershell.exe",
	}

	for _, path := range paths {
		if fullPath, err := exec.LookPath(path); err == nil {
			return fullPath
		}
	}

	return ""
}

// testScript 测试脚本是否可用
func (w *WindowsOCRNative) testScript(powershellPath string) error {
	// 创建一个简单的测试命令，使用与主脚本相同的方式加载 WinRT 类型
	testCmd := fmt.Sprintf(`& {
		try {
			Add-Type -AssemblyName System.Runtime.WindowsRuntime | Out-Null
			$null = [Windows.Media.Ocr.OcrEngine, Windows.Media.Ocr, ContentType = WindowsRuntime]
			$languages = [Windows.Media.Ocr.OcrEngine]::AvailableRecognizerLanguages
			if ($languages -ne $null) {
				Write-Output "OK"
			} else {
				Write-Output "ERROR: AvailableRecognizerLanguages is null"
			}
		} catch {
			Write-Output "ERROR: $_"
		}
	}`)

	cmd := exec.Command(powershellPath, "-NoProfile", "-NonInteractive", "-Command", testCmd)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("PowerShell 测试失败: %w", err)
	}

	outputStr := strings.TrimSpace(string(output))
	if !strings.Contains(outputStr, "OK") {
		return fmt.Errorf("OCR API 不可用: %s", outputStr)
	}

	return nil
}

// createScript 创建 PowerShell OCR 脚本
func (w *WindowsOCRNative) createScript() (string, error) {
	// PowerShell 脚本，直接调用 Windows OCR API
	// 使用 .NET 方式调用 WinRT API
	// 注意：所有错误消息使用英文，避免编码问题
	script := `# Windows OCR PowerShell Script
param(
    [Parameter(Mandatory=$true)]
    [string]$ImagePath
)

$ErrorActionPreference = "Stop"

try {
    # Set console output encoding to UTF-8
    [Console]::OutputEncoding = [System.Text.Encoding]::UTF8
    
    # Load required assemblies
    Add-Type -AssemblyName System.Runtime.WindowsRuntime | Out-Null
    
    # Import WinRT types
    try {
        $null = [Windows.Storage.StorageFile, Windows.Storage, ContentType = WindowsRuntime]
    } catch { }
    
    try {
        $null = [Windows.Media.Ocr.OcrEngine, Windows.Media.Ocr, ContentType = WindowsRuntime]
    } catch { }
    
    try {
        $null = [Windows.Graphics.Imaging.BitmapDecoder, Windows.Graphics.Imaging, ContentType = WindowsRuntime]
    } catch { }
    
    try {
        $null = [Windows.Globalization.Language, Windows.Globalization, ContentType = WindowsRuntime]
    } catch { }
    
    # Create OCR engine
    $engine = $null
    $languages = @("zh-CN", "zh-Hans-CN", "en-US")
    
    foreach ($langCode in $languages) {
        try {
            $lang = New-Object Windows.Globalization.Language($langCode)
            $engine = [Windows.Media.Ocr.OcrEngine]::TryCreateFromLanguage($lang)
            if ($engine -ne $null) {
                break
            }
        } catch {
            continue
        }
    }
    
    if ($engine -eq $null) {
        try {
            $engine = [Windows.Media.Ocr.OcrEngine]::TryCreateFromUserProfileLanguages()
        } catch {
            # Ignore error
        }
    }
    
    if ($engine -eq $null) {
        $errorObj = @{error="Failed to create OCR engine. Please install language pack."}
        Write-Output (ConvertTo-Json -InputObject $errorObj -Compress)
        exit 1
    }
    
    # Check if file exists
    if (-not (Test-Path $ImagePath)) {
        $errorObj = @{error="Image file not found: $ImagePath"}
        Write-Output (ConvertTo-Json -InputObject $errorObj -Compress)
        exit 1
    }
    
    # Load Windows Runtime System Extensions for async operations
    Add-Type -AssemblyName System.Runtime.WindowsRuntime | Out-Null
    $backtick = [char]96
    $asyncOpName = "IAsyncOperation" + $backtick + "1"
    $asTaskGeneric = ([System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object { $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq $asyncOpName })[0]
    Function Await($WinRtTask, $ResultType) {
        $asTask = $asTaskGeneric.MakeGenericMethod($ResultType)
        $netTask = $asTask.Invoke($null, @($WinRtTask))
        $netTask.Wait(-1) | Out-Null
        $netTask.Result
    }
    
    # Load image file
    $storageFileTask = [Windows.Storage.StorageFile]::GetFileFromPathAsync($ImagePath)
    $storageFile = Await $storageFileTask ([Windows.Storage.StorageFile])
    if ($storageFile -eq $null) {
        $errorObj = @{error="Failed to load image file"}
        Write-Output (ConvertTo-Json -InputObject $errorObj -Compress)
        exit 1
    }
    
    $streamTask = $storageFile.OpenAsync([Windows.Storage.FileAccessMode]::Read)
    $stream = Await $streamTask ([Windows.Storage.Streams.IRandomAccessStream])
    if ($stream -eq $null) {
        $errorObj = @{error="Failed to open image file stream"}
        Write-Output (ConvertTo-Json -InputObject $errorObj -Compress)
        exit 1
    }
    
    # Decode image
    $decoderTask = [Windows.Graphics.Imaging.BitmapDecoder]::CreateAsync($stream)
    $decoder = Await $decoderTask ([Windows.Graphics.Imaging.BitmapDecoder])
    if ($decoder -eq $null) {
        $errorObj = @{error="Failed to create image decoder"}
        Write-Output (ConvertTo-Json -InputObject $errorObj -Compress)
        exit 1
    }
    
    $bitmapTask = $decoder.GetSoftwareBitmapAsync()
    $bitmap = Await $bitmapTask ([Windows.Graphics.Imaging.SoftwareBitmap])
    if ($bitmap -eq $null) {
        $errorObj = @{error="Failed to decode image"}
        Write-Output (ConvertTo-Json -InputObject $errorObj -Compress)
        exit 1
    }
    
    # Perform OCR
    $resultTask = $engine.RecognizeAsync($bitmap)
    $result = Await $resultTask ([Windows.Media.Ocr.OcrResult])
    if ($result -eq $null) {
        $errorObj = @{error="OCR recognition returned null result"}
        Write-Output (ConvertTo-Json -InputObject $errorObj -Compress)
        exit 1
    }
    
    # Build result array
    $blocks = New-Object System.Collections.ArrayList
    
    if ($result.Lines -ne $null) {
        foreach ($line in $result.Lines) {
            if ($line.Words -ne $null) {
                foreach ($word in $line.Words) {
                    $rect = $word.BoundingRect
                    $block = @{
                        text = $word.Text
                        x = [int]$rect.X
                        y = [int]$rect.Y
                        width = [int]$rect.Width
                        height = [int]$rect.Height
                    }
                    [void]$blocks.Add($block)
                }
            }
        }
    }
    
    # Output JSON
    $json = $blocks | ConvertTo-Json -Compress -Depth 10
    Write-Output $json
    
} catch {
    $errorMsg = $_.Exception.Message
    if ($_.Exception.InnerException -ne $null) {
        $errorMsg += " (Inner: " + $_.Exception.InnerException.Message + ")"
    }
    $errorObj = @{error=$errorMsg}
    Write-Output (ConvertTo-Json -InputObject $errorObj -Compress)
    exit 1
}
`

	// 保存到临时目录
	tmpDir := os.TempDir()
	scriptPath := filepath.Join(tmpDir, "screenocr_ocr.ps1")

	if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
		return "", err
	}

	return scriptPath, nil
}

// IsAvailable 检查是否可用
func (w *WindowsOCRNative) IsAvailable() bool {
	return w.available
}

// Recognize 识别图片
func (w *WindowsOCRNative) Recognize(img image.Image, preprocess bool) ([]TextBlock, error) {
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

	// 调用 PowerShell 脚本
	return w.runPowerShellOCR(tmpPath)
}

// runPowerShellOCR 调用 PowerShell OCR
func (w *WindowsOCRNative) runPowerShellOCR(imagePath string) ([]TextBlock, error) {
	// 使用缓存的 PowerShell 路径，如果为空则重新查找
	powershellPath := w.powershellPath
	if powershellPath == "" {
		powershellPath = w.findPowerShell()
		if powershellPath == "" {
			return nil, fmt.Errorf("未找到 PowerShell")
		}
		w.powershellPath = powershellPath // 更新缓存
	}

	absPath, err := filepath.Abs(imagePath)
	if err != nil {
		return nil, fmt.Errorf("获取图片绝对路径失败: %w", err)
	}
	
	// 构建 PowerShell 命令
	// 使用 -ExecutionPolicy Bypass 避免执行策略限制
	// 使用 -NoProfile 加快启动速度
	// 使用 -NonInteractive 避免交互提示
	cmd := exec.Command(
		powershellPath,
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-File", w.scriptPath,
		"-ImagePath", absPath,
	)
	
	// 隐藏控制台窗口
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	
	// 设置输出编码为 UTF-8（PowerShell 会自动处理，但显式设置更安全）
	cmd.Env = os.Environ()

	// 捕获 stdout 和 stderr
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	outputStr := strings.TrimSpace(stdout.String())
	errorStr := strings.TrimSpace(stderr.String())

	if err != nil {
		// 尝试从输出中获取错误信息
		if outputStr != "" {
			// 检查是否是 JSON 错误格式
			if strings.HasPrefix(outputStr, `{"error"`) {
				return nil, fmt.Errorf("PowerShell OCR 错误: %s", outputStr)
			}
			return nil, fmt.Errorf("PowerShell 执行失败: %s", outputStr)
		}
		// 如果有 stderr，包含在错误信息中
		if errorStr != "" {
			return nil, fmt.Errorf("PowerShell 执行失败 (stderr: %s): %w", errorStr, err)
		}
		return nil, fmt.Errorf("PowerShell 执行失败: %w", err)
	}

	if outputStr == "" {
		return []TextBlock{}, nil
	}

	return w.parseResult(outputStr)
}

// parseResult 解析 JSON 结果
func (w *WindowsOCRNative) parseResult(jsonStr string) ([]TextBlock, error) {
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
func (w *WindowsOCRNative) GetError() string {
	return w.errorMsg
}

// Close 关闭
func (w *WindowsOCRNative) Close() {}

