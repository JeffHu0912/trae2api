#!/usr/bin/env bash
set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENTITLEMENTS="$DIR/entitlements.plist"
SRC_APP="/Applications/TRAE SOLO CN.app"
DEST_APP="$DIR/traesolo-copy.app"

echo "=== 1. 检查原版与环境 ==="
if [ ! -d "$SRC_APP" ]; then
    echo "[-] 错误: 未找到 $SRC_APP"
    exit 1
fi

echo "=== 2. 复制应用副本 (如已存在则同步) ==="
if [ ! -d "$DEST_APP" ]; then
    echo "[*] 正在复制 $SRC_APP 到 $DEST_APP (约 1.2GB，请稍候)..."
    cp -R "$SRC_APP" "$DEST_APP"
    echo "[+] 复制完成。"
else
    echo "[*] 副本已存在: $DEST_APP"
fi

# 清除隔离属性
xattr -rc "$DEST_APP" 2>/dev/null || true

echo "=== 3. 递归签名内部动态库与依赖项 ==="
find "$DEST_APP/Contents/Resources/app/modules" -type f \( -name "*.dylib" -o -name "agent-tool-host" -o -name "ctx-cli" \) -exec codesign --force --sign - {} + 2>/dev/null || true
find "$DEST_APP/Contents/Frameworks" -type f -name "*.dylib" -exec codesign --force --sign - {} + 2>/dev/null || true

echo "=== 4. 重签 Frameworks ==="
find "$DEST_APP/Contents/Frameworks" -type d -name "*.framework" | while read -r fw; do
    codesign --force --sign - "$fw" 2>/dev/null || true
done

echo "=== 5. 重签 Helper 辅助应用 (注入 get-task-allow 与 disable-library-validation) ==="
for helper in "$DEST_APP/Contents/Frameworks/"*.app; do
    if [ -d "$helper" ]; then
        echo "[*] 重签 Helper: $(basename "$helper")"
        codesign --force --sign - --entitlements "$ENTITLEMENTS" "$helper"
    fi
done

echo "=== 6. 重签主可执行文件与外层 App Bundle ==="
codesign --force --sign - --entitlements "$ENTITLEMENTS" "$DEST_APP/Contents/MacOS/Electron"
codesign --force --sign - --entitlements "$ENTITLEMENTS" "$DEST_APP"

echo "=== 7. 验证重签结果 ==="
HELPER_BIN="$DEST_APP/Contents/Frameworks/TRAE SOLO CN Helper.app/Contents/MacOS/TRAE SOLO CN Helper"
echo "[*] 检查 Helper 签名状态:"
codesign -d --verbose=2 "$HELPER_BIN" 2>&1 | grep -E "flags|Authority|TeamIdentifier" || true
echo "[*] 检查 Helper Entitlements:"
codesign -d --entitlements - "$HELPER_BIN" 2>&1 | grep -E "get-task-allow|disable-library-validation|allow-jit" || true

echo "[+] 全部配置完成！副本路径: $DEST_APP"
