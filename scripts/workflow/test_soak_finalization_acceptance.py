import unittest

from soak_finalization_acceptance import evaluate_finalization


class FinalizationAcceptanceTests(unittest.TestCase):
    def evidence(self, mode="proxy"):
        audit = dict(audit_records=120, rule_observation_records=100,
                     old_token_records_without_matches=10,
                     shape_rejections_without_matches=10,
                     policy={str(i): 10 for i in range(10)})
        if mode == "bypass":
            audit = dict(audit_records=0, bypass_proxy_absence_verified=True)
        return dict(verified=True, performance_qualified=False,
                    outcomes_reconciled=120, audit=audit,
                    cleanup=dict(children_stopped=12, logs_closed=12),
                    privacy=dict(privacy_verified=True, files_checked=4,
                                 protected_values_checked=44001))

    def evaluate(self, report, mode="proxy"):
        return evaluate_finalization(report, mode=mode, expected_outcomes=120,
                                     expected_generations=10, expected_children=12,
                                     minimum_protected_values=44001)

    def test_complete_finalization_is_accepted_without_qualifying_performance(self):
        for mode in ("proxy", "bypass"):
            result = self.evaluate(self.evidence(mode), mode)
            self.assertEqual(result["state"], "pass")
            self.assertFalse(result["performance_qualified"])

    def test_missing_privacy_cleanup_or_outcomes_is_rejected(self):
        for path, value in ((["verified"], False),
                            (["outcomes_reconciled"], 119),
                            (["cleanup", "children_stopped"], 11),
                            (["cleanup", "logs_closed"], 11),
                            (["privacy", "privacy_verified"], False),
                            (["privacy", "files_checked"], 0),
                            (["privacy", "protected_values_checked"], 44000)):
            report = self.evidence()
            target = report
            for key in path[:-1]: target = target[key]
            target[path[-1]] = value
            with self.subTest(path=path), self.assertRaises(ValueError):
                self.evaluate(report)

    def test_incomplete_or_inconsistent_policy_generations_are_rejected(self):
        for key, value in (("policy", {"0": 100}), ("audit_records", 119),
                           ("rule_observation_records", 99),
                           ("old_token_records_without_matches", 9)):
            report = self.evidence()
            report["audit"][key] = value
            with self.subTest(key=key), self.assertRaises(ValueError):
                self.evaluate(report)

    def test_bypass_requires_no_proxy_audit_and_verified_absence(self):
        for key, value in (("audit_records", 1), ("bypass_proxy_absence_verified", False)):
            report = self.evidence("bypass")
            report["audit"][key] = value
            with self.assertRaises(ValueError): self.evaluate(report, "bypass")

    def test_booleans_cannot_replace_integer_evidence(self):
        report = self.evidence()
        report["privacy"]["files_checked"] = True
        with self.assertRaises(ValueError): self.evaluate(report)
