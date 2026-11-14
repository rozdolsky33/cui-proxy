import hashlib
import json
from pathlib import Path
import tempfile
import unittest

from performance_workload import Workload
from soak_images import build_image_case


class ImageCaseTests(unittest.TestCase):
    def test_required_image_shapes_and_extracted_oracle_keep_association(self):
        workload = Workload()
        mappings = {pair[1]: "mapped-" + str(i) for i, pair in enumerate(workload.pairs)}
        with tempfile.TemporaryDirectory() as directory:
            data = b"in-memory-test-image"
            Path(directory, "fixture.png").write_bytes(data)
            fixture = {"file": "fixture.png", "format": "png", "bytes": len(data), "sha256": hashlib.sha256(data).hexdigest(), "expected": "image text", "pages": 1}
            for scenario in ("W05", "W06"):
                with self.subTest(scenario=scenario):
                    case, expected = build_image_case(workload, 0, 262144, [fixture, fixture], directory, mappings, "client-0", scenario)
                    body = json.loads(case["body"])
                    oracle = json.loads(expected)
                    from performance import values_from_body
                    self.assertEqual(values_from_body(oracle), [mappings.get(value, value) for value in case["occurrences"]])
                    output = body["input"][-1]
                    self.assertEqual(output["call_id"], oracle["input"][-1]["call_id"])
                    self.assertIsInstance(output["output"], list)
                    if scenario == "W05":
                        self.assertEqual([p["type"] for p in output["output"]], ["input_text", "input_image", "input_image", "input_text"])
                    else:
                        self.assertEqual(body["input"][-2]["content"][0]["type"], "input_image")
                        self.assertEqual([p["type"] for p in output["output"]], ["input_text", "input_image", "input_text"])
                    from performance import text_layout_hash
                    import copy
                    for mutation in ("association", "order", "missing"):
                        changed = copy.deepcopy(oracle)
                        last = changed["input"][-1]
                        if mutation == "association":
                            last["call_id"] = "wrong-call"
                        elif mutation == "order":
                            last["output"].reverse()
                        else:
                            last["output"].pop(1)
                        self.assertNotEqual(text_layout_hash(expected), text_layout_hash(json.dumps(changed).encode()))
                    self.assertNotIn("image_url", json.dumps(oracle))
                    self.assertNotIn("input_image", json.dumps(oracle))
                    self.assertIn("image text", json.dumps(oracle))
                    self.assertEqual(case["benchmark_session"], "client-0")

    def test_invalid_scenario_fails_before_reading_fixtures(self):
        with self.assertRaises(ValueError):
            build_image_case(Workload(), 0, 262144, [], "/unused", {}, "client-0", "W01")

    def test_execution_uses_extracted_layout_for_proxy_and_original_hash_for_bypass(self):
        from unittest.mock import patch
        from soak_images import execute_image
        from performance import text_layout_hash, values_from_body
        workload = Workload()
        mappings = {pair[1]: "mapped-" + str(i) for i, pair in enumerate(workload.pairs)}
        with tempfile.TemporaryDirectory() as directory:
            data = b"construction-only"
            Path(directory, "fixture.png").write_bytes(data)
            fixture = {"file": "fixture.png", "format": "png", "bytes": len(data), "sha256": hashlib.sha256(data).hexdigest(), "expected": "image text", "pages": 1}
            for scenario in ("W05", "W06"):
                case, expected = build_image_case(workload, 0, 262144, [fixture, fixture], directory, mappings, "client-0", scenario)
                for bypass in (False, True):
                    with self.subTest(scenario=scenario, bypass=bypass), patch("soak_images.performance.request", return_value={"outcome": "supported_success", "completed": True}) as request:
                        row = execute_image("http://unused", workload, 0, 262144, [fixture, fixture], directory, mappings, "client-0", scenario, bypass=bypass)
                        actual_case = request.call_args.args[1]
                        if bypass:
                            self.assertEqual(request.call_args.kwargs["expected_hash"], hashlib.sha256(case["body"]).hexdigest())
                            self.assertEqual(actual_case["occurrences"], values_from_body(json.loads(case["body"])))
                        else:
                            self.assertEqual(request.call_args.kwargs["expected_layout_hash"], text_layout_hash(expected))
                            self.assertEqual(actual_case["occurrences"], case["occurrences"])
                        self.assertTrue(row["semantic_verified"])
                        self.assertEqual(row["scenario"], scenario)
