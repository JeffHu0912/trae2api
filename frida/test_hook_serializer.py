import frida
import time
import subprocess
import threading

js_code = r"""
const mod = Process.findModuleByName("libai_agent.dylib");
if (mod) {
    send({tag: "base", base: mod.base.toString()});

    const addr = mod.base.add(ptr("0x22c3c6c"));
    Interceptor.attach(addr, {
        onEnter(args) {
            send({tag: "hit_22c3c6c", x0: args[0].toString(), x1: args[1].toString()});
        }
    });

    // Also hook the 3 callers: 0x3f5b0c, 0xf0bffc, 0x2f3b160
    for (let off of ["0x3f5b0c", "0xf0bffc", "0x2f3b160"]) {
        try {
            Interceptor.attach(mod.base.add(ptr(off)), {
                onEnter(args) {
                    send({tag: "hit_caller", off: off});
                }
            });
        } catch(e) {}
    }
}
"""

sess = frida.attach(730)
script = sess.create_script(js_code)
def on_m(m, d):
    print(m.get("payload"))

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
