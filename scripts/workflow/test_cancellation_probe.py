import copy
import json
import unittest

from cancellation_probe import run_trial
from cancellation_evidence import evaluate_trial_evidence
from test_cancellation_evidence import evidence


class FakeDriver:
    def __init__(self, failure=None):
        self.value = json.loads(json.dumps(evidence()))
        self.value["operations"]["text"]["started_s"] = 10.1
        self.now = 9.0
        self.failure = failure
        self.events = []
        self.cleaned = False
        self.cancelled = set()
        bounds = self.value["bounds"]
        first = copy.deepcopy(bounds["queued"]["before"])
        first.update(started_s=9.1, ended_s=9.2, active=1, queued=0, marker_count=1)
        first["helpers"] = [bounds["active"]["cancelled_helper"]]
        second = copy.deepcopy(bounds["queued"]["before"])
        second.update(started_s=9.7, ended_s=9.8, queued=0)
        self.observations = [first, second, bounds["queued"]["before"], bounds["queued"]["after"], bounds["active"]["before"], bounds["active"]["after"]]
        self.observations[-1]["stopped_helpers"] = [bounds["active"]["stopped"]]
        for observation in self.observations:
            observation["gate_armed"] = True
        self.audit_calls = 0

    def hit(self, name):
        self.events.append(name)
        if self.failure == name:
            raise RuntimeError("injected driver failure")

    def clock(self): return self.now
    def sleep(self, seconds): self.now += seconds

    def arm(self): self.hit("arm")

    def start(self, role):
        self.hit("start")
        self.events.append("start:" + role)
        self.now = max(self.now, self.value["operations"][role]["started_s"])
        return self.value["operations"][role]["started_s"]

    def observe(self):
        self.hit("observe")
        if not self.observations:
            raise ValueError("fake observations exhausted")
        row = copy.deepcopy(self.observations.pop(0))
        self.now = row["ended_s"]
        return row

    def cancel(self, role):
        self.hit("cancel")
        self.events.append("cancel:" + role)
        self.cancelled.add(role)
        row = self.value["bounds"][role]
        self.now = row["cancel_s"]
        return {key: row[key] for key in ("request_started_s", "cancel_s", "closed_by_probe", "headers_received_before_cancel")}

    def transport(self, role):
        self.hit("transport")
        return self.value["bounds"][role]["transport_end_s"]

    def audit(self):
        self.hit("audit")
        role = "queued" if self.audit_calls == 0 else "active"
        self.audit_calls += 1
        return copy.deepcopy(self.value["audit"][role]["before"])

    def audit_after(self, before, detail, deadline):
        self.hit("audit_after")
        role = "queued" if detail == "attachment_image_busy" else "active"
        return copy.deepcopy(self.value["audit"][role]["after"])

    def release(self):
        self.hit("release")
        self.now = self.value["release_s"]
        return self.now

    def finish(self, deadline):
        self.hit("finish")
        self.now = 16
        return copy.deepcopy(self.value["operations"]), 5

    def receipts(self, operations):
        self.hit("receipts")
        self.now = 16.2
        return copy.deepcopy(self.value["receipts"]), copy.deepcopy(self.value["receipt_observation"])

    def final_helpers(self):
        self.hit("final_helpers")
        return copy.deepcopy(self.value["final_helpers"])

    def cleanup(self):
        self.cleaned = True
        self.hit("cleanup")


class CancellationProbeSequenceTests(unittest.TestCase):
    def test_runs_both_cancellations_then_recovery_and_cleanup(self):
        driver = FakeDriver()
        result = run_trial(driver)
        self.assertTrue(result["completed"])
        self.assertTrue(evaluate_trial_evidence(result)["trial_verified"])
        self.assertTrue(driver.cleaned)
        ordered = [event for event in driver.events if ":" in event]
        self.assertEqual(ordered, ["start:active", "start:survivor", "start:text", "start:queued", "cancel:queued", "start:replacement", "cancel:active"])
        self.assertLess(driver.events.index("cancel:active"), driver.events.index("finish"))

    def test_every_driver_failure_cleans_up_and_preserves_incomplete_record(self):
        for failure in ("arm", "start", "observe", "cancel", "transport", "audit", "audit_after", "release", "finish", "receipts", "final_helpers", "cleanup"):
            driver = FakeDriver(failure)
            retained = {}
            with self.subTest(failure=failure), self.assertRaises(RuntimeError):
                run_trial(driver, retained)
            self.assertTrue(driver.cleaned)
            self.assertIs(retained["completed"], False)

    def test_rejects_survivor_loss_and_extra_queued_admission(self):
        for mutation in ("survivor", "markers"):
            driver = FakeDriver()
            if mutation == "survivor":
                driver.observations[3]["helpers"][1]["start"] = "wrong-process"
            else:
                driver.observations[3]["marker_count"] = 3
            with self.assertRaises(ValueError): run_trial(driver)
            self.assertTrue(driver.cleaned)

    def test_rejects_unarmed_gate_observation(self):
        driver = FakeDriver()
        driver.observations[3]["gate_armed"] = False
        with self.assertRaises(ValueError): run_trial(driver)
        self.assertTrue(driver.cleaned)

    def test_rejects_late_queue_or_active_observation(self):
        for index, ended in ((3, 12.01), (5, 15.01)):
            driver = FakeDriver()
            driver.observations[index]["ended_s"] = ended
            with self.assertRaises(ValueError): run_trial(driver)
            self.assertTrue(driver.cleaned)

    def test_rejects_wrong_stopped_helper_or_unexpected_success(self):
        for mutation in ("helper", "success"):
            driver = FakeDriver()
            if mutation == "helper":
                driver.observations[-1]["stopped_helpers"][0]["helper"]["pid"] = 999
            else:
                driver.value["operations"]["queued"]["result"]["completed"] = True
            with self.assertRaises(ValueError): run_trial(driver)
            self.assertTrue(driver.cleaned)


if __name__ == "__main__": unittest.main()
