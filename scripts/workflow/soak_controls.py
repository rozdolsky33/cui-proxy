"""Scheduled under-load controls without adding hidden workload requests."""
import math
import threading
import time

from soak_lifecycle import lifecycle_step
from soak_recovery import restart_proxy


def control_times(duration, count=10):
    if type(duration) not in (int, float) or not math.isfinite(duration) or duration <= 0 or type(count) is not int or count < 1:
        raise ValueError("invalid control schedule")
    return [duration * (index + 1) / (count + 1) for index in range(count)]


def _times(values):
    result = tuple(values)
    if any(type(value) not in (int, float) or not math.isfinite(value) or value < 0 for value in result) or any(a >= b for a, b in zip(result, result[1:])):
        raise ValueError("invalid control schedule")
    return result


class ControlledExecutor:
    def __init__(self, execute, env, reloader, record, *, restart_times, reload_times, clock=time.monotonic, before_restart=lambda: None):
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

    def _invoke(self, plan, *, admitted=False):
        if not admitted:
            with self.condition: self.active += 1
        try: return self.execute(plan)
        finally:
            with self.condition:
                self.active -= 1
                self.condition.notify_all()

    def _select(self, plan):
        with self.condition:
            if not self.condition.wait_for(lambda: not self.restarting, timeout=45):
                raise TimeoutError("restart admission wait exceeded")
            if self.failed: raise ValueError("control executor unavailable")
            if self.changing:
                self.active += 1
                return None
            elapsed = self.clock() - self.started
            restart_due = len(self.restarts) < len(self.restart_times) and self.restart_times[len(self.restarts)] <= elapsed
            continuation = plan.get("scenario") == "W10" and lifecycle_step(plan)["stage"] == "continue"
            if restart_due and continuation and self.active > 0:
                self.changing = self.restarting = True
                return "restart"
            reload_due = len(self.reloads) < len(self.reload_times) and self.reload_times[len(self.reloads)] <= elapsed
            if reload_due:
                self.changing = True
                return "reload"
            self.active += 1
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
                evidence = restart_proxy(self.env, index, continuation=continuation)
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

    def summary(self):
        with self.condition:
            return {"clock_origin_s": self.started, "failed": self.failed, "restarts_planned": len(self.restart_times), "reloads_planned": len(self.reload_times),
                    "restarts_completed": len(self.restarts), "reloads_completed": len(self.reloads),
                    "restarts": list(self.restarts), "reloads": list(self.reloads)}
