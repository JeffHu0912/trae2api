# TraeWork 纯协议脱敏脱壳方案计划（方案二：Zero-Electron 纯 Go 客户端）

> **目标**：彻底剥离对 macOS 宿主机进程及 Electron 客户端的依赖，逆向还原官方 Trae Work（`work_credits`）通道的底层网络传输、签名算法与数据包格式，在 `trae2api` 中实现纯 Go 原生客户端，达成 100% 容器化、跨平台部署与多账号并发消费能力。

---

## 一、 项目背景与方案价值

### 1.1 现状痛点（方案 B / 现状）
- **平台强绑定**：当前 WorkBridge 运行在 macOS 宿主机上的 `traesolo-copy.app` 副本中，依赖 Mach-O 格式的 `libai_agent.dylib`，无法在 Linux 容器中运行。
- **资源与运维成本**：需要宿主机开机常驻 Electron 后台进程（约 75MB 内存），需管理 launchd 守护进程。
- **单账号瓶颈**：当前 WorkBridge 的 `bridge.js` 仅静态绑定了单个账号文件（`trae-4122512616609817.json`），无法为账号池中的多个账号动态切换 `work_credits` 扣费。

### 1.2 方案二核心收益
1. **纯代码运行（Zero-Electron）**：无任何 GUI、Electron、Xvfb 或系统级动态库依赖，纯静态 Go 编译，Docker 镜像体积 < 25MB，内存占用 < 30MB。
2. **多账号动态并发**：每个请求可按账号池调度器策略，动态携带不同账号的 Token 和设备指纹，实现多账号 `work_credits` 的全自动轮转与打满。
3. **原生跨平台**：可直接部署在任意 Linux 云服务器（x86_64 / arm64）、轻量 VPS 或 Kubernetes 集群。

---

## 二、 技术基线与差异分析

### 2.1 已掌握的资产与已知条件
1. **入参结构明文已捕获**：
   - 参见 `frida/captured_work_packet.json`，已捕获前端驱动 `ai-agent` 的全量参数：
     - `service: "lite"`, `method: "create_chat_session"`
     - `data.mode: "work"`, `data.initial_message.model_name: "DeepSeek-V4-Flash-Official"`
     - 完整的 `user_info`（JWT Token, user_id 等）、`common_params`（device_id, machine_id, arch, system 等）。
2. **底层执行载体定位明确**：
   - 模型请求由 `TRAE SOLO CN Helper.app`（PID utility 进程）内的 `libai_agent.dylib` 发起。
   - 底层采用 Rust 的 **`hyper-rustls 0.27` + `aws-lc-rs`** 构建 HTTP/2 连接。
3. **经反汇编实测验证的 ARM64 探针基准偏移**：
   - **请求 URL 构造点**：`libai_agent.dylib + 0x2f2d628`（`stp x8, x21, [x29, #-0x60]`，`x21` 为目标 URL 字符串指针）
   - **出站 Header 注入点**：`libai_agent.dylib + 0x22bb888`（`[HTTPClient] add_header`，`sp + 0x8e0` / `sp + 0xd40`）
   - **请求 Body 构造点**：`libai_agent.dylib + 0x2f3b280`（`[create_agent_task] request body:`，`x28` 为明文 JSON 切片）
   - **TLS 明文发射点**：`libai_agent.dylib + 0x33f15a0`（`tokio-rustls` 纯明文出站缓冲区）
   - **响应 Headers 解析点**：`libai_agent.dylib + 0xdb4fa8`（`[HTTPClient/Stream] response_headers:`，`x24` 指针）
   - 网络出站边界：`api5-normal.mchost.guru:443` 或 `trae-api-cn.mchost.guru:443`

### 2.2 待攻坚的核心技术卡点（Gap）

```
[前端 JSON-RPC (已掌握)]
         │
         ▼
[Rust libai_agent.dylib 内部黑盒 (待逆向)]
   ├── 1. 签名引擎：是否计算动态 HMAC / ECDSA 签名？(如 X-Signature, X-KSS-*)
   ├── 2. 载荷处理：明文 JSON-RPC 是否经过压缩(Gzip/Zstd)或对称加密(AES-GCM)？
   └── 3. 握手与会话：是否包含预检鉴权(Auth Exchange)或长连接保持？
         │
         ▼
[出站 HTTP/2 Wire 原始流量 (待捕获)]
         │
         ▼
[服务端 api5-normal.mchost.guru:443]
```

---

## 三、 阶段实施路线图

