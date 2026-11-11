#!/usr/bin/env python3
"""Run the text matrix sequentially; retain private reports and captured logs."""
import argparse
import json
import math
import os
from pathlib import Path
import subprocess
import sys
import time

from performance_workload import TIERS


def schedules(mode):
    if mode not in ("smoke", "text-qualification"):
        raise ValueError("invalid matrix mode")
    for size in TIERS["normal"][3]:
        for clients in (1, 4, 8, 16):
            yield dict(size=size, clients=clients, repetitions=3,
                       count=2 if mode == "smoke" else math.ceil(1000 / clients),
                       warmup_seconds=0 if mode == "smoke" else 60,
                       warmup_count=1 if mode == "smoke" else 100)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--seed-binary", type=Path, required=True)
    parser.add_argument("--directory", type=Path, required=True)
    parser.add_argument("--mode", choices=("smoke", "text-qualification"), default="smoke")
    args = parser.parse_args()
    args.directory.mkdir(mode=0o700)
    script = Path(__file__).with_name("performance.py")
    completed = []
    for schedule in schedules(args.mode):
        ident = f'size-{schedule["size"]}-clients-{schedule["clients"]}'
        report = args.directory / (ident + ".json")
        command = [sys.executable, str(script), "--binary", str(args.binary.resolve()),
                   "--seed-binary", str(args.seed_binary.resolve()), "--report", str(report)]
        for name, value in schedule.items(): command += ["--" + name.replace("_", "-"), str(value)]
        started = time.monotonic()
        with open(args.directory / (ident + ".log"), "wb") as log:
            result = subprocess.run(command, stdout=log, stderr=subprocess.STDOUT)
        completed.append(dict(size_bytes=schedule["size"], clients=schedule["clients"], exit=result.returncode,
                              elapsed_s=time.monotonic() - started))
        (args.directory / "progress.json").write_text(json.dumps(dict(mode=args.mode, completed=completed,
            performance_qualified=False, limitations=["text_only", "no_throughput_window", "no_reference_power_gate"]), indent=2))
        print(json.dumps(dict(completed_cells=len(completed), failed_cells=sum(c["exit"] != 0 for c in completed))), flush=True)
        if result.returncode: raise SystemExit(result.returncode)


if __name__ == "__main__": main()
