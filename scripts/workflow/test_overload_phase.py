import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import performance as perf
import performance_load as load
from performance_workload import Workload


class OverloadPhaseTests(unittest.TestCase):
    def test_invalid_load_settings_fail_before_backend_setup(self):
        for settings in ({"rate": 0}, {"duration": float("nan")},
                         {"max_outstanding": 17}, {"size": 0}):
            options = dict(rate=2, duration=1, size=262144, max_outstanding=16,
                           warmup_seconds=0, warmup_count=1)
            options.update(settings)
            with self.subTest(settings=settings), mock.patch(
                    "performance.Environment", side_effect=AssertionError("backend started")):
                with self.assertRaises(ValueError):
                    load.run_overload_phase(None, None, None, **options)

    def test_sampler_cleanup_failure_preserves_complete_slot_denominator(self):
        from types import SimpleNamespace
        row = dict(outcome="supported_success", supported=True, completed=True,
                   status=200, started_s=1.0, finished_s=2.0, first_byte_ms=1.0)
        measured = dict(scheduled=4, sent=1, completed=1, rejected=0, client_unsent=3,
                        records=[row], unsent_records=[dict(reason="client_capacity")] * 3)
        env = SimpleNamespace(url="unused", upstream_url="unused", mappings={}, population={})
        environment = mock.MagicMock()
        environment.__enter__.return_value = env
        sampler = mock.MagicMock()
        sampler.__exit__.side_effect = RuntimeError("cleanup failed")
        with (mock.patch.object(perf, "environment_info", return_value={}),
              mock.patch.object(perf, "Environment", return_value=environment),
              mock.patch.object(perf, "ResourceSampler", return_value=sampler),
              mock.patch.object(perf, "request", return_value=dict(row)),
              mock.patch.object(load, "_text_case", return_value=({"body": b"{}"}, "hash", "hash")),
              mock.patch.object(load, "_drained_metrics", return_value={}),
              mock.patch.object(load, "open_loop", return_value=measured)):
            result = load.run_overload_phase(None, None, Workload(), size=262144,
                                            rate=4, duration=1, warmup_seconds=0, warmup_count=1)
        self.assertTrue(result["collection_failed"])
        self.assertEqual(result["collection_error_kind"], "RuntimeError")
        self.assertEqual(result["sent"] + result["client_unsent"], result["scheduled"])
        self.assertEqual(result["records"], [row])
        self.assertEqual(len(result["unsent_records"]), 3)


    @unittest.skipUnless(os.environ.get("CUI_LOAD_PROXY_TEST") == "1", "native proxy opt-in")
    def test_real_overload_retains_all_scheduled_slots_and_measurements(self):
        runs = []
        for bypass in (True, False):
            result = load.run_overload_phase(
                os.environ["CUI_PROXY_TEST_BINARY"], os.environ["CUI_PROXY_TEST_SEED_BINARY"],
                Workload(), size=262144, rate=4, duration=1, max_outstanding=2,
                bypass=bypass, warmup_seconds=0, warmup_count=1)
            self.assertEqual(result["scheduled"], 4)
            self.assertEqual(result["sent"] + result["client_unsent"], 4)
            self.assertEqual(len(result["records"]), result["sent"])
            self.assertGreater(result["sent"], 0)
            self.assertGreater(result["client_unsent"], 0)
            self.assertEqual(result["completed"], result["sent"])
            self.assertTrue(all(r["outcome"] == "supported_success" for r in result["records"]))
            self.assertFalse(result["performance_qualified"])
            self.assertFalse(result["collection_failed"])
            runs.append(result)
        with tempfile.TemporaryDirectory() as directory:
            perf.write_report(Path(directory) / "report.json", dict(
                schema_version=1, measurements=runs, performance_qualified=False))
