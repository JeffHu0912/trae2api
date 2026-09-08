import frida
import time
import subprocess
import threading

js_code = """
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
                    if (!url.includes("create_agent_task")) return;

                    send({tag: "target_fetch", method: method, url: url});

                    // Dump the req pointer offsets
                    // Let us check all pointers in req from offset 0 to 256
                    for (let i = 0; i < 256; i += 8) {
                        try {
                            const p = req.add(i).readPointer();
                            if (!p.isNull()) {
                                // check if string
                                try {
                                    const str = p.readUtf8String(1024);
                                    if (str && str.length > 0) {
                                        send({tag: "req_field_str", offset: i, val: str});
                                    }
                                } catch(e) {}
                                // check if points to another struct
                                try {
                                    const subP = p.readPointer();
                                    if (!subP.isNull()) {
                                        const subStr = subP.readUtf8String(1024);
                                        if (subStr && subStr.length > 0) {
                                            send({tag: "req_sub_str", offset: i, val: subStr});
                                        }
                                    }
                                } catch(e) {}
                            }
                        } catch(e) {}
                    }
                } catch(e) {
                    send({tag: "err", error: String(e)});
                }
            }
        });
    }
}
"""

sess = frida.attach(32923)
script = sess.create_script(js_code)
def on_m(m, d):
    p = m.get("payload", {})
    tag = p.get("tag")
    if tag == "target_fetch":
        print("[*]", p.get("method"), p.get("url"))
    elif tag == "req_field_str":
        val = p.get("val")
        if len(val) > 100: val = val[:100] + "..."
        print(f"  [+{p.get('offset')}]: {val}")
    elif tag == "req_sub_str":
        val = p.get("val")
        if len(val) > 100: val = val[:100] + "..."
        print(f"  [+{p.get('offset')} -> sub]: {val}")

script.on("message", on_m)
script.load()

def do_curl():
    cmd = """curl -s -N http://127.0.0.1:7865/v1/chat/completions -H "Content-Type: application/json" -H "Authorization: Bearer twbridge-local-7f3a" -d '{"model":"DeepSeek-V4-Flash-Official","messages":[{"role":"user","content":"test target fetch"}],"stream":false}' """
    subprocess.run(cmd, shell=True, capture_output=True)

t = threading.Thread(target=do_curl)
t.start()
t.join(timeout=20)
time.sleep(1)
sess.detach()
