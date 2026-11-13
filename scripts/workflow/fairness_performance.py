#!/usr/bin/env python3
"""Run matched text/image fairness diagnostics against isolated local backends."""
import argparse
import hashlib
import json
import math
import sys
from pathlib import Path

import extraction_fixtures as fixtures
import performance as perf
from extraction_performance import reserve_report, write_report
from performance_load import run_fairness_phase
from performance_stats import summarize_outcomes
from performance_workload import Workload


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', required=True)
    parser.add_argument('--seed-binary', required=True)
    parser.add_argument('--fixtures', type=Path, required=True)
    parser.add_argument('--report', type=Path, required=True)
    parser.add_argument('--sizes', type=int, nargs='+', default=[262144, 786432, 1048576])
    mode_group = parser.add_mutually_exclusive_group()
    mode_group.add_argument('--count', type=int)
    mode_group.add_argument('--duration', type=float)
    parser.add_argument('--clients', type=int, choices=[1, 4, 8, 16], default=4)
    parser.add_argument('--repetitions', type=int, default=3)
    parser.add_argument('--warmup-seconds', type=float, default=60)
    parser.add_argument('--warmup-count', type=int, default=100)
    parser.add_argument('--repetition-offset', type=int, default=0)
    args = parser.parse_args(argv)
    if args.count is None and args.duration is None:
        args.count = 1000
    if args.duration is not None and (not math.isfinite(args.duration) or args.duration <= 0):
        parser.error('duration must be finite and positive')
    if args.repetition_offset < 0:
        parser.error('repetition offset must be nonnegative')
    workload = Workload()
    report = dict(schema_version=1, measurements=[], comparison=[], completed=False, performance_qualified=False, workload=workload.metadata(), settings=dict(clients=args.clients, count=args.count, duration=args.duration, sizes_bytes=args.sizes, repetitions=args.repetitions, warmup_seconds=args.warmup_seconds, warmup_count=args.warmup_count))
    active = {}
    reserved = False
    try:
        if any((size not in workload.sizes for size in args.sizes)) or (args.count is not None and args.count < 1) or args.repetitions < 1 or (args.warmup_count < 1) or (not math.isfinite(args.warmup_seconds)) or (args.warmup_seconds < 0):
            raise ValueError('invalid fairness settings')
        reserve_report(args.report)
        reserved = True
        raw_manifest = (args.fixtures / 'manifest.json').read_bytes()
        manifest = json.loads(raw_manifest)
        fixtures.validate_coverage(manifest)
        if manifest.get('font_name') != 'Menlo-Regular':
            raise ValueError('fixture font mismatch')
        provenance = fixtures.provenance()
        if any((manifest.get(key) != value for key, value in provenance.items())):
            raise ValueError('fixture provenance mismatch')
        report['workload'].update(fixture_manifest_sha256=hashlib.sha256(raw_manifest).hexdigest(), fixture_generator_version=manifest.get('version'), tool_versions=provenance.get('tool_versions', {}))
        write_report(args.report, report)
        for size in args.sizes:
            for repetition in range(args.repetition_offset, args.repetition_offset + args.repetitions):
                arms = {}
                for bypass in (True, False) if repetition % 2 == 0 else (False, True):
                    active = dict(size_bytes=size, repetition=repetition, mode='bypass' if bypass else 'proxy', clients=args.clients)
                    result = run_fairness_phase(args.binary, args.seed_binary, workload, args.fixtures, manifest, bypass=bypass, text_count=args.count, text_size=size, warmup_seconds=args.warmup_seconds, warmup_count=args.warmup_count, clients=args.clients, duration=args.duration)
                    result.update(active)
                    report['measurements'].append(result)
                    arms[bypass] = result
                    write_report(args.report, report)
                    if args.count is not None and len(result['text_records']) != args.count or result.get('text_unsent', 0) or any((row.get('outcome') != 'supported_success' for row in result['records'])):
                        raise RuntimeError('fairness phase failed')
                comparison = perf.compare_runs(arms[False]['text_records'], arms[True]['text_records'])
                comparison.update(size_bytes=size, repetition=repetition)
                report['comparison'].append(comparison)
                write_report(args.report, report)
        report['completed'] = True
        write_report(args.report, report)
        return 0
    except Exception as error:
        if reserved:
            if isinstance(error, perf.PhaseFailure):
                report['measurements'].append(dict(phase=error.phase, records=error.records, outcomes=summarize_outcomes(error.records), performance_qualified=False, **active))
            report['error_kind'] = type(error).__name__
            try:
                write_report(args.report, report)
            except Exception:
                pass
        print('fairness run failed', file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
