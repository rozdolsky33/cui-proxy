import copy
import unittest

from cancellation_evidence import evaluate_trial_evidence
from test_cancellation_acceptance import trial as bounds_trial
import test_soak_warmup_acceptance as warmup_tests


def evidence():
    bounds = bounds_trial()
    prototypes = {}
    for entry in warmup_tests.WarmupAcceptanceTests().report()["results"]:
        prototypes.setdefault(entry["request"]["category"], entry)
    operations = {}
    starts = {"queued": 10.2, "active": 9, "survivor": 9.5, "replacement": 12, "text": 10.3}
    for role, started in starts.items():
        entry = copy.deepcopy(prototypes["text" if role == "text" else "image"])
        entry["request"]["identity"] = role
        entry["result"]["operation_id"] = role
        entry["started_s"], entry["ended_s"] = started, 16.0
        if role in ("queued", "active"):
            entry["result"].update(completed=False, semantic_verified=False, status=0)
        operations[role] = entry
    queued = dict(request_id="queued-audit", decision="blocked", reason="sanitize", detail="attachment_image_busy")
    active = dict(request_id="active-audit", decision="blocked", reason="sanitize", detail="attachment_image_timeout")
    return {
        "bounds": bounds,
        "operations": operations,
        "release_s": 15.0,
        "receipt_observation": {"started_s": 16.1, "ended_s": 16.2},
        "receipts": {"queued": {"complete": True, "count": 0, "body_sha256": None}, "active": {"complete": True, "count": 0, "body_sha256": None}},
        "audit": {
            "queued": {"before": [], "after": [queued]},
            "active": {"before": [queued], "after": [queued, active]},
        },
        "final_helpers": bounds["queued"]["before"]["helpers"] + [bounds["active"]["admitted"]["helper"]],
        "threads_joined": 5,
    }


class CancellationEvidenceTests(unittest.TestCase):
    def test_bound_trial_evidence_does_not_claim_whole_qualification(self):
        result = evaluate_trial_evidence(evidence())
        self.assertTrue(result["trial_verified"])
        self.assertFalse(result["cancellation_qualified"])
        self.assertFalse(result["performance_qualified"])
        self.assertEqual(set(result["operation_ids"]), {"queued", "active", "survivor", "replacement", "text"})

    def test_rejects_upstream_arrival_or_receipt_checked_too_early(self):
        for mutation in ("receipt", "early"):
            report = evidence()
            if mutation == "receipt":
                report["receipts"]["queued"]["count"] = 1
            else:
                report["receipt_observation"]["started_s"] = 14.0
            with self.assertRaises(ValueError):
                evaluate_trial_evidence(report)

    def test_rejects_cancelled_success_or_mismatched_operation_identity(self):
        for mutation in ("completed", "identity", "status"):
            report = evidence()
            if mutation == "completed":
                report["operations"]["queued"]["result"]["completed"] = True
            elif mutation == "identity":
                report["operations"]["active"]["result"]["operation_id"] = "queued"
            else:
                report["operations"]["active"]["result"]["status"] = 200
            with self.assertRaises(ValueError):
                evaluate_trial_evidence(report)

    def test_rejects_failed_recovery_or_unverified_text(self):
        for role in ("survivor", "replacement", "text"):
            report = evidence()
            report["operations"][role]["result"]["semantic_verified"] = False
            with self.assertRaises(ValueError):
                evaluate_trial_evidence(report)

    def test_rejects_extra_helper_unjoined_workers_and_wrong_audit_detail(self):
        for mutation in ("helper", "worker", "audit"):
            report = evidence()
            if mutation == "helper":
                report["final_helpers"].append(copy.deepcopy(report["final_helpers"][0]))
            elif mutation == "worker":
                report["threads_joined"] = 4
            else:
                report["audit"]["active"]["after"][-1]["detail"] = "attachment_image_busy"
            with self.assertRaises(ValueError):
                evaluate_trial_evidence(report)

    def test_requires_audit_history_between_both_cancellations(self):
        report = evidence()
        report["audit"]["active"]["before"] = []
        report["audit"]["active"]["after"] = [report["audit"]["active"]["after"][-1]]
        with self.assertRaises(ValueError):
            evaluate_trial_evidence(report)

    def test_rejects_transport_binding_or_request_deadline_mismatch(self):
        for mutation in ("start", "end", "deadline", "release"):
            report = evidence()
            if mutation == "start":
                report["operations"]["queued"]["started_s"] = 10.3
            elif mutation == "end":
                report["operations"]["queued"]["ended_s"] = 11.1
            elif mutation == "deadline":
                report["operations"]["replacement"]["ended_s"] = 58
            else:
                report["release_s"] = 13
            with self.assertRaises(ValueError):
                evaluate_trial_evidence(report)


if __name__ == "__main__":
    unittest.main()
