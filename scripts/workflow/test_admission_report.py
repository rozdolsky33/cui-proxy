import json
from pathlib import Path
import tempfile
import unittest

import performance


class AdmissionReportTests(unittest.TestCase):
    def test_only_explicit_proxy_overload_is_classified_as_overload(self):
        known = b'{"error":{"code":"overload","type":"cui_proxy_blocked"}}'
        self.assertEqual(performance.failure_outcome(503, known), "proxy_overload")
        for body in [b"", b"invalid", b"[]", b'{"error":"overload"}',
                     b'{"error":{"code":"overload","type":"upstream"}}']:
            self.assertEqual(performance.failure_outcome(503, body), "upstream_failure")
        self.assertEqual(performance.failure_outcome(500, known), "upstream_failure")
        self.assertEqual(performance.failure_outcome(422, known), "unexpected_proxy_refusal")

    def test_admission_metadata_is_allowed_but_unknown_values_are_not(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "report.json"
            metrics = {
                "admission": {"active": 2, "queued": 1, "reserved_input_bytes": 4096},
                "stages_ms": {"admission_wait": {"p50": 1, "p95": 2, "p99": 3, "samples": 4}},
            }
            report = {"schema_version": 1, "measurements": [{"final_metrics": metrics}]}
            performance.write_report(path, report)
            self.assertEqual(json.loads(path.read_text()), report)
            metrics["admission"]["request_payload"] = "withheld"
            with self.assertRaises(ValueError):
                performance.write_report(path, report)
