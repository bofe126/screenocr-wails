# ScreenOCR - 屏幕文字识别工具 (Go Wails 版本)

基于 Go + Wails 重构的屏幕文字识别工具，支持 Windows OCR 和翻译功能。

## 功能特性

- ✅ 全局热键触发（默认 ALT）
- ✅ 屏幕截图和 OCR 识别
- ✅ Windows OCR 引擎支持
- ✅ 文字选择和高亮显示
- ✅ 自动复制到剪贴板
- ✅ 腾讯云翻译 API 集成
- ✅ 系统托盘管理
- ✅ 现代化设置界面

## 技术栈

- **后端**: Go 1.21+
- **前端**: HTML/CSS/JavaScript (Vite)
- **框架**: Wails v2
- **OCR**: Windows Media OCR API
- **翻译**: 腾讯云机器翻译

## 构建要求

- Go 1.21 或更高版本
- Node.js 18+ 和 npm
- Wails CLI: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`

## 快速开始

### 开发模式

```bash
# 安装依赖
cd frontend && npm install && cd ..

# 启动开发服务器
wails dev
```

### 构建发布版

```bash
# Windows
build.bat

# 或手动构建
wails build
```

输出文件: `build/bin/ScreenOCR.exe`

## 配置说明

程序首次运行会在同目录创建 `config.json` 配置文件：

```json
{
  "trigger_delay_ms": 300,
  "hotkey": "alt",
  "auto_copy": true,
  "ocr_engine": "windows",
  "enable_translation": true,
  "translation_target": "zh",
  "tencent_secret_id": "",
  "tencent_secret_key": ""
}
```

### 翻译功能配置

1. 访问 [腾讯云控制台](https://console.cloud.tencent.com/cam/capi)
2. 创建 API 密钥（SecretId 和 SecretKey）
3. 在设置界面填入密钥

## 使用方法

1. 启动程序后，程序会最小化到系统托盘
2. 按住 **ALT** 键（或自定义快捷键）等待屏幕出现蓝色边框
3. OCR 识别完成后边框变为绿色
4. 拖动鼠标选择需要的文字区域
5. 松开鼠标后文字自动复制到剪贴板
6. 如果启用了翻译，会显示翻译弹窗

## 项目结构

```
screenocr-wails/
├── main.go                 # 程序入口
├── app.go                  # 应用逻辑
├── internal/               # 内部包
│   ├── ocr/               # OCR 引擎
│   ├── screenshot/         # 屏幕截图
│   ├── hotkey/            # 热键管理
│   ├── overlay/           # 覆盖层窗口
│   ├── translator/        # 翻译 API
│   └── tray/              # 系统托盘
└── frontend/              # 前端代码
    ├── index.html
    ├── src/
    │   ├── main.js
    │   └── style.css
    └── package.json
```

## 开发说明

### OCR 引擎

- **Windows OCR**: 使用 PowerShell 直接调用 Windows Media OCR API（无需 Python 依赖）
- **Windows OCR (Python)**: 保留的 Python 版本，可通过配置 `ocr_engine: "windows-python"` 使用
- **WeChatOCR**: 占位实现，需要 CGO 支持

### 覆盖层窗口

使用 Win32 API 实现透明分层窗口，支持：
- 全屏透明覆盖
- 鼠标事件处理
- 文字高亮显示
- 截图背景显示

## 许可证

MIT License

## 致谢

- 原项目: [screenocr](https://github.com/your-username/screenocr) (Python 版本)
- [Wails](https://wails.io/) - Go + Web 桌面应用框架

