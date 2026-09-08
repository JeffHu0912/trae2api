/**
 * capture_deep_protocol.js
 * Trae Work 深度线缆级协议全要素探针 (Phase 1 Deep Probe)
 * 
 * 针对 libai_agent.dylib (arm64) 精准 Hook：
 * 1. 系统网络层：getaddrinfo() & connect()
 * 2. 上游端点：Base + 0x2f2d628 (Target Request URL)
 * 3. 请求头：Base + 0x22bb888 ([HTTPClient] add_header)
 * 4. 任务主体：Base + 0x2f3b280 ([create_agent_task] request body)
 * 5. TLS 发射缓冲区：Base + 0x33f15a0 (tokio-rustls plaintext outbound)
 * 6. 响应头：Base + 0xdb4fa8 ([HTTPClient/Stream] response_headers)
 */

const LOG_FILE = "/Users/jeff/project/traework/frida/deep_capture.log";
const JSON_OUT = "/Users/jeff/project/traework/frida/captured_protocol_details.json";

function log(msg) {
    console.log(msg);
}

function safeReadString(p, maxLen = 2048) {
    if (!p || p.isNull()) return "";
    try {
        const s = p.readUtf8String();
        if (s && s.length > 0) return s.slice(0, maxLen);
    } catch(e) {}
    try {
        const s = p.readCString();
        if (s && s.length > 0) return s.slice(0, maxLen);
    } catch(e) {}
    return "";
}

function safeInspectMemory(p, len = 256) {
    if (!p || p.isNull()) return "";
    try {
        const raw = p.readByteArray(len);
        const u8 = new Uint8Array(raw);
        let ascii = "";
        for (let i = 0; i < u8.length; i++) {
            const c = u8[i];
            if (c >= 32 && c <= 126) ascii += String.fromCharCode(c);
            else if (c === 10 || c === 13) ascii += " ";
            else ascii += ".";
        }
        return ascii;
    } catch(e) {
        return "";
    }
}

log("=======================================================");
log("[*] capture_deep_protocol 探针脚本已加载，PID: " + Process.id);
log("=======================================================");

const capturedSession = {
    timestamp: new Date().toISOString(),
    dns: [],
    connections: [],
    urls: [],
    headers: [],
    requestBodies: [],
    tlsOutboundFrames: [],
    responseHeaders: []
};

// 1. DNS 解析
const getaddrinfoPtr = Module.findGlobalExportByName("getaddrinfo");
if (getaddrinfoPtr) {
    Interceptor.attach(getaddrinfoPtr, {
        onEnter(args) {
            try {
                const host = args[0].readUtf8String();
                if (host && (host.includes("trae") || host.includes("mchost") || host.includes("zijie") || host.includes("byte"))) {
                    log(`[DNS Resolve] -> ${host}`);
                    if (!capturedSession.dns.includes(host)) capturedSession.dns.push(host);
                }
            } catch(e) {}
        }
    });
}

// 2. TCP 连接
const connectPtr = Module.findGlobalExportByName("connect");
if (connectPtr) {
    Interceptor.attach(connectPtr, {
        onEnter(args) {
            try {
                const fd = args[0].toInt32();
                const sockaddr = args[1];
                const family = sockaddr.add(1).readU8();
                if (family === 2) { // AF_INET
                    const port = (sockaddr.add(2).readU8() << 8) | sockaddr.add(3).readU8();
                    const ip = [
                        sockaddr.add(4).readU8(),
                        sockaddr.add(5).readU8(),
                        sockaddr.add(6).readU8(),
                        sockaddr.add(7).readU8()
                    ].join(".");
                    if (port === 443 || port === 80) {
                        log(`[TCP Connect] -> ${ip}:${port} (fd: ${fd})`);
                        capturedSession.connections.push({ ip, port, fd, time: Date.now() });
                    }
                }
            } catch (e) {}
        }
    });
}

