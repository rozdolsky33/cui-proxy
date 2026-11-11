"""Controlled-clock checks of real phase orchestration; no native workloads."""
import contextlib
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

import performance as perf
import performance_load as load
from performance_workload import Workload


class IdentityMappings(dict):
    def __missing__(self, key):
        return key


class MeasurementBoundaryTests(unittest.TestCase):
    def test_text_report_retains_actual_window_origin_before_dispatch(self):
        workload = Workload("normal")
        env = mock.Mock()
        env.mappings = IdentityMappings()
        env.vault_count.return_value = len(workload.pairs)
        env.population = dict(compiled_detectors=64, vault_entries=len(workload.pairs))
        env.metrics.return_value = dict(stages_ms={})
        row = dict(operation_id="0", supported=True, completed=True,
                   outcome="supported_success", started_s=205.0, finished_s=801.0,
                   duration_ms=596000.0, first_byte_ms=1.0, prefix_ms=[1.0] * 32)
        sampler = SimpleNamespace(samples=[])
        with mock.patch.object(perf, "Environment", return_value=contextlib.nullcontext(env)), \
             mock.patch.object(perf, "ResourceSampler", return_value=contextlib.nullcontext(sampler)), \
             mock.patch.object(perf, "environment_info", return_value={}), \
             mock.patch.object(perf, "request", return_value=dict(outcome="supported_success")), \
             mock.patch.object(perf, "closed_loop", return_value=([row], dict(supported_success=1))), \
             mock.patch.object(perf.time, "monotonic", side_effect=[100.0, 160.0, 200.0, 805.0]):
            result = perf.run_phase(None, None, workload, 262144, 1, True,
                                    1, 600.0, 0.0, 0)
        self.assertEqual(result.get("window_start_s"), 200.0)
        self.assertEqual(result["successful_completions_per_second"], 0.0)
        self.assertEqual(result["warmup_seconds"], 60.0)

    def test_fairness_reports_observed_warmup_before_measurement(self):
        workload = Workload("normal")
        manifest = dict(images=[dict(index=0), dict(index=100000)])
        env = mock.Mock()
        env.mappings = IdentityMappings()
        env.population = dict(compiled_detectors=64, vault_entries=len(workload.pairs))
        env.metrics.return_value = dict(stages_ms={}, recent_requests=[])
        collector = mock.MagicMock()
        collector.__enter__.return_value = collector
        collector.errors = []
        measured = dict(records=[], text_records=[], image_records=[], text_unsent=0,
                        window_start_s=200.0, window_seconds=1.0, aborted=False)
        import extraction_fixtures as fixtures
        with mock.patch.object(fixtures, "validate_coverage"), \
             mock.patch.object(perf, "Environment", return_value=contextlib.nullcontext(env)), \
             mock.patch.object(perf, "ResourceSampler", return_value=contextlib.nullcontext(SimpleNamespace(samples=[]))), \
             mock.patch.object(perf, "environment_info", return_value={}), \
             mock.patch.object(perf, "request", return_value=dict(outcome="supported_success")), \
             mock.patch.object(load, "_drained_metrics", return_value=dict(recent_requests=[])), \
             mock.patch.object(load, "StageCollector", return_value=collector), \
             mock.patch.object(load, "mixed_closed_loop", side_effect=[dict(records=[dict(client=i, outcome="supported_success") for i in range(4)], aborted=False), measured]), \
             mock.patch.object(load.time, "monotonic", side_effect=[100.0, 160.0, 170.0]):
            result = load.run_fairness_phase(None, None, workload, Path("."), manifest,
                                            bypass=True, text_count=1,
                                            warmup_seconds=0.0, warmup_count=1)
        self.assertEqual(result.get("warmup_seconds"), 70.0)

    def test_boundary_metadata_survives_private_report_validation(self):
        with tempfile.TemporaryDirectory() as directory:
            destination = Path(directory) / "report.json"
            perf.write_report(destination, dict(measurements=[dict(window_start_s=200.0,
                                                                   warmup_seconds=70.0)]))
            report = json.loads(destination.read_text())
            self.assertEqual(report["measurements"][0]["window_start_s"], 200.0)
            self.assertEqual(report["measurements"][0]["warmup_seconds"], 70.0)


    def test_extraction_reports_observed_warmup_before_measurement(self):
        from contextlib import nullcontext
        from types import SimpleNamespace
        from unittest.mock import MagicMock, patch
        import extraction_performance as extraction
        import extraction_fixtures as fixtures
        import performance_load as load
        from performance_workload import Workload
        manifest = dict(version=fixtures.VERSION, seed=1102026, count=1, images=fixtures.recipes('warmup', 1) + fixtures.recipes('measurement', 1), pdfs=fixtures.pdf_recipes())
        env = MagicMock()
        env.__enter__.return_value = env
        env.metrics.return_value = dict(recent_requests=[dict(identity=dict(session='warm'))])
        row = dict(operation_id='measured', supported=True, completed=True, outcome='supported_success', duration_ms=1, started_s=1, finished_s=2)
        retained = [dict(identity=dict(session='measured'))]
        collector = MagicMock()
        collector.__enter__.return_value = collector
        collector.records = retained
        collector.errors = 0
        with patch.object(perf, 'Environment', return_value=env), patch.object(perf, 'ResourceSampler', return_value=nullcontext(SimpleNamespace(samples=[]))), patch.object(perf, 'closed_loop', return_value=([row], None)), patch.object(load, 'StageCollector', return_value=collector), patch.object(extraction, 'measured_stages', return_value={}) as stages, patch.object(perf.time, 'monotonic', side_effect=[100.0, 160.0, 170.0]):
            result = extraction.run_phase(None, None, Workload(), Path('unused'), manifest, manifest['images'][0]['template'], count=1, warmup_seconds=0, warmup_count=1)
        collector.wait_for.assert_called_once_with(1)
        self.assertEqual(result.get('warmup_seconds'), 70.0)

if __name__ == "__main__":
    unittest.main()
