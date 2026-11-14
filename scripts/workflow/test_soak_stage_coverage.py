import unittest

from soak_stage_coverage import evaluate_stage_coverage


class StageCoverageTests(unittest.TestCase):
    def evidence(self):
        return [dict(identity=dict(session="test"), time=str(i),
                     observation_lower_s=10+i, observed_s=10.5+i,
                     stages_ms=dict(extraction_run=100, extraction_wait=1),
                     extraction_cache_hits=0) for i in range(2)]

    def evaluate(self, records, errors=None, restarts=None):
        return evaluate_stage_coverage(records, expected_images=2, started_s=10,
                                       ended_s=20, poll_errors=errors or [],
                                       restart_intervals=restarts or [])

    def test_complete_extraction_coverage_passes(self):
        r = self.evaluate(self.evidence())
        self.assertEqual(r["state"], "pass")
        self.assertEqual(r["extraction_records"], 2)
        self.assertFalse(r["performance_qualified"])

    def test_missing_extra_or_duplicate_records_are_rejected(self):
        records = self.evidence()
        for bad in (records[:1], records + [records[0]], [records[0], records[0]]):
            with self.assertRaises(ValueError): self.evaluate(bad)

    def test_poll_failures_require_recorded_restart_interval(self):
        errors = [dict(observed_s=15, poll_errors=1)]
        self.assertEqual(self.evaluate(self.evidence(), errors, [(14,16)])["state"], "pass")
        with self.assertRaises(ValueError): self.evaluate(self.evidence(), errors)
        with self.assertRaises(ValueError): self.evaluate(self.evidence(), errors, [(16,17)])

    def test_warmup_stages_do_not_substitute_for_measured_work(self):
        records = self.evidence()
        records[0]["observation_lower_s"] = 8
        records[0]["observed_s"] = 9
        with self.assertRaises(ValueError): self.evaluate(records)
        records[0]["observed_s"] = 11
        with self.assertRaises(ValueError): self.evaluate(records)

    def test_extraction_budget_and_observation_bounds_remain_enforced(self):
        for key, value in (("observed_s", float("nan")), ("observation_lower_s", None),
                           ("observation_lower_s", 12)):
            records = self.evidence(); records[0][key] = value
            with self.assertRaises(ValueError): self.evaluate(records)
        records = self.evidence(); records[0]["stages_ms"]["extraction_run"] = 30001
        with self.assertRaises(ValueError): self.evaluate(records)
