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

base_prompt_map = {}
for p in prompt_set:
    base_prompt_map[p.get("prompt_key")] = p

dummy_template = {
    "prompt_key": "summary",
    "prompt_label": "prod_20251124_1630",
    "prompt_template": "Summary: {{query}}",
    "prompt_version": "v1"
}

candidate_keys = [
    "summary",
    "summary_prompt",
    "summary_template",
    "prompt_template_summary",
    "prompt_template_summary_base_prompt",
    "trae_agent_summary.default.summary",
    "trae_agent_solo_coder.doubao_dev.summary",
    "trae_agent_solo_coder.default.summary",
    "trae_agent_summary.doubao_dev.summary",
    "trae_agent_summary.default.master_agent",
    "trae_agent_solo_coder.doubao_dev.summary_agent",
    "trae_agent_solo_coder.default.summary_agent",
    "code_review_summary",
    "trae_agent_solo_coder.doubao_dev.code_review_summary",
    "trae_agent_solo_coder.default.code_review_summary",
    "prompt_template_code_review_summary",
    "session_summary",
    "chat_summary",
]

for k in candidate_keys:
    base_prompt_map[k] = {
        "prompt_key": k,
        "prompt_label": "prod_20251124_1630",
        "prompt_template": "Summary: {{content}}",
        "prompt_version": "v1"
    }

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
    "prompt_template_map": base_prompt_map,
    "user_input": {
        "id": f"msg-{ts}",
        "query": json.dumps([{"type": "text", "data": {"content": "hello"}}]),
    }
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

with urllib.request.urlopen(t_req) as resp:
    for _ in range(10):
        l = resp.readline().decode("utf-8").strip()
        if l: print(" ", l)
