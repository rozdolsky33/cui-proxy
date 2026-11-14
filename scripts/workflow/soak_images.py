"""Required rich image shapes with independent extracted upstream bodies."""

import hashlib
import json

import performance

from extraction_performance import attachment_case
from performance_workload import encode


def build_image_case(workload, serial, size, fixtures, directory, mappings, session, scenario):
    if scenario not in ("W05", "W06") or len(fixtures) != 2:
        raise ValueError("soak_image_case")
    pairs = [attachment_case(workload, serial, size, fixture, directory, mappings,
                             session, include_expected_body=True) for fixture in fixtures]
    source = json.loads(pairs[0][0]["body"])
    expected = json.loads(pairs[0][1])
    image_parts = [json.loads(case["body"])["input"][-1]["content"][0] for case, _ in pairs]
    text_parts = [json.loads(body)["input"][-1]["content"][0] for _, body in pairs]
    source["input"].pop()
    expected["input"].pop()
    before = {"type": "input_text", "text": "image-start"}
    after = {"type": "input_text", "text": "image-end"}
    call_id = f"image-result-{serial}"
    kind = "custom_tool_call_output" if scenario == "W05" else "function_call_output"
    if scenario == "W06":
        source["input"].append({"role": "user", "content": [image_parts.pop(0)]})
        expected["input"].append({"role": "user", "content": [text_parts.pop(0)]})
    source["input"].append({"type": kind, "call_id": call_id,
                            "output": [before, *image_parts, after]})
    expected["input"].append({"type": kind, "call_id": call_id,
                              "output": [before, *text_parts, after]})
    base_count = len(workload.case(serial, size)["occurrences"])
    image_values = [case["occurrences"][base_count:] for case, _ in pairs]
    originals = pairs[0][0]["occurrences"][:base_count]
    if scenario == "W06":
        originals += image_values[0] + ["image-start"] + image_values[1] + ["image-end"]
    else:
        originals += ["image-start"] + image_values[0] + image_values[1] + ["image-end"]
    case = dict(pairs[0][0], body=encode(source), occurrences=originals)
    return case, encode(expected)


def execute_image(url, workload, serial, size, fixtures, directory, mappings,
                  session, scenario, *, bypass=False, operation_id=None):
    case, expected = build_image_case(workload, serial, size, fixtures, directory,
                                      mappings, session, scenario)
    if operation_id is not None:
        case["operation_id"] = operation_id
    if bypass:
        case = dict(case, occurrences=performance.values_from_body(json.loads(case["body"])))
        row = performance.request(url, case, expected_hash=hashlib.sha256(case["body"]).hexdigest())
    else:
        row = performance.request(url, case, expected_layout_hash=performance.text_layout_hash(expected))
    row.update(scenario=scenario, category="image", image_count=2,
               semantic_verified=row.get("completed") is True and row.get("outcome") == "supported_success")
    row["boundary_verified"] = row.get("outcome") == "supported_success"
    return row
