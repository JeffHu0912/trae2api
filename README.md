<div align="center">

<img src="./docs/logo.svg" alt="trae2api logo" width="110" height="110" />


# trae2api

**TRAE SOLO 逆向工程服务 · OpenAI 兼容 API · 可视化账号管理面板**

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat-square&logo=go&logoColor=white)](https://golang.org/)
[![Docker Ready](https://img.shields.io/badge/Docker-Ready-2496ED?style=flat-square&logo=docker&logoColor=white)](https://www.docker.com/)
[![OpenAI Compatible](https://img.shields.io/badge/API-OpenAI%20Compatible-412991?style=flat-square&logo=openai&logoColor=white)](https://platform.openai.com/)
[![Web Admin](https://img.shields.io/badge/Web%20Admin-Built--in-10b981?style=flat-square)](http://localhost:7864/admin)
[![License](https://img.shields.io/badge/License-MIT-blue?style=flat-square)](LICENSE)

</div>

---

> **借鉴说明**：本项目基于上游 [`Sliverkiss/traework2api`](https://github.com/Sliverkiss/traework2api) 改进，新增 Work 通道、`work_client.go` 私有协议、Web 管理控制台、加权防惊群调度等能力。

## 概述

`trae2api` 是一个将 TRAE SOLO 对话通道包装为标准 OpenAI 协议（`/v1/chat/completions` 与 `/v1/models`）的高性能反向代理服务。基于纯 Go 标准库构建，具备极低资源消耗与高并发处理能力。

本项目内置了轻量级 Web 控制台，支持多账号凭证管理、额度监控、自动化轮换保活以及开箱即用的一键网页登录闭环。

## 核心特性

- **OpenAI 协议兼容**：提供标准 `/v1/chat/completions`（支持流式 Streaming 与非流式）与 `/v1/models` 端点，无缝接入 NextChat、Chatbox、Claude Code、Cline 等客户端。
- **可视化 Web 管理控制台**：内置轻量 Web 界面（`GET /admin`），实时展示账号配额（剩余/已用/总量）、签到状态与健康度，支持多账号并发查询与自动刷新。
- **凭证全生命周期管理**：提供 Web 凭证导入、软启停开关、昵称修改、删除及一键 Web 登录闭环（无需手动抓包或提取 Token）。
- **多账号智能调度池**：基于账号可用积分降序挑选，自动处理 1005、429、401、5xx 等异常状态，支持动态冷却与故障自动轮转。
- **自动化运维与保活**：每日定时自动签到，并在 Token 过期前 24 小时自动预刷新与原子落盘，保障长周期稳定可用。
- **新模型支持**：同步支持 glm-5.3、glm-5.2 等新版模型调度，适配最新协议版本。
- **纯净轻量**：纯 Go 标准库开发，零第三方运行时依赖，静态编译产物小巧，内存占用极低。

## 快速开始

### Docker Compose 部署（推荐）

1. **准备目录与配置文件**

```bash
mkdir -p auths data
cp .env.example .env
```

编辑 `.env` 文件，配置自定义的管理鉴权密钥：

```env
TW2A_API_KEY=your_secure_api_key
```

2. **构建并启动服务**

```bash
docker compose up -d --build
```

3. **接口健康检查与模型验证**

```bash
# 健康检查
curl http://127.0.0.1:7864/healthz

# 查看可用模型列表
curl http://127.0.0.1:7864/v1/models

# 查看账号池状态
curl http://127.0.0.1:7864/status
```

### 本地直接运行

环境要求：Go 1.22+

```bash
# 设置访问密钥
export TW2A_API_KEY="your_secure_api_key"

# 编译并启动服务
go build -o trae2api ./cmd/server
./trae2api
```

## Web 管理面板

服务启动后，访问 `http://127.0.0.1:7864/admin` 即可进入可视化管理后台：

- **账号总览**：实时查看各账号的剩余积分、配额总量、已用积分、权益包数及签到状态。
- **Web 登录闭环**：在面板点击发起登录，浏览器完成验证后回调自动回传至服务并完成 Token 换取与热加载，无需手动复制凭证。
- **凭证管理**：支持粘贴 JSON 凭证或回调链接直接导入账号，支持随时启停软开关、修改备注昵称或删除失效账号。
- **安全脱敏**：前端展示严格脱敏（仅显示前缀与长度），写操作（导入、删除、修改）均受 `TW2A_API_KEY` 保护。

## API 调用示例

### 对话补全 (Chat Completions)

```bash
curl -X POST http://127.0.0.1:7864/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${TW2A_API_KEY}" \
  -d '{
    "model": "glm-5.2",
    "messages": [
      {
        "role": "user",
        "content": "请用简短的一句话介绍你自己。"
      }
    ],
    "stream": false
  }'
```

## 运维脚本

项目根目录下提供了便捷的 CLI 运维工具：

```bash
# 全账号批量签到与 Token 保活
./signin.sh

# 账号积分与使用情况报表
./credit.sh

# 输出 JSON 格式报表
./credit.sh -json

# 查看指定 UID 账号
./credit.sh <UID>
```

## 配置项参考

所有配置项均可通过环境变量或 `config.json` 进行调整：

| 环境变量 | 默认值 | 说明 |
|---|---|---|
| `TW2A_API_KEY` | (必填) | 服务鉴权密钥，用于 API 调用与控制台写操作 |
| `TW2A_LISTEN` | `:7864` | 主服务监听地址及端口 |
| `TW2A_AUTH_DIR` | `./auths` | 账号凭证存储目录 |
| `TW2A_STATE_FILE` | `./data/state.json` | 账号池状态持久化文件 |
| `TW2A_DEFAULT_MODEL` | `glm-5.2` | 默认回退请求模型 |
| `TW2A_CALLBACK_PORT` | `18080` | 本地 OAuth 回调端口（设为 0 可关闭） |
| `TW2A_TIMEOUT_SECONDS` | `120` | 上游请求超时时长（秒） |
| `TW2A_ERR_THRESHOLD` | `3` | 触发冷却前的连续错误次数 |
| `TW2A_ERR_COOLDOWN` | `300` | 错误冷却时长（秒） |

## 安全声明

- **本地存储与脱敏**：所有账号凭证仅保存于本地 `auths/` 目录，控制台及日志中所有 Token 均严格脱敏输出。
- **环境隔离**：凭证文件、状态数据及环境配置文件默认加入 `.gitignore`，防止误提交泄漏。

## 开源协议

本项目基于 [MIT License](LICENSE) 许可发布。
