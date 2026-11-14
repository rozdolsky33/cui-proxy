"""Add protocol-correct tool definitions without changing workload text."""

import json


def build_tool_case(case, scenario, call_id, target_size):
    if (scenario not in ("W02", "W04") or not isinstance(call_id, str)
            or not call_id or len(call_id) > 128
            or type(target_size) is not int
            or target_size not in (262144, 786432, 1048576)):
        raise ValueError("soak_tool_case")
    body = json.loads(case["body"])
    if "tools" in body or "tool_choice" in body:
        raise ValueError("soak_tool_case")
    if scenario == "W02":
        name, fields = "apply_edit", ("path", "old", "new")
    else:
        name, fields = "plan_command", ("command",)
    body["tools"] = [{
        "type": "function", "name": name, "strict": True,
        "parameters": {
            "type": "object",
            "properties": {field: {"type": "string"} for field in fields},
            "required": list(fields), "additionalProperties": False,
        },
    }]
    body["tool_choice"] = {"type": "function", "name": name}
    encoded = json.dumps(body, separators=(",", ":"), ensure_ascii=False).encode()
    if abs(len(encoded) - target_size) * 100 > target_size:
        raise ValueError("soak_tool_case_size")
    return dict(case, body=encoded, benchmark_tool_scenario=scenario,
                benchmark_tool_call_id=call_id)
