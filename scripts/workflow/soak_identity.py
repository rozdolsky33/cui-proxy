"""Reference campaign identity checks, independent of acceptance evaluation."""
import copy
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess

from performance_campaign import campaign_identity, check_reference_environment, reference_environment
from soak_build_proof import verify_snapshot_build


def read_build_info(binary):
    output = subprocess.run(["go", "version", "-m", str(binary)], check=True,
                            capture_output=True, text=True).stdout
    settings = {}
    for line in output.splitlines():
        fields = line.strip().split(None, 1)
        if len(fields) != 2 or fields[0] != "build" or "=" not in fields[1]:
            continue
        key, value = fields[1].split("=", 1)
        if key in settings:
            raise ValueError("duplicate embedded build setting")
        settings[key] = value
    return settings


def _object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate build manifest field")
        result[key] = value
    return result


def _manifest(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, "rb") as stream:
        before = os.fstat(stream.fileno())
        if not stat.S_ISREG(before.st_mode) or not 0 < before.st_size <= 1024 * 1024:
            raise ValueError("invalid build manifest file")
        data = stream.read(before.st_size + 1)
        after = os.fstat(stream.fileno())
        fields = ("st_dev", "st_ino", "st_size", "st_mtime_ns", "st_ctime_ns")
        if len(data) != before.st_size or any(getattr(before, key) != getattr(after, key) for key in fields):
            raise ValueError("build manifest changed during read")
    result = json.loads(data, object_pairs_hook=_object)
    if not isinstance(result, dict):
        raise ValueError("build manifest must be an object")
    return result, hashlib.sha256(data).hexdigest()


def make_identity_probe(binary, seed_binary, fixtures, build_manifest,
                        expected_binary_commit, workload, settings, *, binary_source=None):
    """Require clean harness and exact declared binary source, bytes and build."""
    if not isinstance(expected_binary_commit, str) or not re.fullmatch(r"[0-9a-f]{40}", expected_binary_commit):
        raise ValueError("full binary source commit is required")
    settings = copy.deepcopy(settings)
    reproduction_cache = {}

    def probe():
        manifest, manifest_hash = _manifest(build_manifest)
        if (manifest.get("source_commit") != expected_binary_commit
                or manifest.get("source_clean") is not True
                or not isinstance(manifest.get("go_version"), str)
                or "go1.26.11" not in manifest["go_version"].split()):
            raise ValueError("build manifest does not match the declared clean reference build")
        identity = campaign_identity(binary, seed_binary, fixtures,
                                     [{"section": "soak", "settings": settings}], "soak")
        if (identity.get("source", {}).get("dirty") is not False
                or identity.get("binary_sha256") != manifest.get("binary_sha256")
                or not isinstance(manifest.get("binary_sha256"), str)
                or not re.fullmatch(r"[0-9a-f]{64}", manifest["binary_sha256"])):
            raise ValueError("dirty harness or mismatched binary identity")
        build = read_build_info(binary)
        if "vcs.revision" in build or "vcs.modified" in build:
            if build.get("vcs.revision") != expected_binary_commit or build.get("vcs.modified") != "false":
                raise ValueError("embedded binary source does not match the clean declared revision")
            source_proof = {"embedded_clean_revision": True, "source_commit": expected_binary_commit}
        else:
            if binary_source is None:
                raise ValueError("binary without VCS metadata requires its clean source snapshot")
            source_proof = verify_snapshot_build(binary, binary_source, expected_binary_commit,
                                                 manifest.get("command"), reproduction_cache)
        host = reference_environment(binary)
        check_reference_environment(host)
        # Current load remains a preflight check; it is not an immutable identity.
        host = {key: value for key, value in host.items() if key != "load_average"}
        policy = {"rules": workload.rules, "dictionary": workload.dictionary, "pairs": workload.pairs}
        policy_hash = hashlib.sha256(json.dumps(policy, sort_keys=True, allow_nan=False).encode()).hexdigest()
        return {**identity, "host": host, "source_proof": source_proof, "binary_source_commit": expected_binary_commit,
                "build_manifest_sha256": manifest_hash, "policy_sha256": policy_hash,
                "workload": workload.metadata(), "policy_variant": "normal-plus-declared-negative-rule"}

    return probe
