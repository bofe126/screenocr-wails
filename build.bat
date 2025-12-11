@echo off
chcp 65001 >nul
echo ========================================
echo    ScreenOCR Wails 构建脚本
echo ========================================
echo.

:: 检查 Go 是否安装
where go >nul 2>nul
if %errorlevel% neq 0 (
    echo [错误] 未找到 Go，请先安装 Go 1.21+
    echo 下载地址: https://golang.org/dl/
    pause
    exit /b 1
)

:: 检查 Wails 是否安装
where wails >nul 2>nul
if %errorlevel% neq 0 (
    echo [提示] 正在安装 Wails CLI...
    go install github.com/wailsapp/wails/v2/cmd/wails@latest
    if %errorlevel% neq 0 (
        echo [错误] Wails 安装失败
        pause
        exit /b 1
    )
)

:: 检查 Node.js 是否安装
where node >nul 2>nul
if %errorlevel% neq 0 (
    echo [错误] 未找到 Node.js，请先安装 Node.js 18+
    echo 下载地址: https://nodejs.org/
    pause
    exit /b 1
)

echo [1/4] 安装前端依赖...
cd frontend
call npm install
if %errorlevel% neq 0 (
    echo [错误] npm install 失败
    cd ..
    pause
    exit /b 1
)
cd ..

echo [2/4] 下载 Go 依赖...
go mod tidy
if %errorlevel% neq 0 (
    echo [错误] go mod tidy 失败
    pause
    exit /b 1
)

echo [3/4] 启用 CGO（WeChatOCR 需要）...
set CGO_ENABLED=1

echo [4/5] 构建应用...
wails build -clean
if %errorlevel% neq 0 (
    echo [错误] 构建失败
    pause
    exit /b 1
)

echo.
echo ========================================
echo    构建完成!
echo    输出: build\bin\ScreenOCR.exe
echo ========================================
pause
