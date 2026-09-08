#!/usr/bin/env bash
set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP="$DIR/traesolo-copy.app"
FRIDA_BIN="/opt/homebrew/bin/frida"
SCRIPT="$DIR/capture_deep_protocol.js"
LOG_OUT="$DIR/deep_capture.log"

if [ ! -d "$APP" ]; then
    echo "[-] 尚未准备副本，正在自动执行 setup_copy.sh..."
    bash "$DIR/setup_copy.sh"
fi

echo "=== 检查运行中的 ai 辅助进程 ==="
AI_PID=$(pgrep -f "traesolo-copy.*--vscode-crash-reporter-process-type=ai" | head -n 1 || true)

if [ -z "$AI_PID" ]; then
    echo "[*] 未检测到副本 ai 进程，正在启动: $APP ..."
    open -n "$APP"
    echo "[*] 等待 ai 辅助进程启动 (最多等待 20 秒)..."
    for i in {1..20}; do
        AI_PID=$(pgrep -f "traesolo-copy.*--vscode-crash-reporter-process-type=ai" | head -n 1 || true)
        if [ -n "$AI_PID" ]; then
            break
        fi
        sleep 1
    done
fi

if [ -z "$AI_PID" ]; then
    echo "[-] 未能自动定位到 ai 进程 PID，请确认应用是否正常打开。"
    exit 1
fi

echo "[+] 成功定位到 ai 辅助进程 PID: $AI_PID"
echo "[*] 启动全要素深度探针..."
echo "-------------------------------------------------------"
echo "日志将输出到终端并同步写入: $LOG_OUT"
echo "-------------------------------------------------------"

exec "$FRIDA_BIN" -p "$AI_PID" -l "$SCRIPT" -q -t inf -o "$LOG_OUT"
