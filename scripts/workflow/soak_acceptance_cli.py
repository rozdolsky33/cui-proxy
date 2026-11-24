"""Read-only soak evaluation with an exclusively created acceptance artifact."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import stat
import subprocess

from performance_campaign import source_identity
from soak_acceptance import evaluate_completed_soak
from soak_growth_review import evaluator_module_sha256, load_growth_review
from soak_campaign_reader import read_completed_campaign, read_report_bytes


def _expected_identity(path, root):
    if path.resolve().is_relative_to(root.resolve()):
        raise ValueError("soak_expected_identity_not_independent")
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, "rb") as stream:
        before = os.fstat(stream.fileno())
        if not stat.S_ISREG(before.st_mode) or before.st_size > 2 * 1024 * 1024:
            raise ValueError("soak_expected_identity_file")
        data = stream.read(2 * 1024 * 1024 + 1)
        after = os.fstat(stream.fileno())
        fields = ("st_dev", "st_ino", "st_size", "st_mtime_ns", "st_ctime_ns")
        if len(data) != before.st_size or any(getattr(before, key) != getattr(after, key) for key in fields):
            raise ValueError("soak_expected_identity_changed")
    identity = read_report_bytes(data)
    if not isinstance(identity, dict):
        raise ValueError("soak_expected_identity_shape")
    return identity, hashlib.sha256(data).hexdigest()


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", required=True, type=Path)
    parser.add_argument("--expected-identity", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--growth-review", type=Path)
    args = parser.parse_args(argv)
    if args.output.resolve().is_relative_to(args.root.resolve()):
        print("soak acceptance: output must be outside sealed evidence")
        return 1
    try:
        identity, identity_digest = _expected_identity(args.expected_identity, args.root)
        campaign = read_completed_campaign(args.root)
        review = load_growth_review(args.growth_review, args.root) if args.growth_review is not None else None
        result = (evaluate_completed_soak(campaign, identity) if review is None else
                  evaluate_completed_soak(campaign, identity, growth_review=review))
        result.update(root_events_sha256=campaign["root_events_sha256"],
                      root_seal_sha256=campaign["root_seal_sha256"],
                      expected_identity_sha256=identity_digest)
    except (ValueError, OSError, TypeError, KeyError) as error:
        result = dict(state="incomplete", soak_qualified=False, performance_qualified=False,
                      reason="soak_evidence_or_identity_unavailable")
    try:
        evaluator_source = source_identity()
        result["evaluator_source"] = evaluator_source
        if result["soak_qualified"] and evaluator_source.get("dirty") is not False:
            result.update(state="incomplete", soak_qualified=False,
                          reason="soak_evaluator_source_dirty")
        if args.growth_review:
            evaluator_source["module_sha256"] = {
                    "soak_growth_review.py": evaluator_module_sha256(evaluator_source["commit"]),
                }
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError):
        result.update(state="incomplete", soak_qualified=False,
                      reason="soak_evaluator_source_unavailable")
    try:
        data = (json.dumps(result, sort_keys=True, allow_nan=False) + "\n").encode()
        fd = os.open(args.output, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, "wb") as stream:
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
    except (OSError, ValueError, TypeError):
        print("soak acceptance: report not written; existing evidence preserved")
        return 1
    print("soak acceptance: " + result["state"] + "; performance_qualified=false")
    return 0 if result["soak_qualified"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
