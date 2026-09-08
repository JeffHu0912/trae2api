import frida
import time
import subprocess
import threading

js_code = r"""
const mod = Process.findModuleByName("libai_agent.dylib");
if (mod) {
    send({tag: "base", base: mod.base.toString()});

    const addr = mod.base.add(ptr("0xd5aea0"));
    Interceptor.attach(addr, {
        onEnter(args) {
            send({tag: "hit_fetch_stream_v2", x0: args[0].toString()});
            try {
                const reqPtr = args[0];
                send({tag: "req_dump", dump: hexdump(reqPtr, {length: 256})});

                for (let i = 0; i < 256; i += 8) {
                    try {
                        const p = reqPtr.add(i).readPointer();
                        if (!p.isNull()) {
                            const s = p.readUtf8String(1024);
                            if (s && s.length > 3) {
                                send({tag: "req_str", off: i, str: s});
                            }
                        }
                    } catch(e) {}
                }
            } catch(e) {
                send({tag: "err", err: String(e)});
            }
        }
    });
}
"""

sess = frida.attach(730)
script = sess.create_script(js_code)
def on_m(m, d):
    p = m.get("payload", {})
    tag = p.get("tag")
    if tag == "req_str":
        print(f"  [+0x{p['off']:x}]:", p["str"][:150])
    elif tag == "req_dump":
        print(p["dump"])
    else:
        print(p)

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
