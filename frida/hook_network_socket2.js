const kmod = Process.findModuleByName("libsystem_kernel.dylib");
const sendPtr = kmod.findExportByName("send");
const writePtr = kmod.findExportByName("write");
const writevPtr = kmod.findExportByName("writev");

console.log("[+] Hooking write / writev / send in PID: " + Process.id);
console.log("send:", sendPtr, "write:", writePtr, "writev:", writevPtr);

if (sendPtr) {
    Interceptor.attach(sendPtr, {
        onEnter(args) {
            const fd = args[0].toInt32();
            if (fd === 27) {
                console.log(`[send] fd=27 len=${args[2].toInt32()}`);
            }
        }
    });
}

if (writePtr) {
    Interceptor.attach(writePtr, {
        onEnter(args) {
            const fd = args[0].toInt32();
            if (fd === 27) {
                console.log(`[write] fd=27 len=${args[2].toInt32()}`);
            }
        }
    });
}

if (writevPtr) {
    Interceptor.attach(writevPtr, {
        onEnter(args) {
            const fd = args[0].toInt32();
            if (fd === 27) {
                console.log(`[writev] fd=27 iovcnt=${args[2].toInt32()}`);
            }
        }
    });
}
