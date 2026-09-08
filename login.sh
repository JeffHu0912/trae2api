#!/usr/bin/env bash
# login.sh — TRAE SOLO 登录：自生成登录链接 → 浏览器登录 → 粘贴回调链接 → 换 token 落盘。
#
# 流程（不再依赖 scripts/login_local.py）：
#   1. 生成 machine_id/device_id（openssl rand -hex 16，fallback python secrets），
#      本对 id 同时用于登录 URL 与落盘 auth，保证前后一致
#   2. 构造登录 URL（带 127.0.0.1 回调 + 新 machine/device id），打印给用户
#   3. 用户在浏览器打开链接登录 → 浏览器跳到打不开的 127.0.0.1 地址
#   4. 用户复制地址栏完整回调链接，粘贴到这里
#   5. 内嵌 python 解析回调（parse_qs + unquote）→ 提取 refreshToken / userInfo
#   6. ExchangeToken → GetUserInfo → 落盘 auths/trae-{uid}.json
#   7. 自动签到 + 查积分 → 重启容器加载新账号
#
# 用法:
#   ./login.sh
set -euo pipefail

# Windows 商店占位别名（AppInstallerPythonRedirector）非交互下 exit 49，回退到 python
if ! python3 -c 'print(1)' >/dev/null 2>&1; then
  python3() { python "$@"; }
fi

cd "$(dirname "$0")"
AUTH_DIR="./auths"
CONTAINER="trae2api"
CLIENT_ID="en1oxy7wnw8j9n"          # SOLO stable
APP_VERSION="0.1.52"
API_HOST="https://api.trae.com.cn"  # ExchangeToken / GetUserInfo host（auth.apiHost）

mkdir -p "$AUTH_DIR"

# ─── 生成机器/设备 id（hex32）：登录 URL 与落盘共用这一对 ───
MACHINE_ID="$(openssl rand -hex 16 2>/dev/null || python3 -c 'import secrets;print(secrets.token_hex(16))')"
DEVICE_ID="$(openssl rand -hex 16 2>/dev/null || python3 -c 'import secrets;print(secrets.token_hex(16))')"

echo "============================================================"
echo "  TRAE SOLO 登录"
echo "============================================================"
echo ""
echo "步骤："
echo "  1. 在浏览器打开下面链接，用手机号/验证码登录"
echo "  2. 登录成功后浏览器会跳到打不开的 127.0.0.1 地址"
echo "  3. 复制浏览器地址栏的完整链接，粘贴到下面"
echo ""

# ─── 构造登录链接（内嵌 python 保证 URL 编码正确）───
LOGIN_URL="$(MACHINE_ID="$MACHINE_ID" DEVICE_ID="$DEVICE_ID" CLIENT_ID="$CLIENT_ID" APP_VERSION="$APP_VERSION" python3 - <<'PYEOF'
import os, secrets, urllib.parse

params = {
    "login_version": "1",
    "auth_from": "solo",
    "login_channel": "native_ide",
    "plugin_version": "2.3.62834",
    "auth_type": "local",
    "client_id": os.environ["CLIENT_ID"],
    "redirect": "0",
    "login_trace_id": secrets.token_hex(8),   # hex16
    "auth_callback_url": "http://127.0.0.1:18080/authorize",
    "machine_id": os.environ["MACHINE_ID"],
    "device_id": os.environ["DEVICE_ID"],
    "x_device_id": os.environ["DEVICE_ID"],
    "x_machine_id": os.environ["MACHINE_ID"],
    "x_device_brand": "PC",
    "x_device_type": "PC",
    "x_os_version": "1.0",
    "x_app_version": os.environ["APP_VERSION"],
    "x_app_type": "stable",
}
print("https://www.trae.cn/authorization?" + urllib.parse.urlencode(params))
PYEOF
)"

echo "请在浏览器打开（登录链接每次生成新的 machine/device id）："
echo ""
echo "  $LOGIN_URL"
echo ""

# 回调链接含 refreshToken 等敏感参数，read 用 -r 保特殊字符、-s 不回显避免终端留痕
read -rp "登录完成后，请粘贴浏览器地址栏的完整回调链接（不回显）: " -s callback_url || true
echo ""
if [[ -z "$callback_url" ]]; then
    echo "未输入回调链接，已取消"
    exit 1
fi

