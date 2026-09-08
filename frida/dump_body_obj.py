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

                    const p48 = req.add(0x48).readPointer();
                    if (!p48.isNull()) {
                        send({tag: "p48_dump", dump: hexdump(p48, {length: 128})});
                        const sub0 = p48.readPointer();
                        if (!sub0.isNull()) {
                            send({tag: "sub0_dump", dump: hexdump(sub0, {length: 256})});
                            try {
                                const s = sub0.readUtf8String(1024);
                                if (s) send({tag: "sub0_str", str: s});
                            } catch(e) {}
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
    elif tag == "sub0_str":
        print("[*] String:", p.get("str")[:200])
    elif tag.endswith("_dump"):
        print(f"--- {tag} ---")
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
