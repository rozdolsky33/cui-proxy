"""Client-side workflow checks, independent of fake-upstream construction."""

import json
from pathlib import Path
import shlex
import tempfile

import performance
from soak_semantics import verify_tool_effect
from tool_prefix_plan import tool_prefix_plan
from soak_tool_stream import ToolStreamObserver


def run_tool_operation(url, case, expected_hash, *, timeout=45, expected_layout_hash=None, defer_boundary=False):
    scenario = case["benchmark_tool_scenario"]
    call_id = case["benchmark_tool_call_id"]
    originals = case["occurrences"][:2]
    if scenario not in ("W02", "W03", "W04") or len(originals) != 2:
        raise ValueError("soak_tool_operation")
    # These are the client's required effects, not output supplied by the backend.
    if scenario == "W02":
        name = "apply_edit"
        arguments = {"path": "effect.txt", "old": originals[0], "new": originals[1]}
    elif scenario == "W03":
        name = "analyze_test_result"
        arguments = {"output": originals[0], "reference": originals[1], "exit_code": 0}
    else:
        name = "plan_command"
        arguments = {"command": shlex.join([
            "inspect-resource", "--source", originals[0], "--target", originals[1]])}
    expected = {"type": "function_call", "name": name, "call_id": call_id,
                "arguments": json.dumps(arguments, separators=(",", ":"))}
    observer = ToolStreamObserver(expected, prefix_plan=tool_prefix_plan(scenario, originals, expected))
    with tempfile.TemporaryDirectory(prefix="cui-soak-effect-") as directory:
        if scenario == "W02":
            Path(directory, "effect.txt").write_text(originals[0])
        row = performance.request(url, case, expected_hash, timeout=timeout,
                                  expected_layout_hash=expected_layout_hash,
                                  defer_boundary=defer_boundary,
                                  observer_factory=lambda: observer)
        row.update(scenario=scenario, semantic_verified=False)
        if row["outcome"] not in ("supported_success", "boundary_pending"):
            return row
        try:
            if not observer.valid():
                raise ValueError("soak_semantic_mismatch")
            effect = verify_tool_effect(scenario, observer.call, originals,
                                        call_id, workspace=Path(directory))
        except (ValueError, OSError):
            row["outcome"] = "client_or_model_failure"
            row["semantic_failure"] = "tool_effect_mismatch"
        else:
            row.update(semantic_verified=True, effect=effect)
        return row
