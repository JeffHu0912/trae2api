# Trae Work Wire 协议规范文档 (Phase 1 交付物)

> **文档状态**：基于实机在线抓包、Mihomo 连接表元数据、Cronet 符号排查与 JSON-RPC 采样分析定性。  
> **更新时间**：2026-09-07

---

## 一、 网络出站全要素定性

通过对 WorkBridge 发起真实 Work 模式请求（`DeepSeek-V4-Flash-Official`）期间的进程树、Socket 连接与网关路由跟踪，捕获到完整的外网通信要素：

| 维度 | 实测定性值 | 说明 |
|---|---|---|
| **目标域名 (Host)** | `api5-normal.mchost.guru` | 区别于 SOLO 免费通道的 `trae-api-cn.mchost.guru` |
| **端口 (Port)** | `443` | 标准 HTTPS / QUIC 端口 |
| **真实解析 IP** | `223.109.13.65` 等 | 字节跳动云服务边缘节点 |
| **传输层网络协议** | **QUIC (HTTP/3 over UDP)** | 客户端启用 TTNet 引擎，默认强制走 QUIC 协议传输 |
| **发包载体进程** | `TRAE SOLO CN Helper` (NetworkService) | 动态加载 `libsscronet.dylib` (TTNet) 与 `libaha_net.dylib` |
| **流量特征** | 单次交互上行约 **40.6 KB**，下行约 **36.3 KB** | 包含完整工作区环境状态、设备指纹与对话上下文 |

---

## 二、 协议传输架构全景

```
[ trae2api / 客户端 ]
         │ (OpenAI HTTP /v1/chat/completions)
         ▼
[ WorkBridge (bridge.js:7865) ]
         │
         │  两步式 JSON-RPC 调用 (经由 AhaRpc IPC)
         │  1. method: "create_chat_session" (mode: "work")
         │  2. method: "subscribe_events" (chat_session_id)
         ▼
[ 本地 ai-agent (PID 32935 / libai_agent.dylib) ]
         │
         │  上下文编排与参数结构序列化
         ▼
[ TTNet 网络服务 (PID 32923 / libsscronet.dylib) ]
         │
         │  QUIC (HTTP/3 UDP :443)
         ▼
[ 云端集群 api5-normal.mchost.guru:443 ]
```

---

## 三、 关键上行入参规范 (Request Payload)

请求通过 JSON-RPC `lite.create_chat_session` 构造，其关键参数结构如下：

```json
{
  "packet_type": "request",
  "channel_id": "<UUID>",
  "session_id": "<UUID>",
  "params": {
    "service": "lite",
    "method": "create_chat_session",
    "data": {
      "mode": "work",
      "origin": "lite",
      "auto_create_project": false,
      "initial_message": {
        "model_name": "DeepSeek-V4-Flash-Official",
        "agent_type": "solo_work_lite",
        "query": "[{\"type\":\"text\",\"data\":{\"content\":\"用户输入\"}}]",
        "custom_model": {
          "config_name": "DeepSeek-V4-Flash-Official",
          "display_model_name": "DeepSeek-V4-Flash 正式版",
          "prompt_max_tokens": 168000,
          "use_remote_service": true
        }
      }
    },
    "user_info": {
      "user_id": "<UID>",
      "token": "<JWT_ACCESS_TOKEN>",
      "scope": "marscode",
      "loginScope": "trae"
    },
    "common_params": {
      "device_id": "<16位数字 DeviceID>",
      "machine_id": "<64位 Hex MachineID>",
      "product_code": "SOLO_Lite",
      "solo_chat_mode": "work",
      "app_version": "0.1.63",
      "build_version": "2.3.81345",
      "arch": "arm64",
      "system": "darwin"
    }
  }
}
```

---

## 四、 下行事件流规范 (Downstream Event Stream)

云端通过 QUIC 双向流实时推回 16 类标准事件，由 `subscribe_events` 实时分发：

1. **`vm_operation_progress`**：远程开发环境状态同步（`initializing` -> `Workspace ready`）；
2. **`session_updated`**：会话元数据更新，下发全局唯一的 `chat_session_id`；
3. **`plan_item`**：包含模型阶段性思考输出：
   - `payload.thought`：模型的推理思考文本；
   - `payload.tool_call_info`：工具调用摘要；
4. **`output`**：模型的文本生成片段增量（`payload.choices[].text`）；
5. **`token_usage`**：消耗的实际 Token 与配额结算（含 `prompt_tokens`, `completion_tokens`）；
6. **`done`**：完成信号，带有 `payload.last_assistant_response`。

---

## 五、 第一阶段结论

1. **通道确认存在且完全可达**：`api5-normal.mchost.guru` 是官方 Work 专属端点；
2. **通信阻碍确认**：上游默认走 QUIC (UDP 443)，且由 `NetworkService` 维护底层会话；若纯 Go 脱壳模拟，必须解决 **QUIC/HTTP3 客户端建联** 或 **引导上游降级为标准 HTTP/2 TCP**。
