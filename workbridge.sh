#!/usr/bin/env bash
# ==============================================================================
# WorkBridge 无头静默常驻管理器 (方案 1)
# ==============================================================================

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_BIN="$DIR/frida/traesolo-copy.app/Contents/MacOS/Electron"
LOG_OUT="$DIR/frida/bridge.log"
PLIST_LABEL="com.traework.workbridge"
PLIST_PATH="$HOME/Library/LaunchAgents/${PLIST_LABEL}.plist"
PORT=7865
TOKEN="twbridge-local-7f3a"

function check_status() {
    local pid=$(pgrep -f "traesolo-copy.*/Electron" | head -n 1 || true)
    if [ -n "$pid" ]; then
        local mem=$(ps -o rss= -p "$pid" | awk '{printf "%.1f MB", $1/1024}')
        echo "[+] WorkBridge 正在后台运行 (PID: $pid, 内存: $mem)"
        local health=$(curl -s --connect-timeout 2 "http://127.0.0.1:$PORT/healthz" 2>/dev/null || true)
        if [ "$health" = "ok" ]; then
            echo "[+] 接口响应正常: http://127.0.0.1:$PORT (状态: $health)"
        else
            echo "[!] 警告: 进程存在但端口 $PORT 尚未就绪或未能响应"
        fi
        return 0
    else
        echo "[-] WorkBridge 未运行"
        return 1
    fi
}

function start_daemon() {
    if check_status >/dev/null 2>&1; then
        echo "[!] WorkBridge 已在运行中，无需重复启动"
        return 0
    fi
    echo "[*] 启动 WorkBridge 无头守护进程 (无窗口、无 Dock 图标、低开销)..."
    export WORKBRIDGE_HEADLESS=1
    nohup "$APP_BIN" --disable-gpu --disable-software-rasterizer > "$LOG_OUT" 2>&1 &
    
    local wait_count=0
    echo -n "[*] 等待服务初始化"
    while [ $wait_count -lt 15 ]; do
        sleep 1
        echo -n "."
        if curl -s --connect-timeout 1 "http://127.0.0.1:$PORT/healthz" 2>/dev/null | grep -q "ok"; then
            echo ""
            echo "[+] WorkBridge 启动成功！已在后台默默监听端口 $PORT"
            check_status
            return 0
        fi
        wait_count=$((wait_count + 1))
    done
    echo ""
    echo "[-] 启动超时，请查看日志: $LOG_OUT"
    return 1
}

function stop_daemon() {
    echo "[*] 正在停止 WorkBridge..."
    local pids=$(pgrep -f "traesolo-copy" || true)
    if [ -n "$pids" ]; then
        echo "$pids" | xargs kill -9 2>/dev/null || true
        sleep 1
        echo "[+] WorkBridge 进程已完全终止"
    else
        echo "[*] 未发现运行中的 WorkBridge 进程"
    fi
}

function install_service() {
    echo "[*] 正在生成 macOS launchd 守护配置文件: $PLIST_PATH"
    cat <<EOF > "$PLIST_PATH"
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>${PLIST_LABEL}</string>
    <key>ProgramArguments</key>
    <array>
        <string>${APP_BIN}</string>
        <string>--disable-gpu</string>
        <string>--disable-software-rasterizer</string>
    </array>
    <key>EnvironmentVariables</key>
    <dict>
        <key>WORKBRIDGE_HEADLESS</key>
        <string>1</string>
    </dict>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>${LOG_OUT}</string>
    <key>StandardErrorPath</key>
    <string>${LOG_OUT}</string>
</dict>
</plist>
EOF
    echo "[*] 加载 launchd 守护服务..."
    launchctl unload "$PLIST_PATH" 2>/dev/null || true
    launchctl load -w "$PLIST_PATH"
    echo "[+] 系统级自愈守护已生效！开机自动在后台启动，崩溃自动拉起。"
    sleep 3
    check_status
}

function uninstall_service() {
    echo "[*] 卸载 launchd 守护服务..."
    launchctl unload -w "$PLIST_PATH" 2>/dev/null || true
    rm -f "$PLIST_PATH"
    stop_daemon
    echo "[+] 已移除系统守护服务并停止进程"
}

case "$1" in
    start)
        start_daemon
        ;;
    stop)
        stop_daemon
        ;;
    restart)
        stop_daemon
        sleep 1
        start_daemon
        ;;
    status)
        check_status
        ;;
    install-service)
        install_service
        ;;
    uninstall-service)
        uninstall_service
        ;;
    *)
        echo "用法: $0 {start|stop|restart|status|install-service|uninstall-service}"
        echo ""
        echo "  start             手动后台启动无头 WorkBridge"
        echo "  stop              停止 WorkBridge"
        echo "  restart           重启 WorkBridge"
        echo "  status            查看运行状态与健康度"
        echo "  install-service   安装为 macOS 开机自启守护 (launchd)"
        echo "  uninstall-service 卸载 macOS 开机自启守护"
        exit 1
        ;;
esac
