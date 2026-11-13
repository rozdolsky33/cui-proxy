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

    def test_duration_capacity_forwards_settings_and_preserves_arms(self):
        cli = importlib.import_module('fairness_performance')
        text = dict(operation_id='text', outcome='supported_success', supported=True, completed=True, first_byte_ms=1.0, duration_ms=2.0, prefix_ms=list(range(1, 33)), relay_ms=[0.1] * 32, relay_flush_ms=[0.0] * 32)
        image = dict(operation_id='image', outcome='supported_success', supported=True, completed=True)
        result = dict(records=[text, image], text_records=[text], image_records=[image], performance_qualified=False)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'manifest.json').write_text(json.dumps(dict(font_name='Menlo-Regular')))
            with mock.patch.object(cli.fixtures, 'validate_coverage'), mock.patch.object(cli.fixtures, 'provenance', return_value={}), mock.patch.object(cli, 'run_fairness_phase', side_effect=lambda *a, **k: dict(result)) as run, mock.patch.object(cli.perf, 'compare_runs', wraps=cli.perf.compare_runs) as compare:
                code = cli.main(['--binary', 'unused', '--seed-binary', 'unused', '--fixtures', str(root), '--report', str(root / 'report.json'), '--sizes', '262144', '--duration', '120', '--repetitions', '2', '--repetition-offset', '1', '--clients', '8'])
            self.assertEqual(code, 0)
            self.assertEqual([c.kwargs['bypass'] for c in run.call_args_list], [False, True, True, False])
            self.assertEqual(compare.call_count, 2)
            for call in compare.call_args_list:
                self.assertEqual(call.args[0], [text])
                self.assertEqual(call.args[1], [text])
            report = json.loads((root / 'report.json').read_text())
            self.assertEqual(len(report['measurements']), 4)
            self.assertFalse(report['performance_qualified'])
        for invocation in run.call_args_list:
            self.assertEqual(invocation.kwargs['clients'], 8)
            self.assertEqual(invocation.kwargs['duration'], 120)
            self.assertIsNone(invocation.kwargs['text_count'])
        self.assertEqual(report['settings']['clients'], 8)
        self.assertEqual(report['settings']['duration'], 120)
        self.assertIsNone(report['settings']['count'])

    def test_capacity_cli_rejects_conflicting_or_invalid_settings(self):
        cli = importlib.import_module("fairness_performance")
        required = ["--binary", "unused", "--seed-binary", "unused",
                    "--fixtures", "unused", "--report", "unused"]
        cases = (["--count", "1", "--duration", "1"], ["--duration", "0"],
                 ["--duration", "-1"], ["--duration", "nan"],
                 ["--duration", "inf"], ["--clients", "3"])
        for extra in cases:
            with self.subTest(arguments=extra), contextlib.redirect_stderr(io.StringIO()):
                with self.assertRaises(SystemExit) as error:
                    cli.main(required + extra)
                self.assertEqual(error.exception.code, 2)

    def test_duration_comparison_retains_unequal_arm_counts(self):
        cli = importlib.import_module('fairness_performance')
        text = dict(operation_id='text', outcome='supported_success', supported=True, completed=True, first_byte_ms=1.0, duration_ms=2.0, prefix_ms=list(range(1, 33)), relay_ms=[0.1] * 32, relay_flush_ms=[0.0] * 32)
        image = dict(operation_id='image', outcome='supported_success', supported=True, completed=True)
        result = dict(records=[text, image], text_records=[text], image_records=[image], performance_qualified=False)
        extra_text = dict(text, operation_id='extra-text')
        other_result = dict(result, records=[text, extra_text, image], text_records=[text, extra_text])
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'manifest.json').write_text(json.dumps(dict(font_name='Menlo-Regular')))
            with mock.patch.object(cli.fixtures, 'validate_coverage'), mock.patch.object(cli.fixtures, 'provenance', return_value={}), mock.patch.object(cli, 'run_fairness_phase', side_effect=[result, other_result]) as run:
                code = cli.main(['--binary', 'unused', '--seed-binary', 'unused', '--fixtures', str(root), '--report', str(root / 'report.json'), '--sizes', '262144', '--duration', '120', '--repetitions', '1', '--repetition-offset', '0', '--clients', '8'])
            self.assertEqual(code, 0)
            report = json.loads((root / 'report.json').read_text())
        self.assertEqual([len(phase['text_records']) for phase in report['measurements']], [1, 2])
        self.assertEqual([len(phase['records']) for phase in report['measurements']], [2, 3])
        self.assertFalse(report['comparison'][0]['paired_samples_valid'])
        self.assertFalse(report['performance_qualified'])

    def test_omitted_mode_preserves_reference_defaults(self):
        cli = importlib.import_module('fairness_performance')
        text = dict(operation_id='text', outcome='supported_success', supported=True, completed=True, first_byte_ms=1.0, duration_ms=2.0, prefix_ms=list(range(1, 33)), relay_ms=[0.1] * 32, relay_flush_ms=[0.0] * 32)
        image = dict(operation_id='image', outcome='supported_success', supported=True, completed=True)
        result = dict(records=[text, image], text_records=[text], image_records=[image], performance_qualified=False)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'manifest.json').write_text(json.dumps(dict(font_name='Menlo-Regular')))
            with mock.patch.object(cli.fixtures, 'validate_coverage'), mock.patch.object(cli.fixtures, 'provenance', return_value={}), mock.patch.object(cli, 'run_fairness_phase', side_effect=RuntimeError('controlled phase stop')) as run:
                code = cli.main(['--binary', 'unused', '--seed-binary', 'unused', '--fixtures', str(root), '--report', str(root / 'report.json'), '--sizes', '262144', '--repetitions', '2', '--repetition-offset', '1'])
            self.assertEqual(code, 1)
            report = json.loads((root / 'report.json').read_text())
        self.assertEqual(run.call_count, 1)
        self.assertEqual(run.call_args.kwargs['clients'], 4)
        self.assertEqual(run.call_args.kwargs['text_count'], 1000)
        self.assertIsNone(run.call_args.kwargs['duration'])
        self.assertEqual(report['settings']['count'], 1000)
        self.assertFalse(report['performance_qualified'])
