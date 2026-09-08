#!/usr/bin/env bash
set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP="$DIR/traesolo-copy.app"
FRIDA_BIN="/opt/homebrew/bin/frida"
SCRIPT="$DIR/capture_work_credits.js"

if [ ! -d "$APP" ]; then
    echo "[-] 尚未准备副本，正在自动执行 setup_copy.sh..."
    bash "$DIR/setup_copy.sh"
fi

echo "=== 检查运行中的 ai 进程 ==="
ORIG_PID=$(pgrep -f "/Applications/TRAE SOLO CN.app/Contents/MacOS/Electron" | head -n 1 || true)
if [ -n "$ORIG_PID" ]; then
    echo "[!] 提示: 检测到原版 TRAE 正在运行 (PID: $ORIG_PID)。"
    echo "    为避免数据库 (database.db/storage.json) 锁冲突，建议在捕获前彻底退出原版应用。"
fi
# 优先查找副本进程
AI_PID=$(pgrep -f "traesolo-copy.*--vscode-crash-reporter-process-type=ai" | head -n 1 || true)

if [ -z "$AI_PID" ]; then
    echo "[*] 未检测到运行中的副本 ai 进程。"
    echo "[*] 正在启动副本应用: $APP ..."
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
    echo "[-] 未能自动获取到 ai 进程 PID，请确认副本是否已成功打开并登录。"
    echo "    手动查找命令: pgrep -f 'process-type=ai'"
    exit 1
fi

echo "[+] 成功定位到 ai 辅助进程 PID: $AI_PID"
echo "[*] 启动 Frida 附加注入..."
echo "-------------------------------------------------------"
echo "提示：请在打开的 TRAE 窗口中，进入 Work/SOLO 模式并发送一条对话。"
echo "日志将同时写入: $DIR/capture.log"
echo "-------------------------------------------------------"

exec "$FRIDA_BIN" -p "$AI_PID" -l "$SCRIPT" -q -t inf -o "$DIR/capture.log"
