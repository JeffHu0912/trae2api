const modAha = Process.findModuleByName("libaha_net.dylib");
if (!modAha) {
    console.log("[-] libaha_net.dylib not found in this process!");
} else {
    console.log("[+] libaha_net.dylib base: " + modAha.base);

    const fnFetch = modAha.findExportByName("AhaNet_fetch");
    if (!fnFetch) {
        console.log("[-] AhaNet_fetch not exported!");
    } else {
        console.log("[+] Hooking AhaNet_fetch at " + fnFetch);
        Interceptor.attach(fnFetch, {
            onEnter(args) {
                try {
                    const req = args[0];
                    if (req.isNull()) {
                        console.log("[-] AhaNet_fetch called with NULL req");
                        return;
                    }

                    const methodPtr = req.readPointer();
                    const method = methodPtr.isNull() ? "(null)" : methodPtr.readCString();

                    const urlPtr = req.add(8).readPointer();
                    const url = urlPtr.isNull() ? "(null)" : urlPtr.readCString();

                    console.log("\n=======================================================");
                    console.log(`[AhaNet_fetch] Method: ${method}, URL: ${url}`);

                    const headersPtr = req.add(16).readPointer();
                    const headerCount = req.add(24).readU32();
                    console.log(`[Headers Count: ${headerCount}]`);

                    if (!headersPtr.isNull() && headerCount > 0 && headerCount < 100) {
                        for (let i = 0; i < headerCount; i++) {
                            const entry = headersPtr.add(i * 16);
                            const kPtr = entry.readPointer();
                            const vPtr = entry.add(8).readPointer();
                            const k = kPtr.isNull() ? "(null)" : kPtr.readCString();
                            const v = vPtr.isNull() ? "(null)" : vPtr.readCString();
                            console.log(`  ${k}: ${v}`);
                        }
                    }

                    // Dump the rest of the req struct
                    console.log("\n[Req Struct Dump (first 128 bytes)]:");
                    console.log(hexdump(req, { length: 128 }));

                    // Let's inspect potential body pointers at offset 32, 40, 48, etc.
                    for (let off = 32; off < 96; off += 8) {
                        try {
                            const p = req.add(off).readPointer();
                            if (!p.isNull()) {
                                const str = p.readUtf8String(512);
                                if (str && str.length > 5) {
                                    console.log(`[Req offset +${off} string]: ${str}`);
                                }
                            }
                        } catch(e) {}
                    }
                    console.log("=======================================================\n");
                } catch(e) {
                    console.log("[-] Error in AhaNet_fetch hook: " + e);
                }
            }
        });
    }
}