### 阶段一：出站协议全要素深度捕获 (Deep Wire-Level Packet Inspection)
**目标**：捕获从 `libai_agent.dylib` 发送给 `api5-normal.mchost.guru` 的真实 HTTP/2 请求报文（URL、全部 Header、原始 Body 二进制），明确载荷是否被加密。

- **实施步骤**：
  1. **升级 Frida 探针脚本**：在 `frida/capture_work_credits.js` 基础上，针对 `+0x2f2d628` (URL)、`+0x22bb888` (Headers)、`+0x2f3b280` (Body) 挂钩，同时拦截 `connect` 建立连接后的首个 TLS 发射包。
  2. **提取完整 Request 结构**：
     - 记录确切的 HTTP Path（例如 `/api/agent/v3/...` 或 `/v2/lite/...`）。
     - 记录所有特殊 Header 列表（如 `X-Device-Id`, `X-Machine-Id`, `X-Timestamp`, `X-Signature`, `X-Tt-Token`, `Authorization`）。
  3. **提取 Response 与 SSE 原始帧**：
     - 拦截上行发送完成后，服务端下发的第一帧及后续流式分块。
  4. **载荷属性定性**：
     - 若 Body 为明文 JSON：直接进入阶段三；
     - 若 Body 为二进制加密体（如 `encrypted_prompt_set`）：进入阶段二进行密码学逆向。

- **产出交付物**：
  - `docs/work-wire-protocol-spec.md`：包含抓包还原的完整 HTTP/2 报文样例。

---

### 阶段二：密码学与动态签名逆向 (Cryptographic & Sign Reversing)
**目标**：如果请求包含动态签名或载荷加密，分析其算法逻辑并在纯代码中重现。

- **实施步骤**：
  1. **静态反汇编分析**：
     - 将 `libai_agent.dylib` 载入 Hopper Disassembler / Ghidra。
     - 检索字符串：查找 Header 名（如 `x-signature`、`sign`、`token`、`encrypt` 等）的引用交叉点。
     - 检索 Rust Crypto 符号：定位 `ring`、`aes_gcm`、`sha2`、`hmac` 等依赖项。
  2. **动态寄存器与入参追踪**：
     - 使用 Frida 对签名计算函数的入参寄存器做跟踪，对比（待签名原文字符串 ↔ 输出签名），确认是否为标准 `HMAC-SHA256` 或私钥签名。
     - 提取 Salt / Key 来源：确认秘钥是写死在二进制中的常量、由 `deviceId` 派生，还是由登录时返回的 `accessToken` 解密得出。
  3. **原型验证**：
     - 编写独立的验证脚本（Node.js / Go），传入相同入参，验证生成的签名/密文与官方客户端生成的完全一致。

- **产出交付物**：
  - `docs/work-crypto-algorithm.md`：签名与加密算法规范文档。
  - `frida/verify_signature.go`：算法对齐独立验证程序。

---

### 阶段三：纯 Go 协议客户端研发 (`internal/upstream/work_client.go`)
**目标**：脱离任何外部依赖，在 Go 中实现对 `api5-normal.mchost.guru` 的高并发调用。

- **实施步骤**：
  1. **HTTP/2 客户端与连接池实现**：
     - 基于标准 `net/http` 与 `golang.org/x/net/http2` 构建支持 H2 多路复用的长连接池。
     - 模拟合法 TLS Client Hello 指纹（必要时利用 `utls` 库对抗服务端指纹识别）。
  2. **请求组装器（Builder）**：
     - 根据传入的 `*auth.Auth`，动态填入对应的 `AccessToken`、`DeviceID`、`MachineID`。
     - 执行阶段二沉淀的签名与加密算法，生成完整的 Headers 与 Body。
  3. **下行流式解析器（SSE Stream Parser）**：
     - 编写高性能流式解析器，监听 `plan_item`、`output`、`token_usage`、`done` 等事件。
     - 转换为标准 OpenAI `chat.completion.chunk` SSE 规范。
  4. **单元测试与 Mock 回放**：
     - 使用阶段一捕获的真实报文构建 Mock Server，验证边界情况（网络断开、超时、429 等）。

- **产出交付物**：
  - `internal/upstream/work_client.go`
  - `internal/upstream/work_client_test.go`

---

### 阶段四：网关层平滑接管与全容器化交付
**目标**：在 `trae2api` 主程序中替换原本的 `proxyWorkBridge`，完成多账号调度与 Dockerfile 瘦身。

