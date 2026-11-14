"""Metadata-only closed-loop assignment and exact bypass replay."""

import copy
import math
import threading

from soak_schedule import churn_due_indices, plan_request


def _time(value):
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) or value < 0:
        raise ValueError("soak_journal_time")
    return value


class SoakJournal:
    def __init__(self, phase, duration=7200):
        plan_request(0, 0, phase)
        if _time(duration) == 0:
            raise ValueError("soak_journal_duration")
        self.phase, self.duration = phase, duration
        self.serials = [0] * 4
        self.active = {}
        self.assigned = 0
        self.last_elapsed = 0
        self.rows = []
        self.lock = threading.Lock()

    def claim(self, client, elapsed):
        # Workers pass a clock callback so timestamp order follows lock order.
        with self.lock:
            if callable(elapsed):
                elapsed = elapsed()
            _time(elapsed)
            plan_request(client, 0, self.phase)
            if elapsed < self.last_elapsed or client in self.active:
                raise ValueError("soak_journal_order")
            self.last_elapsed = elapsed
            if elapsed >= self.duration:
                return None
            row = plan_request(client, self.serials[client], self.phase)
            indices = []
            if self.phase == "churn" and row["category"] == "text":
                indices = list(churn_due_indices(elapsed, self.assigned, 40))
            row.update(elapsed_s=elapsed, churn_indices=indices)
            self.assigned += len(indices)
            self.serials[client] += 1
            self.active[client] = row["identity"]
            self.rows.append(row)
            return copy.deepcopy(row)

    def complete(self, identity):
        with self.lock:
            if not isinstance(identity, (tuple, list)) or len(identity) != 3:
                raise ValueError("soak_journal_completion")
            phase, client, serial = identity
            plan_request(client, serial, phase)
            if self.active.get(client) != tuple(identity):
                raise ValueError("soak_journal_completion")
            del self.active[client]

    def snapshot(self):
        with self.lock:
            if self.active:
                raise ValueError("soak_journal_inflight")
            return copy.deepcopy(self.rows)


def replay_requests(records, phase, duration=7200):
    """Validate assignment history and group it without rescheduling churn."""
    journal = SoakJournal(phase, duration)
    clients = [[] for _ in range(4)]
    if not isinstance(records, list):
        raise ValueError("soak_journal_replay")
    for row in records:
        if not isinstance(row, dict) or set(row) != {"identity", "category", "scenario", "size_bytes", "elapsed_s", "churn_indices"}:
            raise ValueError("soak_journal_replay")
        identity = row["identity"]
        if not isinstance(identity, (tuple, list)) or len(identity) != 3:
            raise ValueError("soak_journal_replay")
        if not isinstance(row["churn_indices"], list) or any(type(i) is not int for i in row["churn_indices"]):
            raise ValueError("soak_journal_replay")
        normalized = dict(row, identity=tuple(identity))
        expected = journal.claim(identity[1], row["elapsed_s"])
        if normalized != expected:
            raise ValueError("soak_journal_replay")
        journal.complete(identity)
        clients[identity[1]].append(copy.deepcopy(normalized))
    return clients
