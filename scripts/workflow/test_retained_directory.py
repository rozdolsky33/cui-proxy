import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch
import performance


class RetainedDirectoryTests(unittest.TestCase):
    def test_setup_failure_retains_only_explicit_evidence_directory(self):
        with tempfile.TemporaryDirectory() as parent:
            destination=Path(parent)/"private-run"
            env=performance.Environment("unused", "unused", object(),262144,retained_directory=destination)
            def fail_spawn(*args,**kwargs):
                (env.directory/"failure.log").write_text("captured failure")
                raise RuntimeError("setup failure")
            with patch("performance.harness.port",return_value=50001),patch.object(env,"spawn",side_effect=fail_spawn):
                with self.assertRaises(RuntimeError):env.__enter__()
            self.assertTrue((destination/"failure.log").exists())
            self.assertEqual(destination.stat().st_mode & 0o777,0o700)

    def test_default_setup_failure_still_removes_temporary_files(self):
        env=performance.Environment("unused","unused",object(),262144)
        def fail_spawn(*args,**kwargs):raise RuntimeError("setup failure")
        with patch("performance.harness.port",return_value=50001),patch.object(env,"spawn",side_effect=fail_spawn):
            with self.assertRaises(RuntimeError):env.__enter__()
        self.assertFalse(env.directory.exists())

    def test_existing_destination_is_not_reused_or_deleted(self):
        with tempfile.TemporaryDirectory() as parent:
            destination=Path(parent)/"private-run";destination.mkdir();marker=destination/"keep";marker.write_text("keep")
            env=performance.Environment("unused","unused",object(),262144,retained_directory=destination)
            with patch("performance.harness.port",return_value=50001),patch.object(env,"spawn",side_effect=AssertionError("unexpected spawn")):
                with self.assertRaises(FileExistsError):env.__enter__()
            self.assertEqual(marker.read_text(),"keep")
