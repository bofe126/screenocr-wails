@echo off
chcp 65001 >nul
echo 启动开发模式...

:: 检查依赖
if not exist "frontend\node_modules" (
    echo 安装前端依赖...
    cd frontend
    call npm install
    cd ..
)

:: 启用 CGO（WeChatOCR 需要）
set CGO_ENABLED=1

:: 启动 Wails 开发服务器
wails dev

