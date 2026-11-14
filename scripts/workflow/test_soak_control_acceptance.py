import unittest

from soak_controls import control_times
from soak_lifecycle import lifecycle_step
from soak_schedule import plan_request
from soak_control_acceptance import evaluate_controls


class ControlAcceptanceTests(unittest.TestCase):
    def evidence(self):
        origin = 100.0
        report = dict(clock_origin_s=origin, failed=False, restarts_planned=10,
                      reloads_planned=10, restarts_completed=10, reloads_completed=10,
                      restarts=[], reloads=[])
        phases = {phase: dict(phase=phase, completed=True, duration_s=7200,
                             elapsed_s=7200, started_s=origin + offset, results=[])
                  for phase, offset in [("fixed", 0), ("churn", 7200)]}
        snapshots = []
        serials = [serial for serial in range(5000)
                   if plan_request(client=2, serial=serial, phase="fixed")["scenario"] == "W10"
                   and lifecycle_step(plan_request(client=2, serial=serial, phase="fixed"))["stage"] == "continue"]
        for index, deadline in enumerate(control_times(14400, 10), 1):
            phase = "fixed" if deadline < 7200 else "churn"
            for kind, delay in [("restart", 0), ("reload", 5)]:
                serial = serials[index-1] if kind == "restart" else index
                identity = [phase, 2, serial]
                row = dict(control=kind, index=index, identity=identity,
                           started_s=deadline+delay, elapsed_s=deadline+delay+2,
                           active_requests_before=1)
                if kind == "restart":
                    row["evidence"] = dict(restart_index=index, graceful_exit=True,
                        ready=True, continuation_verified=True, vault_before=10000,
                        vault_after=10000, elapsed_s=1.5)
                    snapshots.append(dict(event="stage_restart_snapshot",
                        observed_s=origin+deadline+.25, records=index*10))
                else:
                    row["evidence"] = dict(generation=index, reload_returned_success=True, rule_count=53)
                report[kind+"s"].append(row)
                phases[phase]["results"].append(dict(identity=identity, request=dict(
                    completed=True, semantic_verified=True, boundary_verified=True,
                    upstream_receipt_verified=True, outcome="supported_success")))
        return report, phases, snapshots

    def check(self, evidence):
        report, phases, snapshots = evidence
        return evaluate_controls(report, phase_reports=phases, snapshots=snapshots, expected_rule_count=53)

    def test_original_control_counts_and_absolute_fault_intervals(self):
        result = self.check(self.evidence())
        self.assertEqual(result["state"], "pass")
        self.assertEqual(len(result["fault_intervals"]), 20)
        self.assertEqual(len(result["restart_intervals"]), 10)
        self.assertFalse(result["performance_qualified"])

    def test_missing_duplicate_or_unobserved_controls_rejected(self):
        mutations = [lambda r,p,s: r.update(restarts_completed=9),
                     lambda r,p,s: r["restarts"].pop(),
                     lambda r,p,s: r["reloads"][1].update(index=1),
                     lambda r,p,s: r["restarts"][0].update(active_requests_before=0),
                     lambda r,p,s: s.pop(),
                     lambda r,p,s: s[0].update(observed_s=0),
                     lambda r,p,s: r.update(clock_origin_s=float("nan")),
                     lambda r,p,s: p["fixed"].update(duration_s=150),
                     lambda r,p,s: p["fixed"]["results"].pop(0)]
        for mutate in mutations:
            data = self.evidence()
            mutate(*data)
            with self.subTest(mutation=mutate):
                with self.assertRaises(ValueError):
                    self.check(data)

    def test_recovery_and_policy_proofs_cannot_be_replaced_by_health(self):
        for field, value in [("continuation_verified", False), ("graceful_exit", False),
                             ("vault_after", 9999), ("restart_index", True)]:
            data = self.evidence()
            data[0]["restarts"][0]["evidence"][field] = value
            with self.assertRaises(ValueError):
                self.check(data)
        data = self.evidence()
        data[0]["reloads"][0]["evidence"]["rule_count"] = 52
        with self.assertRaises(ValueError):
            self.check(data)

    def test_overlapping_or_out_of_horizon_faults_rejected(self):
        for field, value in [("started_s", -1), ("elapsed_s", 15000), ("elapsed_s", float("inf"))]:
            data = self.evidence()
            data[0]["restarts"][0][field] = value
            with self.assertRaises(ValueError):
                self.check(data)
        data = self.evidence()
        data[0]["reloads"][0]["started_s"] = data[0]["restarts"][0]["started_s"]
        with self.assertRaises(ValueError):
            self.check(data)

    def test_restart_continuation_must_be_a_supported_success(self):
        data = self.evidence()
        data[1]["fixed"]["results"][0]["request"]["outcome"] = "cancelled"
        with self.assertRaises(ValueError):
            self.check(data)

    def test_failed_or_unattested_controls_remain_incomplete(self):
        mutations = [lambda r,p,s: r.update(failed=True),
                     lambda r,p,s: r.pop("clock_origin_s"),
                     lambda r,p,s: r["reloads"][0]["evidence"].update(generation=True),
                     lambda r,p,s: s[1].update(records=0),
                     lambda r,p,s: p["fixed"]["results"][0]["request"].update(boundary_verified=False)]
        for mutate in mutations:
            data = self.evidence()
            mutate(*data)
            with self.assertRaises(ValueError):
                self.check(data)

    def test_reload_can_preserve_a_verified_expected_rejection(self):
        data = self.evidence()
        control = data[0]["reloads"][0]
        outcome = next(row for row in data[1]["fixed"]["results"] if row["identity"] == control["identity"])
        serial = next(i for i in range(1000) if plan_request(client=2, serial=i, phase="fixed")["scenario"] == "W11")
        identity = ["fixed", 2, serial]
        control["identity"] = identity
        outcome["identity"] = identity
        outcome["request"].update(outcome="expected_rejection", completed=False, semantic_verified=False,
                                  expected_failure_verified=True, boundary_verified=True, upstream_arrivals=0)
        self.assertEqual(self.check(data)["state"], "pass")
        outcome["request"]["upstream_arrivals"] = 1
        with self.assertRaises(ValueError):
            self.check(data)
