#!/usr/bin/env python3
"""Run isolated diagnostic open-loop text workloads and retain slot accounting."""
import argparse
import math
import sys
from pathlib import Path

import performance as perf
from extraction_performance import reserve_report, write_report
from performance_load import run_overload_phase
from performance_stats import summarize_outcomes
from performance_workload import Workload


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True)
    parser.add_argument("--seed-binary", required=True)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--sizes", nargs="+", type=int, default=[262144, 786432, 1048576])
    parser.add_argument("--rates", nargs="+", type=float, default=[1, 2, 4, 8])
    parser.add_argument("--duration", type=float, default=120)
    parser.add_argument("--max-outstanding", type=int, default=16)
    parser.add_argument("--warmup-seconds", type=float, default=60)
    parser.add_argument("--warmup-count", type=int, default=100)
    args = parser.parse_args(argv)
    workload = Workload()
    report = dict(schema_version=1, measurements=[], completed=False, performance_qualified=False)
    reserved = False
    active = {}
    try:
        if (any(size not in workload.sizes for size in args.sizes) or
                any(not math.isfinite(rate) or rate <= 0 for rate in args.rates) or
                not math.isfinite(args.duration) or args.duration <= 0 or
                not 1 <= args.max_outstanding <= 16 or
                not math.isfinite(args.warmup_seconds) or args.warmup_seconds < 0 or
                args.warmup_count < 1):
            raise ValueError("invalid overload settings")
        reserve_report(args.output)
        reserved = True
        write_report(args.output, report)
        for size in args.sizes:
            for rate in args.rates:
                for bypass in (True, False):
                    active = dict(size_bytes=size, offered_rate=rate,
                                  requested_duration_s=args.duration,
                                  max_outstanding=args.max_outstanding,
                                  mode="bypass" if bypass else "proxy")
                    result = run_overload_phase(
                        args.binary, args.seed_binary, workload, size=size, rate=rate,
                        duration=args.duration, max_outstanding=args.max_outstanding,
                        bypass=bypass, warmup_seconds=args.warmup_seconds,
                        warmup_count=args.warmup_count)
                    result.update(active)
                    report["measurements"].append(result)
                    write_report(args.output, report)
                    if result.get("collection_failed") or any(
                            row.get("outcome") not in ("supported_success", "proxy_overload")
                            for row in result["records"]):
                        raise RuntimeError("overload phase failed")
        report["completed"] = True
        write_report(args.output, report)
        return 0
    except Exception as error:
        if reserved:
            if isinstance(error, perf.PhaseFailure):
                report["measurements"].append(dict(
                    phase=error.phase, records=error.records,
                    outcomes=summarize_outcomes(error.records), performance_qualified=False, **active))
            report["error_kind"] = type(error).__name__
            try:
                write_report(args.output, report)
            except Exception:
                pass  # The atomic writer preserves the most recent complete evidence.
        print("overload run failed", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
