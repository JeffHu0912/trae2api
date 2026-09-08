import frida
import time
import subprocess
import threading
import sys

PID = 730

js_code = r"""
const mod = Process.findModuleByName("libai_agent.dylib");
if (mod) {
    send({tag: "base", base: mod.base.toString()});

    const targetAddr = mod.base.add(ptr("0x2f3c1d4"));
    Interceptor.attach(targetAddr, {
        onEnter(args) {
            try {
                const ptrVal = this.context.x8;
                const lenVal = this.context.x28.toInt32();
                send({tag: "hit_2f3c1d4", ptr: ptrVal.toString(), len: lenVal});
                if (lenVal > 0 && !ptrVal.isNull()) {
                    const raw = ptrVal.readUtf8String(lenVal);
                    send({tag: "json_str", len: lenVal, json: raw});
                }
            } catch(e) {
                send({tag: "err", error: String(e)});
            }
        }
    });
} else {
    send({tag: "err", error: "libai_agent.dylib not found"});
}
"""

sess = frida.attach(PID)
script = sess.create_script(js_code)

captured_json = []
def on_m(m, d):
    p = m.get("payload", {})
    tag = p.get("tag")
    print(f"[{tag}]", p)
    if tag == "json_str":
        captured_json.append(p.get("json"))

script.on("message", on_m)
script.load()

print("[*] Hook ready at 0x2f3c1d4. Triggering curl...")
def do_curl():
    cmd = """curl -s -N http://127.0.0.1:7865/v1/chat/completions -H "Content-Type: application/json" -H "Authorization: Bearer twbridge-local-7f3a" -d '{"model":"DeepSeek-V4-Flash-Official","messages":[{"role":"user","content":"what is 1+1?"}],"stream":false}' """
    r = subprocess.run(cmd, shell=True, capture_output=True, text=True)
    print("Curl finished, resp:", r.stdout[:100])

t = threading.Thread(target=do_curl)
t.start()
t.join(timeout=20)
time.sleep(1)
sess.detach()

if captured_json:
    with open("/Users/jeff/project/traework/frida/REAL_create_agent_task.json", "w") as f:
        f.write(captured_json[0])
    print("\n=======================================================")
    print("[+] SUCCESSFULLY SAVED /Users/jeff/project/traework/frida/REAL_create_agent_task.json!")
    print("=======================================================\n")
else:
    print("[-] captured_json is empty")
