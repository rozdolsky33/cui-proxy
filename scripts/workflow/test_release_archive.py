import hashlib
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

import release_archive as archive


class ReleaseArchiveTests(unittest.TestCase):
    def source(self, directory):
        source = directory / "source"
        (source / "examples").mkdir(parents=True)
        for name in ("LICENSE", "README.md", "examples/config.yaml", "examples/hosts.txt"):
            (source / name).write_text("example member " + name + "\n")
        binary = directory / "retained-binary"
        binary.write_bytes(b"retained exact executable bytes")
        binary.chmod(0o755)
        return source, binary

    def test_exact_byte_archive_is_explicit_and_extracts_same_bytes(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            source, binary = self.source(root)
            expected = hashlib.sha256(binary.read_bytes()).hexdigest()
            asset = root / "verification.tar.gz"
            result = archive.create_verification_archive(binary, source, asset, expected)
            self.assertEqual(result["packaging_method"], "exact_byte_verification_archive")
            self.assertFalse(result["stock_builder_executed_by_probe"])
            checked = archive.verify_archive(asset, result["archive_sha256"], expected, source_root=source)
            self.assertTrue(checked["stock_member_layout_verified"])
            extracted = archive.extract_archive(asset, root / "extracted", result["archive_sha256"], expected,
                                                source_root=source)
            self.assertEqual(extracted.read_bytes(), binary.read_bytes())
            self.assertEqual(hashlib.sha256(asset.read_bytes()).hexdigest(), result["archive_sha256"])
            self.assertEqual(hashlib.sha256(binary.read_bytes()).hexdigest(), expected)

    def malicious(self, directory, members):
        path = directory / "malicious.tar.gz"
        with tarfile.open(path, "w:gz") as output:
            for name, kind in members:
                info = tarfile.TarInfo(name)
                if kind == "link":
                    info.type = tarfile.SYMTYPE
                    info.linkname = "outside"
                    output.addfile(info)
                elif kind == "device":
                    info.type = tarfile.CHRTYPE
                    output.addfile(info)
                else:
                    data = b"member"
                    info.size = len(data)
                    output.addfile(info, io.BytesIO(data))
        return path

    def test_traversal_absolute_links_devices_duplicates_and_unknown_members_fail_closed(self):
        variants = [[("../escape", "file")], [("/absolute", "file")], [("cui-proxy", "link")],
                    [("cui-proxy", "device")], [("cui-proxy", "file"), ("./cui-proxy", "file")],
                    [("unexpected", "file")]]
        for members in variants:
            with self.subTest(members=members), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary).resolve()
                path = self.malicious(root, members)
                with self.assertRaises(ValueError):
                    archive.verify_archive(path, hashlib.sha256(path.read_bytes()).hexdigest(), "0" * 64)

    def test_source_member_hash_binary_hash_archive_hash_and_existing_destination_are_mandatory(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            source, binary = self.source(root)
            expected = hashlib.sha256(binary.read_bytes()).hexdigest()
            asset = root / "verification.tar.gz"
            result = archive.create_verification_archive(binary, source, asset, expected)
            for asset_hash, binary_hash in (("0" * 64, expected), (result["archive_sha256"], "0" * 64)):
                with self.assertRaises(ValueError):
                    archive.verify_archive(asset, asset_hash, binary_hash, source_root=source)
            (source / "README.md").write_text("changed source member\n")
            with self.assertRaises(ValueError):
                archive.verify_archive(asset, result["archive_sha256"], expected, source_root=source)
            destination = root / "existing"
            destination.mkdir()
            (destination / "keep").write_text("keep")
            with self.assertRaises((ValueError, FileExistsError)):
                archive.extract_archive(asset, destination, result["archive_sha256"], expected)
            self.assertEqual((destination / "keep").read_text(), "keep")
            with patch.object(archive, "MAX_ARCHIVE_BYTES", 1):
                with self.assertRaises(ValueError):
                    archive.verify_archive(asset, result["archive_sha256"], expected)
            with patch.object(archive, "MAX_TOTAL_BYTES", 1):
                with self.assertRaises(ValueError):
                    archive.verify_archive(asset, result["archive_sha256"], expected)

    def test_baseline_provenance_binds_local_asset_checksums_and_binary(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            source, binary = self.source(root)
            binary_hash = hashlib.sha256(binary.read_bytes()).hexdigest()
            asset = root / "prior-release.tar.gz"
            result = archive.create_verification_archive(binary, source, asset, binary_hash)
            checksums = root / "checksums.txt"
            checksums.write_text(result["archive_sha256"] + "  " + asset.name + "\n")
            record = {"schema_version": 1, "kind": "operator_supplied_release_asset", "version": "0.7.3",
                      "release_tag": "v0.7.3", "source_commit": None, "binary_sha256": binary_hash,
                      "asset": {"path": asset.name, "sha256": result["archive_sha256"], "bytes": asset.stat().st_size},
                      "checksums": {"path": checksums.name, "sha256": hashlib.sha256(checksums.read_bytes()).hexdigest(),
                                    "bytes": checksums.stat().st_size}}
            path = root / "provenance.json"
            path.write_text(json.dumps(record))
            verified = archive.verify_baseline_provenance(binary, path)
            self.assertEqual(verified["binary_sha256"], binary_hash)
            self.assertEqual(verified["version"], "0.7.3")
            checksums.write_text("0" * 64 + "  " + asset.name + "\n")
            with self.assertRaises(ValueError):
                archive.verify_baseline_provenance(binary, path)


if __name__ == "__main__":
    unittest.main()
