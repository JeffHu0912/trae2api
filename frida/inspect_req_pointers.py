import frida
import time
import subprocess
import threading

js_code = r"""
const modAha = Process.findModuleByName("libaha_net.dylib");
if (modAha) {
    const fnFetch = modAha.findExportByName("AhaNet_fetch");
    if (fnFetch) {
        Interceptor.attach(fnFetch, {
            onEnter(args) {
                try {
                    const req = args[0];
                    const urlPtr = req.add(8).readPointer();
                    const url = urlPtr.readCString();
                    if (!url.includes("create_agent_task")) return;

                    send({tag: "target_fetch", url: url});

                    // Inspect pointers at offset 0x40, 0x48, 0x58, 0x70
                    const offsets = [0x1c, 0x20, 0x28, 0x30, 0x38, 0x40, 0x48, 0x58, 0x60, 0x68, 0x70];
                    for (let off of offsets) {
                        try {
                            const p = req.add(off).readPointer();
                            if (!p.isNull()) {
                                // check if points to bytes
                                const dump = hexdump(p, {length: 64});
                                send({tag: "ptr_dump", off: off, ptr: p.toString(), dump: dump});
                            }
                        } catch(e) {}
                    }
                } catch(e) {
                    send({tag: "err", err: String(e)});
                }
            }
        });
    }
}
"""

sess = frida.attach(709)
script = sess.create_script(js_code)
def on_m(m, d):
    p = m.get("payload", {})
    tag = p.get("tag")
    if tag == "target_fetch":
        print("[*] Target fetch:", p.get("url"))
    elif tag == "ptr_dump":
        print(f"\n--- Offset +0x{p.get('off'):x} ({p.get('ptr')}) ---")
        print(p.get("dump"))
    elif tag == "err":
        print("Error:", p.get("err"))

script.on("message", on_m)
script.load()

def do_curl():
    cmd = """curl -s -N http://127.0.0.1:7865/v1/chat/completions -H "Content-Type: application/json" -H "Authorization: Bearer twbridge-local-7f3a" -d "{\\"model\\":\\"DeepSeek-V4-Flash-Official\\",\\"messages\\":[{\\"role\\":\\"user\\",\\"content\\":\\"ping\\"}],\\"stream\\":false}" """
    subprocess.run(cmd, shell=True, capture_output=True)

t = threading.Thread(target=do_curl)
t.start()
t.join(timeout=15)
time.sleep(1)
sess.detach()
