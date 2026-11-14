"""Lifecycle workflow steps occupy existing request slots, preserving mix."""

from soak_schedule import plan_request

_STAGES = {
    "W07": ("parent_dispatch", "child_0", "child_1", "child_2", "parent_collect"),
    "W09": ("history", "resume", "compaction"),
    "W11": ("blocked", "unsupported", "malformed_json"),
    "W12": ("upstream_error", "malformed_success", "cancel"),
}


def lifecycle_step(plan):
    phase, client, serial = plan["identity"]
    expected = plan_request(client, serial, phase)
    if expected["category"] != "lifecycle" or any(plan.get(key) != value for key, value in expected.items() if key != "identity"):
        raise ValueError("soak_lifecycle_schedule")
    scenario = expected["scenario"]
    cycle = serial // 50
    if scenario == "W10":
        stage, workflow = ("establish" if cycle == 0 else "continue"), 0
    else:
        stages = _STAGES[scenario]
        stage, workflow = stages[cycle % len(stages)], cycle // len(stages)
    return {"stage": stage, "workflow": (phase, client, scenario, workflow)}
