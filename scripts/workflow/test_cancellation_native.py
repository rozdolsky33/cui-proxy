from pathlib import Path
from types import SimpleNamespace
import tempfile
import http.client
import time
from unittest import mock
from soak_phase import OperationFailure
import unittest

from cancellation_native import NativeTrialDriver, parse_processes


class FakeGate:
    def __init__(self, root):
        self.generation = Path(root) / ("a" * 32)
        self.generation.mkdir()
        self.armed = False
        self.rows = [{"pid": 101, "generation": "a" * 32, "started_ns": 1, "input_sha256": "b" * 64}]
    def arm(self): self.armed = True
    def release(self): self.armed = False
    def snapshot(self): return list(self.rows)


class NativeDriverObservationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.gate = FakeGate(self.temp.name)
        self.processes = {
            90: {"pid": 90, "parent_pid": 1, "state": "S", "start": "proxy-start"},
            101: {"pid": 101, "parent_pid": 90, "state": "S", "start": "helper-start"},
        }
        self.metrics = {"extractor_active": 1, "extractor_queued": 0}
        self.env = SimpleNamespace(url="http://127.0.0.1:12345", ui="http://127.0.0.1:12346", directory=Path(self.temp.name), proxy=SimpleNamespace(pid=90))
        self.driver = NativeTrialDriver(self.env, lambda plan: {}, {}, self.gate, {"pid": 90, "start": "proxy-start"}, process_reader=lambda: self.processes, metrics_reader=lambda: self.metrics)
        self.addCleanup(self.driver.cleanup)
        self.driver.arm()

    def test_binds_marker_to_actual_parent_and_start_signature(self):
        row = self.driver.observe()
        self.assertEqual(row["active"], 1)
        self.assertTrue(row["gate_armed"])
        self.assertEqual(row["helpers"][0]["start"], "helper-start")
        self.assertEqual(row["helpers"][0]["parent_start"], "proxy-start")
        self.assertEqual(row["marker_count"], 1)

    def test_rejects_wrong_parent_and_changed_proxy_identity(self):
        self.processes[101]["parent_pid"] = 91
        with self.assertRaises(ValueError): self.driver.observe()
        self.processes[101]["parent_pid"] = 90
        self.processes[90]["start"] = "different-start"
        with self.assertRaises(ValueError): self.driver.observe()

    def test_unknown_owned_process_state_cannot_prove_liveness(self):
        for pid in (90, 101):
            original = self.processes[pid]["state"]
            self.processes[pid]["state"] = "?s"
            with self.subTest(pid=pid), self.assertRaises(ValueError): self.driver.observe()
            self.processes[pid]["state"] = original

    def test_retains_original_identity_when_pid_is_reused(self):
        self.driver.observe()
        self.processes[101]["start"] = "replacement-process"
        self.metrics["extractor_active"] = 0
        row = self.driver.observe()
        self.assertEqual(row["helpers"], [])
        self.assertEqual(row["stopped_helpers"][0]["helper"]["start"], "helper-start")
        self.assertEqual(row["stopped_helpers"][0]["state"], "absent")

    def test_observes_zombie_as_terminal_not_live(self):
        self.driver.observe()
        self.processes[101]["state"] = "Z"
        self.metrics["extractor_active"] = 0
        row = self.driver.observe()
        self.assertEqual(row["helpers"], [])
        self.assertEqual(row["stopped_helpers"][0]["state"], "zombie")

    def test_detects_release_marker_and_invalid_metric_type(self):
        (self.gate.generation / "release").touch()
        self.assertFalse(self.driver.observe()["gate_armed"])
        self.metrics["extractor_active"] = True
        with self.assertRaises(ValueError): self.driver.observe()

    def test_refuses_unidentified_final_marker(self):
        with self.assertRaises(ValueError): self.driver.final_helpers()
        self.driver.observe()
        self.assertEqual(len(self.driver.final_helpers()), 1)

    def test_preserves_intended_interruption_result_and_joins_worker(self):
        self.driver.plans = {"queued": {"identity": "queued-operation"}}
        observed = {"completed": False, "status": 0, "outcome": "interrupted"}
        def execute(plan): raise OperationFailure(observed)
        self.driver.execute = execute
        self.driver.start("queued")
        operations, joined = self.driver.finish(time.monotonic() + 1)
        self.assertEqual(operations["queued"]["result"], observed)
        self.assertEqual(joined, 1)
        self.assertGreaterEqual(operations["queued"]["ended_s"], operations["queued"]["started_s"])

    def test_supported_failure_propagates_and_cleanup_restores_factory(self):
        self.driver.plans = {"text": {"identity": "text-operation"}}
        def execute(plan): raise OperationFailure({"completed": False})
        self.driver.execute = execute
        self.driver.start("text")
        with self.assertRaises(OperationFailure): self.driver.finish(time.monotonic() + 1)
        self.driver.cleanup()
        self.assertTrue(self.driver.factory_restored)
        self.assertEqual(self.driver.workers_joined, 1)

    def test_gate_cleanup_failure_still_restores_factory_and_joins_workers(self):
        with mock.patch.object(self.gate, "release", side_effect=ValueError("injected release failure")):
            with self.assertRaises(ValueError): self.driver.cleanup()
        self.assertTrue(self.driver.factory_restored)
        self.assertIs(http.client.HTTPConnection, self.driver._original_factory)
        self.driver.cleanup()

    def test_process_parser_accepts_standard_ps_status_modifiers(self):
        for state in ("S+", "R<", "SN", "Ssl+", "Zs", "?", "?s"):
            with self.subTest(state=state):
                self.assertEqual(parse_processes(f"90 1 {state} process-start\n")[90]["state"], state)

    def test_process_parser_rejects_duplicate_or_incomplete_identity(self):
        parsed = parse_processes("90 1 S proxy-start\n101 90 Z helper-start\n")
        self.assertEqual(parsed[101]["parent_pid"], 90)
        for text in ("90 1 S\n", "90 1 S one\n90 1 S two\n", "bad 1 S identity\n"):
            with self.assertRaises(ValueError): parse_processes(text)


    def test_supported_failure_retains_actual_outcome_after_cleanup(self):
        from soak_phase import OperationFailure
        self.driver.plans = {"survivor": {"identity": "failed-operation"}}
        result = {"status": 503, "completed": False}
        def execute(plan):
            raise OperationFailure(result)
        self.driver.execute = execute
        try:
            with self.assertRaises(OperationFailure):
                self.driver.start("survivor")
                self.driver.finish(self.driver.clock() + 45)
        finally:
            self.driver.cleanup()
        saved = self.driver.snapshot_operations()
        self.assertEqual(saved["survivor"]["result"], result)
        self.assertIn("started_s", saved["survivor"])
        self.assertIn("ended_s", saved["survivor"])
        saved["survivor"]["result"]["status"] = 200
        self.assertEqual(self.driver.operations["survivor"]["result"]["status"], 503)



if __name__ == "__main__": unittest.main()
