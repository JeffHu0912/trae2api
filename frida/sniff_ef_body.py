import frida
import time
import subprocess
import threading

js_code = r"""
const mod = Process.findModuleByName("Electron Framework");
if (mod) {
    send({tag: "ef_base", base: mod.base.toString()});
    // Offset 0x6278a0c
    const addr = mod.base.add(ptr("0x6278a0c"));
    Interceptor.attach(addr, {
        onEnter(args) {
            try {
                // ldp x1, x2, [x19, #0x48] is just executed or about to execute?
                // addr is 0x6278a0c which IS ldp x1, x2, [x19, #0x48]
                const x19 = this.context.x19;
                const p1 = x19.add(0x48).readPointer();
                const len = x19.add(0x50).readU64().valueOf();
                send({tag: "hit_body_prep", len: len, ptr: p1.toString()});
                if (len > 0 && !p1.isNull()) {
                    const raw = p1.readByteArray(Math.min(len, 65536));
                    send({tag: "raw_body_data", len: len}, raw);
                }
            } catch(e) {
                send({tag: "err", error: String(e)});
            }
        }
    });
}
"""

sess = frida.attach(32923)
script = sess.create_script(js_code)

captured = []
def on_m(m, d):
    payload = m.get("payload", {})
    tag = payload.get("tag")
    print(f"[{tag}]", payload)
    if tag == "raw_body_data" and d:
        captured.append(d)

script.on("message", on_m)
script.load()

print("[*] Hook ready. Triggering curl...")
def do_curl():
    cmd = """curl -s -N http://127.0.0.1:7865/v1/chat/completions -H "Content-Type: application/json" -H "Authorization: Bearer twbridge-local-7f3a" -d '{"model":"DeepSeek-V4-Flash-Official","messages":[{"role":"user","content":"tell me a haiku about go language"}],"stream":false}' """
    r = subprocess.run(cmd, shell=True, capture_output=True, text=True)
    print("Curl finished, resp:", r.stdout[:100])

t = threading.Thread(target=do_curl)
t.start()
t.join(timeout=20)
time.sleep(1)
sess.detach()

for i, b in enumerate(captured):
    with open(f"/Users/jeff/project/traework/frida/captured_electron_body_{i}.bin", "wb") as f:
        f.write(b)
    print(f"Wrote captured_electron_body_{i}.bin, size={len(b)}")
