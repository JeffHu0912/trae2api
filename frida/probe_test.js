const mod = Process.findModuleByName("libai_agent.dylib");
if (!mod) {
    console.log("[-] libai_agent.dylib not found!");
} else {
    console.log("[+] libai_agent.dylib base: " + mod.base);

    function dumpMem(p, len = 256) {
        if (!p || p.isNull()) return "(null)";
        try {
            const raw = p.readByteArray(len);
            const u8 = new Uint8Array(raw);
            let s = "";
            for (let i = 0; i < u8.length; i++) {
                const c = u8[i];
                if (c >= 32 && c <= 126) s += String.fromCharCode(c);
                else s += ".";
            }
            return s;
        } catch(e) {
            return "(err: " + e + ")";
        }
    }

    function dumpRegs(ctx, regs) {
        let out = {};
        for (const r of regs) {
            try {
                const p = ctx[r];
                out[r] = {
                    hex: p ? p.toString() : "null",
                    preview: dumpMem(p, 64)
                };
            } catch(e) {}
        }
        return out;
    }

    // 1. URL construction point
    Interceptor.attach(mod.base.add(ptr("0x2f2d628")), {
        onEnter(args) {
            console.log("\n>>> [PROBE] Hit 0x2f2d628 (URL)");
            console.log("x21:", this.context.x21, dumpMem(this.context.x21, 128));
            console.log("x8:", this.context.x8, dumpMem(this.context.x8, 64));
            console.log("x9:", this.context.x9, dumpMem(this.context.x9, 64));
            console.log("sp:", this.context.sp, dumpMem(this.context.sp, 128));
        }
    });

    // 2. Request body point
    Interceptor.attach(mod.base.add(ptr("0x2f3b280")), {
        onEnter(args) {
            console.log("\n>>> [PROBE] Hit 0x2f3b280 (Body)");
            console.log("x28:", this.context.x28, dumpMem(this.context.x28, 256));
            console.log("x8:", this.context.x8);
            try {
                const len = this.context.x8.toInt32();
                if (len > 0 && len < 100000) {
                    console.log("Body string:\n", this.context.x28.readUtf8String(len));
                }
            } catch(e) {
                console.log("Body read error:", e);
            }
        }
    });

    // 3. Header point
    Interceptor.attach(mod.base.add(ptr("0x22bb888")), {
        onEnter(args) {
            console.log("\n>>> [PROBE] Hit 0x22bb888 (Header)");
            console.log("x27:", this.context.x27, dumpMem(this.context.x27, 64));
            console.log("sp@0x8e0:", dumpMem(this.context.sp.add(0x8e0), 128));
            console.log("sp@0xd40:", dumpMem(this.context.sp.add(0xd40), 128));
        }
    });

    // 4. TLS outbound point
    Interceptor.attach(mod.base.add(ptr("0x33f15a0")), {
        onEnter(args) {
            console.log("\n>>> [PROBE] Hit 0x33f15a0 (TLS)");
            console.log("args[0]:", args[0]);
            console.log("args[1]:", args[1], dumpMem(args[1], 128));
            console.log("args[2]:", args[2]);
        }
    });

    // 5. Response headers
    Interceptor.attach(mod.base.add(ptr("0xdb4fa8")), {
        onEnter(args) {
            console.log("\n>>> [PROBE] Hit 0xdb4fa8 (Response Headers)");
            console.log("x24:", this.context.x24, dumpMem(this.context.x24, 256));
        }
    });
}
