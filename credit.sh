#!/usr/bin/env bash
# credit.sh — TRAE SOLO 积分日报
#
# 用法:
#   ./credit.sh            # 人类可读日报
#   ./credit.sh -json      # 原始 JSON
#   ./credit.sh <uid>      # 指定账号
#
# 二进制升级: go build -o credit ./cmd/credit
set -euo pipefail
cd "$(dirname "$0")"

BIN=./credit
if [ ! -x "$BIN" ]; then
    echo "build credit ..."
    go build -o "$BIN" ./cmd/credit
fi

if [[ "${1:-}" == "-json" ]]; then
    exec "$BIN"
fi
exec "$BIN" "$@" -pretty
