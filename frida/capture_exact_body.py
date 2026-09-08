import frida
import time
import subprocess
import threading
import json

js_code = """
const modCronet = Process.findModuleByName("libsscronet.dylib");

const fnUploadRead = modCronet.findExportByName("Cronet_UploadDataProvider_Read");
const fnOnReadSucceeded = modCronet.findExportByName("Cronet_UploadDataSink_OnReadSucceeded");
const fnBufferGetData = new NativeFunction(modCronet.findExportByName("Cronet_Buffer_GetData"), "pointer", ["pointer"]);

const sinkToBuffer = new Map();

if (fnUploadRead) {
    Interceptor.attach(fnUploadRead, {
        onEnter(args) {
            const sinkPtr = args[1].toString();
            const bufPtr = args[2];
            sinkToBuffer.set(sinkPtr, bufPtr);
        }
    });
}

if (fnOnReadSucceeded) {
    Interceptor.attach(fnOnReadSucceeded, {
        onEnter(args) {
            try {
                const sinkPtr = args[0].toString();
                const bytesRead = args[1].toInt32();
                const bufPtr = sinkToBuffer.get(sinkPtr);
                if (bufPtr && bytesRead > 0) {
                    const dataPtr = fnBufferGetData(bufPtr);
                    if (dataPtr && !dataPtr.isNull()) {
                        const raw = dataPtr.readByteArray(bytesRead);
                        send({tag: "body_read", bytesRead: bytesRead}, raw);
                    }
                }
            } catch(e) {
                send({tag: "err", error: String(e)});
            }
        }
    });
}

const fnUrlInit = modCronet.findExportByName("Cronet_UrlRequest_InitWithParams");
if (fnUrlInit) {
    Interceptor.attach(fnUrlInit, {
        onEnter(args) {
            try {
                const url = args[2].readCString();
                send({tag: "cronet_url", url: url});
            } catch(e) {}
        }
    });
}
"""

sess = frida.attach(32923)
script = sess.create_script(js_code)

body_parts = []
def on_message(msg, data):
    p = msg.get("payload", {})
    tag = p.get("tag")
    if tag == "cronet_url":
        print("[*] URL:", p.get("url"))
    elif tag == "body_read":
        print(f"[+] Body Read: {p.get('bytesRead')} bytes")
        if data:
            body_parts.append(data)
    elif tag == "err":
        print("[-] Error:", p.get("error"))

script.on("message", on_message)
script.load()

print("[*] Script loaded. Triggering curl...")
def do_curl():
    cmd = """curl -s -N http://127.0.0.1:7865/v1/chat/completions -H "Content-Type: application/json" -H "Authorization: Bearer twbridge-local-7f3a" -d '{"model":"DeepSeek-V4-Flash-Official","messages":[{"role":"user","content":"explain quantum computing in one sentence"}],"stream":false}' """
    r = subprocess.run(cmd, shell=True, capture_output=True, text=True)
    print("Curl finished, resp:", r.stdout[:120])

t = threading.Thread(target=do_curl)
t.start()
t.join(timeout=30)
time.sleep(2)
sess.detach()

for i, part in enumerate(body_parts):
    with open(f"/Users/jeff/project/traework/frida/body_part_{i}.bin", "wb") as f:
        f.write(part)
    print(f"Saved body part {i}, len={len(part)}")