// 3. 挂钩 libai_agent.dylib 内部关键偏移
function hookAiAgent() {
    const mod = Process.findModuleByName("libai_agent.dylib");
    if (!mod) return false;

    log("[+] 发现 libai_agent.dylib 基地址: " + mod.base);

    // 3.1 请求 URL 拼接点 (Base + 0x2f2d628)
    try {
        const urlAddr = mod.base.add(ptr("0x2f2d628"));
        Interceptor.attach(urlAddr, {
            onEnter(args) {
                try {
                    // x21 存放 URL 字符串指针
                    const x21 = this.context.x21;
                    const urlStr = safeReadString(x21, 512);
                    if (urlStr) {
                        log(`\n[URL Hook] Target URL: ${urlStr}`);
                        capturedSession.urls.push({ url: urlStr, time: Date.now() });
                    }
                } catch(e) {
                    log("[-] URL Hook 读取异常: " + e);
                }
            }
        });
        log("[+] 已挂钩 URL 构造点: Base + 0x2f2d628");
    } catch(e) {
        log("[-] 挂钩 URL 构造点失败: " + e);
    }

    // 3.2 请求 Header 组装点 (Base + 0x22bb888)
    try {
        const headerAddr = mod.base.add(ptr("0x22bb888"));
        Interceptor.attach(headerAddr, {
            onEnter(args) {
                try {
                    // 检查栈和寄存器中的 Header 键值对
                    const sp = this.context.sp;
                    const val1 = safeInspectMemory(sp.add(0x8e0), 128);
                    const val2 = safeInspectMemory(sp.add(0xd40), 64);
                    log(`[Header Hook] Stack @ 0x8e0: ${val1.trim()}`);
                    capturedSession.headers.push({ stackVal: val1, time: Date.now() });
                } catch(e) {}
            }
        });
        log("[+] 已挂钩 Header 组装点: Base + 0x22bb888");
    } catch(e) {
        log("[-] 挂钩 Header 组装点失败: " + e);
    }

    // 3.3 请求 Body 组装点 (Base + 0x2f3b280)
    try {
        const bodyAddr = mod.base.add(ptr("0x2f3b280"));
        Interceptor.attach(bodyAddr, {
            onEnter(args) {
                try {
                    const x28 = this.context.x28;
                    // x28 是指向 Rust 字符串/切片的指针
                    const bodyStr = safeReadString(x28, 65536);
                    if (bodyStr && bodyStr.includes("{")) {
                        log(`\n>>>>>>>>>>>>>>>> [REQUEST BODY JSON (${bodyStr.length} chars)] >>>>>>>>>>>>>>>>`);
                        log(bodyStr.slice(0, 1000) + (bodyStr.length > 1000 ? "...[截断]" : ""));
                        log("----------------------------------------------------------------");
                        capturedSession.requestBodies.push({ body: bodyStr, time: Date.now() });
                    }
                } catch(e) {
                    log("[-] Body Hook 读取异常: " + e);
                }
            }
        });
        log("[+] 已挂钩 Body 组装点: Base + 0x2f3b280");
    } catch(e) {
        log("[-] 挂钩 Body 组装点失败: " + e);
    }

    // 3.4 TLS 明文发射点 (Base + 0x33f15a0)
    try {
        const tlsAddr = mod.base.add(ptr("0x33f15a0"));
        Interceptor.attach(tlsAddr, {
            onEnter(args) {
                try {
                    const buf = args[1];
                    const len = args[2].toInt32();
                    if (len > 0) {
                        const raw = buf.readByteArray(len);
                        const u8 = new Uint8Array(raw);
                        
                        let ascii = "";
                        let printableCount = 0;
                        for (let i = 0; i < u8.length; i++) {
                            const c = u8[i];
                            if (c >= 32 && c <= 126) {
                                ascii += String.fromCharCode(c);
                                printableCount++;
                            } else if (c === 10 || c === 13) {
                                ascii += "\n";
                                printableCount++;
                            } else {
                                ascii += ".";
                            }
                        }

                        const isRelevant = /create_agent_task|llm_utils_chat|llm_raw_chat|messages|session_id|authorization|x-ide|token|mchost|POST|GET|PRI \* HTTP/i.test(ascii);
                        if (isRelevant || (printableCount / len > 0.4 && len > 50)) {
                            log(`\n>>>>>>>>>>>>>>>> [TLS OUTBOUND FRAME (${len} bytes)] >>>>>>>>>>>>>>>>`);
                            log(ascii);
                            log("----------------------------------------------------------------");
                            capturedSession.tlsOutboundFrames.push({
                                len: len,
                                ascii: ascii,
                                hexHead: Array.from(u8.slice(0, Math.min(len, 64))).map(b => b.toString(16).padStart(2, '0')).join(' '),
                                time: Date.now()
                            });
                        }
                    }
                } catch(e) {}
            }
        });
        log("[+] 已挂钩 TLS 发射点: Base + 0x33f15a0");
    } catch(e) {
        log("[-] 挂钩 TLS 发射点失败: " + e);
    }

    // 3.5 响应头捕获点 (Base + 0xdb4fa8)
    try {
        const respHeaderAddr = mod.base.add(ptr("0xdb4fa8"));
        Interceptor.attach(respHeaderAddr, {
            onEnter(args) {
                try {
                    const x24 = this.context.x24;
                    const respStr = safeReadString(x24, 2048);
                    if (respStr) {
                        log(`\n[Response Headers Hook]: ${respStr}`);
                        capturedSession.responseHeaders.push({ headers: respStr, time: Date.now() });
                    }
                } catch(e) {}
            }
        });
        log("[+] 已挂钩 Response Headers 捕获点: Base + 0xdb4fa8");
    } catch(e) {
        log("[-] 挂钩 Response Headers 失败: " + e);
    }

    return true;
}

if (!hookAiAgent()) {
    log("[*] 等待 libai_agent.dylib 动态加载...");
    const timer = setInterval(() => {
        if (hookAiAgent()) {
            clearInterval(timer);
        }
    }, 500);
}
