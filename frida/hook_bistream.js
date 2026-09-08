const mod = Process.findModuleByName("libsscronet.dylib");
if (!mod) {
    console.log("[-] libsscronet.dylib not found!");
} else {
    console.log("[+] libsscronet.dylib base: " + mod.base);

    // 1. bidirectional_stream_start
    const fnStart = mod.findExportByName("bidirectional_stream_start");
    if (fnStart) {
        Interceptor.attach(fnStart, {
            onEnter(args) {
                try {
                    const stream = args[0];
                    const url = args[1].readCString();
                    const method = args[3].readCString();
                    console.log("\n=======================================================");
                    console.log(`[BIDIRECTIONAL STREAM START] Method: ${method}, URL: ${url}`);
                    
                    // headers array: args[4]
                    const hArray = args[4];
                    if (!hArray.isNull()) {
                        const count = hArray.readU64().valueOf();
                        const capacity = hArray.add(8).readU64().valueOf();
                        const entries = hArray.add(16).readPointer();
                        console.log(`Headers count: ${count}`);
                        if (!entries.isNull()) {
                            for (let i = 0; i < count; i++) {
                                const kPtr = entries.add(i * 16).readPointer();
                                const vPtr = entries.add(i * 16 + 8).readPointer();
                                const k = kPtr.isNull() ? "" : kPtr.readCString();
                                const v = vPtr.isNull() ? "" : vPtr.readCString();
                                console.log(`  ${k}: ${v}`);
                            }
                        }
                    }
                    console.log("=======================================================\n");
                } catch(e) {
                    console.log("[-] bidirectional_stream_start error:", e);
                }
            }
        });
        console.log("[+] Hooked bidirectional_stream_start");
    }

    // 2. bidirectional_stream_write
    const fnWrite = mod.findExportByName("bidirectional_stream_write");
    if (fnWrite) {
        Interceptor.attach(fnWrite, {
            onEnter(args) {
                try {
                    const buf = args[1];
                    const count = args[2].toInt32();
                    const endOfStream = args[3].toInt32();
                    console.log(`\n>>> [BIDIRECTIONAL STREAM WRITE] ${count} bytes (end: ${endOfStream})`);
                    if (count > 0 && !buf.isNull()) {
                        const text = buf.readUtf8String(Math.min(count, 4096));
                        console.log(text || hexdump(buf, { length: Math.min(count, 128) }));
                    }
                } catch(e) {
                    console.log("[-] bidirectional_stream_write error:", e);
                }
            }
        });
        console.log("[+] Hooked bidirectional_stream_write");
    }

    // 3. bidirectional_stream_read
    const fnRead = mod.findExportByName("bidirectional_stream_read");
    if (fnRead) {
        Interceptor.attach(fnRead, {
            onEnter(args) {
                this.buf = args[1];
                this.cap = args[2].toInt32();
            },
            onLeave(retval) {
                try {
                    // In many implementations read callback or return gives read count
                } catch(e) {}
            }
        });
    }

    // 4. BiStreamClient
    const fnBiSend = mod.findExportByName("Cronet_BiStreamClient_SendData");
    if (fnBiSend) {
        Interceptor.attach(fnBiSend, {
            onEnter(args) {
                try {
                    const len = args[2].toInt32();
                    console.log(`\n>>> [Cronet_BiStreamClient_SendData] len: ${len}`);
                    if (len > 0) {
                        console.log(args[1].readUtf8String(Math.min(len, 2048)));
                    }
                } catch(e) {}
            }
        });
        console.log("[+] Hooked Cronet_BiStreamClient_SendData");
    }

    const fnBiRecv = mod.findExportByName("Cronet_BiStreamDelegate_OnReceivedData");
    if (fnBiRecv) {
        Interceptor.attach(fnBiRecv, {
            onEnter(args) {
                try {
                    const len = args[2].toInt32();
                    console.log(`\n<<< [Cronet_BiStreamDelegate_OnReceivedData] len: ${len}`);
                    if (len > 0) {
                        console.log(args[1].readUtf8String(Math.min(len, 500)));
                    }
                } catch(e) {}
            }
        });
        console.log("[+] Hooked Cronet_BiStreamDelegate_OnReceivedData");
    }
}
