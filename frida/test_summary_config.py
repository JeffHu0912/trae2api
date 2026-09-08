import urllib.request
import json
import time

auth_data = json.load(open("/Users/jeff/project/traework/auths/trae-4122512616609817.json"))
token = auth_data["auth"]["accessToken"]
did = auth_data["auth"]["deviceId"]
mid = auth_data["auth"]["machineId"]
uid = auth_data["account"]["uid"]

# 1. Fetch get_detail_param
req = urllib.request.Request("https://trae-api-cn.mchost.guru/api/ide/v1/get_detail_param",
    data=json.dumps({
        "function": "solo_work_lite",
        "need_prompt": True,
        "poly_prompt": True,
    }).encode("utf-8"),
    headers={
        "Content-Type": "application/json",
        "Authorization": "Cloud-IDE-JWT " + token,
        "X-Cloudide-Token": token,
        "X-Ide-Token": token,
        "X-Uid": uid,
        "X-Device-Id": did,
        "X-Machine-Id": mid,
        "X-App-Id": "6eefa01c-1036-4c7e-9ca5-d891f63bfcd8",
        "X-Ide-Version": "0.1.63",
        "X-Ide-Version-Code": "20260901"
    }
)
detail_res = json.loads(urllib.request.urlopen(req).read().decode("utf-8"))
config_list = detail_res.get("config_info_list", [])
prompt_set = detail_res.get("metadata", {}).get("encrypted_prompt_set", [])

model_config_map = {}
for c in config_list:
    cname = c.get("config_name")
    model_config_map[cname] = c

prompt_map = {}
for p in prompt_set:
    prompt_map[p.get("prompt_key")] = p

print(f"Loaded {len(model_config_map)} model configs, {len(prompt_map)} prompts")

# 2. Try variations of create_agent_task payload
variations = [
    {
        "name": "with_model_config_map_and_prompt_map",
        "fields": {
            "model_config": model_config_map,
            "prompt_template_map": prompt_map,
        }
    },
    {
        "name": "with_config_info_list",
        "fields": {
            "config_info_list": config_list,
            "encrypted_prompt_set": prompt_set,
        }
    },
    {
        "name": "with_function_config_and_model_config",
        "fields": {
            "function_config": {
                "solo_work_lite": config_list,
                "summary": model_config_map.get("summary")
            },
            "model_config": model_config_map.get("DeepSeek-V4-Flash-Official"),
            "prompt_template_map": prompt_map,
        }
    }
]

for var in variations:
    print(f"\n=== Testing {var['name']} ===")
    ts = int(time.time() * 1000)
    task_payload = {
        "conversation_id": f"conv-{ts}",
        "session_id": f"sess-{ts}",
        "user_id": uid,
        "device_id": did,
        "agent_type": "solo_work_lite",
        "model_name": "DeepSeek-V4-Flash-Official__dev",
        "config_name": "DeepSeek-V4-Flash-Official",
        "ide_version": "0.1.63",
        "version_code": 20260901,
        "mode_type": 1,
        "plugin_channel": "stable",
        "history_id_list": [],
        "user_input": {
            "id": f"msg-{ts}",
            "query": json.dumps([{"type": "text", "data": {"content": "hello"}}]),
            "messages": [
                {"role": "user", "content": "hello"}
            ]
        },
        **var["fields"]
    }

    t_req = urllib.request.Request("https://api5-normal.mchost.guru/api/agent/v3/create_agent_task",
        data=json.dumps(task_payload).encode("utf-8"),
        headers={
            "Content-Type": "application/json",
            "Accept": "text/event-stream, application/json",
            "User-Agent": "TraeClient/TTNet",
            "Authorization": "Cloud-IDE-JWT " + token,
            "X-Cloudide-Token": token,
            "X-Ide-Token": token,
            "X-Uid": uid,
            "X-Device-Id": did,
            "X-Machine-Id": mid,
            "X-App-Id": "6eefa01c-1036-4c7e-9ca5-d891f63bfcd8",
            "X-App-Version": "default",
            "X-App-Version-Code": "20260901",
            "X-Ide-Version": "0.1.63",
            "X-Ide-Version-Code": "20260901",
            "X-Version-Code": "20260901",
            "Request-Traffic-Type": "prod"
        }
    )

    try:
        with urllib.request.urlopen(t_req) as resp:
            lines = [resp.readline().decode("utf-8").strip() for _ in range(10)]
            print("Response status:", resp.status)
            for l in lines:
                if l: print(" ", l)
    except urllib.error.HTTPError as e:
        print("HTTP Error:", e.code, e.read().decode("utf-8"))
