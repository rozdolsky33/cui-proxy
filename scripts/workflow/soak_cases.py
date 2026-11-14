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


def inject_churn(case, workload, indices, target_size):
    if (not isinstance(indices, list) or len(indices) > 40
            or any(type(index) is not int or index < 0 for index in indices)
            or len(set(indices)) != len(indices)
            or type(target_size) is not int
            or target_size not in (262144, 786432, 1048576)):
        raise ValueError("soak_churn_assignment")
    body = json.loads(case["body"])
    occurrences = list(case["occurrences"])
    if len(occurrences) != 40 or len(case["near_misses"]) != 40 or len(body.get("input", [])) != 40:
        raise ValueError("soak_churn_shape")
    for position, index in enumerate(indices):
        original = occurrences[position]
        replacement = workload.churn_value(index)
        item = body["input"][position]
        if item.get("type") == "function_call_output":
            holder, field = item, "output"
        else:
            content = item.get("content", [])
            if len(content) != 1 or content[0].get("type") != "input_text":
                raise ValueError("soak_churn_shape")
            holder, field = content[0], "text"
        if not isinstance(holder.get(field), str) or holder[field].count(original) != 1:
            raise ValueError("soak_churn_shape")
        holder[field] = holder[field].replace(original, replacement, 1)
        occurrences[position] = replacement
    encoded = json.dumps(body, separators=(",", ":"), ensure_ascii=False).encode()
    if abs(len(encoded) - target_size) * 100 > target_size:
        raise ValueError("soak_churn_size")
    for near_miss in case["near_misses"]:
        value = near_miss.encode()
        if encoded.count(value) != case["body"].count(value):
            raise ValueError("soak_churn_near_miss")
    return dict(case, body=encoded, occurrences=occurrences)
