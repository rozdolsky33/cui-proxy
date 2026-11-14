"""Persistent-environment phase execution; callbacks own actual protocol work."""

from concurrent.futures import ThreadPoolExecutor
import copy
import threading
import time

from soak_journal import SoakJournal


class PhaseFailure(RuntimeError):
    def __init__(self, report):
        super().__init__("soak_phase_failed")
        self.report = report


def run_phase(execute, phase, *, duration=7200, clock=time.monotonic,
              record=lambda event: None, stop=None):
    journal = SoakJournal(phase, duration)
    external_stop = stop if stop is not None else threading.Event()
    stop = threading.Event()
    started = clock()
    results, failures = [], []
    evidence_lock = threading.Lock()

    def emit(event):
        with evidence_lock:
            record(copy.deepcopy(event))

    def worker(client):
        while not stop.is_set() and not external_stop.is_set():
            plan = journal.claim(client, lambda: clock() - started)
            if plan is None:
                return
            result = {"outcome": "harness_failure", "completed": False}
            failure = None
            try:
                emit(dict(plan, event="assigned"))
                result = execute(copy.deepcopy(plan))
                if not isinstance(result, dict) or not isinstance(result.get("outcome"), str):
                    raise ValueError("soak_phase_result")
                result = copy.deepcopy(result)
            except Exception:
                result = {"outcome": "harness_failure", "completed": False}
                failure = "execution_or_record_failure"
                stop.set()
            finally:
                journal.complete(plan["identity"])
                row = {"identity": plan["identity"], "request": result}
                with evidence_lock:
                    results.append(row)
                    if failure is not None:
                        failures.append(failure)
                try:
                    emit(dict(row, event="completed"))
                except Exception:
                    with evidence_lock:
                        failures.append("record_failure")
                    stop.set()

    executor = ThreadPoolExecutor(max_workers=4)
    try:
        futures = [executor.submit(worker, client) for client in range(4)]
        for future in futures:
            future.result()
    finally:
        # Set stop before waiting so interrupt/error cleanup cannot keep scheduling.
        executor_stop = stop.is_set() or external_stop.is_set()
        stop.set()
        executor.shutdown(wait=True, cancel_futures=True)
    elapsed = clock() - started
    report = dict(phase=phase, duration_s=duration, elapsed_s=elapsed,
                  completed=not executor_stop and not failures and elapsed >= duration,
                  schedule=journal.snapshot(), results=results, failures=failures)
    if failures:
        raise PhaseFailure(report)
    return report
