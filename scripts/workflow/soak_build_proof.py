"""Reproducible-source proof for binaries without embedded VCS metadata."""
import hashlib
from pathlib import Path
import subprocess
import tempfile


def _git(source, *args):
    return subprocess.run(["git", "-C", str(source), *args], check=True,
                          capture_output=True, text=True).stdout.strip()


def verify_snapshot_build(binary, source, commit, command, cache):
    source = Path(source).resolve()
    if _git(source, "rev-parse", "HEAD") != commit:
        raise ValueError("source snapshot revision mismatch")
    if _git(source, "status", "--porcelain", "--untracked-files=all"):
        raise ValueError("source snapshot is not clean")
    ignored = _git(source, "ls-files", "--others", "--ignored", "--exclude-standard").splitlines()
    if any("__pycache__" not in Path(name).parts or not name.endswith(".pyc") for name in ignored):
        raise ValueError("untracked ignored source could affect the build")
    if (not isinstance(command, list) or len(command) != 5
            or command[:3] != ["go", "build", "-o"] or command[4] != "./cmd/cui-proxy"):
        raise ValueError("unsupported reproducible build command")
    digest = hashlib.sha256(Path(binary).read_bytes()).hexdigest()
    key = (str(source), commit, digest, tuple(command))
    if key not in cache:
        with tempfile.TemporaryDirectory(prefix="cui-build-proof-") as directory:
            output = Path(directory) / "proxy"
            subprocess.run(["go", "build", "-o", str(output), "./cmd/cui-proxy"],
                           cwd=source, check=True, capture_output=True)
            if hashlib.sha256(output.read_bytes()).hexdigest() != digest:
                raise ValueError("reproduced binary differs from measured binary")
        # Catch source mutation during the verification build as well.
        if _git(source, "rev-parse", "HEAD") != commit or _git(source, "status", "--porcelain", "--untracked-files=all"):
            raise ValueError("source snapshot changed during reproduction")
        ignored_after = _git(source, "ls-files", "--others", "--ignored", "--exclude-standard").splitlines()
        if any("__pycache__" not in Path(name).parts or not name.endswith(".pyc") for name in ignored_after):
            raise ValueError("ignored source appeared during reproduction")
        cache[key] = {"reproduced_binary": True, "source_commit": commit,
                      "binary_sha256": digest}
    return dict(cache[key])
