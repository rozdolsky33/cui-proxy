"""Keep lifecycle dependencies in memory; expose only counts in reports."""

import copy
import threading

from soak_lifecycle import lifecycle_step


class LifecycleState:
    def __init__(self):
        self.workflows = {}
        self.lock = threading.Lock()

    def _position(self, plan):
        step = lifecycle_step(plan)
        scenario = plan["scenario"]
        if scenario not in ("W07", "W09", "W10"):
            raise ValueError("soak_lifecycle_dependency")
        cycle = plan["identity"][2] // 50
        length = {"W07": 5, "W09": 3, "W10": None}[scenario]
        index = cycle if length is None else cycle % length
        return step["workflow"], index, length

    def begin(self, plan):
        key, index, _ = self._position(plan)
        with self.lock:
            state = self.workflows.get(key)
            if state is None:
                if index != 0:
                    raise ValueError("soak_lifecycle_dependency")
                state = {"contexts": [], "active": None, "failed": False}
                self.workflows[key] = state
            if state["failed"] or state["active"] is not None or len(state["contexts"]) != index:
                raise ValueError("soak_lifecycle_dependency")
            state["active"] = tuple(plan["identity"])
            return copy.deepcopy(state["contexts"])

    def finish(self, plan, row, context):
        key, _, _ = self._position(plan)
        with self.lock:
            state = self.workflows.get(key)
            if state is None or state["active"] != tuple(plan["identity"]):
                raise ValueError("soak_lifecycle_dependency")
            verified = row.get("outcome") == "supported_success" and all(
                row.get(field) is True for field in ("completed", "semantic_verified", "boundary_verified"))
            state["active"] = None
            if not verified:
                state["failed"] = True
                return False
            state["contexts"].append(copy.deepcopy(context))
            return True

    def summary(self):
        with self.lock:
            counts = {"W07": 0, "W09": 0}
            continuations = 0
            for key, state in self.workflows.items():
                if state["failed"] or state["active"] is not None:
                    continue
                scenario = key[2]
                if scenario == "W10":
                    continuations += max(0, len(state["contexts"]) - 1)
                elif len(state["contexts"]) == {"W07": 5, "W09": 3}[scenario]:
                    counts[scenario] += 1
            return {"completed_workflows": counts, "continuations": continuations,
                    "failed_workflows": sum(state["failed"] for state in self.workflows.values())}
