#!/usr/bin/env python3
"""Run the retained reference soak campaign; evaluation remains a separate gate."""
import argparse
import math
from pathlib import Path
import sys

from performance_workload import Workload
from soak_finalization import finalize_evidence
from soak_identity import make_identity_probe
from soak_rejections import BLOCK_CANARY, negative_policy
from soak_run import run_campaign
from soak_reload import renamed_policy


def _positive_seconds(value):
    number = float(value)
    if not math.isfinite(number) or number <= 0:
        raise argparse.ArgumentTypeError("duration must be finite and positive")
    return number


def _positive_count(value):
    number = int(value)
    if number <= 0:
        raise argparse.ArgumentTypeError("count must be positive")
    return number


def _control_count(value):
    number = int(value)
    if number < 0:
        raise argparse.ArgumentTypeError("control count must be nonnegative")
    return number


def _drain(value):
    number = float(value)
    if not math.isfinite(number) or not 0 <= number <= 45:
        raise argparse.ArgumentTypeError("drain must be between zero and 45 seconds")
    return number


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("binary", "seed-binary", "fixtures", "build-manifest", "output"):
        parser.add_argument("--" + name, required=True, type=Path)
    parser.add_argument("--binary-commit", required=True)
    parser.add_argument("--binary-source", type=Path)
    parser.add_argument("--fixed-seconds", type=_positive_seconds, default=7200)
    parser.add_argument("--churn-seconds", type=_positive_seconds, default=7200)
    parser.add_argument("--warmup-seconds", type=_positive_seconds, default=60)
    parser.add_argument("--warmup-count", type=_positive_count, default=100)
    parser.add_argument("--control-count", type=_control_count, default=10)
    parser.add_argument("--drain-seconds", type=_drain, default=45)
    args = parser.parse_args(argv)
    settings = {name: getattr(args, name) for name in
                ("fixed_seconds", "churn_seconds", "warmup_seconds", "warmup_count",
                 "control_count", "drain_seconds")}
    try:
        workload = Workload("normal")
        workload.rules = renamed_policy(negative_policy({"rules": workload.rules}), 0)["rules"]
        forbidden = {BLOCK_CANARY}
        for pair in workload.pairs:
            if len(pair) != 2 or any(not isinstance(value, str) or not value for value in pair):
                raise ValueError("invalid workload privacy oracle")
            # Seed pairs are (class, original), not (original, replacement).
            forbidden.add(pair[1])

        def finalize(env, mode, result):
            mappings = result.get("final_mappings", {})
            if not isinstance(mappings, dict):
                raise ValueError("invalid final mapping privacy oracle")
            for original, replacement in mappings.items():
                if any(not isinstance(value, str) or not value for value in (original, replacement)):
                    raise ValueError("invalid final mapping privacy oracle")
                forbidden.update((original, replacement))
            return finalize_evidence(env, mode, result, forbidden_values=tuple(sorted(forbidden)))

        probe = make_identity_probe(args.binary, args.seed_binary, args.fixtures,
                                    args.build_manifest, args.binary_commit, workload, settings,
                                    binary_source=args.binary_source)
        result = run_campaign(args.output, args.binary, args.seed_binary, workload, args.fixtures,
                              identity_probe=probe, finalize_mode=finalize, **settings)
        if result.get("completed") is not True:
            raise ValueError("campaign did not complete")
    except Exception as error:
        print(f"soak campaign failed; error-kind={type(error).__name__}; private evidence retained when created",
              file=sys.stderr)
        return 1
    print("soak campaign recorded; performance qualification remains pending")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
