import frida
import time
import subprocess
import threading

js_code = r"""
const mod = Process.findModuleByName("libai_agent.dylib");
if (mod) {
    send({tag: "base", base: mod.base.toString()});

    // Hook at 0x2f3b160 (call to serialize)
    const addr = mod.base.add(ptr("0x2f3b160"));
    Interceptor.attach(addr, {
        onEnter(args) {
            send({tag: "hit_serialize_call"});
            this.sp = this.context.sp;
        },
        onLeave(retval) {
            send({tag: "leave_serialize_call", ret: retval.toString()});
            // Let's inspect the stack at sp + 0xade0, or check memory
            try {
                // Let's check registers x0, x8, x19, x20, x28
                for (let reg of ["x0", "x1", "x8", "x19", "x20", "x28"]) {
                    const p = this.context[reg];
                    if (!p.isNull()) {
                        try {
                            const s = p.readUtf8String(1024);
                            if (s && s.includes("{")) {
                                send({tag: "reg_json", reg: reg, val: s.slice(0, 500)});
                            }
                        } catch(e) {}
                    }
                }
            } catch(e) {}
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
    cmd = """curl -s -N http://127.0.0.1:7865/v1/chat/completions -H "Content-Type: application/json" -H "Authorization: Bearer twbridge-local-7f3a" -d '{"model":"DeepSeek-V4-Flash-Official","messages":[{"role":"user","content":"test prompt"}],"stream":false}' """
    r = subprocess.run(cmd, shell=True, capture_output=True, text=True)
    print("Curl finished, resp:", r.stdout[:100])

t = threading.Thread(target=do_curl)
t.start()
t.join(timeout=20)
time.sleep(1)
sess.detach()
