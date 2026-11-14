import copy
import hashlib
import json
import unittest
from performance_workload import Workload
from soak_provenance_acceptance import evaluate_provenance


class ProvenanceAcceptanceTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.workload = Workload("normal").metadata()

    def evidence(self):
        sha = lambda text: hashlib.sha256(text.encode()).hexdigest()
        revision = hashlib.sha1(b"runtime-source").hexdigest()
        settings = dict(fixed_seconds=7200, churn_seconds=7200, warmup_seconds=60,
                        warmup_count=100, drain_seconds=45, control_count=10)
        identity = dict(profile="soak", source=dict(commit=hashlib.sha1(b"harness-source").hexdigest(),
                        dirty=False, script_sha256=sha("scripts")), python_version="3.14.5",
                        binary_sha256=sha("binary"), seed_binary_sha256=sha("seed"),
                        fixture_manifest_sha256=sha("fixtures"), policy_sha256=sha("policy"),
                        build_manifest_sha256=sha("build"), binary_source_commit=revision,
                        workload=copy.deepcopy(self.workload), policy_variant="normal-plus-declared-negative-rule",
                        jobs_sha256=sha(json.dumps([dict(section="soak", settings=settings)], sort_keys=True)),
                        source_proof=dict(reproduced_binary=True, source_commit=revision, binary_sha256=sha("binary")),
                        host=dict(hardware_model="MacBookPro18,3", architecture="arm64", logical_cpus=8,
                                  memory_bytes=32*1024**3, macos_version="26.7.2", go_version="go1.26.11",
                                  on_ac=True, low_power=False, competing_work=0, optimized_build=True))
        return identity, copy.deepcopy(identity), settings

    def test_original_reference_identity_is_bound_to_settings(self):
        result = evaluate_provenance(*self.evidence())
        self.assertEqual(result["state"], "pass")
        self.assertFalse(result["performance_qualified"])

    def test_stale_binary_or_script_is_rejected(self):
        for key in ("binary_sha256", "seed_binary_sha256", "policy_sha256", "fixture_manifest_sha256"):
            identity, expected, settings = self.evidence()
            identity[key] = "0"*64
            with self.assertRaises(ValueError):
                evaluate_provenance(identity, expected, settings)

    def test_matching_but_invalid_reference_cannot_qualify(self):
        mutations = [lambda i: i["source"].update(dirty=True),
                     lambda i: i["host"].update(logical_cpus=16),
                     lambda i: i.update(jobs_sha256="0"*64),
                     lambda i: i["workload"].update(mapping_pairs=9999),
                     lambda i: i["source_proof"].update(source_commit="0"*40),
                     lambda i: i.update(policy_variant="normal"),
                     lambda i: i["source_proof"].update(reproduced_binary=False)]
        for mutate in mutations:
            identity, _, settings = self.evidence()
            mutate(identity)
            with self.assertRaises(ValueError):
                evaluate_provenance(identity, copy.deepcopy(identity), settings)

    def test_short_or_invalid_original_settings_are_rejected(self):
        for key, value in [("fixed_seconds", 150), ("churn_seconds", 150), ("warmup_seconds", 1),
                           ("warmup_count", 8), ("control_count", 1), ("drain_seconds", 46),
                           ("warmup_seconds", float("nan")), ("warmup_count", True)]:
            identity, expected, settings = self.evidence()
            settings[key] = value
            with self.assertRaises(ValueError):
                evaluate_provenance(identity, expected, settings)
