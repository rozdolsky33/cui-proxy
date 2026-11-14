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

    def sample(self):
        if self.failure is not None:
            return
        try:
            errors = self.errors
            super().sample()
            now = self.clock()
            if type(now) not in (int, float) or not math.isfinite(now):
                raise ValueError("invalid stage observation clock")
            if self.errors != errors:
                self.record_evidence({"event": "stage_poll_error", "observed_s": now,
                                      "poll_errors": self.errors})
            for key, row in self._records.items():
                if key not in self.observed:
                    self.record_evidence({"event": "request_stages", "observed_s": now,
                                          "request": row})
                    self.observed[key] = now
                row["observed_s"] = self.observed[key]
        except BaseException as error:
            self.failure = error
            self.phase_stop.set()

    def __exit__(self, *args):
        result = super().__exit__(*args)
        if self.failure is not None and not args[0]:
            raise RuntimeError("stage evidence collection failed") from self.failure
        return result
