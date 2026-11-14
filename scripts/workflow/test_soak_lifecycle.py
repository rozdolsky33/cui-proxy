import collections
import unittest
from soak_schedule import plan_request
from soak_lifecycle import lifecycle_step


class LifecycleScheduleTests(unittest.TestCase):
    def test_delegation_uses_five_existing_slots_with_three_distinct_children(self):
        for client in range(4):
            plans = [plan_request(client, serial, "fixed") for serial in range(250)]
            counts = collections.Counter(plan["category"] for plan in plans)
            self.assertEqual(counts, {"text": 175, "image": 50, "lifecycle": 25})
            stages = [lifecycle_step(plan) for plan in plans if plan["scenario"] == "W07"]
            self.assertEqual([step["stage"] for step in stages], ["parent_dispatch", "child_0", "child_1", "child_2", "parent_collect"])
            self.assertEqual(len({step["workflow"] for step in stages}), 1)
            self.assertEqual(stages[0]["workflow"][:2], ("fixed", client))

    def test_history_and_failure_variants_are_replayed_deterministically(self):
        expected = {"W09": ["history", "resume", "compaction"], "W11": ["blocked", "unsupported", "malformed_json"], "W12": ["upstream_error", "malformed_success", "cancel"]}
        for scenario, stages in expected.items():
            plans = [plan_request(0, serial, "churn") for serial in range(150)]
            selected = [plan for plan in plans if plan["scenario"] == scenario]
            self.assertEqual([lifecycle_step(plan)["stage"] for plan in selected], stages)
            for plan in selected:
                restored = dict(plan, identity=list(plan["identity"]))
                self.assertEqual(lifecycle_step(plan), lifecycle_step(restored))

    def test_restart_slots_establish_once_then_check_continuation(self):
        selected = [plan_request(0, serial, "fixed") for serial in range(100) if plan_request(0, serial, "fixed")["scenario"] == "W10"]
        self.assertEqual([lifecycle_step(plan)["stage"] for plan in selected], ["establish", "continue"])
        self.assertEqual(lifecycle_step(selected[0])["workflow"], lifecycle_step(selected[1])["workflow"])

    def test_text_or_changed_lifecycle_plan_is_rejected(self):
        with self.assertRaises(ValueError):
            lifecycle_step(plan_request(0, 0, "fixed"))
        with self.assertRaises(ValueError):
            lifecycle_step(dict(plan_request(0, 9, "fixed"), scenario="W12"))
