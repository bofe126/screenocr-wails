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

echo [1/5] 安装前端依赖...
cd frontend
call npm install
if %errorlevel% neq 0 (
    echo [错误] npm install 失败
    cd ..
    pause
    exit /b 1
)

echo [2/5] 构建前端...
call npm run build
if %errorlevel% neq 0 (
    echo [错误] 前端构建失败
    cd ..
    pause
    exit /b 1
)
cd ..

echo [3/5] 下载 Go 依赖...
go mod tidy
if %errorlevel% neq 0 (
    echo [错误] go mod tidy 失败
    pause
    exit /b 1
)

echo [4/5] 启用 CGO（WeChatOCR 需要）...
set CGO_ENABLED=1

echo [4.1/5] 说明：当前环境缺少 MinGW 静态库，发布将随包携带 DLL...

echo [图标] 准备应用图标...
if not exist build mkdir build
if not exist build\windows mkdir build\windows
copy /Y assets\icons\appicon.png build\appicon.png >nul
copy /Y assets\icons\windows\icon.ico build\windows\icon.ico >nul

echo [5/5] 构建应用...
wails build -clean
if %errorlevel% neq 0 (
    echo [错误] 构建失败
    pause
    exit /b 1
)

echo [后处理] 复制 MinGW 运行时 DLL...
:: 注意：wails build 开启 Clean Bin Dir，会清空 build\bin，所以必须在 build 之后复制
:: 查找 MinGW DLL 路径
set MINGW_BIN=
for %%p in (D:\mingw64\bin C:\mingw64\bin C:\msys64\mingw64\bin) do (
    if exist "%%p\libstdc++-6.dll" set MINGW_BIN=%%p
)
if defined MINGW_BIN (
    echo 从 %MINGW_BIN% 复制 DLL...
    if not exist build\bin mkdir build\bin
    copy /Y "%MINGW_BIN%\libstdc++-6.dll" build\bin\ >nul
    copy /Y "%MINGW_BIN%\libwinpthread-1.dll" build\bin\ >nul
) else (
    echo [警告] 未找到 MinGW DLL，请确保 libstdc++-6.dll 和 libwinpthread-1.dll 在 PATH 中
)

echo [检查] 确认 DLL 已存在于 build\bin...
if not exist build\bin\libstdc++-6.dll echo [警告] build\bin 缺少 libstdc++-6.dll
if not exist build\bin\libwinpthread-1.dll echo [警告] build\bin 缺少 libwinpthread-1.dll

echo.
echo ========================================
echo    构建完成!
echo    输出: build\bin\ScreenOCR.exe
echo ========================================
pause
