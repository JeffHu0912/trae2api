import frida
import time
import subprocess
import threading
import json

js_code = r"""
const modAha = Process.findModuleByName("libaha_net.dylib");
if (modAha) {
    const fnDone = modAha.findExportByName("AhaNet_ReqBodyNotifier_read_done");
    if (fnDone) {
        let chunkIdx = 0;
        Interceptor.attach(fnDone, {
            onEnter(args) {
                const len = args[2].toInt32();
                if (len > 0) {
                    const bufPtr = args[0].add(8).readPointer();
                    if (!bufPtr.isNull()) {
                        const raw = bufPtr.readByteArray(len);
                        send({idx: chunkIdx, len: len}, raw);
                        chunkIdx++;
                    }
                }
            }
        });
    }
}
"""

sess = frida.attach(709)
script = sess.create_script(js_code)

saved = []
def on_m(m, d):
    p = m.get("payload", {})
    if d:
        idx = p["idx"]
        saved.append((idx, d))
        print(f"[Chunk {idx}] {len(d)} bytes")

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

for idx, b in saved:
    with open(f"/Users/jeff/project/traework/frida/chunk_{idx:02d}.bin", "wb") as f:
        f.write(b)
print(f"[*] Saved {len(saved)} chunks to frida/chunk_XX.bin")