# ─── 解析回调 → ExchangeToken → GetUserInfo（内嵌 Python）───
RESULT=$(CLIENT_ID="$CLIENT_ID" API_HOST="$API_HOST" APP_VERSION="$APP_VERSION" \
MACHINE_ID="$MACHINE_ID" DEVICE_ID="$DEVICE_ID" CALLBACK_URL="$callback_url" python3 - <<'PYEOF'
import json, os, sys, time, urllib.parse, urllib.request, urllib.error

# 常量从 bash 环境透传，避免两处硬编码漂移
CLIENT_ID = os.environ["CLIENT_ID"]
API_HOST = os.environ["API_HOST"]
APP_VERSION = os.environ["APP_VERSION"]
MACHINE_ID = os.environ["MACHINE_ID"]
DEVICE_ID = os.environ["DEVICE_ID"]
CALLBACK = os.environ["CALLBACK_URL"]

def http_post_json(url, body, headers, timeout=60, retries=2):
    """POST JSON；非 2xx（HTTPError）立即失败，网络瞬时错误重试 retries 次。"""
    req = urllib.request.Request(url, method="POST")
    for k, v in headers.items():
        req.add_header(k, v)
    data = json.dumps(body).encode()
    for attempt in range(retries + 1):
        try:
            with urllib.request.urlopen(req, data, timeout=timeout) as resp:
                return json.loads(resp.read().decode() or "{}")
        except urllib.error.HTTPError as e:
            raw = e.read().decode(errors="replace")
            print(f"[!] HTTP {e.code}: {raw[:400]}", file=sys.stderr)
            sys.exit(1)
        except (urllib.error.URLError, TimeoutError, ConnectionError) as e:
            if attempt < retries:
                print(f"[*] 网络瞬时错误，重试 {attempt + 1}/{retries}: {e}", file=sys.stderr)
                time.sleep(1)
                continue
            print(f"[!] 请求失败: {e}", file=sys.stderr)
            sys.exit(1)

def parse_json_param(raw):
    """解回调里 URL 编码的 JSON 参数；parse_qs 已解一层，再容错解一层 unquote。"""
    if not raw:
        return None
    for val in (raw, urllib.parse.unquote(raw)):
        try:
            obj = json.loads(val)
            if isinstance(obj, dict):
                return obj
        except Exception:
            continue
    return None

def fix_mojibake(s, uid):
    """修复回调 userInfo 中文被双重编码导致的昵称乱码（如 'Óû§8847309959'）。
    尝试常见错误编码回转；无法修复时回退为 用户+uid末4位。"""
    if not s:
        return s
    for enc in ("latin-1", "cp1252"):
        try:
            fixed = s.encode(enc).decode("utf-8")
            if fixed and all(ch.isprintable() for ch in fixed):
                return fixed
        except Exception:
            continue
    # 无 CJK 字符 → 判定为乱码，回退
    if not any('\u4e00' <= ch <= '\u9fff' for ch in s):
        return "用户" + (uid[-4:] or "")
    return s

# ─── 解析回调链接（parse_qs + unquote 处理 URL 编码）───
qs = urllib.parse.parse_qs(urllib.parse.urlparse(CALLBACK).query)
refresh_token = (qs.get("refreshToken") or [""])[0]
user_info = parse_json_param((qs.get("userInfo") or [""])[0]) or {}
user_jwt = parse_json_param((qs.get("userJwt") or [""])[0]) or {}

uid = str(user_info.get("UserID") or "")
nickname = fix_mojibake(str(user_info.get("ScreenName") or ""), uid)
ent_id = str(user_info.get("TenantID") or "")

# 容错：回调缺 refreshToken 时，尝试 userJwt 里的 Token/RefreshToken
jwt_token = str(user_jwt.get("Token") or "")
jwt_refresh = str(user_jwt.get("RefreshToken") or "")
if not refresh_token:
    refresh_token = jwt_refresh

token, new_refresh, expires_at = "", refresh_token, 0
if refresh_token:
    # a. ExchangeToken（access token + refreshToken 轮换）
    body = {"ClientID": CLIENT_ID, "RefreshToken": refresh_token, "ClientSecret": "-", "UserID": ""}
    resp = http_post_json(API_HOST + "/cloudide/api/v3/trae/oauth/ExchangeToken", body,
                          {"Content-Type": "application/json", "User-Agent": f"Trae/{APP_VERSION}"})
    result = resp.get("Result") or {}
    token = result.get("Token") or ""
    if not token:
        print("[!] ExchangeToken 失败: " + json.dumps(resp, ensure_ascii=False)[:300], file=sys.stderr)
        sys.exit(1)
    new_refresh = result.get("RefreshToken") or refresh_token
    expires_at = int(result.get("TokenExpireAt") or 0)
    if expires_at > 10**12:      # TokenExpireAt 毫秒 → Unix 秒
        expires_at //= 1000
    if expires_at <= time.time():
        expires_at = int(time.time()) + int(result.get("TokenExpireDuration") or 1209600)
    print(f"[*] ExchangeToken 成功: Token {token[:20]}...(已隐藏)")
else:
    # 兜底：无 refreshToken 时直接用 userJwt 的 Token
    token = jwt_token
    expires_at = int(user_jwt.get("TokenExpireAt") or 0)
    if expires_at > 10**12:      # TokenExpireAt 毫秒 → Unix 秒
        expires_at //= 1000
    if not token:
        print("[!] 回调链接缺少 refreshToken，且 userJwt 也没有 Token", file=sys.stderr)
        sys.exit(1)
    print("[*] 无 refreshToken，使用 userJwt 的 Token 兜底")

# b. GetUserInfo 确认（拿 EnterpriseID；失败不阻塞，回退回调 userInfo）
try:
    ui = http_post_json(API_HOST + "/cloudide/api/v3/trae/GetUserInfo",
                        {"ReqSource": "IDE", "IDEVersion": APP_VERSION},
                        {"Content-Type": "application/json", "x-cloudide-token": token,
                         "User-Agent": f"Trae/{APP_VERSION}"})
    u = ui.get("Result") or ui
    if u.get("UserID"):
        uid = str(u.get("UserID") or uid)
        nickname = str(u.get("ScreenName") or nickname)
        ent_id = str(u.get("EnterpriseID") or ent_id)
except Exception as e:
    print(f"[*] GetUserInfo 失败（使用回调 userInfo）: {e}", file=sys.stderr)
if not uid:
    print("[!] 未能获取 uid，请检查 token 是否有效", file=sys.stderr)
    sys.exit(1)

# c. 输出凭证 JSON（供 bash 解析落盘）
out = {
    "uid": uid, "nickname": nickname, "enterprise_id": ent_id,
    "access_token": token, "refresh_token": new_refresh, "expires_at": expires_at,
    "api_host": API_HOST, "machine_id": MACHINE_ID, "device_id": DEVICE_ID,
}
print("JSON:" + json.dumps(out))
PYEOF
)

