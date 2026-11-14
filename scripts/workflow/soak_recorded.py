"""Persist phase events before allowing later scheduled work."""
from pathlib import Path
import time

from soak_evidence import EvidenceWriter
from soak_phase import run_phase, run_replay


def run_recorded_phase(execute, phase, directory, *, duration=7200, clock=time.monotonic, stop=None, replay=None):
    directory = Path(directory)
    directory.mkdir(mode=0o700)
    with EvidenceWriter(directory / "events.jsonl") as writer:
        try:
            kwargs = dict(duration=duration, clock=clock, stop=stop, record=writer)
            if replay is None:
                report = run_phase(execute, phase, **kwargs)
            else:
                report = run_replay(execute, replay, phase, **kwargs)
        except BaseException:
            if not writer.failed:
                writer.seal(directory / "seal.json", completed=False)
            raise
        writer.seal(directory / "seal.json", completed=report["completed"] is True)
    return report
