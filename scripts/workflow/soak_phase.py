"""Persistent-environment phase execution; callbacks own actual protocol work."""
import math
from fractions import Fraction

from concurrent.futures import ThreadPoolExecutor
import copy
import threading
import time

from soak_journal import SoakJournal, replay_requests


class OperationFailure(ValueError):
    """Stop scheduling while preserving the observed request metadata."""
    def __init__(self, result):
        super().__init__("scenario verification failed")
        self.result = dict(result)


class PhaseFailure(RuntimeError):
    def __init__(self, report):
        super().__init__("soak_phase_failed")
        self.report = report


def run_phase(execute, phase, *, duration=7200, clock=time.monotonic,
              record=lambda event: None, stop=None, drain_seconds=0):
    journal = SoakJournal(phase, duration, drain_seconds=drain_seconds)
    started = clock()
    return _execute_clients(
        execute, phase, duration, clock, record, stop, started,
        lambda client: journal.claim(client, lambda: clock() - started),
        journal.complete, journal.snapshot, replay=False, drain_seconds=drain_seconds)


def run_replay(execute, records, phase, *, duration=7200, clock=time.monotonic,
               record=lambda event: None, stop=None, drain_seconds=0, wait=None):
    clients = replay_requests(records, phase, duration, drain_seconds=drain_seconds)
    if any(not rows for rows in clients):
        raise ValueError("soak_replay_missing_client")
    pending = [iter(rows) for rows in clients]
    schedule = copy.deepcopy(records)
    return _execute_clients(
        execute, phase, duration, clock, record, stop, clock(),
        lambda client: next(pending[client], None), lambda identity: None,
        lambda: schedule, replay=True, drain_seconds=drain_seconds, wait=wait)


def _execute_clients(execute, phase, duration, clock, record, stop, started,
                     next_plan, complete, snapshot, *, replay, drain_seconds=0, wait=None):
    external_stop = stop if stop is not None else threading.Event()
    stop = threading.Event()
    results, failures = [], []
    exhausted = set()
    evidence_lock = threading.Lock()

    def emit(event):
        with evidence_lock:
            record(copy.deepcopy(event))

    def wait_until(offset):
        while not stop.is_set() and not external_stop.is_set():
            remaining = started + offset - clock()
            if remaining <= 0:
                return True
            (wait or external_stop.wait)(min(remaining, 0.05))
        return False

    def worker(client):
        while not stop.is_set() and not external_stop.is_set():
            plan = next_plan(client)
            if plan is None:
                with evidence_lock:
                    exhausted.add(client)
                return
            result = {"outcome": "harness_failure", "completed": False}
            failure = None
            if replay and not wait_until(plan["elapsed_s"]):
                return
            try:
                emit(dict(plan, event="assigned"))
                result = execute(copy.deepcopy(plan))
                if not isinstance(result, dict) or not isinstance(result.get("outcome"), str):
                    raise ValueError("soak_phase_result")
                result = copy.deepcopy(result)
            except OperationFailure as error:
                result = error.result
                failure = 'execution_or_record_failure'
                stop.set()
            except Exception:
                result = {"outcome": "harness_failure", "completed": False}
                failure = "execution_or_record_failure"
                stop.set()
            finally:
                complete(plan["identity"])
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
        if replay:
            wait_until(duration)
    finally:
        # Set stop before waiting so interrupt/error cleanup cannot keep scheduling.
        executor_stop = stop.is_set() or external_stop.is_set()
        stop.set()
        executor.shutdown(wait=True, cancel_futures=True)
    elapsed = clock() - started
    report = dict(phase=phase, duration_s=duration, elapsed_s=elapsed,
                  completed=not executor_stop and not failures and len(exhausted) == 4
                  and (replay or elapsed >= duration), replay=replay,
                  schedule=snapshot(), results=results, failures=failures, started_s=started)
    if drain_seconds:
        due = math.ceil(Fraction(str(duration)) * 100 / 60)
        assigned = sum(len(row["churn_indices"]) for row in report["schedule"])
        report.update(churn_horizon_s=duration, drain_admission_seconds=drain_seconds,
                      churn_due_pairs=due, churn_assigned_pairs=assigned,
                      churn_drain_completed=assigned == due,
                      drain_requests=sum(row["drain"] for row in report["schedule"]),
                      elapsed_beyond_horizon_s=max(0, elapsed-duration) if not replay else None)
        if assigned != due:
            report["completed"] = False
            report["drain_failure"] = "churn_backlog"
    if failures or report.get("churn_drain_completed") is False:
        raise PhaseFailure(report)
    return report
