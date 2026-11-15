import json
import os
from pathlib import Path
import tempfile
import unittest

from cancellation_gate import GatedOCR


class GatedOCRTests(unittest.TestCase):
    def delegate(self, directory):
        path = Path(directory) / 'real-ocr'
        path.write_text('#!/bin/sh\nexit 0\n')
        path.chmod(0o700)
        return path

    def test_private_gate_arms_releases_and_restores_on_error(self):
        with tempfile.TemporaryDirectory() as directory:
            delegate = self.delegate(directory)
            gate = GatedOCR(Path(directory) / 'gate', delegate)
            with self.assertRaisesRegex(RuntimeError, 'injected'):
                with gate:
                    self.assertEqual(gate.directory.stat().st_mode & 0o777, 0o700)
                    self.assertEqual(gate.launcher.stat().st_mode & 0o777, 0o700)
                    compile(gate.launcher.read_text().split('\n', 1)[1], str(gate.launcher), 'exec')
                    self.assertEqual(gate.snapshot(), [])
                    generation = gate.arm()
                    self.assertEqual(generation.parent, gate.directory)
                    with self.assertRaises(ValueError):
                        gate.arm()
                    raise RuntimeError('injected')
            self.assertTrue((generation / 'release').is_file())
            self.assertTrue(delegate.is_file())

    def test_refuses_existing_or_symlink_directory(self):
        with tempfile.TemporaryDirectory() as directory:
            delegate = self.delegate(directory)
            existing = Path(directory) / 'existing'
            existing.mkdir()
            link = Path(directory) / 'link'
            link.symlink_to(existing, target_is_directory=True)
            for path in (existing, link):
                with self.subTest(path=path.name), self.assertRaises(FileExistsError):
                    with GatedOCR(path, delegate):
                        pass

    def test_snapshot_requires_current_regular_complete_marker(self):
        with tempfile.TemporaryDirectory() as directory:
            with GatedOCR(Path(directory) / 'gate', self.delegate(directory)) as gate:
                generation = gate.arm()
                marker = dict(pid=123, generation=generation.name, started_ns=5, input_sha256='a' * 64)
                path = generation / '123.json'
                path.write_text(json.dumps(marker))
                self.assertEqual(gate.snapshot(), [marker])
                marker['generation'] = 'b' * 32
                path.write_text(json.dumps(marker))
                with self.assertRaises(ValueError):
                    gate.snapshot()
                path.unlink()
                path.symlink_to(gate.directory / 'control.json')
                with self.assertRaises((ValueError, OSError)):
                    gate.snapshot()

    def test_snapshot_rejects_duplicate_fields_and_unbounded_markers(self):
        with tempfile.TemporaryDirectory() as directory:
            with GatedOCR(Path(directory) / 'gate', self.delegate(directory)) as gate:
                generation = gate.arm()
                path = generation / '123.json'
                path.write_text('{"pid":123,"pid":124}')
                with self.assertRaises(ValueError):
                    gate.snapshot()
                path.write_text(' ' * 4097)
                with self.assertRaises(ValueError):
                    gate.snapshot()


if __name__ == '__main__':
    unittest.main()
