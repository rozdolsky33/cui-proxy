import hashlib
import unittest
from unittest.mock import Mock, patch

from soak_journal import SoakJournal
import soak_mapping_evidence as evidence


class MappingEvidenceTests(unittest.TestCase):
    def phase(self):
        journal = SoakJournal("churn", duration=1)
        plan = journal.claim(0, 0.6); journal.complete(plan["identity"])
        return {"phase": "churn", "duration_s": 1, "completed": True, "schedule": journal.snapshot(), "results": [{"identity": plan["identity"], "request": {"outcome": "boundary_pending", "completed": True, "semantic_verified": True, "boundary_sha256": hashlib.sha256(b"masked-0 masked-1").hexdigest()}}]}

    def workload(self):
        workload = Mock(); workload.churn_value.side_effect = lambda index: f"new-{index}"; return workload

    def finalize(self, report=None, mappings=None):
        with patch("soak_mapping_evidence.build_text_case", return_value={"body": b"new-0 new-1", "occurrences": ["new-0", "new-1"]}):
            return evidence.finalize_mapping_evidence(report or self.phase(), self.workload(), {"seed": "masked-seed"}, mappings or {"seed": "masked-seed", "new-0": "masked-0", "new-1": "masked-1"})

    def test_persisted_pairs_and_independent_body_digest_resolve_pending_rows(self):
        report = self.phase(); result = self.finalize(report)
        self.assertEqual(result["mapping_evidence"]["unmet_requirements"], [])
        self.assertEqual(result["mapping_evidence"]["persisted_churn_pairs"], 2)
        self.assertEqual(result["results"][0]["request"]["outcome"], "supported_success")
        self.assertEqual(report["results"][0]["request"]["outcome"], "boundary_pending")

    def test_missing_mutated_and_unexpected_mappings_cannot_pass(self):
        for mappings, reason in [({"seed": "changed", "new-0": "masked-0", "new-1": "masked-1"}, "seed_mapping_changed"), ({"seed": "masked-seed", "new-0": "masked-0"}, "missing_churn_mapping"), ({"seed": "masked-seed", "new-0": "masked-0", "new-1": "masked-1", "extra": "masked-extra"}, "unexpected_new_mapping")]:
            self.assertIn(reason, self.finalize(mappings=mappings)["mapping_evidence"]["unmet_requirements"])

    def test_wrong_boundary_and_duplicate_results_are_rejected(self):
        report = self.phase(); report["results"][0]["request"]["boundary_sha256"] = "0" * 64
        self.assertIn("unverified_text_boundary", self.finalize(report)["mapping_evidence"]["unmet_requirements"])
        report = self.phase(); report["results"].append(report["results"][0])
        with self.assertRaises(ValueError): self.finalize(report)

    def test_unassigned_due_pairs_remain_explicit(self):
        report = self.phase(); report["duration_s"] = 2
        result = self.finalize(report)
        self.assertEqual(result["mapping_evidence"]["unassigned_due_pairs"], 2)
        self.assertIn("churn_backlog", result["mapping_evidence"]["unmet_requirements"])

    def test_separate_vault_observations_preserve_prior_snapshot(self):
        import json, tempfile
        from pathlib import Path
        from types import SimpleNamespace
        from performance_churn import inspect_vault
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); original = root / "observed.json"; original.write_text("retain")
            payload = {"population": 1, "mappings": {"original": "masked"}}
            for name in ["before.json", "after.json"]:
                target = root / name
                def inspect(command, **kwargs):
                    self.assertIn(name, [str(value) for value in command])
                    target.write_text(json.dumps(payload))
                    target.chmod(0o600)
                    return SimpleNamespace(returncode=0)
                with patch("subprocess.run", side_effect=inspect):
                    self.assertEqual(inspect_vault(root, "seed-helper", observation_name=name), payload)
                self.assertTrue(target.exists())
            self.assertEqual(original.read_text(), "retain")
            with self.assertRaises(ValueError): inspect_vault(root, "seed-helper", observation_name="before.json")
            for name in ["../outside.json", "/tmp/outside.json", "", "bad.txt"]:
                with patch("subprocess.run") as command:
                    with self.assertRaises(ValueError): inspect_vault(root, "seed-helper", observation_name=name)
                command.assert_not_called()

    def test_supported_text_is_rechecked_without_losing_verified_boundary(self):
        report = self.phase(); report["results"][0]["request"].update(outcome="supported_success", boundary_verified=True)
        result = self.finalize(report)
        self.assertEqual(result["mapping_evidence"]["unmet_requirements"], [])
        self.assertTrue(result["results"][0]["request"]["boundary_verified"])
        report["results"][0]["request"]["boundary_sha256"] = "0" * 64
        self.assertIn("unverified_text_boundary", self.finalize(report)["mapping_evidence"]["unmet_requirements"])
