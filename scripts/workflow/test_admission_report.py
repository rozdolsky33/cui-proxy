import json
from pathlib import Path
import tempfile
import unittest

import performance


class AdmissionReportTests(unittest.TestCase):
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
