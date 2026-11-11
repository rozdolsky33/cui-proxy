import copy
import unittest

from churn_smoke import validate_report


class ChurnSmokeTests(unittest.TestCase):
    def report(self):
        phases = []
        for mode in ("proxy", "bypass"):
            phases.append(dict(mode=mode, scheduled=1, sent=1, completed=1,
                               client_unsent=0, supported_success=1,
                               initial_vault_population=10000 if mode == "proxy" else 0,
                               population=dict(vault_entries=10000),
                               final_vault_population=10001 if mode == "proxy" else 10000,
                               live_vault_population=10001 if mode == "proxy" else 0,
                               observed_new_pairs=1 if mode == "proxy" else None,
                               verified_new_pairs=1 if mode == "proxy" else None,
                               records=[dict(outcome="supported_success", prefix_ms=[1] * 32)]))
        return dict(performance_qualified=False, measurements=phases)

    def test_complete_real_smoke_contract_is_accepted(self):
        validate_report(self.report())

    def test_seeded_bypass_file_is_verified_even_when_live_vault_is_unloaded(self):
        report = self.report()
        bypass = report["measurements"][1]
        self.assertEqual(bypass["initial_vault_population"], 0)
        self.assertEqual(bypass["live_vault_population"], 0)
        self.assertEqual(bypass["final_vault_population"], 10000)
        validate_report(report)
        bypass["population"]["vault_entries"] = 0
        with self.assertRaises(ValueError):
            validate_report(report)

    def test_missing_growth_or_success_or_denominator_is_rejected(self):
        for field, value in (("observed_new_pairs", 0), ("verified_new_pairs", 0),
                             ("final_vault_population", 10000), ("client_unsent", 1),
                             ("supported_success", 0), ("sent", 0)):
            report = self.report()
            report["measurements"][0][field] = value
            with self.assertRaises(ValueError):
                validate_report(report)

    def test_missing_or_duplicate_arm_or_pending_boundary_is_rejected(self):
        report = self.report()
        for phases in (report["measurements"][:1], [report["measurements"][0]] * 2):
            with self.assertRaises(ValueError):
                validate_report(dict(report, measurements=phases))
        report = copy.deepcopy(report)
        report["measurements"][0]["records"][0]["outcome"] = "boundary_pending"
        with self.assertRaises(ValueError):
            validate_report(report)

    def test_partial_stream_or_qualification_claim_is_rejected(self):
        report = self.report()
        report["measurements"][0]["records"][0]["prefix_ms"] = [1]
        with self.assertRaises(ValueError):
            validate_report(report)
        with self.assertRaises(ValueError):
            validate_report(dict(self.report(), performance_qualified=True))
