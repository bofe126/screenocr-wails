@echo off
chcp 65001 >nul
echo ========================================
echo    ScreenOCR Dev Mode
echo ========================================
echo.

:: Set Go proxy for faster downloads (China)
set GOPROXY=https://goproxy.cn,direct

:: Find MinGW GCC
where gcc.exe >nul 2>nul
if %errorlevel% equ 0 (
    echo [MinGW] GCC found
    set "CC=gcc"
    set "CXX=g++"
)

:: Enable CGO
set CGO_ENABLED=1

:: Build frontend if dist is empty
if not exist "frontend\dist\index.html" (
    echo Building frontend...
    cd frontend
    call npm run build
    cd ..
)

:: Start Wails dev (skip frontend rebuild with -s flag)
wails dev -s
