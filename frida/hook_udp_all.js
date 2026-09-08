const kmod = Process.findModuleByName("libsystem_kernel.dylib");
const targets = [
    "sendto", "__sendto", "__sendto_nocancel", "sendto$NOCANCEL",
    "sendmsg", "__sendmsg", "__sendmsg_nocancel", "sendmsg$NOCANCEL", "sendmsg_x"
];

console.log("[+] Hooking all UDP send variants in PID: " + Process.id);
targets.forEach(t => {
    const p = kmod.findExportByName(t);
    if (p) {
        console.log(`Hooking ${t} at ${p}`);
        Interceptor.attach(p, {
            onEnter(args) {
                const fd = args[0].toInt32();
                if (fd === 27) {
                    let modName = "unknown";
                    try {
                        const m = Process.getModuleByAddress(this.returnAddress);
                        if (m) modName = m.name + "+" + this.returnAddress.sub(m.base);
                    } catch(e) {}
                    console.log(`[${t}] fd=27 caller=${modName}`);
                }
            }
        });
    }
});
