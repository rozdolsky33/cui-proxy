import contextlib
import io
import json
from pathlib import Path
import tempfile
import unittest

from soak_acceptance_cli import main


class AcceptanceCLITests(unittest.TestCase):
    def run_cli(self, root, identity, output):
        with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
            return main(["--root", str(root), "--expected-identity", str(identity), "--output", str(output)])

    def test_incomplete_root_retains_unqualified_diagnostic(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory); root = base / "campaign"; root.mkdir()
            identity = base / "expected.json"; identity.write_text("{}")
            output = base / "acceptance.json"
            self.assertEqual(self.run_cli(root, identity, output), 1)
            result = json.loads(output.read_text())
            self.assertFalse(result["soak_qualified"])
            self.assertFalse(result["performance_qualified"])
            self.assertEqual(output.stat().st_mode & 0o777, 0o600)

    def test_existing_acceptance_evidence_is_never_overwritten(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory); root = base / "campaign"; root.mkdir()
            identity = base / "expected.json"; identity.write_text("{}")
            output = base / "acceptance.json"; output.write_text("retained")
            self.assertEqual(self.run_cli(root, identity, output), 1)
            self.assertEqual(output.read_text(), "retained")

    def test_output_cannot_change_the_sealed_campaign(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory); root = base / "campaign"; root.mkdir()
            identity = base / "expected.json"; identity.write_text("{}")
            output = root / "acceptance.json"
            self.assertEqual(self.run_cli(root, identity, output), 1)
            self.assertFalse(output.exists())

    def test_expected_identity_must_be_an_independent_regular_file(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory); root = base / "campaign"; root.mkdir()
            inside = root / "identity.json"; inside.write_text("{}")
            output = base / "acceptance.json"
            self.assertEqual(self.run_cli(root, inside, output), 1)
            self.assertFalse(json.loads(output.read_text())["soak_qualified"])
            target = base / "target.json"; target.write_text("{}")
            link = base / "link.json"; link.symlink_to(target)
            output2 = base / "acceptance2.json"
            self.assertEqual(self.run_cli(root, link, output2), 1)
            self.assertFalse(json.loads(output2.read_text())["soak_qualified"])
