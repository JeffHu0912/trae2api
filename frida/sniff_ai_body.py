import frida
import time
import subprocess
import threading

js_code = r"""
const mod = Process.findModuleByName("libai_agent.dylib");
if (mod) {
    send({tag: "base", base: mod.base.toString()});
    const bodyAddr = mod.base.add(ptr("0x2f3b280"));
    Interceptor.attach(bodyAddr, {
        onEnter(args) {
            send({tag: "hit_body_addr"});
            try {
                const x28 = this.context.x28;
                send({tag: "x28_ptr", ptr: x28.toString()});
                // x28 is pointer to Rust String or slice
                // Let's inspect x28 as pointer
                const str = x28.readUtf8String(65536);
                send({tag: "x28_str", str: str});
            } catch(e) {
                send({tag: "x28_err", err: String(e)});
            }
        }
    });
}
"""

sess = frida.attach(32935)
script = sess.create_script(js_code)

captured_str = []
def on_m(m, d):
    payload = m.get("payload", {})
    tag = payload.get("tag")
    print(f"[{tag}]", payload)
    if tag == "x28_str":
        captured_str.append(payload.get("str"))

script.on("message", on_m)
script.load()

print("[*] Script loaded in 32935. Triggering curl...")
def do_curl():
    cmd = """curl -s -N http://127.0.0.1:7865/v1/chat/completions -H "Content-Type: application/json" -H "Authorization: Bearer twbridge-local-7f3a" -d '{"model":"DeepSeek-V4-Flash-Official","messages":[{"role":"user","content":"test prompt for create_agent_task"}],"stream":false}' """
    r = subprocess.run(cmd, shell=True, capture_output=True, text=True)
    print("Curl finished, resp:", r.stdout[:100])

t = threading.Thread(target=do_curl)
t.start()
t.join(timeout=30)
time.sleep(2)
sess.detach()

if captured_str:
    with open("/Users/jeff/project/traework/frida/captured_create_agent_task_plain.json", "w") as f:
        f.write(captured_str[0])
    print("[+] Wrote captured_create_agent_task_plain.json!")
