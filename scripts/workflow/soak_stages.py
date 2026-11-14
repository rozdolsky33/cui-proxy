"""Durable monotonic observations around the existing bounded stage poller."""
import math
import time

from performance_load import StageCollector


class SoakStageCollector(StageCollector):
    def __init__(self, env, warm_sessions, *, record, phase_stop, clock=time.monotonic):
        super().__init__(env, warm_sessions)
        self.record_evidence = record
        self.phase_stop = phase_stop
        self.clock = clock
        self.observed = {}
        self.failure = None
        self.previous_success_start = None
        self.last_poll_end = None

    def sample(self):
        if self.failure is not None:
            return
        try:
            started = self.clock()
            if (type(started) not in (int, float) or not math.isfinite(started)
                    or self.last_poll_end is not None and started < self.last_poll_end):
                raise ValueError("invalid stage observation clock")
            errors = self.errors
            super().sample()
            now = self.clock()
            if type(now) not in (int, float) or not math.isfinite(now) or now < started:
                raise ValueError("invalid stage observation clock")
            if self.errors != errors:
                self.record_evidence({"event": "stage_poll_error", "observed_s": now,
                                      "poll_errors": self.errors})
            for key, row in self._records.items():
                if key not in self.observed:
                    self.record_evidence({"event": "request_stages", "observed_s": now,
                                          "observation_lower_s": self.previous_success_start,
                                          "request": row})
                    self.observed[key] = (self.previous_success_start, now)
                row["observation_lower_s"], row["observed_s"] = self.observed[key]
            if self.errors == errors:
                self.previous_success_start = started
            self.last_poll_end = now
        except BaseException as error:
            self.failure = error
            self.phase_stop.set()

    def __exit__(self, *args):
        result = super().__exit__(*args)
        if self.failure is not None and not args[0]:
            raise RuntimeError("stage evidence collection failed") from self.failure
        return result
