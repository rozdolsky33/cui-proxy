import copy
import hashlib
import unittest

from soak_journal import SoakJournal
from soak_trace_acceptance import evaluate_phase_trace, verify_semantic_outcome


class TraceAcceptanceTests(unittest.TestCase):
    def pair(self, phase="fixed"):
        journal = SoakJournal(phase, duration=7200, drain_seconds=45 if phase == "churn" else 0)
        for serial in range(200):
            for client in range(4):
                plan = journal.claim(client, serial*35.995)
                journal.complete(plan["identity"])
        if phase == "churn":
            while sum(len(row["churn_indices"]) for row in journal.snapshot()) < 12000:
                plan = journal.claim(0, 7200.1)
                self.assertIsNotNone(plan)
                journal.complete(plan["identity"])
        schedule = journal.snapshot()
        results = []
        for plan in schedule:
            row = dict(operation_id=str(plan["identity"]), category=plan["category"],
                       scenario=plan["scenario"], completed=True, semantic_verified=True,
                       boundary_verified=True, upstream_receipt_verified=True,
                       outcome="supported_success", supported=True, duration_ms=2000, first_byte_ms=1000,
                       prefix_ms=[1000+i for i in range(32)], relay_ms=[0]*32, relay_flush_ms=[0]*32)
            if plan["category"] == "image":
                row["fixture_evidence"] = [dict(id=f"image-{i}", sha256=hashlib.sha256(str(i).encode()).hexdigest()) for i in range(2)]
            if plan["scenario"] == "W11":
                row.update(outcome="expected_rejection", completed=False, semantic_verified=False,
                           expected_failure_verified=True, upstream_arrivals=0)
            if plan["scenario"] == "W12":
                row.update(outcome="upstream_failure", semantic_verified=False, expected_failure_verified=True)
            results.append(dict(identity=plan["identity"], request=row))
        due = 12000 if phase == "churn" else 0
        mapping = dict(initial_pairs=10000, final_pairs=10000+due, assigned_churn_pairs=due,
                       persisted_churn_pairs=due, due_pairs=due, unassigned_due_pairs=0,
                       changed_seed_mappings=0, unexpected_new_mappings=0, unverified_text_boundaries=0,
                       final_mappings_sha256=hashlib.sha256(b"mapping-proof").hexdigest(), unmet_requirements=[])
        proxy = dict(phase=phase, completed=True, duration_s=7200, elapsed_s=7201, started_s=100,
                     replay=False, schedule=schedule, results=results, failures=[], mapping_evidence=mapping)
        bypass = copy.deepcopy(proxy)
        bypass["replay"] = True
        for entry in bypass["results"]:
            row = entry["request"]
            if row["scenario"] == "W11":
                row.update(outcome="bypass_negative_control", bypass_observed_outcome="transformation_error")
        return proxy, bypass

    def test_full_cycles_and_original_churn_population(self):
        for phase in ("fixed", "churn"):
            result = evaluate_phase_trace(*self.pair(phase))
            self.assertEqual(result["state"], "pass")
            self.assertEqual(result["complete_cycle_categories"], dict(text=560, image=160, lifecycle=80))
            self.assertEqual(result["assigned_churn_pairs"], 12000 if phase == "churn" else 0)
            self.assertFalse(result["performance_qualified"])
            self.assertEqual(sum(result["category_counts"].values()),
                             sum(result["supported_work_categories"].values()) + result["drain_requests"])
            self.assertEqual(result["drain_requests"] > 0, phase == "churn")

    def test_mutated_trace_or_hidden_failure_is_rejected(self):
        for mutation in (lambda p,b: b["schedule"][0].update(elapsed_s=1),
                         lambda p,b: p.update(failures=[dict(code="unexpected")]),
                         lambda p,b: p["results"][0]["request"].update(duration_ms=45001),
                         lambda p,b: p["results"][0]["request"].update(semantic_verified=False),
                         lambda p,b: p.update(replay=True)):
            pair = self.pair()
            mutation(*pair)
            with self.assertRaises(ValueError):
                evaluate_phase_trace(*pair)

    def test_wrong_mapping_counts_or_unmet_requirements_are_rejected(self):
        for field, value in [("persisted_churn_pairs", 11999), ("final_pairs", 21999),
                             ("changed_seed_mappings", 1), ("unverified_text_boundaries", True),
                             ("unmet_requirements", ["pending"])]:
            pair = self.pair("churn")
            pair[0]["mapping_evidence"][field] = value
            with self.assertRaises(ValueError):
                evaluate_phase_trace(*pair)

    def test_image_proofs_must_match_bypass(self):
        pair = self.pair()
        row = next(r["request"] for r in pair[1]["results"] if r["request"]["category"] == "image")
        row["fixture_evidence"][0]["sha256"] = "0"*64
        with self.assertRaises(ValueError):
            evaluate_phase_trace(*pair)

    def test_expected_negatives_do_not_claim_supported_success(self):
        plan = dict(scenario="W11")
        row = dict(outcome="expected_rejection", completed=False, expected_failure_verified=True,
                   boundary_verified=True, upstream_arrivals=0)
        verify_semantic_outcome(plan, row, "proxy")
        row["upstream_arrivals"] = 1
        with self.assertRaises(ValueError):
            verify_semantic_outcome(plan, row, "proxy")
        plan = dict(scenario="W12")
        row = dict(outcome="cancelled", completed=True, expected_failure_verified=True,
                   boundary_verified=True, upstream_receipt_verified=True, upstream_released=True,
                   upstream_release_observed_ms=10)
        verify_semantic_outcome(plan, row, "proxy")
        row["upstream_released"] = False
        with self.assertRaises(ValueError):
            verify_semantic_outcome(plan, row, "proxy")
