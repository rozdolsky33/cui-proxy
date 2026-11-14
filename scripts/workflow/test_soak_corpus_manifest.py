import os
import tempfile
from pathlib import Path
import unittest
from soak_corpus_attestation import load_manifest


class CorpusManifestTests(unittest.TestCase):
    def test_loads_json_object(self):
        with tempfile.TemporaryDirectory() as folder:
            (Path(folder)/"manifest.json").write_bytes(b'{"version":2,"count":100}')
            self.assertEqual(load_manifest(folder),{"version":2,"count":100})

    def test_rejects_duplicates_nonfinite_invalid_encoding_and_nonobjects(self):
        cases=[b'{"version":1,"version":2}',b'{"nested":{"x":1,"x":2}}',
               b'{"x":NaN}',b'{"x":Infinity}',b'{"x":1e999}',b'[]',b'null',b'\xff',b'{']
        with tempfile.TemporaryDirectory() as folder:
            for data in cases:
                (Path(folder)/"manifest.json").write_bytes(data)
                with self.assertRaises(ValueError):load_manifest(folder)

    def test_rejects_symlink_directory_fifo_and_oversize(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder);path=root/"manifest.json"
            (root/"real.json").write_bytes(b'{}');path.symlink_to(root/"real.json")
            with self.assertRaises(ValueError):load_manifest(root)
            path.unlink();path.mkdir()
            with self.assertRaises(ValueError):load_manifest(root)
            path.rmdir();os.mkfifo(path)
            with self.assertRaises(ValueError):load_manifest(root)
            path.unlink()
            with path.open("wb") as stream:stream.truncate(4*1024*1024+1)
            with self.assertRaises(ValueError):load_manifest(root)
