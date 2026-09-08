import frida
import time
import subprocess
import threading

js_code = r"""
const mod = Process.findModuleByName("libai_agent.dylib");
if (mod) {
    send({tag: "base", base: mod.base.toString()});

    // Hook at 0x306dc10 (resolved cloud task identity)
    const addr = mod.base.add(ptr("0x306dc10"));
    Interceptor.attach(addr, {
        onEnter(args) {
            send({tag: "hit_resolved_identity"});
            // Inspect registers x19, x20, x21, x22, x23, x24, x25, sp
            for (let reg of ["x0", "x1", "x2", "x3", "x4", "x5", "x8", "x9", "x19", "x20", "x21", "x22", "x23", "x24", "x25", "x26", "x27", "x28"]) {
                try {
                    const p = this.context[reg];
                    if (!p.isNull()) {
                        const s = p.readUtf8String(256);
                        if (s && s.length > 0) {
                            send({tag: "reg_str", reg: reg, val: s});
                        }
                    }
                } catch(e) {}
            }
        }
    });
}
"""

sess = frida.attach(32935)
script = sess.create_script(js_code)
def on_m(m, d):
    print(m)

script.on("message", on_m)
script.load()

def do_curl():
    cmd = """curl -s -N http://127.0.0.1:7865/v1/chat/completions -H "Content-Type: application/json" -H "Authorization: Bearer twbridge-local-7f3a" -d '{"model":"DeepSeek-V4-Flash-Official","messages":[{"role":"user","content":"test identity"}],"stream":false}' """
    r = subprocess.run(cmd, shell=True, capture_output=True, text=True)
    print("Curl finished, resp:", r.stdout[:100])

t = threading.Thread(target=do_curl)
t.start()
t.join(timeout=20)
time.sleep(1)
sess.detach()
