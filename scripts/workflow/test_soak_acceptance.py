import unittest

from soak_acceptance import REQUIRED_GATES, evaluate_completed_soak, summarize_acceptance
import test_soak_provenance_acceptance as provenance_tests


class SoakAcceptanceTests(unittest.TestCase):
    def gates(self):
        return {name: dict(state="pass", performance_qualified=False) for name in REQUIRED_GATES}

    def test_soak_pass_does_not_qualify_the_remaining_performance_contract(self):
        result = summarize_acceptance(self.gates())
        self.assertTrue(result["soak_qualified"])
        self.assertFalse(result["performance_qualified"])

    def test_every_original_gate_is_required(self):
        for name in REQUIRED_GATES:
            for state in ("fail", "incomplete"):
                gates = self.gates(); gates[name]["state"] = state
                with self.subTest(name=name, state=state):
                    self.assertFalse(summarize_acceptance(gates)["soak_qualified"])
            gates = self.gates(); del gates[name]
            self.assertFalse(summarize_acceptance(gates)["soak_qualified"])

    def test_short_campaign_is_rejected_before_reading_measurements(self):
        provenance_tests.ProvenanceAcceptanceTests.setUpClass()
        identity, expected, settings = provenance_tests.ProvenanceAcceptanceTests().evidence()
        settings["fixed_seconds"] = 150
        result = evaluate_completed_soak(dict(identity=identity, settings=settings), expected)
        self.assertFalse(result["soak_qualified"])
        self.assertEqual(result["gates"]["provenance"]["state"], "incomplete")

    def test_missing_or_malformed_sealed_campaign_cannot_qualify(self):
        for campaign in ({}, None, dict(identity={}, settings={})):
            self.assertFalse(evaluate_completed_soak(campaign, {})["soak_qualified"])
