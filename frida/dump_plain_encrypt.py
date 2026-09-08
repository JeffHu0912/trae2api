import frida
import time
import subprocess
import threading

js_code = r"""
const mod = Process.findModuleByName("libsscronet.dylib");
if (mod) {
    send({tag: "cronet_base", base: mod.base.toString()});

    const addr = mod.base.add(ptr("0x3729f4"));
    Interceptor.attach(addr, {
        onEnter(args) {
            try {
                const pObj = args[1];
                const len = args[2].toInt32();
                send({tag: "hit_encrypt", len: len, pObj: pObj.toString()});
                if (!pObj.isNull()) {
                    const dataPtr = pObj.add(0x10).readPointer();
                    send({tag: "data_ptr", dataPtr: dataPtr.toString()});
                    if (!dataPtr.isNull() && len > 0) {
                        const raw = dataPtr.readByteArray(len);
                        send({tag: "plain_body", len: len}, raw);
                        try {
                            const s = dataPtr.readUtf8String(Math.min(len, 4096));
                            if (s) send({tag: "plain_str", str: s});
                        } catch(e) {}
                    }
                }
            } catch(e) {
                send({tag: "err", err: String(e)});
            }
        }
    });
}
"""

sess = frida.attach(709)
script = sess.create_script(js_code)

captured_plain = []
def on_m(m, d):
    p = m.get("payload", {})
    tag = p.get("tag")
    if tag == "plain_str":
        print(f"\n[PLAINTEXT STRING ({len(p.get('str'))} bytes)]:")
        print(p.get("str")[:500])
    elif tag == "plain_body" and d:
        captured_plain.append(d)
        print(f"[PLAINTEXT BINARY]: {len(d)} bytes")
    elif tag == "hit_encrypt":
        print("[*] Encrypt called, input len:", p.get("len"))
    else:
        print(p)

script.on("message", on_m)
script.load()

print("[*] Hook ready. Triggering curl...")
def do_curl():
    cmd = """curl -s -N http://127.0.0.1:7865/v1/chat/completions -H "Content-Type: application/json" -H "Authorization: Bearer twbridge-local-7f3a" -d "{\\"model\\":\\"DeepSeek-V4-Flash-Official\\",\\"messages\\":[{\\"role\\":\\"user\\",\\"content\\":\\"ping\\"}],\\"stream\\":false}" """
    subprocess.run(cmd, shell=True, capture_output=True)

t = threading.Thread(target=do_curl)
t.start()
t.join(timeout=15)
time.sleep(1)
sess.detach()

for i, b in enumerate(captured_plain):
    with open(f"/Users/jeff/project/traework/frida/captured_plain_body_{i}.bin", "wb") as f:
        f.write(b)
    print(f"Saved captured_plain_body_{i}.bin, len={len(b)}")
