import copy
import math
import unittest
import tempfile
from pathlib import Path
import json
import performance

from stage_attribution import workload_stages


class WorkloadStageTests(unittest.TestCase):
    def rows(self):
        return [
            {"time": "completion1", "identity": {"session": "text1"}, "stages_ms": {"body_inspect": 2.0, "mapping_checkpoint": 0.0}},
            {"time": "completion2", "identity": {"session": "image1"}, "stages_ms": {"body_inspect": 3.0, "extraction_run": 4.0, "extraction_wait": 0.0, "mapping_checkpoint": 1.0}},
        ]

    def test_groups_and_zero_are_preserved_without_identity(self):
        got = workload_stages(self.rows(), set(), 1, 1)
        self.assertEqual(got["text_requests"]["samples_ms"]["body_inspect"], [2.0])
        self.assertEqual(got["image_requests"]["samples_ms"]["extraction_run"], [4.0])
        self.assertEqual(got["text_requests"]["samples_ms"]["mapping_checkpoint"], [0.0])
        self.assertEqual(got["text_requests"]["coverage"]["mapping_checkpoint"], 1)
        self.assertEqual(got["text_requests"]["requests"], 1)
        self.assertNotIn("text1", str(got))
        self.assertNotIn("image1", str(got))
        self.assertNotIn("identity", str(got))

    def test_warmup_rows_are_excluded(self):
        rows = self.rows()
        warm = copy.deepcopy(rows[0])
        warm["identity"]["session"] = "warm"
        self.assertEqual(workload_stages([warm] + rows, {"warm"}, 1, 1), workload_stages(rows, set(), 1, 1))

    def test_older_binary_checkpoint_is_missing_not_zero(self):
        rows = self.rows()
        for row in rows:
            del row["stages_ms"]["mapping_checkpoint"]
        got = workload_stages(rows, set(), 1, 1)
        self.assertEqual(got["text_requests"]["samples_ms"]["mapping_checkpoint"], [])
        self.assertEqual(got["text_requests"]["coverage"]["mapping_checkpoint"], 0)

    def test_empty_workload_is_valid(self):
        got = workload_stages([], set(), 0, 0)
        self.assertEqual(got["text_requests"]["requests"], 0)
        self.assertEqual(got["image_requests"]["requests"], 0)

    def test_missing_or_extra_rows_fail(self):
        for counts in [(2, 1), (1, 2), (0, 1), (1, 0)]:
            with self.subTest(counts=counts), self.assertRaisesRegex(ValueError, "stage_coverage_invalid"):
                workload_stages(self.rows(), set(), *counts)

    def test_duplicate_completion_records_fail(self):
        rows = self.rows()
        rows[1]["identity"]["session"] = rows[0]["identity"]["session"]
        rows[1]["time"] = rows[0]["time"]
        with self.assertRaisesRegex(ValueError, "stage_coverage_invalid"):
            workload_stages(rows, set(), 1, 1)

    def test_invalid_durations_fail(self):
        for value in [True, -1, math.inf, math.nan, 10**400, "private-marker", None]:
            rows = self.rows()
            rows[0]["stages_ms"]["body_inspect"] = value
            with self.subTest(kind=type(value).__name__), self.assertRaisesRegex(ValueError, "stage_coverage_invalid"):
                workload_stages(rows, set(), 1, 1)

    def test_unknown_stage_fails_without_value_in_error(self):
        rows = self.rows()
        rows[0]["stages_ms"]["private-marker"] = 1
        with self.assertRaises(ValueError) as error:
            workload_stages(rows, set(), 1, 1)
        self.assertEqual(str(error.exception), "stage_coverage_invalid")

    def test_partial_extraction_stage_fails(self):
        for key in ["extraction_run", "extraction_wait"]:
            rows = self.rows()
            del rows[1]["stages_ms"][key]
            with self.assertRaisesRegex(ValueError, "stage_coverage_invalid"):
                workload_stages(rows, set(), 1, 1)

    def test_malformed_rows_fail(self):
        for bad in [None, {}, {"identity": None}, {"identity": {"session": ""}}, {"identity": {"session": True}}, {"identity": {"session": "text1"}, "stages_ms": []}]:
            with self.subTest(kind=type(bad).__name__), self.assertRaisesRegex(ValueError, "stage_coverage_invalid"):
                workload_stages([bad], set(), 1, 0)

    def test_invalid_expected_counts_fail(self):
        for count in [-1, True, 1.0]:
            with self.assertRaisesRegex(ValueError, "stage_coverage_invalid"):
                workload_stages([], set(), count, 0)

    def test_malformed_collection_fails(self):
        for recent in [None, {}, "private-marker"]:
            with self.assertRaisesRegex(ValueError, "stage_coverage_invalid"):
                workload_stages(recent, set(), 0, 0)

    def test_same_session_distinct_completions_are_valid(self):
        rows = self.rows()
        rows[1]["identity"]["session"] = rows[0]["identity"]["session"]
        got = workload_stages(rows, set(), 1, 1)
        self.assertEqual(got["text_requests"]["requests"], 1)
        self.assertEqual(got["image_requests"]["requests"], 1)

    def test_missing_completion_time_fails(self):
        rows = self.rows()
        del rows[0]["time"]
        with self.assertRaisesRegex(ValueError, "stage_coverage_invalid"):
            workload_stages(rows, set(), 1, 1)

    def test_stage_groups_pass_existing_report_privacy_guard(self):
        report = {"measurements": [{"request_stage_samples": workload_stages(self.rows(), set(), 1, 1)}]}
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "report.json"
            performance.write_report(path, report)
            self.assertEqual(json.loads(path.read_text()), report)

    def test_report_privacy_still_rejects_raw_image_key(self):
        report = {"measurements": [{"image": "private-marker"}]}
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaisesRegex(ValueError, "private or unknown report fields"):
                performance.write_report(Path(directory) / "report.json", report)
