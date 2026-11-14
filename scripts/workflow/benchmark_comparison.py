"""Numeric-only, report-only comparisons of a fixed Go microbenchmark."""

import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import signal
import tempfile

from performance_campaign import execute_command
import re
import statistics

METRICS = ("ns_per_op", "bytes_per_op", "allocations_per_op")
ITERATIONS = 10
REPETITIONS = 5
_LINE = re.compile(
    r"BenchmarkRequestWalk-[1-9][0-9]*\s+([0-9]+)\s+(\S+)\s+ns/op"
    r"(?:\s+(\S+)\s+MB/s)?\s+([0-9]+)\s+B/op\s+([0-9]+)\s+allocs/op"
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
                          bytes_per_op=int(match[4]), allocations_per_op=int(match[5]))
            if match[3] is not None:
                throughput = float(match[3])
                if not math.isfinite(throughput) or throughput < 0:
                    raise ValueError("benchmark_throughput_range")
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


def _write_summary(path, report):
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(mode="w", dir=path.parent,
                                         prefix="." + path.name + ".", delete=False) as stream:
            temporary = Path(stream.name)
            json.dump(report, stream, allow_nan=False, sort_keys=True, indent=2)
        os.replace(temporary, path)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


def run_comparison(base_binary, candidate_binary, base_commit, candidate_commit,
                   report_path, capture_directory):
    for revision in (base_commit, candidate_commit):
        if not isinstance(revision, str) or re.fullmatch(r"(?:[0-9a-f]{40}|[0-9a-f]{64})", revision) is None:
            raise ValueError("benchmark_source_identity")
    binaries = {"base": Path(base_binary).resolve(strict=True),
                "candidate": Path(candidate_binary).resolve(strict=True)}
    identities = {arm: hashlib.sha256(path.read_bytes()).hexdigest()
                  for arm, path in binaries.items()}
    report_path = Path(report_path)
    capture_directory = Path(capture_directory)
    descriptor = os.open(report_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    os.close(descriptor)
    report = dict(schema_version=1, report_only=True, performance_qualified=False,
                  benchmark="BenchmarkRequestWalk", iterations=ITERATIONS,
                  repetitions=REPETITIONS, gomaxprocs=2,
                  status="incomplete", base_commit=base_commit, candidate_commit=candidate_commit,
                  base_binary_sha256=identities["base"],
                  candidate_binary_sha256=identities["candidate"], completed_invocations=0)
    samples = {"base": [], "candidate": []}
    try:
        _write_summary(report_path, report)
        capture_directory.mkdir(mode=0o700)
        for repetition in range(REPETITIONS):
            order = ("base", "candidate") if repetition % 2 == 0 else ("candidate", "base")
            for arm in order:
                for name, binary in binaries.items():
                    if hashlib.sha256(binary.read_bytes()).hexdigest() != identities[name]:
                        raise ValueError("benchmark_binary_changed")
                log = capture_directory / f"{repetition}-{arm}.log"
                command = [str(binaries[arm]), "-test.run=^$", "-test.bench=^BenchmarkRequestWalk$",
                           "-test.benchtime=10x", "-test.benchmem", "-test.count=1",
                           "-test.cpu=2", "-test.timeout=30s"]
                if execute_command(command, log) != 0:
                    raise RuntimeError("benchmark_command_failed")
                for name, binary in binaries.items():
                    if hashlib.sha256(binary.read_bytes()).hexdigest() != identities[name]:
                        raise ValueError("benchmark_binary_changed")
                samples[arm].append(parse_output(log.read_text()))
                report["completed_invocations"] += 1
                _write_summary(report_path, report)
        report["comparison"] = compare_samples(samples["base"], samples["candidate"])
        report["status"] = "complete"
        _write_summary(report_path, report)
        return report
    except BaseException as error:
        report["status"] = "interrupted" if isinstance(error, (KeyboardInterrupt, SystemExit)) else "failed"
        try:
            _write_summary(report_path, report)
        except OSError:
            pass
        raise


def main(argv=None):
    parser = argparse.ArgumentParser(description="Captured, report-only equal-work benchmark comparison")
    parser.add_argument("--base-binary", type=Path, required=True)
    parser.add_argument("--candidate-binary", type=Path, required=True)
    parser.add_argument("--base-commit", required=True)
    parser.add_argument("--candidate-commit", required=True)
    parser.add_argument("--report", type=Path, required=True)
    parser.add_argument("--captures", type=Path, required=True)
    args = parser.parse_args(argv)
    previous = signal.getsignal(signal.SIGTERM)
    def interrupted(signum, frame):
        raise KeyboardInterrupt
    signal.signal(signal.SIGTERM, interrupted)
    try:
        run_comparison(args.base_binary, args.candidate_binary, args.base_commit,
                       args.candidate_commit, args.report, args.captures)
    except KeyboardInterrupt:
        print("benchmark comparison interrupted; available evidence retained")
        return 130
    except Exception:
        print("benchmark comparison failed; available evidence retained")
        return 1
    finally:
        signal.signal(signal.SIGTERM, previous)
    print("benchmark comparison complete (report-only)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
