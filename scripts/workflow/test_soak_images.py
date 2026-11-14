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
                    self.assertNotIn("image_url", json.dumps(oracle))
                    self.assertNotIn("input_image", json.dumps(oracle))
                    self.assertIn("image text", json.dumps(oracle))
                    self.assertEqual(case["benchmark_session"], "client-0")

    def test_invalid_scenario_fails_before_reading_fixtures(self):
        with self.assertRaises(ValueError):
            build_image_case(Workload(), 0, 262144, [], "/unused", {}, "client-0", "W01")
