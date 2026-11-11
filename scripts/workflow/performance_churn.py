"""Deterministic, unqualified mapping-growth workload construction.

This module only constructs cases and offered-load offsets. It does not run
traffic, resolve boundary checks, or establish performance qualification.
"""
import math

from performance_workload import Workload


class ChurnWorkload:
    def __init__(self, seed=1102026):
        self.initial = Workload("normal", seed)
        expanded = Workload("vault", seed)
        count = len(self.initial.pairs)
        if (expanded.pairs[:count] != self.initial.pairs
                or expanded.rules != self.initial.rules
                or expanded.dictionary != self.initial.dictionary):
            raise ValueError("churn_population_identity")
        self._fresh = [tuple(pair) for pair in expanded.pairs[count:]]
        initial = {tuple(pair) for pair in self.initial.pairs}
        if (len(set(self._fresh)) != len(self._fresh)
                or initial.intersection(self._fresh)
                or {p[0] for p in self._fresh} != {p[0] for p in initial}):
            raise ValueError("churn_population_disjointness")

    def new_pair(self, index):
        if type(index) is not int or not 0 <= index < len(self._fresh):
            raise ValueError("churn_pair_index")
        return self._fresh[index]

    def case(self, index, size):
        pair = self.new_pair(index)
        case = self.initial.case(index, size)
        old = case["occurrences"][0]
        if not any(p[0] == pair[0] and p[1] == old for p in self.initial.pairs):
            raise ValueError("churn_pair_class")
        original = case["body"]
        changed = original.replace(old.encode(), pair[1].encode())
        if changed == original or any(
                changed.count(value.encode()) != original.count(value.encode())
                for value in case["near_misses"]):
            raise ValueError("churn_negative_controls")
        if abs(len(changed) - size) > size * .01:
            raise ValueError("churn_body_size")
        return dict(case, body=changed, occurrences=[
            pair[1] if value == old else value for value in case["occurrences"]])


def scheduled_offsets(duration):
    """Offer exactly 100 new pairs/minute, excluding the window end."""
    if (type(duration) not in (int, float) or not math.isfinite(duration)
            or not 0 < duration <= 54000):
        raise ValueError("churn_window")
    return [index * 3 / 5 for index in range(math.ceil(duration * 5 / 3))
            if index * 3 / 5 < duration]


def finalize_boundary(row, case, mappings):
    """Resolve provisional evidence without promoting any earlier failure."""
    import hashlib
    if row.get("outcome") != "boundary_pending":
        return dict(row)
    result = dict(row)
    expected = case["body"]
    for value in dict.fromkeys(case["occurrences"]):
        replacement = mappings.get(value)
        if not isinstance(replacement, str) or not replacement or replacement == value:
            result["outcome"] = "boundary_evidence_missing"
            return result
        expected = expected.replace(value.encode(), replacement.encode())
    result["outcome"] = ("supported_success" if hashlib.sha256(expected).hexdigest()
                         == row.get("boundary_sha256") else "transformation_error")
    return result


def resolve_records(rows, workload, size, mappings):
    """Keep attempted outcomes and distinguish retained from verified growth."""
    seen, observed, verified = set(), set(), set()
    resolved = []
    unresolved = 0
    for row in rows:
        index = row.get("churn_index")
        if index is None:
            unresolved += 1
            retained = dict(row)
            if retained.get("outcome") == "boundary_pending":
                retained["outcome"] = "boundary_evidence_missing"
            resolved.append(retained)
            continue
        pair = workload.new_pair(index)
        if index in seen:
            raise ValueError("churn_duplicate_index")
        seen.add(index)
        replacement = mappings.get(pair[1])
        if isinstance(replacement, str) and replacement and replacement != pair[1]:
            observed.add(index)
        result = finalize_boundary(row, workload.case(index, size), mappings)
        if result.get("outcome") == "supported_success" and index in observed:
            verified.add(index)
        resolved.append(result)
    return dict(records=resolved, observed_new_pairs=len(observed),
                verified_new_pairs=len(verified), unresolved_indices=unresolved)


