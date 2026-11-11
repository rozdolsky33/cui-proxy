import unittest
import tempfile
from pathlib import Path
from types import SimpleNamespace
import os
import signal
import subprocess
import sys
import time

from harness import CheckFailure, Environment, check_operation, mutation_checks, operation


class OracleTests(unittest.TestCase):
    def good(self):
        return dict(status=200, response={"a": "original-a", "b": "original-b"},
                    expected={"a": "original-a", "b": "original-b"},
                    captures=[b'{"a":"masked-a","b":"masked-b"}'],
                    originals=["original-a", "original-b"], expected_status=200)

    def test_success_checks_semantics(self):
        check_operation(**self.good())

    def test_leaked_original_rejected(self):
        case = self.good()
        case["captures"] = [b'{"a":"original-a"}']
        with self.assertRaises(CheckFailure):
            check_operation(**case)

    def test_swapped_restoration_rejected(self):
        case = self.good()
        case["response"] = {"a": "original-b", "b": "original-a"}
        with self.assertRaises(CheckFailure):
            check_operation(**case)

    def test_forwarded_negative_rejected(self):
        with self.assertRaises(CheckFailure):
            check_operation(422, None, None, [b"{}"], [], 422)

    def test_wrong_status_rejected(self):
        case = self.good()
        case["status"] = 422
        with self.assertRaises(CheckFailure):
            check_operation(**case)

    def test_missing_upstream_evidence_rejected(self):
        case = self.good()
        case["captures"] = []
        with self.assertRaises(CheckFailure):
            check_operation(**case)

    def test_bypass_still_checks_semantics(self):
        case = self.good()
        case["captures"] = [b"original-a original-b"]
        case["bypass"] = True
        check_operation(**case)
        case["response"] = {}
        with self.assertRaises(CheckFailure):
            check_operation(**case)

    def test_mutations_detected(self):
        self.assertEqual(mutation_checks(), 3)

    def test_embedded_json_semantics_ignore_serialization(self):
        case = self.good()
        case.update(response={"text": '{"a":"original-a"}'},
                    expected={"text": '{ "a": "original-a" }'})
        check_operation(**case)
        case["response"] = {"text": '{"a":"original-b"}'}
        with self.assertRaises(CheckFailure):
            check_operation(**case)

    def test_unicode_encoded_original_is_still_a_leak(self):
        case = self.good()
        case["captures"] = [b'{"a":"\\u006friginal-a"}']
        with self.assertRaises(CheckFailure):
            check_operation(**case)

    def test_json_value_types_are_preserved(self):
        case = self.good()
        case.update(response={"flag": True}, expected={"flag": 1})
        with self.assertRaises(CheckFailure):
            check_operation(**case)

    def test_error_and_cancellation_paths_check_upstream_leaks(self):
        for case, status in [("W12-cancel", 200), ("W12-non-sse", 502)]:
            with self.subTest(case=case), tempfile.TemporaryDirectory() as td:
                path = Path(td)
                (path / ("c1-" + case + "-1.cancelled")).touch()
                env = SimpleNamespace(captures=path,
                                      send=lambda *args, **kwargs: (status, b"{}", 1),
                                      capture=lambda ident: [b'{"value":"privatevalue000011"}'])
                result = operation(env, case, 1, 1)
                self.assertFalse(result["passed"])
                self.assertEqual(result["assertion"], "original_leaked")

    def test_cleanup_stops_descendants_of_exited_parent(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            child_code = "import pathlib,signal,time; p=pathlib.Path(" + repr(str(root / "stopped")) + "); signal.signal(signal.SIGTERM,lambda *args:(p.touch(),exit(0))); pathlib.Path(" + repr(str(root / "ready")) + ").touch(); time.sleep(60)"
            parent_code = "import subprocess,sys; subprocess.Popen([sys.executable,'-c'," + repr(child_code) + "],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)"
            parent = subprocess.Popen([sys.executable, "-c", parent_code], start_new_session=True)
            try:
                parent.wait(timeout=5)
                end = time.monotonic() + 5
                while not (root / "ready").exists() and time.monotonic() < end:
                    time.sleep(.02)
                self.assertTrue((root / "ready").exists())
                env = Environment(Path("unused"))
                env.children = [parent]
                env.temp = SimpleNamespace(cleanup=lambda: None)
                env.__exit__()
                end = time.monotonic() + 2
                while not (root / "stopped").exists() and time.monotonic() < end:
                    time.sleep(.02)
                self.assertTrue((root / "stopped").exists())
            finally:
                try:
                    os.killpg(parent.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass


if __name__ == "__main__":
    unittest.main()
