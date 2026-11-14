import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from soak_build_proof import verify_snapshot_build


class BuildProofTests(unittest.TestCase):
    def test_reproduction_is_cached_but_clean_source_is_rechecked(self):
        with tempfile.TemporaryDirectory() as directory:
            binary = Path(directory) / "measured"
            binary.write_bytes(b"binary")
            status = {"dirty": False}
            def git(source, *args):
                if args[0] == "rev-parse":
                    return "a" * 40
                return "changed" if status["dirty"] else ""
            def build(command, **kwargs):
                Path(command[3]).write_bytes(binary.read_bytes())
            with patch("soak_build_proof._git", side_effect=git), patch("soak_build_proof.subprocess.run", side_effect=build) as run:
                args = (binary, directory, "a" * 40, ["go", "build", "-o", str(binary), "./cmd/cui-proxy"], {})
                self.assertTrue(verify_snapshot_build(*args)["reproduced_binary"])
                self.assertTrue(verify_snapshot_build(*args)["reproduced_binary"])
                self.assertEqual(run.call_count, 1)
                status["dirty"] = True
                with self.assertRaises(ValueError):
                    verify_snapshot_build(*args)

    def test_wrong_revision_or_ignored_source_prevents_build(self):
        for revision, ignored in (("b" * 40, ""), ("a" * 40, "ignored.go")):
            with tempfile.TemporaryDirectory() as directory:
                binary = Path(directory) / "measured"
                binary.write_bytes(b"binary")
                def git(source, *args):
                    return revision if args[0] == "rev-parse" else ignored if args[0] == "ls-files" else ""
                with patch("soak_build_proof._git", side_effect=git), patch("soak_build_proof.subprocess.run") as run:
                    with self.assertRaises(ValueError):
                        verify_snapshot_build(binary, directory, "a" * 40,
                                              ["go", "build", "-o", str(binary), "./cmd/cui-proxy"], {})
                    run.assert_not_called()

    def test_reproduced_byte_difference_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            binary = Path(directory) / "measured"
            binary.write_bytes(b"binary")
            def git(source, *args):
                return "a" * 40 if args[0] == "rev-parse" else ""
            def build(command, **kwargs):
                Path(command[3]).write_bytes(b"different")
            with patch("soak_build_proof._git", side_effect=git), patch("soak_build_proof.subprocess.run", side_effect=build):
                with self.assertRaises(ValueError):
                    verify_snapshot_build(binary, directory, "a" * 40,
                                          ["go", "build", "-o", str(binary), "./cmd/cui-proxy"], {})
