/**
 * capture_cronet_protocol.js
 * 
 * 在 Chromium NetworkService 进程 (PID 32923) 中拦截 libsscronet.dylib 的全量 HTTP 请求与流式响应：
 * 1. Cronet_UrlRequest_InitWithParams: 获取请求 URL
 * 2. Cronet_UrlRequestParams_http_method_set: 获取请求 Method
 * 3. Cronet_UrlRequestParams_request_headers_add: 获取所有 Headers
 * 4. Cronet_UploadDataProvider_Read: 获取 POST Request Body (明文/密文)
 * 5. Cronet_UrlRequestCallback_OnResponseStarted: 获取 Response Status & Headers
 * 6. Cronet_UrlRequestCallback_OnReadCompleted: 获取 Response SSE 流数据
 */

const LOG_FILE = "/Users/jeff/project/traework/frida/cronet_capture.log";

function log(msg) {
    console.log(msg);
}

log("=======================================================");
log("[*] Cronet 协议全要素拦截探针加载成功，PID: " + Process.id);
log("=======================================================");

const mod = Process.findModuleByName("libsscronet.dylib");
if (!mod) {
    log("[-] 未找到 libsscronet.dylib");
} else {
    log("[+] libsscronet.dylib 基地址: " + mod.base);

    const fnInitWithParams = mod.findExportByName("Cronet_UrlRequest_InitWithParams");
    const fnMethodSet = mod.findExportByName("Cronet_UrlRequestParams_http_method_set");
    const fnHeaderAdd = mod.findExportByName("Cronet_UrlRequestParams_request_headers_add");
    const fnHeaderNameGet = new NativeFunction(mod.findExportByName("Cronet_HttpHeader_name_get"), "pointer", ["pointer"]);
    const fnHeaderValueGet = new NativeFunction(mod.findExportByName("Cronet_HttpHeader_value_get"), "pointer", ["pointer"]);

    const fnUploadRead = mod.findExportByName("Cronet_UploadDataProvider_Read");
    const fnBufferGetData = new NativeFunction(mod.findExportByName("Cronet_Buffer_GetData"), "pointer", ["pointer"]);
    const fnBufferGetSize = new NativeFunction(mod.findExportByName("Cronet_Buffer_GetSize"), "uint64", ["pointer"]);

    const fnOnResponseStarted = mod.findExportByName("Cronet_UrlRequestCallback_OnResponseStarted");
    const fnOnReadCompleted = mod.findExportByName("Cronet_UrlRequestCallback_OnReadCompleted");
    const fnStatusCodeGet = new NativeFunction(mod.findExportByName("Cronet_UrlResponseInfo_http_status_code_get"), "int", ["pointer"]);
    const fnUrlGet = new NativeFunction(mod.findExportByName("Cronet_UrlResponseInfo_url_get"), "pointer", ["pointer"]);

    // 用于关联 request 对象与上下文数据
    const activeRequests = new Map();

    // 1. 拦截 URL
    if (fnInitWithParams) {
        Interceptor.attach(fnInitWithParams, {
            onEnter(args) {
                try {
                    const reqPtr = args[0];
                    const urlPtr = args[2];
                    const url = urlPtr.readCString();
                    if (url && (url.includes("trae") || url.includes("mchost") || url.includes("agent") || url.includes("lite"))) {
                        log(`\n>>>>>>>>>>>>>>>> [CRONET HTTP REQUEST] >>>>>>>>>>>>>>>>`);
                        log(`[URL] ${url}`);
                        activeRequests.set(reqPtr.toString(), {
                            url: url,
                            headers: {},
                            bodyChunks: [],
                            time: Date.now()
                        });
                    }
                } catch(e) {
                    log("[-] InitWithParams error: " + e);
                }
            }
        });
        log("[+] 已挂钩 Cronet_UrlRequest_InitWithParams");
    }

    // 2. 拦截 Header 添加
    if (fnHeaderAdd) {
        Interceptor.attach(fnHeaderAdd, {
            onEnter(args) {
                try {
                    const headerPtr = args[1];
                    const namePtr = fnHeaderNameGet(headerPtr);
                    const valPtr = fnHeaderValueGet(headerPtr);
                    if (namePtr && !namePtr.isNull() && valPtr && !valPtr.isNull()) {
                        const name = namePtr.readCString();
                        const val = valPtr.readCString();
                        log(`  [Header] ${name}: ${val}`);
                    }
                } catch(e) {}
            }
        });
        log("[+] 已挂钩 Cronet_UrlRequestParams_request_headers_add");
    }

    // 3. 拦截 Method 设置
    if (fnMethodSet) {
        Interceptor.attach(fnMethodSet, {
            onEnter(args) {
                try {
                    const method = args[1].readCString();
                    log(`  [Method] ${method}`);
                } catch(e) {}
            }
        });
        log("[+] 已挂钩 Cronet_UrlRequestParams_http_method_set");
    }

    // 4. 拦截 Request Body 上传流
    if (fnUploadRead) {
        Interceptor.attach(fnUploadRead, {
            onEnter(args) {
                try {
                    const bufferPtr = args[2];
                    if (bufferPtr && !bufferPtr.isNull()) {
                        const dataPtr = fnBufferGetData(bufferPtr);
                        const size = fnBufferGetSize(bufferPtr).valueOf();
                        if (size > 0 && dataPtr && !dataPtr.isNull()) {
                            const raw = dataPtr.readByteArray(Math.min(size, 8192));
                            const str = dataPtr.readUtf8String(Math.min(size, 8192));
                            log(`\n>>>>>>>>>>>>>>>> [REQUEST UPLOAD BODY (${size} bytes)] >>>>>>>>>>>>>>>>`);
                            log(str || "[Non-UTF8 or binary]");
                            log("----------------------------------------------------------------");
                        }
                    }
                } catch(e) {}
            }
        });
        log("[+] 已挂钩 Cronet_UploadDataProvider_Read");
    }

    // 5. 拦截 Response 头部
    if (fnOnResponseStarted) {
        Interceptor.attach(fnOnResponseStarted, {
            onEnter(args) {
                try {
                    const respInfo = args[2];
                    const status = fnStatusCodeGet(respInfo);
                    const urlPtr = fnUrlGet(respInfo);
                    const url = urlPtr && !urlPtr.isNull() ? urlPtr.readCString() : "";
                    if (url && (url.includes("trae") || url.includes("mchost") || url.includes("agent"))) {
                        log(`\n<<<<<<<<<<<<<<<< [CRONET RESPONSE STARTED (Status: ${status})] <<<<<<<<<<<<<<<<`);
                        log(`[URL] ${url}`);
                    }
                } catch(e) {}
            }
        });
        log("[+] 已挂钩 Cronet_UrlRequestCallback_OnResponseStarted");
    }

    // 6. 拦截 Response 数据块 (SSE 流)
    if (fnOnReadCompleted) {
        Interceptor.attach(fnOnReadCompleted, {
            onEnter(args) {
                try {
                    const respInfo = args[2];
                    const bufferPtr = args[3];
                    const urlPtr = fnUrlGet(respInfo);
                    const url = urlPtr && !urlPtr.isNull() ? urlPtr.readCString() : "";
                    if (url && (url.includes("trae") || url.includes("mchost") || url.includes("agent"))) {
                        const dataPtr = fnBufferGetData(bufferPtr);
                        const size = fnBufferGetSize(bufferPtr).valueOf();
                        if (size > 0 && dataPtr && !dataPtr.isNull()) {
                            const text = dataPtr.readUtf8String(Math.min(size, 4096));
                            if (text) {
                                log(`[Response Chunk ${size}B]: ${text.trim().slice(0, 300)}`);
                            }
                        }
                    }
                } catch(e) {}
            }
        });
        log("[+] 已挂钩 Cronet_UrlRequestCallback_OnReadCompleted");
    }
}
