import tempfile
import io
import json
from unittest import mock

import extraction_performance
import unittest
from pathlib import Path

import performance
import performance_load


class CacheReportTests(unittest.TestCase):
    def test_numeric_cache_counter_is_allowed(self):
        with tempfile.TemporaryDirectory() as directory:
            performance.write_report(Path(directory) / "report.json", {
                "measurements": [{"final_metrics": {"extraction_cache_hits": 3}}]
            })

    def test_cache_fingerprint_remains_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(ValueError):
                performance.write_report(Path(directory) / "report.json", {
                    "measurements": [{"final_metrics": {"image_fingerprint": "private"}}]
                })

    def test_metrics_collector_keeps_counter_and_supports_old_binary(self):
        env = performance.Environment.__new__(performance.Environment)
        env.ui = "http://127.0.0.1:1"
        for source in ({"extraction_cache_hits": 5}, {}):
            with mock.patch.object(performance.harness.OPENER, "open", return_value=io.BytesIO(json.dumps(source).encode())):
                result = env.metrics()
            if source:
                self.assertEqual(result["extraction_cache_hits"], 5)
            else:
                self.assertNotIn("extraction_cache_hits", result)

    def test_known_zero_wait_remains_measurable(self):
        recent = [{"identity": {"session": "measured"}, "stages_ms": {
            "extraction_wait": 0, "extraction_run": 1.2}, "extraction_cache_hits": 1}]
        result = extraction_performance.measured_stages(recent, set(), 1)
        self.assertEqual(result["extraction_wait"], [0])

    def test_cache_counts_exclude_warmup_and_accept_old_reference(self):
        rows = [{"identity": {"session": "warm"}, "extraction_cache_hits": 8},
                {"identity": {"session": "measured"}, "extraction_cache_hits": 2}]
        self.assertEqual(extraction_performance.measured_cache_hits(rows, {"warm"}, 1), 2)
        self.assertEqual(extraction_performance.measured_cache_hits(
            [{"identity": {"session": "old-reference"}}], set(), 1), 0)

    def test_cache_counts_reject_non_numeric_and_impossible_values(self):
        for value in (-1, 9, True, "private"):
            with self.subTest(value_type=type(value).__name__):
                with self.assertRaises(ValueError):
                    extraction_performance.measured_cache_hits(
                        [{"identity": {"session": "measured"}, "extraction_cache_hits": value}], set(), 1)

    def test_stage_collector_retains_numeric_cache_count(self):
        class Env:
            def metrics(self, include_recent=False):
                return {"recent_requests": [{"time": "2026-11-13T00:00:00Z",
                    "identity": {"session": "measured"},
                    "stages_ms": {"extraction_wait": 0, "extraction_run": 1.2},
                    "extraction_cache_hits": 2}]}
        collector = performance_load.StageCollector(Env(), set())
        collector.sample()
        self.assertEqual(collector.records[0]["extraction_cache_hits"], 2)
