import contextlib
import json
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

import soak_identity


class SoakIdentityTests(unittest.TestCase):
    def setup_case(self, directory, stack):
        commit, digest = "a" * 40, "b" * 64
        manifest = Path(directory) / "build.json"
        manifest.write_text(json.dumps({"source_commit": commit, "source_clean": True,
                                       "binary_sha256": digest,
                                       "go_version": "go version go1.26.11 darwin/arm64", "command": ["go", "build"]}))
        identity = stack.enter_context(patch.object(soak_identity, "campaign_identity", create=True))
        identity.return_value = {"binary_sha256": digest, "source": {"dirty": False, "commit": "c" * 40}}
        host = stack.enter_context(patch.object(soak_identity, "reference_environment", create=True))
        host.return_value = {"go_version": "go1.26.11", "hardware_model": "reference", "load_average": [1, 2, 3]}
        stack.enter_context(patch.object(soak_identity, "check_reference_environment", create=True))
        build = stack.enter_context(patch.object(soak_identity, "read_build_info", create=True))
        build.return_value = {"vcs.revision": commit, "vcs.modified": "false"}
        workload = SimpleNamespace(rules=[], dictionary={}, pairs=[], metadata=lambda: {"tier": "normal"})
        args = (Path("binary"), Path("seed"), Path("fixtures"), manifest, commit, workload, {})
        return args, identity, host, build

    def test_binds_manifest_binary_source_and_stable_host_facts(self):
        with tempfile.TemporaryDirectory() as directory, contextlib.ExitStack() as stack:
            args, identity, host, build = self.setup_case(directory, stack)
            probe = soak_identity.make_identity_probe(*args)
            first = probe()
            host.return_value["load_average"] = [2, 3, 4]
            self.assertEqual(probe(), first)
            self.assertEqual(first["binary_source_commit"], "a" * 40)
            self.assertNotIn("load_average", first["host"])

    def test_embedded_wrong_revision_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory, contextlib.ExitStack() as stack:
            args, identity, host, build = self.setup_case(directory, stack)
            build.return_value["vcs.revision"] = "d" * 40
            with self.assertRaises(ValueError):
                soak_identity.make_identity_probe(*args)()

    def test_changed_binary_digest_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory, contextlib.ExitStack() as stack:
            args, identity, host, build = self.setup_case(directory, stack)
            identity.return_value["binary_sha256"] = "d" * 64
            with self.assertRaises(ValueError):
                soak_identity.make_identity_probe(*args)()

    def test_dirty_harness_cannot_establish_identity(self):
        with tempfile.TemporaryDirectory() as directory, contextlib.ExitStack() as stack:
            args, identity, host, build = self.setup_case(directory, stack)
            identity.return_value["source"]["dirty"] = True
            with self.assertRaises(ValueError):
                soak_identity.make_identity_probe(*args)()

    def test_missing_embedded_revision_requires_reproducible_source_proof(self):
        with tempfile.TemporaryDirectory() as directory, contextlib.ExitStack() as stack:
            args, identity, host, build = self.setup_case(directory, stack)
            build.return_value = {}
            verify = stack.enter_context(patch.object(soak_identity, "verify_snapshot_build", create=True))
            verify.return_value = {"reproduced_binary": True}
            probe = soak_identity.make_identity_probe(*args, binary_source=Path("snapshot"))
            self.assertTrue(probe()["source_proof"]["reproduced_binary"])
            verify.assert_called_once()

    def test_missing_embedded_revision_without_source_stays_rejected(self):
        with tempfile.TemporaryDirectory() as directory, contextlib.ExitStack() as stack:
            args, identity, host, build = self.setup_case(directory, stack)
            build.return_value = {}
            with self.assertRaises(ValueError):
                soak_identity.make_identity_probe(*args)()
