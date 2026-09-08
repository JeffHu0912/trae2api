import frida
import time
import subprocess
import threading

js_code = r"""
const mod = Process.findModuleByName("Electron Framework");
if (mod) {
    const fnCtor = mod.findExportByName("_ZN2v86String9Utf8ValueC1EPNS_7IsolateENS_5LocalINS_5ValueEEE");
    if (fnCtor) {
        Interceptor.attach(fnCtor, {
            onEnter(args) {
                this.self = args[0];
            },
            onLeave(retval) {
                try {
                    const strPtr = this.self.readPointer();
                    if (!strPtr.isNull()) {
                        const s = strPtr.readUtf8String(100000);
                        if (s && (s.includes("create_agent_task") || (s.includes("DeepSeek") && s.includes("{")) || s.includes("conversation_id"))) {
                            send({tag: "v8_captured_str", len: s.length, str: s});
                        }
                    }
                } catch(e) {}
            }
        });
        send({tag: "hooked_v8"});
    }
}
"""

sess = frida.attach(709)
script = sess.create_script(js_code)

captured = []
def on_m(m, d):
    p = m.get("payload", {})
    tag = p.get("tag")
    if tag == "v8_captured_str":
        print(f"[+] Captured V8 String ({p.get('len')} chars)")
        captured.append(p.get("str"))
    elif tag == "hooked_v8":
        print("[*] Hooked v8::String::Utf8Value")

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

if captured:
    with open("/Users/jeff/project/traework/frida/captured_v8_str.json", "w") as f:
        f.write(captured[0])
    print("[+] Successfully wrote captured_v8_str.json!")