# 提取 JSON 行
CRED=$(echo "$RESULT" | sed -n 's/^JSON://p')
if [[ -z "$CRED" ]]; then
    echo "$RESULT" >&2
    echo "解析/换 token 失败，请检查回调链接是否完整"
    exit 1
fi

ACCT_UID=$(echo "$CRED" | python3 -c "import json,sys; print(json.load(sys.stdin)['uid'])")
NICKNAME=$(echo "$CRED" | python3 -c "import json,sys; print(json.load(sys.stdin)['nickname'])")
TOKEN=$(echo "$CRED" | python3 -c "import json,sys; print(json.load(sys.stdin)['access_token'])")
REFRESH=$(echo "$CRED" | python3 -c "import json,sys; print(json.load(sys.stdin)['refresh_token'])")
EXPIRES_AT=$(echo "$CRED" | python3 -c "import json,sys; print(json.load(sys.stdin)['expires_at'])")
MACHINE_ID=$(echo "$CRED" | python3 -c "import json,sys; print(json.load(sys.stdin)['machine_id'])")
DEVICE_ID=$(echo "$CRED" | python3 -c "import json,sys; print(json.load(sys.stdin)['device_id'])")

# ─── 落盘 auths/trae-{uid}.json（与 internal/auth 读取格式一致）───
AUTH_FILE="$AUTH_DIR/trae-${ACCT_UID}.json"
if [[ -f "$AUTH_FILE" ]]; then
    echo "账号已存在（uid=$ACCT_UID），将覆盖更新凭证"
    ACTION="覆盖"
else
    echo "新账号（uid=$ACCT_UID），新增 auth 文件"
    ACTION="新增"
