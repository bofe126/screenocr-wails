@echo off
chcp 65001 >nul
echo ========================================
echo    ScreenOCR Wails Build (MinGW)
echo ========================================
echo.

:: Set Go proxy for faster downloads (China)
set GOPROXY=https://goproxy.cn,direct

:: Find MinGW GCC in PATH
where gcc.exe >nul 2>nul
if %errorlevel% equ 0 (
    echo [MinGW] GCC found
    set "CC=gcc"
    set "CXX=g++"
) else (
    echo [Warning] MinGW not found, please ensure gcc.exe is in PATH
)

:: Check Go
where go >nul 2>nul
if %errorlevel% neq 0 (
    echo [Error] Go not found, please install Go 1.21+
    pause
    exit /b 1
)

:: Check Wails
where wails >nul 2>nul
if %errorlevel% neq 0 (
    echo [Info] Installing Wails CLI...
    go install github.com/wailsapp/wails/v2/cmd/wails@latest
)

:: Check Node.js
where node >nul 2>nul
if %errorlevel% neq 0 (
    echo [Error] Node.js not found, please install Node.js 18+
    pause
    exit /b 1
)

echo [1/4] Installing frontend dependencies...
cd frontend
call npm install
cd ..

echo [2/4] Building frontend...
cd frontend
call npm run build
cd ..

echo [3/4] Enable CGO...
set CGO_ENABLED=1

echo [4/4] Building application...
wails build -clean

echo.
echo ========================================
echo    Build Complete!
echo    Output: build\bin\ScreenOCR.exe
echo ========================================
pause
