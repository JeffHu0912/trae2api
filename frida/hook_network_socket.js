const kmod = Process.findModuleByName("libsystem_kernel.dylib");
const sendtoPtr = kmod.findExportByName("sendto");
const sendmsgPtr = kmod.findExportByName("sendmsg");

console.log("[+] Hooking BSD socket APIs in PID: " + Process.id);
console.log("sendtoPtr:", sendtoPtr, "sendmsgPtr:", sendmsgPtr);

if (sendtoPtr) {
    Interceptor.attach(sendtoPtr, {
        onEnter(args) {
            const fd = args[0].toInt32();
            const len = args[2].toInt32();
            let modName = "unknown";
            let offset = "0";
            try {
                const mod = Process.getModuleByAddress(this.returnAddress);
                if (mod) {
                    modName = mod.name;
                    offset = this.returnAddress.sub(mod.base).toString();
                }
            } catch(e) {}
            console.log(`[sendto] fd=${fd}, len=${len}, caller=${modName}+${offset}`);
        }
    });
}

if (sendmsgPtr) {
    Interceptor.attach(sendmsgPtr, {
        onEnter(args) {
            const fd = args[0].toInt32();
            let modName = "unknown";
            let offset = "0";
            try {
                const mod = Process.getModuleByAddress(this.returnAddress);
                if (mod) {
                    modName = mod.name;
                    offset = this.returnAddress.sub(mod.base).toString();
                }
            } catch(e) {}
            console.log(`[sendmsg] fd=${fd}, caller=${modName}+${offset}`);
        }
    });
}
