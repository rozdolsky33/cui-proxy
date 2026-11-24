"""Value-free resource evidence and equivalent idle GC observations."""
import math
import time
import threading
from soak_resource_series import EPSILON

RUNTIME_FIELDS = ("heap_objects_bytes", "live_heap_bytes", "goroutines", "gc_cycles")


def _integer(value):
    return type(value) is int and value >= 0


def _seconds(value):
    if type(value) not in (int, float) or not math.isfinite(value) or value <= 0:
        raise ValueError("invalid observation deadline")
    return value


def resource_metadata(sample):
    metrics = sample.get("metrics")
    metrics = metrics if isinstance(metrics, dict) else {}
    runtime = metrics.get("runtime")
    runtime = runtime if isinstance(runtime, dict) else {}
    runtime_available = runtime.get("available") is True and all(_integer(runtime.get(key)) for key in RUNTIME_FIELDS)
    row = {key: runtime.get(key) if runtime_available else None for key in RUNTIME_FIELDS}
    row.update(runtime_available=runtime_available)
    for key in ("proxy_rss_bytes", "child_rss_bytes"):
        row[key] = sample.get(key) if _integer(sample.get(key)) else None
    row["rss_available"] = all(row[key] is not None for key in ("proxy_rss_bytes", "child_rss_bytes"))
    row["active_requests"] = metrics.get("active_requests") if _integer(metrics.get("active_requests")) else None
    row["activity_available"] = row["active_requests"] is not None
    return row


def _observation(metrics, started, clock, record, event):
    row = resource_metadata({"metrics": metrics()})
    row.update(event=event, elapsed_s=clock() - started)
    record(row)
    if not row["runtime_available"] or not row["activity_available"]:
        raise ValueError("idle runtime observation unavailable")
    return row


def wait_for_idle(metrics, *, baseline=None, timeout=60, clock=time.monotonic, sleep=time.sleep, record=lambda row: None):
    _seconds(timeout)
    if baseline is not None and not _integer(baseline): raise ValueError("invalid idle baseline")
    started = clock()
    while True:
        row = _observation(metrics, started, clock, record, "idle_observation")
        if row["elapsed_s"] > timeout: raise TimeoutError("idle deadline exceeded")
        if row["active_requests"] == 0 and (baseline is None or row["goroutines"] <= baseline + 5):
            return row
        remaining = timeout - (clock() - started)
        if remaining <= 0: raise TimeoutError("idle deadline exceeded")
        sleep(min(1, remaining))


def observe_idle_gc(metrics, *, timeout=300, clock=time.monotonic, sleep=time.sleep, record=lambda row: None):
    """Wait for two real GC cycle advances with no load; never force/relabel GC.

    Use this identical procedure for the pre-load and post-drain heap samples.
    The independent 60-second goroutine deadline is checked by wait_for_idle.
    """
    _seconds(timeout)
    started = clock()
    initial = None
    previous = None
    while True:
        row = _observation(metrics, started, clock, record, "idle_gc_observation")
        if row["elapsed_s"] > timeout: raise TimeoutError("idle GC deadline exceeded")
        if row["active_requests"] != 0: raise ValueError("load during idle GC observation")
        if initial is None: initial = row["gc_cycles"]
        if previous is not None and row["gc_cycles"] < previous: raise ValueError("GC counter reset during observation")
        previous = row["gc_cycles"]
        if row["gc_cycles"] - initial >= 2:
            row.update(procedure="two_observed_idle_gc_cycles", initial_gc_cycles=initial, cycles_observed=row["gc_cycles"] - initial)
            return row
        remaining = timeout - (clock() - started)
        if remaining <= 0: raise TimeoutError("idle GC deadline exceeded")
        sleep(min(1, remaining))


def next_slot(slot, elapsed, interval):
    # Late polls catch up with a new real observation, retaining its actual bounds.
    return slot + 1


def measure_resources(env):
    """Reuse the existing bounded process-tree/RSS and metrics collector."""
    from performance import ResourceSampler
    sampler = ResourceSampler(env)
    sampler.started = time.monotonic()
    def measure():
        sampler.sample()
        return sampler.samples.pop()
    return measure


class ResourceMonitor:
    def __init__(self, measure, record, *, phase_stop=None, interval=1, clock=time.monotonic):
        self.interval = _seconds(interval)
        self.measure, self.record, self.clock = measure, record, clock
        self.phase_stop = phase_stop
        self.stop = threading.Event()
        self.failure = None
        self.thread = None
        self.started = None
        self.samples = 0

    def _take(self, slot):
        observation_started = self.clock()
        error_kind = None
        try:
            sample = self.measure()
            row = resource_metadata(sample)
        except Exception as error:
            row = resource_metadata({})
            error_kind = type(error).__name__
        observed = self.clock()
        if (type(observation_started) not in (int, float) or not math.isfinite(observation_started)
                or type(observed) not in (int, float) or not math.isfinite(observed)
                or observation_started < self.started or observed < observation_started):
            self.failure = ValueError("resource observation clock")
            if self.phase_stop is not None:
                self.phase_stop.set()
            return False
        row.update(event="resource", slot=slot, elapsed_s=observed - self.started)
        row["observation_started_s"] = observation_started
        row["observed_s"] = observed
        # Qualification resource slots are one second. Enforce the same existing
        # series start bound here, retaining the actual invalid observation.
        if self.interval == 1 and not -EPSILON <= observation_started - (self.started + slot) < 1 + EPSILON:
            self.failure = ValueError("resource_series_clock_origin")
            error_kind = "resource_series_clock_origin"
            self.stop.set()
            if self.phase_stop is not None:
                self.phase_stop.set()
        if error_kind is not None: row["sample_error"] = error_kind
        try:
            self.record(row)
        except BaseException as error:
            self.failure = error
            self.stop.set()
            if self.phase_stop is not None: self.phase_stop.set()
            raise
        self.samples += 1
        return row

    def raise_if_failed(self):
        if self.failure is not None:
            raise RuntimeError("resource monitor failed") from self.failure

    def __enter__(self):
        self.started = self.clock()
        self._take(0)
        def run():
            slot = next_slot(0, self.clock() - self.started, self.interval)
            try:
                while not self.stop.wait(max(0, self.started + slot * self.interval - self.clock())):
                    self._take(slot)
                    slot = next_slot(slot, self.clock() - self.started, self.interval)
            except BaseException as error:
                self.failure = error
                self.stop.set()
                if self.phase_stop is not None: self.phase_stop.set()
        self.thread = threading.Thread(target=run, name="soak-resource-monitor", daemon=True)
        self.thread.start()
        return self

    def __exit__(self, *args):
        self.stop.set()
        if self.thread is not None:
            self.thread.join(timeout=10)
            if self.thread.is_alive(): raise ValueError("resource monitor did not stop")
        if self.failure is not None: raise ValueError("resource evidence recording failed") from self.failure
