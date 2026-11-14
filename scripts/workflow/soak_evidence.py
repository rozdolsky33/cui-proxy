"""Private, incremental metadata evidence; framing integrity is not qualification."""
import hashlib
import json
import math
import os
from pathlib import Path
import threading

MAX_RECORD_BYTES = 131072
ZERO_HASH = "0" * 64
PRIVATE_KEYS = {"body", "text", "content", "original", "originals", "protected", "occurrences", "mapping", "mappings", "forbidden_values", "arguments", "message", "output", "stderr", "stdout"}


def _metadata(value):
    if isinstance(value, dict):
        for key, child in value.items():
            if not isinstance(key, str) or key in PRIVATE_KEYS:
                raise ValueError("private or invalid evidence field")
            _metadata(child)
    elif isinstance(value, (list, tuple)):
        for child in value: _metadata(child)
    elif isinstance(value, float):
        if not math.isfinite(value): raise ValueError("nonfinite evidence")
    elif value is not None and not isinstance(value, (str, int, bool)):
        raise ValueError("invalid evidence value")


def _encode(value):
    _metadata(value)
    return json.dumps(value, sort_keys=True, separators=(",", ":"), allow_nan=False).encode()


def _pairs(pairs):
    result = {}
    for key, value in pairs:
        if key in result: raise ValueError("duplicate evidence field")
        result[key] = value
    return result


class EvidenceWriter:
    def __init__(self, path):
        self.path = Path(path)
        self.lock = threading.Lock()
        self.sequence = 0
        self.previous = ZERO_HASH
        self.failed = False
        self.fd = os.open(self.path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        try:
            directory = os.open(self.path.parent, os.O_RDONLY)
            try: os.fsync(directory)
            finally: os.close(directory)
        except BaseException:
            os.close(self.fd)
            self.fd = None
            raise

    def __enter__(self):
        return self

    def __exit__(self, *args):
        with self.lock:
            if self.fd is not None:
                os.close(self.fd)
                self.fd = None

    def __call__(self, event):
        with self.lock:
            if self.fd is None or self.failed:
                raise ValueError("evidence writer unavailable")
            if not isinstance(event, dict): raise ValueError("invalid evidence event")
            record = {"sequence": self.sequence, "previous": self.previous, "event": event}
            digest = hashlib.sha256(_encode(record)).hexdigest()
            encoded = _encode(dict(record, sha256=digest)) + b"\n"
            if len(encoded) > MAX_RECORD_BYTES: raise ValueError("oversize evidence record")
            try:
                view = memoryview(encoded)
                while view:
                    written = os.write(self.fd, view)
                    if written <= 0: raise OSError("evidence write made no progress")
                    view = view[written:]
                os.fsync(self.fd)
            except BaseException:
                self.failed = True
                raise
            self.sequence += 1
            self.previous = digest

    def seal(self, path, *, completed):
        """Anchor the exact final prefix; incomplete runs can still retain evidence."""
        with self.lock:
            if self.fd is None or self.failed or type(completed) is not bool:
                raise ValueError("evidence writer unavailable")
            encoded = _encode({"schema_version": 1, "count": self.sequence,
                               "last_sha256": self.previous, "run_completed": completed}) + b"\n"
            self.failed = True  # No further append, including after a seal failure.
            fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            try:
                view = memoryview(encoded)
                while view:
                    written = os.write(fd, view)
                    if written <= 0: raise OSError("seal write made no progress")
                    view = view[written:]
                os.fsync(fd)
            finally: os.close(fd)
            directory = os.open(Path(path).parent, os.O_RDONLY)
            try: os.fsync(directory)
            finally: os.close(directory)


def read_evidence(path):
    """Return validated records, retaining a partial tail as explicitly incomplete.

    A true framing_complete flag does not prove that all requests or phases ran.
    Final run manifests must separately reconcile counts and the last digest.
    """
    events = []
    previous = ZERO_HASH
    trailing = 0
    with Path(path).open("rb") as stream:
        while True:
            line = stream.readline(MAX_RECORD_BYTES + 1)
            if not line: break
            if len(line) > MAX_RECORD_BYTES: raise ValueError("oversize evidence record")
            if not line.endswith(b"\n"):
                trailing = len(line)
                break
            try:
                row = json.loads(line, object_pairs_hook=_pairs)
                if not isinstance(row, dict) or set(row) != {"sequence", "previous", "event", "sha256"}:
                    raise ValueError("invalid evidence envelope")
                digest = row.pop("sha256")
                if type(row["sequence"]) is not int or row["sequence"] != len(events) or row["previous"] != previous:
                    raise ValueError("evidence sequence mismatch")
                if not isinstance(row["event"], dict) or digest != hashlib.sha256(_encode(row)).hexdigest():
                    raise ValueError("evidence digest mismatch")
            except (UnicodeError, json.JSONDecodeError, TypeError, RecursionError) as error:
                raise ValueError("invalid evidence encoding") from error
            events.append(row["event"])
            previous = digest
    return {"events": events, "framing_complete": trailing == 0, "trailing_bytes": trailing, "last_sha256": previous}


def read_sealed_evidence(path, seal_path):
    report = read_evidence(path)
    with Path(seal_path).open("rb") as stream:
        encoded = stream.read(4097)
    if len(encoded) > 4096: raise ValueError("oversize evidence seal")
    try:
        seal = json.loads(encoded, object_pairs_hook=_pairs)
        if not isinstance(seal, dict) or set(seal) != {"schema_version", "count", "last_sha256", "run_completed"}:
            raise ValueError("invalid evidence seal")
        if type(seal["schema_version"]) is not int or seal["schema_version"] != 1 or type(seal["count"]) is not int or type(seal["run_completed"]) is not bool:
            raise ValueError("invalid evidence seal types")
        if not report["framing_complete"] or seal["count"] != len(report["events"]) or seal["last_sha256"] != report["last_sha256"]:
            raise ValueError("evidence seal mismatch")
    except (UnicodeError, json.JSONDecodeError, TypeError, RecursionError) as error:
        raise ValueError("invalid evidence seal encoding") from error
    report["run_completed"] = seal["run_completed"]
    return report
