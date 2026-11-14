import copy
import hashlib
import unittest

from cancellation_qualification import evaluate_cancellation
from test_cancellation_evidence import evidence as trial_evidence
import test_soak_provenance_acceptance as provenance_tests
import test_soak_warmup_acceptance as warmup_tests


def report():
    provenance_tests.ProvenanceAcceptanceTests.setUpClass()
    identity, expected, settings = provenance_tests.ProvenanceAcceptanceTests().evidence()
    trials = []
    terminal_helpers = []
    for index in range(3):
        trial = trial_evidence()
        offset = 100 * (index + 1)
        generation = format(index + 1, "032x")
        def change(value):
            if isinstance(value, dict):
                for key, item in value.items():
                    if key.endswith("_s") and type(item) in (int, float):
                        value[key] = item + offset
                    elif key == "generation":
                        value[key] = generation
                    elif key == "pid":
                        value[key] = item + 1000 * index
                    else:
                        change(item)
            elif isinstance(value, list):
                for item in value:
                    change(item)
        # Deep-copy via serialization so shared test helper dictionaries are
        # transformed once per observation, just like read-back JSON evidence.
        import json
        trial = json.loads(json.dumps(trial))
        change(trial)
        for role, entry in trial["operations"].items():
            operation = f"trial-{index}-{role}"
            entry["request"]["identity"] = operation
            entry["result"]["operation_id"] = operation
            for number, fixture in enumerate(entry["result"].get("fixture_evidence", [])):
                fixture["id"] = f"trial-{index}-{role}-image-{number}"
                fixture["sha256"] = hashlib.sha256(fixture["id"].encode()).hexdigest()
        for snapshots in trial["audit"].values():
            for rows in snapshots.values():
                for row in rows:
                    row["request_id"] = f"trial-{index}-" + row["request_id"]
        for helper in trial["final_helpers"]:
            terminal_helpers.append({"helper": copy.deepcopy(helper), "state": "absent", "observed_s": 320.0})
        trials.append(trial)
    parent = trials[0]["final_helpers"][0]
    result = {
        "schema_version": 1,
        "completed": True,
        "mode": "proxy",
        "collector_completed": False,
        "performance_qualified": False,
        "identity": identity,
        "settings": settings,
        "warmup": warmup_tests.WarmupAcceptanceTests().report(),
        "warmup_ended_s": 100.0,
        "trials": trials,
        "finalization": {
            "started_s": 319.0, "ended_s": 321.0,
            "privacy": {"privacy_verified": True, "files_checked": 3, "protected_values_checked": 40},
            "cleanup": {
                "logs_closed": True, "gate_released": True, "factory_restored": True,
                "threads_joined": 15,
                "owned_children": [
                    {"role": "proxy", "pid": parent["parent_pid"], "start": parent["parent_start"], "state": "absent", "observed_s": 320.0},
                    {"role": "backend", "pid": 91, "start": "backend-start", "state": "absent", "observed_s": 320.0},
                ],
                "helpers_terminal": terminal_helpers,
            },
        },
    }
    return result, expected


class CancellationQualificationTests(unittest.TestCase):
    def test_complete_bound_artifact_qualifies_only_cancellation(self):
        value, expected = report()
        result = evaluate_cancellation(value, expected)
        self.assertTrue(result["cancellation_qualified"])
        self.assertFalse(result["performance_qualified"])
        self.assertEqual(result["trials"], 3)
        self.assertEqual(result["operations"], 15)

    def test_rejects_missing_trial_duplicate_identity_and_reused_generation(self):
        for mutation in ("missing", "operation", "audit", "generation"):
            value, expected = report()
            if mutation == "missing":
                value["trials"].pop()
            elif mutation == "operation":
                first = value["trials"][0]["operations"]["text"]["request"]["identity"]
                value["trials"][1]["operations"]["text"]["request"]["identity"] = first
                value["trials"][1]["operations"]["text"]["result"]["operation_id"] = first
            elif mutation == "audit":
                first = value["trials"][0]["audit"]["active"]["after"][-1]["request_id"]
                value["trials"][1]["audit"]["active"]["after"][-1]["request_id"] = first
            else:
                first = value["trials"][0]["final_helpers"][0]["generation"]
                def replace(node):
                    if isinstance(node, dict):
                        for key, item in node.items():
                            if key == "generation": node[key] = first
                            else: replace(item)
                    elif isinstance(node, list):
                        for item in node: replace(item)
                replace(value["trials"][1])
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                evaluate_cancellation(value, expected)

    def test_rejects_short_warmup_or_mismatched_provenance(self):
        for mutation in ("warmup", "provenance"):
            value, expected = report()
            if mutation == "warmup": value["warmup"]["elapsed_seconds"] = 59.9
            else: expected["binary_sha256"] = "f" * 64
            with self.assertRaises(ValueError): evaluate_cancellation(value, expected)

    def test_rejects_incomplete_privacy_or_cleanup(self):
        for field in ("privacy_verified", "protected_values_checked", "files_checked", "logs_closed", "gate_released", "factory_restored", "threads_joined"):
            value, expected = report()
            target = value["finalization"]["privacy"] if field in value["finalization"]["privacy"] else value["finalization"]["cleanup"]
            target[field] = False if type(target[field]) is bool else 0
            with self.subTest(field=field), self.assertRaises(ValueError): evaluate_cancellation(value, expected)

    def test_rejects_live_missing_stale_or_wrong_helper_cleanup(self):
        for mutation in ("live", "missing", "stale", "identity"):
            value, expected = report()
            rows = value["finalization"]["cleanup"]["helpers_terminal"]
            if mutation == "live": rows[0]["state"] = "running"
            elif mutation == "missing": rows.pop()
            elif mutation == "stale": rows[0]["observed_s"] = 200
            else: rows[0]["helper"]["start"] = "wrong-start"
            with self.assertRaises(ValueError): evaluate_cancellation(value, expected)

    def test_rejects_proxy_identity_mismatch_and_nonterminal_owned_child(self):
        for mutation in ("identity", "state", "duplicate"):
            value, expected = report()
            children = value["finalization"]["cleanup"]["owned_children"]
            if mutation == "identity": children[0]["start"] = "wrong-start"
            elif mutation == "state": children[1]["state"] = "running"
            else: children[1]["pid"] = children[0]["pid"]
            with self.assertRaises(ValueError): evaluate_cancellation(value, expected)

    def test_rejects_full_performance_claim_or_wrong_schema(self):
        for key, replacement in (("performance_qualified", True), ("collector_completed", True), ("completed", False), ("schema_version", True), ("mode", "bypass")):
            value, expected = report()
            value[key] = replacement
            with self.assertRaises(ValueError): evaluate_cancellation(value, expected)

    def test_rejects_nonsequential_trials_or_early_finalization(self):
        for mutation in ("order", "warmup", "finalization"):
            value, expected = report()
            if mutation == "order": value["trials"].reverse()
            elif mutation == "warmup": value["warmup_ended_s"] = 110
            else: value["finalization"]["started_s"] = 315
            with self.assertRaises(ValueError): evaluate_cancellation(value, expected)


if __name__ == "__main__": unittest.main()