fi
MACHINE_ID="$MACHINE_ID" DEVICE_ID="$DEVICE_ID" TOKEN="$TOKEN" REFRESH="$REFRESH" EXPIRES_AT="$EXPIRES_AT" ACCT_UID="$ACCT_UID" NICKNAME="$NICKNAME" AUTH_FILE="$AUTH_FILE" ACTION="$ACTION" python3 - <<'PYEOF'
import json, os
auth = {
    "account": {"uid": os.environ["ACCT_UID"], "enterpriseId": "", "nickname": os.environ["NICKNAME"]},
    "auth": {
        "accessToken": os.environ["TOKEN"],
        "refreshToken": os.environ["REFRESH"],
        "expiresAt": int(os.environ["EXPIRES_AT"]),
        "domain": "trae.cn",
        "apiHost": "https://api.trae.com.cn",
        "machineId": os.environ["MACHINE_ID"],
        "deviceId": os.environ["DEVICE_ID"],
    },
}
# 原子写回：tmp + rename，避免写入中断损坏已有凭证（Windows GBK 编码/中断防御）
import os as _os
_tmp = _os.environ["AUTH_FILE"] + ".tmp"
with open(_tmp, "w", encoding="utf-8") as _f:
    json.dump(auth, _f, indent=1, ensure_ascii=False)
_os.replace(_tmp, _os.environ["AUTH_FILE"])
print(f"已保存（{os.environ['ACTION']}）: {os.environ['AUTH_FILE']}")
PYEOF

# ─── 自动签到 + 查积分 ─────────────────────────────
TOKEN="$TOKEN" ACCT_UID="$ACCT_UID" DEVICE_ID="$DEVICE_ID" python3 - <<'PYEOF'
import json, os, urllib.request
UG = "https://api.trae.cn"
HDRS = {
    "Content-Type": "application/json",
    "Authorization": "Cloud-IDE-JWT " + os.environ["TOKEN"],
    "X-User-Region": "CN",
    "X-Device-Id": os.environ["DEVICE_ID"],
}
def post(path, body=b"{}"):
    req = urllib.request.Request(UG + path, method="POST", data=body, headers=HDRS)
    with urllib.request.urlopen(req, timeout=15) as r:
        return json.loads(r.read().decode() or "{}")
try:
    st = post("/trae/api/v2/ug/checkin_credits/status")
    print(f"签到状态: checked_in={st.get('checked_in')} credits={st.get('credits')} enable={st.get('enable')}")
    if not st.get("checked_in") and st.get("enable"):
        r = post("/trae/api/v2/ug/checkin_credits/claim")
        print(f"签到: {r.get('message', 'success')}")
except Exception as e:
    print(f"签到: {e}")
try:
    ent = post("/trae/api/v2/pay/ide_user_ent_usage")
    packs = ent.get("user_entitlement_pack_list") or []
    total = sum(p.get("entitlement_base_info", {}).get("quota", {}).get("credits_limit", 0) for p in packs)
    print(f"当前积分: {total}")
except Exception as e:
    print(f"查积分: {e}")
PYEOF

# ─── 重启容器加载新账号 ──────────────────────────
echo ""
if docker ps --format '{{.Names}}' | grep -q "^${CONTAINER}$"; then
    echo "重启 $CONTAINER 加载新账号..."
    docker restart "$CONTAINER" >/dev/null
    sleep 2
    COUNT=$(curl -s http://127.0.0.1:7864/status -H "Authorization: Bearer ${TW2A_API_KEY:-}" 2>/dev/null | python3 -c "import json,sys; print(len(json.load(sys.stdin).get('accounts',[])))" 2>/dev/null || echo "?")
    echo "服务已重启，当前账号数: $COUNT"
else
    echo "容器 $CONTAINER 未运行，auth 文件已保存，下次启动自动加载"
fi

echo ""
echo "============================================================"
echo "  登录完成！"
echo "  UID: $ACCT_UID"
echo "  Nickname: ${NICKNAME:-（未获取到）}"
echo "  Token: ${TOKEN:0:20}...(已隐藏)"
echo "  有效期: $(date -d "@$EXPIRES_AT" '+%Y-%m-%d %H:%M' 2>/dev/null || echo "$EXPIRES_AT")"
echo "============================================================"
