import copy
import json
from pathlib import Path
import sys
import tempfile
import threading
import unittest
from unittest.mock import Mock, patch

import soak_campaign
import soak_campaign_evidence as campaign_evidence
from soak_campaign_evidence import CampaignEvidence
import soak_evidence
import soak_resources as resources
from test_soak_resources import metrics


class BoundaryEncodingTests(unittest.TestCase):
    def test_actual_save_report_permits_a_sampler_heartbeat_during_encoding(self):
        entered = threading.Event()
        ready = threading.Event()
        encoding = threading.Event()
        observed = []
        original = campaign_evidence._encode
        def watched(value):
            encoding.set()
            entered.set()
            try:
                return original(value)
            finally:
                encoding.clear()
        def heartbeat():
            ready.set()
            entered.wait(5)
            observed.append(encoding.is_set())
        values = [index / 7.0 for index in range(128)]
        report = {"results": [{"prefix_ms": values, "relay_ms": values} for _ in range(256)]}
        interval = sys.getswitchinterval()
        thread = threading.Thread(target=heartbeat)
        try:
            with tempfile.TemporaryDirectory() as temporary:
                with CampaignEvidence(Path(temporary) / "campaign", {}, {}) as campaign:
                    thread.start()
                    self.assertTrue(ready.wait(2))
                    sys.setswitchinterval(30.0)
                    with patch.object(campaign_evidence, "_encode", side_effect=watched):
                        campaign.save_report("fixed-summary.json", report)
        finally:
            sys.setswitchinterval(interval)
            entered.set()
            thread.join(5)
        self.assertFalse(thread.is_alive())
        self.assertEqual(observed, [True], "sampler thread could not run during actual report encoding")

    def test_cooperative_canonical_bytes_match_legacy_for_nested_floats_and_unicode(self):
        value = {"unicode": "\u00e9\u4e16\u754c\n", "nested": [{"z": -0.0, "a": 1e-30},
            {"values": [None, True, False, 1.2345678901234567, 1e100]}]}
        before = copy.deepcopy(value)
        expected = json.dumps(value, sort_keys=True, separators=(",", ":"), allow_nan=False).encode()
        self.assertEqual(soak_evidence._encode(value), expected)
        self.assertEqual(value, before)
        for invalid in (float("nan"), float("inf"), -float("inf")):
            with self.subTest(kind=repr(invalid)), self.assertRaises(ValueError):
                soak_evidence._encode({"value": invalid})


class ExistingResourceBoundFailfastTests(unittest.TestCase):
    def monitor(self, stop):
        values = iter([1002.001344, 1002.070772])
        rows = []
        monitor = resources.ResourceMonitor(lambda: {"available": True, "proxy_rss_bytes": 100,
            "child_rss_bytes": 0, "metrics": metrics()}, rows.append, phase_stop=stop,
            clock=lambda: next(values))
        monitor.started = 1000.0
        return monitor, rows

    def test_one_true_unobserved_nominal_second_records_real_clock_failure_and_stops(self):
        stop = threading.Event()
        monitor, rows = self.monitor(stop)
        monitor._take(1)
        self.assertIsNotNone(monitor.failure)
        self.assertTrue(stop.is_set())
        self.assertTrue(monitor.stop.is_set())
        self.assertEqual(len(rows), 1)
        self.assertEqual(rows[0]["slot"], 1)
        self.assertEqual(rows[0]["observation_started_s"], 1002.001344)
        self.assertEqual(rows[0]["observed_s"], 1002.070772)
        self.assertEqual(rows[0]["sample_error"], "resource_series_clock_origin")

    def test_outer_boundary_clock_failure_prevents_churn_phase_start(self):
        stop = threading.Event()
        monitor, rows = self.monitor(stop)
        resource_context = Mock()
        resource_context.__enter__ = Mock(return_value=monitor)
        resource_context.__exit__ = Mock(return_value=False)
        stage_context = Mock()
        stage_context.__enter__ = Mock(return_value=Mock())
        stage_context.__exit__ = Mock(return_value=False)
        phases = []
        campaign = Mock()
        campaign.root = Path("/private/tmp/unused-resource-test")
        def save(name, value):
            if name == "proxy-fixed-summary.json":
                monitor._take(1)
        campaign.save_report.side_effect = save
        def run(*args, **kwargs):
            phase = kwargs.get("phase")
            if phase is None:
                phase = next((value for value in args if value in ("fixed", "churn")), None)
            phases.append(phase)
            if phase == "churn":
                raise RuntimeError("test_churn_entered")
            return {"completed": True, "mapping_evidence": {"unmet_requirements": []}, "results": []}
        env = Mock()
        env.bypass = False
        env.metrics.return_value = {"active_requests": 0}
        controlled = Mock()
        controlled.summary.return_value = {"failed": False, "restarts_completed": 0, "reloads_completed": 0}
        patches = {"run_warmup": Mock(return_value={"results": []}),
            "wait_for_idle": Mock(return_value={}), "observe_idle_gc": Mock(return_value={}),
            "inspect_vault": Mock(return_value={"mappings": {}}), "ScenarioExecutor": Mock(),
            "PolicyReloader": Mock(), "ControlledExecutor": Mock(return_value=controlled),
            "ResourceMonitor": Mock(return_value=resource_context),
            "StageCollector": Mock(return_value=stage_context), "measure_resources": Mock(),
            "run_recorded_phase": Mock(side_effect=run),
            "finalize_mapping_evidence": Mock(side_effect=lambda report, *args, **kwargs: report)}
        with patch.multiple(soak_campaign, **patches), patch.object(soak_campaign.threading, "Event", return_value=stop):
            try:
                soak_campaign.run_proxy_phases(env, Mock(), Mock(), Path("/private/tmp/unused-fixtures"),
                    campaign, fixed_seconds=10, churn_seconds=10, control_count=0)
            except RuntimeError:
                pass
        self.assertEqual(phases, ["fixed"])
        self.assertIsNotNone(monitor.failure)
        self.assertEqual(len(rows), 1)


    def test_existing_clock_origin_tolerance_is_not_tightened(self):
        for delta in (-0.0000005, 1.0000005):
            stop = threading.Event()
            values = iter([1001.0 + delta, 1001.01 + delta])
            rows = []
            monitor = resources.ResourceMonitor(lambda: {"available": True,
                "proxy_rss_bytes": 100, "child_rss_bytes": 0, "metrics": metrics()},
                rows.append, phase_stop=stop, clock=lambda: next(values))
            monitor.started = 1000.0
            monitor._take(1)
            with self.subTest(delta=delta):
                self.assertIsNone(monitor.failure)
                self.assertFalse(stop.is_set())
                self.assertNotIn("sample_error", rows[0])
