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
                    const url = req.add(8).readPointer().readCString();
                    if (!url.includes("create_agent_task")) return;

                    send({tag: "target_fetch", url: url});

                    const pArray = req.add(0x40).readPointer();
                    if (!pArray.isNull()) {
                        for (let i = 0; i < 8; i++) {
                            const subP = pArray.add(i * 8).readPointer();
                            if (!subP.isNull()) {
                                try {
                                    send({tag: "sub_ptr", index: i, ptr: subP.toString(), dump: hexdump(subP, {length: 128})});
                                    const s = subP.readUtf8String(1024);
                                    if (s) send({tag: "sub_str", index: i, str: s});
                                } catch(e) {}
                            }
                        }
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
    elif tag == "sub_str":
        print(f"  [{p.get('index')} STR]:", p.get("str")[:120])
    elif tag == "sub_ptr":
        print(f"\n--- Item {p.get('index')} ({p.get('ptr')}) ---")
        print(p.get("dump"))

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
