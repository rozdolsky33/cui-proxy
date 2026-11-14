import hashlib
import json
import unittest
from unittest.mock import patch

from performance_workload import Workload
from soak_schedule import plan_request
from soak_lifecycle_state import LifecycleState
from soak_continuations import build_continuation_case, execute_continuation


class ContinuationTests(unittest.TestCase):
    def setUp(self):
        self.workload = Workload()
        self.mappings = {pair[1]: "mapped-" + str(i) for i, pair in enumerate(self.workload.pairs)}

    def plans(self, scenario, count):
        return [plan_request(0, serial, "fixed") for serial in range(50 * count) if plan_request(0, serial, "fixed")["scenario"] == scenario]

    def test_parent_collection_contains_each_verified_child_result(self):
        previous = []
        for plan in self.plans("W07", 5):
            case, context = build_continuation_case(self.workload, plan, self.mappings, previous)
            body = json.loads(case["body"])
            if previous and len(previous) < 4:
                self.assertIn(previous[0]["tag"], json.dumps(body))
            if len(previous) == 4:
                for child in previous[1:]:
                    self.assertIn(child["tag"], json.dumps(body))
            previous.append(context)
        self.assertEqual(len(previous), 5)

    def test_resume_carries_history_and_compaction_stays_opaque(self):
        previous = []
        for plan in self.plans("W09", 3):
            case, context = build_continuation_case(self.workload, plan, self.mappings, previous)
            body = json.loads(case["body"])
            if previous:
                self.assertIn(previous[-1]["tag"], json.dumps(body))
            previous.append(context)
        opaque = [item for item in body["input"] if item.get("type") == "compaction"]
        self.assertEqual(len(opaque), 1)
        self.assertIn("encrypted_content", opaque[0])

    def test_restart_continuation_uses_old_substitutions_without_reseeding_originals(self):
        first, continuation = self.plans("W10", 2)
        original_case, context = build_continuation_case(self.workload, first, self.mappings, [])
        case, _ = build_continuation_case(self.workload, continuation, {}, [context])
        self.assertTrue(case["benchmark_existing_substitutions"])
        for original in original_case["protected_occurrences"]:
            self.assertNotIn(original.encode(), case["body"])
            self.assertIn(self.mappings[original].encode(), case["body"])

    def test_wrong_context_cannot_be_reused_by_another_client(self):
        plan = self.plans("W07", 1)[0]
        _, context = build_continuation_case(self.workload, plan, self.mappings, [])
        other = next(plan_request(1, serial, "fixed") for serial in range(50, 100) if plan_request(1, serial, "fixed")["scenario"] == "W07")
        with self.assertRaises(ValueError):
            build_continuation_case(self.workload, other, self.mappings, [context])

    def test_failed_request_does_not_enable_next_step(self):
        state = LifecycleState()
        first, second = self.plans("W07", 2)
        with patch("soak_continuations.performance.request", return_value={"outcome": "transformation_error", "completed": False}):
            row = execute_continuation("http://unused", self.workload, first, self.mappings, state)
        self.assertFalse(row["semantic_verified"])
        with self.assertRaises(ValueError):
            state.begin(second)
