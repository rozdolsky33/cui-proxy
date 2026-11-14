import unittest
from soak_lifecycle_state import LifecycleState
from soak_schedule import plan_request


class LifecycleStateTests(unittest.TestCase):
    def plans(self, scenario, count):
        return [plan_request(0, serial, "fixed") for serial in range(count * 50) if plan_request(0, serial, "fixed")["scenario"] == scenario]

    def success(self):
        return {"outcome": "supported_success", "completed": True, "semantic_verified": True, "boundary_verified": True}

    def test_children_and_collection_require_prior_verified_context(self):
        state = LifecycleState()
        plans = self.plans("W07", 5)
        for index, plan in enumerate(plans):
            previous = state.begin(plan)
            self.assertEqual(previous, [{"step": n} for n in range(index)])
            previous.append({"step": "external mutation"})
            self.assertTrue(state.finish(plan, self.success(), {"step": index}))
        self.assertEqual(state.summary()["completed_workflows"]["W07"], 1)

    def test_pending_or_failed_step_cannot_enable_successor(self):
        plans = self.plans("W09", 2)
        for changed in ({"outcome": "boundary_pending"}, {"semantic_verified": False}, {"boundary_verified": False}, {"completed": False}):
            state = LifecycleState()
            state.begin(plans[0])
            self.assertFalse(state.finish(plans[0], dict(self.success(), **changed), {"context": "not accepted"}))
            with self.assertRaises(ValueError):
                state.begin(plans[1])
            self.assertEqual(state.summary()["failed_workflows"], 1)

    def test_missing_or_duplicate_predecessor_fails(self):
        state = LifecycleState()
        plans = self.plans("W07", 2)
        with self.assertRaises(ValueError):
            state.begin(plans[1])
        state.begin(plans[0])
        with self.assertRaises(ValueError):
            state.begin(plans[0])
        state.finish(plans[0], self.success(), {})
        with self.assertRaises(ValueError):
            state.finish(plans[0], self.success(), {})

    def test_restart_continuation_does_not_claim_restart_evidence(self):
        state = LifecycleState()
        for plan in self.plans("W10", 3):
            state.begin(plan)
            state.finish(plan, self.success(), {})
        summary = state.summary()
        self.assertEqual(summary["continuations"], 2)
        self.assertNotIn("restarts", summary)
