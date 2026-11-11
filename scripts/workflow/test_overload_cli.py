import contextlib
import importlib
import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock


class OverloadCLITests(unittest.TestCase):
    def test_warmup_failure_identifies_the_attempted_matrix_arm(self):
        cli = importlib.import_module("overload_performance")
        failure = cli.perf.PhaseFailure("warmup", [])
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "report.json"
            with mock.patch.object(cli, "run_overload_phase", side_effect=failure), contextlib.redirect_stderr(io.StringIO()):
                code = cli.main(["--binary", "unused", "--seed-binary", "unused",
                                 "--output", str(output), "--sizes", "262144",
                                 "--rates", "4", "--duration", "1"])
            self.assertEqual(code, 1)
            phase = json.loads(output.read_text())["measurements"][0]
            self.assertEqual(phase["phase"], "warmup")
            self.assertEqual(phase["mode"], "bypass")
            self.assertEqual(phase["size_bytes"], 262144)
            self.assertEqual(phase["offered_rate"], 4)
            self.assertEqual(phase["requested_duration_s"], 1)


    def test_collection_failure_retains_counts_and_returns_failure(self):
        cli = importlib.import_module("overload_performance")
        result = dict(scheduled=4, sent=2, completed=2, rejected=0, client_unsent=2,
                      records=[], unsent_records=[], collection_failed=True,
                      collection_error_kind="RuntimeError", performance_qualified=False)
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "report.json"
            with mock.patch.object(cli, "run_overload_phase", return_value=result) as run, contextlib.redirect_stderr(io.StringIO()):
                code = cli.main(["--binary", "unused", "--seed-binary", "unused",
                                 "--output", str(output), "--sizes", "262144",
                                 "--rates", "4", "--duration", "1"])
            self.assertEqual(code, 1)
            self.assertEqual(run.call_count, 1)
            report = json.loads(output.read_text())
            self.assertFalse(report["completed"])
            self.assertEqual(report["measurements"][0]["client_unsent"], 2)
            self.assertEqual(report["measurements"][0]["scheduled"], 4)

    def test_existing_report_is_preserved_before_any_backend_work(self):
        cli = importlib.import_module("overload_performance")
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "report.json"
            output.write_text("previous evidence")
            with mock.patch.object(cli, "run_overload_phase") as run, contextlib.redirect_stderr(io.StringIO()):
                code = cli.main(["--binary", "unused", "--seed-binary", "unused", "--output", str(output)])
            self.assertEqual(code, 1)
            run.assert_not_called()
            self.assertEqual(output.read_text(), "previous evidence")
