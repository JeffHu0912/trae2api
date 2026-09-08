/**
 * capture_ahanet_live.js
 * 挂钩 libaha_net.dylib 与 libsscronet.dylib 中的所有发送与接收函数
 */

const modAha = Process.findModuleByName("libaha_net.dylib");
if (modAha) {
    console.log("[+] libaha_net base:", modAha.base);

    // Hook WsClient send
    const fnSend = modAha.findExportByName("AhaNet_WsClient_send");
    if (fnSend) {
        Interceptor.attach(fnSend, {
            onEnter(args) {
                try {
                    const len = args[2].toInt32();
                    const text = args[1].readUtf8String(Math.min(len, 2048));
                    console.log("\n>>>>>>>>>>>>>>>> [AhaNet WS SEND (" + len + "B)] >>>>>>>>>>>>>>>>");
                    console.log(text || "[Binary Data]");
                    console.log("----------------------------------------------------------------");
                } catch(e) {
                    console.log("[-] WS Send error:", e);
                }
            }
        });
        console.log("[+] 已挂钩 AhaNet_WsClient_send");
    }

    // Hook Fetch
    const fnFetch = modAha.findExportByName("AhaNet_fetch");
    if (fnFetch) {
        Interceptor.attach(fnFetch, {
            onEnter(args) {
                try {
                    const url = args[0].readPointer().readCString();
                    console.log("\n>>>>>>>>>>>>>>>> [AhaNet FETCH] >>>>>>>>>>>>>>>>");
                    console.log("URL:", url);
                } catch(e) {}
            }
        });
        console.log("[+] 已挂钩 AhaNet_fetch");
    }
}

// 检查 SSL_write
try {
    const sslWrite = Module.findExportByName("libsscronet.dylib", "SSL_write") || Module.findExportByName("libaha_net.dylib", "SSL_write");
    if (sslWrite) {
        Interceptor.attach(sslWrite, {
            onEnter(args) {
                try {
                    const len = args[2].toInt32();
                    if (len > 0) {
                        const str = args[1].readUtf8String(Math.min(len, 2048));
                        if (str && (str.includes("POST") || str.includes("GET") || str.includes("mchost") || str.includes("agent") || str.includes("lite") || str.includes("session"))) {
                            console.log("\n>>>>>>>>>>>>>>>> [SSL_write (" + len + "B)] >>>>>>>>>>>>>>>>");
                            console.log(str.slice(0, 500));
                            console.log("----------------------------------------------------------------");
                        }
                    }
                } catch(e) {}
            }
        });
        console.log("[+] 已挂钩 SSL_write");
    }
} catch(e) {}
