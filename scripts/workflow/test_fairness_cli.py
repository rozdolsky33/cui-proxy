import contextlib
import importlib
import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock


class FairnessCLITests(unittest.TestCase):
    def test_repetitions_alternate_arms_and_compare_only_text_records(self):
        cli = importlib.import_module("fairness_performance")
        text = dict(operation_id="text", outcome="supported_success", supported=True, completed=True,
                    first_byte_ms=1.0, duration_ms=2.0, prefix_ms=list(range(1, 33)),
                    relay_ms=[0.1] * 32, relay_flush_ms=[0.0] * 32)
        image = dict(operation_id="image", outcome="supported_success", supported=True, completed=True)
        result = dict(records=[text, image], text_records=[text], image_records=[image], performance_qualified=False)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "manifest.json").write_text(json.dumps(dict(font_name="Menlo-Regular")))
            with (mock.patch.object(cli.fixtures, "validate_coverage"),
                  mock.patch.object(cli.fixtures, "provenance", return_value={}),
                  mock.patch.object(cli, "run_fairness_phase", side_effect=lambda *a, **k: dict(result)) as run,
                  mock.patch.object(cli.perf, "compare_runs", wraps=cli.perf.compare_runs) as compare):
                code = cli.main(["--binary", "unused", "--seed-binary", "unused",
                                 "--fixtures", str(root), "--report", str(root / "report.json"),
                                 "--sizes", "262144", "--count", "1", "--repetitions", "2", "--repetition-offset", "1"])
            self.assertEqual(code, 0)
            self.assertEqual([c.kwargs["bypass"] for c in run.call_args_list], [False, True, True, False])
            self.assertEqual(compare.call_count, 2)
            for call in compare.call_args_list:
                self.assertEqual(call.args[0], [text])
                self.assertEqual(call.args[1], [text])
            report = json.loads((root / "report.json").read_text())
            self.assertEqual(len(report["measurements"]), 4)
            self.assertFalse(report["performance_qualified"])

    def test_existing_evidence_is_preserved_without_starting_a_phase(self):
        cli = importlib.import_module("fairness_performance")
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "report.json"
            output.write_text("previous evidence")
            with mock.patch.object(cli, "run_fairness_phase") as run, contextlib.redirect_stderr(io.StringIO()):
                code = cli.main(["--binary", "unused", "--seed-binary", "unused",
                                 "--fixtures", directory, "--report", str(output)])
            self.assertEqual(code, 1)
            run.assert_not_called()
            self.assertEqual(output.read_text(), "previous evidence")
