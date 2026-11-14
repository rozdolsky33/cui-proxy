"""Independent controlled tool effects for sustained workflow verification."""

import json
import os
import pathlib
import shlex
import stat


def _fail():
    raise ValueError("soak_semantic_mismatch")


def _values(values):
    if (type(values) not in (list, tuple) or len(values) < 2
            or any(type(value) is not str or not value for value in values[:2])):
        _fail()
    return values[0], values[1]


def build_tool_call(scenario, received_values, call_id):
    """Fake upstream construction; never accepts an original-value oracle."""
    first, second = _values(received_values)
    if type(call_id) is not str or not call_id:
        _fail()
    if scenario == "W03":
        return {"type": "function_call", "name": "analyze_test_result", "call_id": call_id,
                "arguments": json.dumps({"output": first, "reference": second, "exit_code": 0}, separators=(",", ":"))}
    if scenario == "W02":
        name = "apply_edit"
        arguments = {"path": "effect.txt", "old": first, "new": second}
    elif scenario == "W04":
        name = "plan_command"
        arguments = {"command": shlex.join(
            ["inspect-resource", "--source", first, "--target", second])}
    else:
        _fail()
    return {"type": "function_call", "name": name, "call_id": call_id,
            "arguments": json.dumps(arguments, ensure_ascii=False, separators=(",", ":"))}


def _unique(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            _fail()
        result[key] = value
    return result


def _constant(_value):
    _fail()


def verify_tool_effect(scenario, call, originals, expected_call_id, workspace=None):
    """Verify restored arguments independently; return metadata-only evidence.

    The caller owns the workspace and creates its initial effect.txt. No tool
    supplied path or command is executed. A semantic discrepancy is a failure,
    including changes to shell tokenization after string restoration.
    """
    try:
        first, second = _values(originals)
        if (type(call) is not dict
                or set(call) != {"type", "name", "call_id", "arguments"}
                or call["type"] != "function_call"
                or type(expected_call_id) is not str or not expected_call_id
                or call["call_id"] != expected_call_id
                or type(call["arguments"]) is not str):
            _fail()
        args = json.loads(call["arguments"], object_pairs_hook=_unique,
                          parse_constant=_constant)
        if type(args) is not dict:
            _fail()
        if scenario == "W03":
            if (call["name"] != "analyze_test_result" or type(args.get("exit_code")) is not int
                    or args != {"output": first, "reference": second, "exit_code": 0}):
                _fail()
            return "test_followup_verified"
        if scenario == "W02":
            if (call["name"] != "apply_edit"
                    or args != {"path": "effect.txt", "old": first, "new": second}
                    or workspace is None):
                _fail()
            target = pathlib.Path(workspace) / "effect.txt"
            descriptor = os.open(target, os.O_RDWR | os.O_NOFOLLOW | os.O_NONBLOCK)
            with os.fdopen(descriptor, "r+", encoding="utf-8") as output:
                if not stat.S_ISREG(os.fstat(output.fileno()).st_mode):
                    _fail()
                if output.read() != first:
                    _fail()
                output.seek(0)
                output.write(second)
                output.truncate()
                output.flush()
                output.seek(0)
                if output.read() != second:
                    _fail()
            return {"effect": "file_edit_verified"}
        if scenario == "W04":
            if (call["name"] != "plan_command" or set(args) != {"command"}
                    or type(args["command"]) is not str
                    or shlex.split(args["command"]) !=
                    ["inspect-resource", "--source", first, "--target", second]):
                _fail()
            return {"effect": "command_tokens_verified"}
        _fail()
    except (ValueError, TypeError, OSError, RecursionError):
        raise ValueError("soak_semantic_mismatch") from None
