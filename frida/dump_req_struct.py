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
                    const method = req.readPointer().readCString();
                    const url = req.add(8).readPointer().readCString();
                    send({tag: "fetch_call", method: method, url: url});

                    // Let us dump 128 bytes of req structure
                    send({tag: "req_dump", dump: hexdump(req, {length: 128})});
                } catch(e) {
                    send({tag: "err", error: String(e)});
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
    if tag == "fetch_call":
        print("[*] AhaNet_fetch:", p.get("method"), p.get("url"))
    elif tag == "req_dump":
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
