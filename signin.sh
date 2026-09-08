#!/usr/bin/env bash
# signin.sh — 批量签到脚本：遍历 auths/ 下所有 trae-*.json 账号
# 用法: ./signin.sh [auths_dir]
set -e
cd "$(dirname "$0")"

BIN=./signin_bin
if [ ! -x "$BIN" ]; then
    echo "build signin_bin ..."
    go build -o "$BIN" ./cmd/signin
fi

exec "$BIN" "${1:-auths}"