- **实施步骤**：
  1. **网关逻辑改造（`internal/server/handler.go`）**：
     - 原逻辑：`proxyWorkBridge(w, body, stream)` 转发 HTTP 到 7865。
     - 新逻辑：直接调用 `h.workClient.ChatStream(ctx, account, req)`，直接消耗当前调度选中账号的 `work_credits`。
     - 扩展多账号支持：打破单账号限制，支持按可用积分降序在多个账号之间轮流扣除 `work_credits`。
  2. **配置与回退开关设计**：
     - 增加 `TW2A_WORK_MODE=native|bridge|disabled`，支持故障时一键切回外部 Bridge。
  3. **容器化重构**：
     - 确认无需任何额外依赖，直接基于 Alpine 或 Scratch 构建最终 Docker 镜像。
     - 移除 `docker-compose.yml` 中的 `extra_hosts: host-gateway`。
  4. **线上端到端校验**：
     - 验证流式与非流式调用；
     - 调用 `credit.sh` 实时监控各账号 `work_credits` 是否正常扣减。

---

## 四、 风险评估与应对预案

| 风险项 | 影响评估 | 应对预案 |
|---|---|---|
| **算法包含高强度混淆/硬件私钥签名** | 逆向还原耗时不可控 | 若阶段二探测到难以还原的设备私钥安全芯片签名，立即平滑回退至**方案一（Linux 版 Trae 镜像集成）**，依然能达成无宿主机依赖的容器化目标。 |
| **官方升级协议版本（Version Drift）** | 协议失效导致请求被拒 | 设计协议降级机制，保持请求版本号、UA 和 IdeVersionCode 的集中配置化管理，定期比对官方新版本。 |
| **风控关联封号风险** | 账号异常被封禁 | 必须严格保证每个账号使用其专有且固化的 `deviceId` 和 `machineId`，绝不跨账号共享设备指纹，并在出站请求中精准模拟原生客户端的全部参数。 |

---

## 五、 执行进展与里程碑检查清单 (Milestone Tracking)

- [x] **M0: 方案规划与静态反汇编** - 完成方案二可行性规划文件编写，利用 LLDB 对 `libai_agent.dylib` 完成 ARM64 汇编反编译，准确定位 URL (`0x2f2d628`)、Header (`0x22bb888`)、Body (`0x2f3b280`)、TLS (`0x33f15a0`)、Response (`0xdb4fa8`) 五大核心探针偏移。
- [x] **M1: 协议抓包与 Wire 报文定性** - 完成在线请求捕获与网络协议全要素还原：目标端点确认为 `api5-normal.mchost.guru:443`，底层由 Chromium NetworkService (`libsscronet.dylib` / `libaha_net.dylib`) 走 **QUIC (HTTP/3 over UDP)** 传输，产出全套协议规范文档 [`docs/work-wire-protocol-spec.md`](work-wire-protocol-spec.md)。
- [x] **M2: 签名算法与协议降级分析** - 验证服务端 100% 兼容标准 HTTP/2 TCP 握手（成功实现 QUIC 降级）；逆向证实无需任何私钥/动态签名或 AES 载荷加密，依赖纯 Cloud-IDE-JWT 鉴权体系与明文 JSON/SSE 传输；交付算法文档 [`docs/work-crypto-algorithm.md`](work-crypto-algorithm.md) 与独立验证程序 [`frida/verify_signature.go`](../frida/verify_signature.go)。
- [x] **M3: Go 原型跑通** - 完成 `internal/upstream/work_client.go` 原生客户端实现、HTTP/2 连接池、Header 矩阵与下行 Work SSE 流式转换，单元测试 100% 通过；编写验证程序 `cmd/verify_work_credits`，实机完成单账号单次成功扣费验证（扣除 ~1.4 点 `work_credits`）。
- [x] **M4: 服务集成** - 改造 `internal/pool`（新增 Work 积分状态机、独立冷却与 `PickWorkExcluding` 动态调度）、`cmd/server`（注入 `WorkClient` 与 `TW2A_WORK_MODE=auto|native|bridge|disabled` 环境变量）与 `internal/server/handler.go`（彻底移除旧单账号 `proxyWorkBridge`，接入多账号动态轮转、故障重试与异步积分刷新）；单元测试 100% 通过并通过实机多账号与网关端到端验证。
- [x] **M5: 容器交付** - 优化 `Dockerfile` 与 `docker-compose.yml`，全容器化重建并部署上线（内存常驻仅 ~11.5MB）；实测通过 SOLO 与 Work 双通道流式/非流式端到端请求与扣费闭环验证，方案二全生命周期圆满达成。
