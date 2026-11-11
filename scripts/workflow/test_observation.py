import copy
import unittest

import harness
import observation


class ObservationTests(unittest.TestCase):
    def snapshots(self):
        stages = {
            name: {"p50": 1, "p95": 1, "p99": 1, "samples": 1}
            for name in observation.STAGES
        }
        stages["upstream_wait"]["p95"] = 80
        del stages["extraction_wait"]
        del stages["extraction_run"]
        text = {"stages_ms": stages, "active_requests": 0,
                "extractor_queued": 0, "extractor_active": 0,
                "request_size_buckets": {"le_4096": 1}}
        image = copy.deepcopy(text)
        for stage in image["stages_ms"].values():
            stage["samples"] += 1
        image["stages_ms"]["extraction_wait"] = {"p50": 1, "p95": 1, "p99": 1, "samples": 1}
        image["stages_ms"]["extraction_run"] = {"p50": 80, "p95": 80, "p99": 80, "samples": 1}
        image["request_size_buckets"]["le_4096"] = 2
        return text, image

    def test_observed_stages_and_cleanup(self):
        observation.check_snapshots(*self.snapshots())

    def test_missing_stage_rejected(self):
        text, image = self.snapshots()
        text["stages_ms"]["upstream_wait"]["samples"] = 0
        with self.assertRaises(harness.CheckFailure):
            observation.check_snapshots(text, image)

    def test_extraction_charged_to_body_rejected(self):
        text, image = self.snapshots()
        image["stages_ms"]["body_inspect"]["p95"] = 90
        with self.assertRaises(harness.CheckFailure):
            observation.check_snapshots(text, image)

    def test_correlation_disabled_by_default(self):
        text, image = self.snapshots()
        image["recent_requests"] = [{"request": "unwanted-record"}]
        with self.assertRaises(harness.CheckFailure):
            observation.check_snapshots(text, image)

    def test_leaked_gauges_rejected(self):
        text, image = self.snapshots()
        image["extractor_queued"] = 1
        with self.assertRaises(harness.CheckFailure):
            observation.check_snapshots(text, image)
