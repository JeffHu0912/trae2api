/**
 * Trae Work (work_credits) Frida Hook 探针脚本
 * 终极明文拦截版：挂钩 tokio-rustls TLS 流缓冲与系统网络解析
 */

const LOG_FILE_PATH = "/Users/jeff/project/traework/frida/capture.log";

function logToFile(msg) {
    console.log(msg);
}

console.log("[*] Frida 脚本加载成功，PID:", Process.id);

// 1. Hook DNS 域名解析 (getaddrinfo)，直接还原所有真实访问域名（突破 Clash Fake-IP）
const getaddrinfoPtr = Module.findGlobalExportByName("getaddrinfo");
if (getaddrinfoPtr) {
    Interceptor.attach(getaddrinfoPtr, {
        onEnter(args) {
            try {
                const host = args[0].readUtf8String();
                if (host && (host.includes("trae") || host.includes("mchost") || host.includes("zijie") || host.includes("byte"))) {
                    logToFile(`\n[DNS Resolve] Host: ${host}`);
                }
            } catch(e) {}
        }
    });
    console.log("[+] 已挂钩 getaddrinfo() 域名解析");
}

// 2. Hook connect() 监听出站 IP 与端口
const connectPtr = Module.findGlobalExportByName("connect");
if (connectPtr) {
    Interceptor.attach(connectPtr, {
        onEnter(args) {
            try {
                const fd = args[0].toInt32();
                const sockaddr = args[1];
                const family = sockaddr.add(1).readU8();
                if (family === 2) {
                    const port = (sockaddr.add(2).readU8() << 8) | sockaddr.add(3).readU8();
                    const ip = [
                        sockaddr.add(4).readU8(),
                        sockaddr.add(5).readU8(),
                        sockaddr.add(6).readU8(),
                        sockaddr.add(7).readU8()
                    ].join(".");
                    if (port === 443 || port === 80) {
                        logToFile(`[TCP Connect] -> ${ip}:${port} (fd: ${fd})`);
                    }
                }
            } catch (e) {}
        }
    });
    console.log("[+] 已挂钩 connect() 连接监视");
}

// 3. Hook libai_agent.dylib 中的 TLS 明文数据流 (tokio-rustls write_buf)
function initAiAgentHooks() {
    const mod = Process.findModuleByName("libai_agent.dylib");
    if (!mod) {
        return false;
    }

    console.log("\n=======================================================");
    console.log("[+] 发现 libai_agent.dylib 基址:", mod.base);
    console.log("=======================================================");

    // tokio-rustls 写入 TLS 之前的纯明文缓冲区 (Base + 0x33f15a0)
    // args[1]: 指向明文数据的指针
    // args[2]: 数据的字节长度
    const tlsWriteBufOffset = ptr("0x33f15a0");
    const hookAddr = mod.base.add(tlsWriteBufOffset);

    try {
        Interceptor.attach(hookAddr, {
            onEnter(args) {
                try {
                    const buf = args[1];
                    const len = args[2].toInt32();
                    if (len > 0) {
                        const raw = buf.readByteArray(len);
                        const u8 = new Uint8Array(raw);
                        
                        // 提取所有可打印字符构成的字符串片段
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

                        // 判断是否为 HTTP 请求或包含关键信息
                        const isRelevant = /create_agent_task|llm_utils_chat|llm_raw_chat|messages|session_id|authorization|x-ide|token|mchost|POST|GET/i.test(ascii);

                        if (isRelevant || (printableCount / len > 0.4 && len > 50)) {
                            logToFile("\n>>>>>>>>>>>>>>>> [TLS PLAINTEXT OUTBOUND (" + len + " bytes)] >>>>>>>>>>>>>>>>");
                            logToFile(ascii);
                            logToFile("----------------------------------------------------------------");
                        }
                    }
                } catch(e) {
                    logToFile("[-] 解析 TLS 明文缓冲出错: " + e);
                }
            }
        });
        console.log("[+] 已挂钩 tokio-rustls TLS 明文发射点 (Base + 0x33f15a0)");
    } catch (e) {
        console.log("[-] 挂钩 TLS 发射点失败: " + e);
    }

    return true;
}

if (!initAiAgentHooks()) {
    console.log("[*] 等待 libai_agent.dylib 加载...");
    const interval = setInterval(() => {
        if (initAiAgentHooks()) {
            clearInterval(interval);
        }
    }, 500);
}
