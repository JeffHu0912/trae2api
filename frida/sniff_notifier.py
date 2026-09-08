import frida
import time
import subprocess
import threading

js_code = r"""
const modAha = Process.findModuleByName("libaha_net.dylib");
if (modAha) {
    const fnDone = modAha.findExportByName("AhaNet_ReqBodyNotifier_read_done");
    if (fnDone) {
        Interceptor.attach(fnDone, {
            onEnter(args) {
                try {
                    const notifier = args[0];
                    const ok = args[1].toInt32();
                    const len = args[2].toInt32();
                    if (!notifier.isNull() && len > 0) {
                        const bufPtr = notifier.readPointer();
                        send({tag: "read_done", buf: bufPtr.toString(), len: len, ok: ok});
                        if (!bufPtr.isNull()) {
                            const raw = bufPtr.readByteArray(len);
                            send({tag: "body_chunk_raw", len: len}, raw);
                            try {
                                const s = bufPtr.readUtf8String(Math.min(len, 2048));
                                if (s && s.length > 5) {
                                    send({tag: "body_chunk_str", str: s});
                                }
                            } catch(e) {}
                        }
                    }
                } catch(e) {
                    send({tag: "err", err: String(e)});
                }
            }
        });
        send({tag: "hooked_read_done"});
    }
}
"""

sess = frida.attach(709)
script = sess.create_script(js_code)

body_chunks = []
def on_m(m, d):
    p = m.get("payload", {})
    tag = p.get("tag")
    if tag == "body_chunk_str":
        print("[STR]:", p.get("str")[:300])
    elif tag == "body_chunk_raw" and d:
        body_chunks.append(d)
        print(f"[RAW]: {len(d)} bytes")
    elif tag == "read_done":
        print("[READ DONE]:", p)
    elif tag == "hooked_read_done":
        print("[*] Hooked AhaNet_ReqBodyNotifier_read_done!")

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

for i, b in enumerate(body_chunks):
    with open(f"/Users/jeff/project/traework/frida/captured_notifier_{i}.bin", "wb") as f:
        f.write(b)
    print(f"Saved captured_notifier_{i}.bin, len={len(b)}")