def inspect_vault(directory, seed_binary):
    """Read a new private snapshot; never create or seed a missing vault."""
    import json
    import os
    from pathlib import Path
    import stat
    import subprocess
    output = Path(directory) / "observed.json"
    if os.path.lexists(output):
        raise ValueError("churn_observation_exists")
    try:
        result = subprocess.run([str(seed_binary), "inspect", str(directory)],
                                capture_output=True, timeout=60)
        if result.returncode:
            raise ValueError("churn_observation_command")
        descriptor = os.open(output, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(descriptor, "rb") as stream:
            info = os.fstat(stream.fileno())
            if (not stat.S_ISREG(info.st_mode) or info.st_size > 16 * 1024 * 1024
                    or info.st_mode & 0o077):
                raise ValueError("churn_observation_file")
            def unique_pairs(pairs):
                value = {}
                for key, item in pairs:
                    if key in value:
                        raise ValueError("churn_observation_duplicate")
                    value[key] = item
                return value
            data = json.loads(stream.read(16 * 1024 * 1024 + 1), object_pairs_hook=unique_pairs)
    except (OSError, subprocess.SubprocessError, json.JSONDecodeError) as error:
        raise ValueError("churn_observation_unavailable") from error
    if not isinstance(data, dict) or set(data) != {"population", "mappings"}:
        raise ValueError("churn_observation_schema")
    population, mappings = data["population"], data["mappings"]
    if (type(population) is not int or population < 0 or not isinstance(mappings, dict)
            or population != len(mappings)
            or any(not isinstance(key, str) or not key or not isinstance(value, str)
                   or not value or key == value for key, value in mappings.items())):
        raise ValueError("churn_observation_population")
    return data


class ChurnPhaseFailure(RuntimeError):
    def __init__(self, report):
        super().__init__("churn_phase_failed")
        self.report = report


def run_phase(binary, seed_binary, workload, *, size=1048576, duration=600,
              bypass=False, warmup_seconds=60, warmup_count=100, clock=None):
    """Run isolated growth traffic; retain incomplete evidence on every failure."""
    import hashlib
    import time
    import performance
    import performance_load
    offsets = scheduled_offsets(duration)
    if (type(warmup_seconds) not in (int, float) or not math.isfinite(warmup_seconds)
            or warmup_seconds < 0 or type(warmup_count) is not int or warmup_count < 1
            or type(bypass) is not bool):
        raise ValueError("churn_settings")
    # Validate size and population before opening any backend or private state.
    workload.case(0, size)
    clock = clock or time.monotonic
    report = dict(mode="bypass" if bypass else "proxy", performance_qualified=False,
                  size_bytes=size, clients=4, rate=100 / 60, planned_pairs=len(offsets),
                  window_seconds=duration, records=[], unsent_records=[],
                  warmup_completed=0, warmup_seconds=0, observed_new_pairs=None,
                  verified_new_pairs=None, final_vault_population=None,
                  initial_vault_population=None, resource_samples=[])
    measured = []
    try:
        with performance.Environment(binary, seed_binary, workload.initial, size) as env:
            with performance.ResourceSampler(env) as sampler:
                warm_started = clock()
                warm = []
                def warm_operation(client, serial):
                    case = workload.initial.case(len(warm) + client + serial * 4, size)
                    expected = case["body"]
                    if not bypass:
                        for original in dict.fromkeys(case["occurrences"]):
                            expected = expected.replace(original.encode(), env.mappings[original].encode())
                    return performance.request(env.upstream_url if bypass else env.url,
                                               case, hashlib.sha256(expected).hexdigest())
                while len(warm) < warmup_count or clock() - warm_started < warmup_seconds:
                    batch, _ = performance.closed_loop(warm_operation, 4, per_client=1)
                    warm.extend(batch)
                    report["warmup_completed"] = sum(r.get("outcome") == "supported_success" for r in warm)
                    report["warmup_seconds"] = clock() - warm_started
                    if any(r.get("outcome") != "supported_success" for r in warm):
                        report["warmup_records"] = warm
                        raise ValueError("churn_warmup")
                report["initial_vault_population"] = env.vault_count()
                report["initial_metrics"] = env.metrics()
                def operation(index):
                    started = clock()
                    retained = dict(churn_index=index, operation_id="churn-" + str(index),
                                    supported=True, completed=False, outcome="unresolved",
                                    started_s=started, duration_ms=0)
                    measured.append(retained)
                    try:
                        case = workload.case(index, size)
                        retained.update(performance.request(
                            env.upstream_url if bypass else env.url, case,
                            hashlib.sha256(case["body"]).hexdigest() if bypass else None,
                            defer_boundary=not bypass))
                    except Exception:
                        retained.update(outcome="client_or_model_failure",
                                        finished_s=clock(), duration_ms=(clock() - started) * 1000)
                    return retained
                report.update(performance_load.open_loop(operation, 100 / 60, duration, 4))
                if not bypass:
                    observation = inspect_vault(env.directory, seed_binary)
                    report.update(resolve_records(report["records"], workload, size, observation["mappings"]))
                    report["final_vault_population"] = observation["population"]
                else:
                    report["final_vault_population"] = env.vault_count()
                report["final_metrics"] = env.metrics()
                report["resource_samples"] = list(sampler.samples)
                report.update(performance.resource_summary(sampler.samples))
                if any(row.get("outcome") != "supported_success" for row in report["records"]):
                    raise ValueError("churn_operation_failure")
        return report
    except BaseException as error:
        if not report["records"]:
            report["records"] = [dict(row) for row in measured]
        report["error_kind"] = "churn_phase_failed"
        raise ChurnPhaseFailure(report) from error


def main(argv=None):
    import argparse
    import sys
    import performance
    from extraction_performance import reserve_report, write_report
    from performance_stats import summarize_outcomes
    parser = argparse.ArgumentParser(description="Isolated mapping-growth measurements (unqualified)")
    parser.add_argument("--binary", required=True)
    parser.add_argument("--seed-binary", required=True)
    parser.add_argument("--report", required=True)
    parser.add_argument("--duration", type=float, default=600)
    parser.add_argument("--size", type=int, default=1048576)
    parser.add_argument("--warmup-seconds", type=float, default=60)
    parser.add_argument("--warmup-count", type=int, default=100)
    parser.add_argument("--repetitions", type=int, default=3)
    parser.add_argument("--seed", type=int, default=1102026)
    args = parser.parse_args(argv)
    try:
        scheduled_offsets(args.duration)
        if (args.repetitions < 1 or args.warmup_count < 1
                or not math.isfinite(args.warmup_seconds) or args.warmup_seconds < 0):
            raise ValueError("churn_cli_settings")
        workload = ChurnWorkload(args.seed)
        workload.case(0, args.size)
        reserve_report(args.report)
    except (OSError, ValueError):
        print("churn setup or report destination unavailable", file=sys.stderr)
        return 1
    report = dict(schema_version=1, performance_qualified=False,
                  workload=workload.initial.metadata(), measurements=[],
                  settings=dict(workload_kind="mapping_churn", seed=args.seed,
                                size_bytes=args.size, clients=4, offered_rate=100 / 60,
                                requested_duration_s=args.duration, repetitions=args.repetitions,
                                warmup_seconds=args.warmup_seconds, warmup_count=args.warmup_count),
                  unmet_requirements=["exploratory growth evidence is not normal-tier qualification"])
    code = 0
    try:
        report["environment"] = performance.environment_info(args.binary)
        for repetition in range(args.repetitions):
            for bypass in ((False, True) if repetition % 2 == 0 else (True, False)):
                try:
                    phase = run_phase(args.binary, args.seed_binary, workload, size=args.size,
                                      duration=args.duration, bypass=bypass,
                                      warmup_seconds=args.warmup_seconds, warmup_count=args.warmup_count)
                except ChurnPhaseFailure as error:
                    phase = error.report
                    phase["repetition"] = repetition
                    report["measurements"].append(phase)
                    phase.update(summarize_outcomes(phase["records"]))
                    raise
                phase["repetition"] = repetition
                report["measurements"].append(phase)
                phase.update(summarize_outcomes(phase["records"]))
    except BaseException:
        report["setup_failure"] = "churn_run_failed"
        code = 1
    try:
        write_report(args.report, report)
    except (OSError, ValueError):
        print("churn report persistence failed", file=sys.stderr)
        return 1
    print("churn measurements retained; performance remains unqualified")
    return code


if __name__ == "__main__":
    raise SystemExit(main())
