import hashlib
import os
from pathlib import Path
import tempfile
import unittest
from soak_corpus_files import verify_image_file


class CorpusFileTests(unittest.TestCase):
    def test_verified_file_returns_only_identity_hash_and_size(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder);data=b"fixture bytes";(root/"image.bin").write_bytes(data)
            row=dict(id="image-0",file="image.bin",bytes=len(data),sha256=hashlib.sha256(data).hexdigest())
            self.assertEqual(verify_image_file(root,row),{k:row[k] for k in ("id","bytes","sha256")})

    def test_changed_size_hash_or_unsafe_path_is_rejected(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder);(root/"image.bin").write_bytes(b"fixture bytes")
            row=dict(id="image-0",file="image.bin",bytes=13,sha256=hashlib.sha256(b"fixture bytes").hexdigest())
            for change in [dict(bytes=12),dict(bytes=True),dict(sha256="f"*64),dict(file="../image.bin"),dict(file="/image.bin")]:
                with self.subTest(change=list(change)),self.assertRaises(ValueError):verify_image_file(root,dict(row,**change))

    def test_symlinks_directories_and_fifo_are_rejected(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder);(root/"real").mkdir();(root/"real"/"image.bin").write_bytes(b"x")
            (root/"link.bin").symlink_to(root/"real"/"image.bin")
            (root/"linked").symlink_to(root/"real",target_is_directory=True)
            os.mkfifo(root/"pipe")
            for filename in ["link.bin","linked/image.bin","real","pipe","missing"]:
                row=dict(id="image-0",file=filename,bytes=1,sha256=hashlib.sha256(b"x").hexdigest())
                with self.subTest(kind=filename),self.assertRaises(ValueError):verify_image_file(root,row)
