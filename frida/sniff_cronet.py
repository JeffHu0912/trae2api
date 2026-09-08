import frida
import time
import subprocess
import threading
import json

js_code = """
const modCronet = Process.findModuleByName("libsscronet.dylib");

const fnUploadRead = modCronet.findExportByName("Cronet_UploadDataProvider_Read");
const fnBufferGetData = new NativeFunction(modCronet.findExportByName("Cronet_Buffer_GetData"), "pointer", ["pointer"]);
const fnBufferGetSize = new NativeFunction(modCronet.findExportByName("Cronet_Buffer_GetSize"), "uint64", ["pointer"]);

const fnHeaderAdd = modCronet.findExportByName("Cronet_UrlRequestParams_request_headers_add");
const fnHeaderNameGet = new NativeFunction(modCronet.findExportByName("Cronet_HttpHeader_name_get"), "pointer", ["pointer"]);
const fnHeaderValueGet = new NativeFunction(modCronet.findExportByName("Cronet_HttpHeader_value_get"), "pointer", ["pointer"]);

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

if (fnHeaderAdd) {
    Interceptor.attach(fnHeaderAdd, {
        onEnter(args) {
            try {
                const headerPtr = args[1];
                const namePtr = fnHeaderNameGet(headerPtr);
                const valPtr = fnHeaderValueGet(headerPtr);
                if (namePtr && !namePtr.isNull() && valPtr && !valPtr.isNull()) {
                    send({tag: "header", name: namePtr.readCString(), val: valPtr.readCString()});
                }
            } catch(e) {}
        }
    });
}

if (fnUploadRead) {
    Interceptor.attach(fnUploadRead, {
        onEnter(args) {
            this.bufPtr = args[2];
        },
        onLeave(retval) {
            try {
                if (this.bufPtr && !this.bufPtr.isNull()) {
                    const dataPtr = fnBufferGetData(this.bufPtr);
                    const size = fnBufferGetSize(this.bufPtr).valueOf();
                    if (size > 0 && dataPtr && !dataPtr.isNull()) {
                        const raw = dataPtr.readByteArray(size);
                        send({tag: "body_chunk", size: size}, raw);
                    }
                }
            } catch(e) {}
        }
    });
}
"""

sess = frida.attach(32923)
script = sess.create_script(js_code)

bodies = []
headers = []
urls = []

def on_message(msg, data):
    p = msg.get("payload", {})
    tag = p.get("tag")
    if tag == "cronet_url":
        u = p.get("url")
        urls.append(u)
        print("[*] URL:", u)
    elif tag == "header":
        h_name = p.get("name")
        h_val = p.get("val")
        headers.append((h_name, h_val))
        print(f"    Header: {h_name}: {h_val}")
    elif tag == "body_chunk":
        if data:
            print(f"[+] Body Chunk {len(data)} bytes")
            bodies.append(data)

script.on("message", on_message)
script.load()

print("[*] Script loaded. Triggering curl...")
def do_curl():
    cmd = """curl -s -N http://127.0.0.1:7865/v1/chat/completions -H "Content-Type: application/json" -H "Authorization: Bearer twbridge-local-7f3a" -d '{"model":"DeepSeek-V4-Flash-Official","messages":[{"role":"user","content":"hello work test"}],"stream":false}' """
    r = subprocess.run(cmd, shell=True, capture_output=True, text=True)
    print("Curl finished, resp:", r.stdout[:200])

t = threading.Thread(target=do_curl)
t.start()
t.join(timeout=30)
time.sleep(2)
sess.detach()

with open("/Users/jeff/project/traework/frida/sniffed_bodies.bin", "wb") as f:
    for b in bodies:
        f.write(b)
        f.write(b"\n---CHUNK_BOUNDARY---\n")

print(f"[*] Captured {len(urls)} URLs, {len(headers)} Headers, {len(bodies)} Body chunks.")
