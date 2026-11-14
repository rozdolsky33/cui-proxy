"""Numeric-only, report-only comparisons of a fixed Go microbenchmark."""

import math
import re
import statistics

METRICS = ("ns_per_op", "bytes_per_op", "allocations_per_op")
ITERATIONS = 10
REPETITIONS = 5
_LINE = re.compile(
    r"BenchmarkRequestWalk-[1-9][0-9]*\s+([0-9]+)\s+(\S+)\s+ns/op"
    r"\s+([0-9]+)\s+B/op\s+([0-9]+)\s+allocs/op"
)


def _validate_sample(sample):
    if not isinstance(sample, dict) or set(sample) != {"iterations", *METRICS}:
        raise ValueError("benchmark_sample_shape")
    if type(sample["iterations"]) is not int or sample["iterations"] != ITERATIONS:
        raise ValueError("benchmark_iteration_count")
    for metric in METRICS:
        value = sample[metric]
        if type(value) not in (int, float):
            raise ValueError("benchmark_metric_type")
        try:
            finite = math.isfinite(value)
        except OverflowError:
            finite = False
        if not finite or value < 0 or (metric == "ns_per_op" and value == 0):
            raise ValueError("benchmark_metric_range")
        if metric != "ns_per_op" and type(value) is not int:
            raise ValueError("benchmark_allocation_type")


def parse_output(text, iterations=ITERATIONS):
    """Accept exactly one fixed-work sample; never return captured raw text."""
    if not isinstance(text, str) or type(iterations) is not int or iterations != ITERATIONS:
        raise ValueError("benchmark_parse_arguments")
    sample = None
    for line in text.splitlines():
        line = line.strip()
        if not line or line == "PASS" or line.startswith(("goos:", "goarch:", "pkg:", "cpu:")):
            continue
        match = _LINE.fullmatch(line)
        if match is None or sample is not None:
            raise ValueError("benchmark_output_shape")
        try:
            sample = dict(iterations=int(match[1]), ns_per_op=float(match[2]),
                          bytes_per_op=int(match[3]), allocations_per_op=int(match[4]))
        except (ValueError, OverflowError) as error:
            raise ValueError("benchmark_output_numeric") from error
        _validate_sample(sample)
    if sample is None:
        raise ValueError("benchmark_sample_missing")
    return sample


def compare_samples(base, candidate):
    """Report medians and signed changes; timing regressions never become failure."""
    for arm in (base, candidate):
        if not isinstance(arm, (list, tuple)) or len(arm) != REPETITIONS:
            raise ValueError("benchmark_repetition_count")
        for sample in arm:
            _validate_sample(sample)
    before = {key: statistics.median(row[key] for row in base) for key in METRICS}
    after = {key: statistics.median(row[key] for row in candidate) for key in METRICS}
    absolute = {key: after[key] - before[key] for key in METRICS}
    relative = {}
    for key in METRICS:
        if before[key] == 0:
            relative[key] = 0.0 if after[key] == 0 else None
        else:
            relative[key] = absolute[key] / before[key] * 100
            if not math.isfinite(relative[key]):
                raise ValueError("benchmark_change_range")
    return dict(report_only=True, sample_count=REPETITIONS, base=before,
                candidate=after, relative_change_percent=relative,
                absolute_change=absolute)


def run_comparison(base_binary, candidate_binary, base_commit, candidate_commit,
                   report_path, capture_directory):
    return None
