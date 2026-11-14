import json
from pathlib import Path
import tempfile
import unittest

from soak_identity_record import retain_initial_identity


class IdentityRecordTests(unittest.TestCase):
    def test_initial_identity_is_retained_before_measurement_and_never_replaced(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory); path = base / "expected.json"; root = base / "campaign"
            identities = iter([dict(revision="initial"), dict(revision="changed")])
            probe = retain_initial_identity(lambda: next(identities), path, root)
            self.assertFalse(path.exists())
            self.assertEqual(probe(), dict(revision="initial"))
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            self.assertEqual(probe(), dict(revision="changed"))
            self.assertEqual(json.loads(path.read_text()), dict(revision="initial"))

    def test_existing_identity_and_sealed_root_paths_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory); path = base / "expected.json"; root = base / "campaign"
            path.write_text("retained")
            with self.assertRaises(FileExistsError): retain_initial_identity(lambda: {}, path, root)()
            self.assertEqual(path.read_text(), "retained")
            with self.assertRaises(ValueError): retain_initial_identity(lambda: {}, root / "expected.json", root)

    def test_probe_failure_does_not_create_a_misleading_identity(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory); path = base / "expected.json"
            def fail(): raise ValueError("probe_failed")
            with self.assertRaises(ValueError): retain_initial_identity(fail, path, base / "campaign")()
            self.assertFalse(path.exists())
