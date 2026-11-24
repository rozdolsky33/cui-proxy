"""Scheduled under-load controls without adding hidden workload requests."""
import math
import threading
import time

from soak_lifecycle import lifecycle_step
from soak_recovery import RestartNotEligible, restart_proxy


def control_times(duration, count=10):
    if type(duration) not in (int, float) or not math.isfinite(duration) or duration <= 0 or type(count) is not int or count < 1:
        raise ValueError("invalid control schedule")
    return [duration * (index + 1) / (count + 1) for index in range(count)]


def _times(values):
    result = tuple(values)
    if any(type(value) not in (int, float) or not math.isfinite(value) or value < 0 for value in result) or any(a >= b for a, b in zip(result, result[1:])):
        raise ValueError("invalid control schedule")
    return result


def _request_kind(plan):
    # These lifecycle cases dispatch ordinary input_text requests, as in
    # ScenarioExecutor's execute_continuation branch. Keep scheduling metadata intact.
    if plan["category"] == "lifecycle" and plan["scenario"] in {"W07", "W09", "W10"}:
        return "text"
    return plan["category"]


class ControlledExecutor:
    def __init__(self, execute, env, reloader, record, *, restart_times, reload_times, clock=time.monotonic, before_restart=lambda: None, image_checkpoint=None):
        self.execute, self.env, self.reloader, self.record = execute, env, reloader, record
        self.restart_times, self.reload_times = _times(restart_times), _times(reload_times)
        self.clock = clock
        self.before_restart = before_restart
        self.started = clock()
        self.condition = threading.Condition()
        self.active = 0
        self.changing = False
        self.restarting = False
        self.failed = False
        self.restarts = []
        self.reloads = []
        self.image_checkpoint = image_checkpoint
        self.active_categories = {"text": 0, "image": 0, "lifecycle": 0}
        self.completed_images = 0
        self.last_stage_checkpoint = None

    def _invoke(self, plan, *, admitted=False):
        category = _request_kind(plan)
        if not admitted:
            with self.condition:
                self.active += 1
                self.active_categories[category] += 1
        try:
            return self.execute(plan)
        finally:
            with self.condition:
                self.active -= 1
                self.active_categories[category] -= 1
                if category == "image":
                    self.completed_images += 1
                self.condition.notify_all()

    def _restart_eligible_locked(self):
        return (self.active_categories["image"] == 0 and
                self.active_categories["lifecycle"] == 0 and self.active_categories["text"] > 0)

    def _signal_checkpoint(self):
        with self.condition:
            if not self._restart_eligible_locked():
                raise RestartNotEligible("restart text load no longer eligible")
            started = self.clock()
            active = self.env.metrics().get("active_requests")
            observed = self.clock()
            if type(active) is not int or active < 0:
                raise ValueError("restart active request observation invalid")
            if not self._restart_eligible_locked() or active < self.active_categories["text"]:
                raise RestartNotEligible("restart text load no longer eligible")
            if (type(started) not in (int, float) or not math.isfinite(started) or
                    type(observed) not in (int, float) or not math.isfinite(observed) or observed < started):
                raise ValueError("restart signal observation clock invalid")
            stages = self.last_stage_checkpoint
            if type(stages) is not dict or stages.get("image_records") != stages.get("expected_image_records"):
                raise ValueError("restart image stage checkpoint invalid")
            return dict(admitted_text_operations=self.active_categories["text"], admitted_image_operations=0,
                        admitted_other_operations=0, active_requests=active, observation_started_s=started,
                        observed_s=observed, completed_images=self.completed_images,
                        image_stage_records=stages["image_records"],
                        expected_image_stage_records=stages["expected_image_records"])


    def _select(self, plan):
        with self.condition:
            if not self.condition.wait_for(lambda: not self.restarting, timeout=45):
                raise TimeoutError("restart admission wait exceeded")
            if self.failed: raise ValueError("control executor unavailable")
            if self.changing:
                self.active += 1
                self.active_categories[_request_kind(plan)] += 1
                return None
            elapsed = self.clock() - self.started
            restart_due = len(self.restarts) < len(self.restart_times) and self.restart_times[len(self.restarts)] <= elapsed
            continuation = plan.get("scenario") == "W10" and lifecycle_step(plan)["stage"] == "continue"
            if restart_due and continuation and self.active > 0 and (self.image_checkpoint is None or self._restart_eligible_locked()):
                self.changing = self.restarting = True
                return "restart"
            reload_due = len(self.reloads) < len(self.reload_times) and self.reload_times[len(self.reloads)] <= elapsed
            if reload_due:
                self.changing = True
                return "reload"
            self.active += 1
            self.active_categories[_request_kind(plan)] += 1
            return None

    def __call__(self, plan):
        kind = self._select(plan)
        if kind is None: return self._invoke(plan, admitted=True)
        began = False
        index = len(self.restarts) + 1 if kind == "restart" else len(self.reloads) + 1
        try:
            with self.condition: admitted = self.active
            active = self.env.metrics().get("active_requests")
            if type(active) is not int or active < 0: raise ValueError("control load evidence unavailable")
            if active == 0 or (kind == "restart" and active < admitted): return self._invoke(plan)
            started = self.clock() - self.started
            self.record({"event": "control_started", "control": kind, "index": index,
                         "identity": plan["identity"], "elapsed_s": started, "active_requests": active, "admitted_requests": admitted})
            began = True
            if kind == "restart":
                rows = []
                def continuation():
                    if rows: raise ValueError("duplicate restart continuation")
                    row = self._invoke(plan)
                    rows.append(row)
                    return row
                self.before_restart()
                if self.image_checkpoint is not None:
                    self.last_stage_checkpoint = self.image_checkpoint(self.completed_images)
                evidence = restart_proxy(self.env, index, continuation=continuation, **({"before_signal": self._signal_checkpoint} if self.image_checkpoint is not None else {}))
                if len(rows) != 1 or any(evidence.get(key) is not True for key in ("graceful_exit", "ready", "continuation_verified")):
                    raise ValueError("restart continuation evidence missing")
                row = rows[0]
            else:
                evidence = self.reloader.reload(index)
                if evidence.get("generation") != index or evidence.get("reload_returned_success") is not True:
                    raise ValueError("reload evidence missing")
                row = None
            event = {"event": "control_completed", "control": kind, "index": index,
                     "identity": plan["identity"], "started_s": started, "elapsed_s": self.clock() - self.started,
                     "active_requests_before": active, "evidence": evidence}
            self.record(event)
            with self.condition:
                (self.restarts if kind == "restart" else self.reloads).append(event)
            if row is None: row = self._invoke(plan)
            return row
        except RestartNotEligible:
            pass
        except BaseException as error:
            with self.condition: self.failed = True
            if began:
                try:
                    self.record({"event": "control_failed", "control": kind, "index": index,
                                 "identity": plan["identity"], "elapsed_s": self.clock() - self.started,
                                 "error_kind": type(error).__name__})
                except BaseException:
                    pass  # Original error propagates; missing final evidence cannot qualify.
            raise
        finally:
            with self.condition:
                self.changing = self.restarting = False
                self.condition.notify_all()
        return self._invoke(plan)

    def summary(self):
        with self.condition:
            return {"clock_origin_s": self.started, "failed": self.failed, "restarts_planned": len(self.restart_times), "reloads_planned": len(self.reload_times),
                    "restarts_completed": len(self.restarts), "reloads_completed": len(self.reloads),
                    "restarts": list(self.restarts), "reloads": list(self.reloads)}
