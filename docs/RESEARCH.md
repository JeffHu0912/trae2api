# TRAE 反代研究笔记

> 本仓库的记录：TRAE（字节跳动 AI IDE）各通道的反代可行性、积分体系、限流机制与实测发现。
> 记录时间：2026-08。部分结论来自公开逆向调查（文末标注来源），部分为本地实测。

---

## 1. 上游反代项目全景

| 项目 | 通道 | 说明 |
|---|---|---|
| [Sliverkiss/traework2api](https://github.com/Sliverkiss/traework2api) | `solo_work_lite` | Go，本仓库上游。SOLO 免费对话 → OpenAI 兼容 |
| [Ttungx/trae-solo-local-api](https://github.com/Ttungx/trae-solo-local-api) | `solo_work_lite` | JS，同通道 |
| [laojichao/trae-local-api](https://github.com/laojichao/trae-local-api) | Trae IDE (CN/SOLO/SG) | Node，四版本，tc 加密 auth 自动解密 |
| [muskke/trae-api-proxy](https://github.com/muskke/trae-api-proxy) | Trae API | Go，Header 签名 + payload 转换 |
| [Sliverkiss/workbuddy2api](https://github.com/Sliverkiss/workbuddy2api) | 腾讯 WorkBuddy | **注意：WorkBuddy ≠ TRAE Work**，不同产品 |

所有可用的反代都走 **SOLO/IDE 免费通道（消耗 ide_credits）**，没有任何项目反代 work 通道（work_credits）。

## 2. SOLO 通道机制

- 对话端点：`POST https://trae-api-cn.mchost.guru/api/agent/v3/llm_utils_chat`
- 关键参数：`function: "solo_work_lite"`（实测：其他 function 值如 `work`/`solo`/`work_lite` 均无效）
- 模型表：`POST /api/ide/v1/get_detail_param`（`config_name` 列表，动态下发）
- 鉴权：`Cloud-IDE-JWT <accessToken>` + `X-Cloudide-Token` + `X-Uid` + `X-Machine-Id` / `X-Device-Id`
- token 获取：`refreshToken` → `ExchangeToken`（轮换 refresh）→ `GetUserInfo`（uid）
- 登录：`https://www.trae.cn/authorization` 强制回调 `127.0.0.1`（浏览器与服务器无需同机，回调链接粘贴即可）
- SSE 格式：`event:notify_usage` / `event:metadata` / `event:error` / `event:done`

### 版本号与模型解锁（实测）

- 上游按 `X-Ide-Version` / `X-App-Version-Code` 版本控制模型可用性
- 实测：`0.1.43` 请求 `glm-5.3` 报 `4001 param is invalid`；**`0.1.52`（20260811）正常对话**
- 模型列表随版本动态更新（`get_detail_param` 0.1.52 返回 35 个 config，含 glm-5.3）

## 3. 积分体系（实测）

对话流中 `event:notify_usage` 返回计费结构：

```json
"billing_mode": "credits",
"cn_credits_remain_info": {"ide_credits": 0, "work_credits": 2000}
```

| 类型 | 用途 | 反代可用 |
|---|---|---|
| `ide_credits` | SOLO 对话（`solo_work_lite`） | 可用（本项目采用） |
| `work_credits` | TRAE Work 编程 Agent | 不可用（见下文说明） |

- 额度查询：`POST /trae/api/v2/pay/ide_user_ent_usage`（聚合 `user_entitlement_pack_list[].entitlement_base_info.quota.credits_limit`，`usage.credits_amount` 为已用）
- **注意**：`ide_user_ent_usage` 聚合的是 entitlement 包（含 work 包），显示 `remain=2000` 实为 work_credits，**不代表 SOLO 通道可用额度**。SOLO 真正看 `notify_usage.cn_credits_remain_info.ide_credits`
- 签到：`POST /trae/api/v2/ug/checkin_credits/status` + `/claim`（每日 +200，高峰常返回 `9074 当前使用人数太多`）
- ide_credits 耗尽：对话报 `4008 Your requests have exceeded the quota`（视为配额限制，短时重试无效，等每日重置或签到）

## 4. work 通道最新突破（实机捕获与结论修正）

> **2026-09 最新实测证实**：此前关于“work 通道完全不可反代”的推论已被推翻。
> 原生客户端并非必须由云端编排加密，而是由前端（Workbench / TransportManager）通过标准 JSON-RPC 驱动本地 `ai-agent` 进程的 `lite.create_chat_session` 接口。

### 4.1 核心机制实测验证
- **扣费证据**：实测调用 `mode: "work"` 后，账号的 `work_credits`（福利积分）从 2144 减少至 2139，成功消耗！
- **通道协议**：
  - 前端向本地 `ai-agent` 发送 `service: "lite"`, `method: "create_chat_session"`;
  - `initial_message` 中包含 `mode: "work"`, `agent_type: "solo_work_lite"`, `model_name: "DeepSeek-V4-Flash-Official"`;
  - 底层 Rust 进程随后向 `api5-normal.mchost.guru:443` 建立 HTTP/2 多路复用连接传输加密流；
  - 客户端实时接收 16 类标准 SSE 事件流（`metadata`, `plan_item`, `output`, `token_usage`, `done` 等）。

### 4.2 反代落地路径
1. **方案 B（推荐，零封号风险）**：直接通过本地 IPC / JSON-RPC 调用本机的 `ai-agent` 服务（`service: "lite"`, `method: "create_chat_session"`），将 `trae2api` 包装成标准 OpenAI 接口。由本地进程自行处理设备签名与上游通信，100% 消耗 `work_credits`。
2. **完整抓包数据**：完整请求体结构及 38 个 Stream Chunks 样例已固化保存于 `frida/captured_work_packet.json`。

### 4.3 Docker 部署（方案 B host-gateway 拓扑）
- **WorkBridge（扣分引擎）**：常驻宿主机 `traesolo-copy.app`，其 `bridge.js` 监听 `0.0.0.0:7865`，可选 Bearer token 鉴权（缺省 `twbridge-local-7f3a`）。
- **trae2api（Docker）**：`docker-compose.yml` 新增 `extra_hosts: host-gateway`，`TW2A_WORK_BRIDGE_URL=http://host-gateway:7865` + `TW2A_WORK_BRIDGE_TOKEN`。
- **三条自动路由**：①请求 Work 模型直接走本地桥接；②免费通道 4008 自动降级；③账号全部冷却时兜底。
- 已实测端到端通过：非流式/流式均返回正确内容，work_credits 实时扣减，普通模型(glm-5.2)在 ide_credits=0 时自动降级成功。


## 5. 限流 / 错误码速查（实测 + 上游代码）

| code | 含义 | 处理 |
|---|---|---|
| 1001 | 认证失败（token 失效） | 重新登录（换 refreshToken） |
| 1005 | plan 权益不足 | 长冷却（12h） |
| 4001 | 参数无效（模型不存在/版本不匹配） | 升级 `IdeVersion` 或换模型 |
| 4008 | 配额超限（ide_credits 耗尽） | 等每日重置 / 签到 |
| 4011 | 请求频率超限 | 等限流窗口 |
| 9074 | 签到人数过多 | 重试（`checkin_retry.sh`） |
| 429 | 软限流 | 短冷却（60s） |

## 6. Windows 环境坑（本仓库实测修复）

- **python3 商店占位别名**：`C:\Users\...\WindowsApps\python3` 指向 `AppInstallerPythonRedirector.exe`，非交互下恒 exit 49（连 `print('hi')` 都失败）。修复：检测后回退 `python`
- **GBK 写文件炸**：`open("w")` 默认 GBK，遇非 GBK 字符抛 `UnicodeEncodeError` 且**先清空已有文件**（实测损坏凭证）。修复：UTF-8 + tmp/rename 原子写
- **回调昵称双重编码**：TRAE 回调 `userInfo` 中文双重 URL 编码，`parse_qs` 一层解不干净 → 昵称乱码（实测 `Óû§8847309959`）。修复：`fix_mojibake` 回转失败则回退 `用户+uid末4位`

## 7. 结论

1. **SOLO 免费通道（ide_credits）是唯一可反代的 TRAE 通道**，上游刻意放行 `solo_work_lite`（免费引流，额度天花板 + 限流兜底）
2. **work_credits（TRAE Work）无法通过 API 反代**——加密配置锁在本地进程，商业上也不可能开放
3. 反代的价值场景：作为 **Claude Code / Cline / Codex 等自带 agent 编排客户端的模型后端**（OpenAI 兼容 + function calling），而非替代 work 的云端 agent
