#!/usr/bin/env python3
"""Sequential, resumable diagnostic measurement campaigns with private evidence."""
import argparse
import fcntl
import hashlib
import json
import math
import os
import stat
import signal
import subprocess
import sys
import tempfile
import time
from pathlib import Path

CLEANUP_GRACE = 5.0


def make_jobs(profile="reference", sections=("text", "throughput", "extraction", "fairness", "overload")):
    supported = {"text", "throughput", "extraction", "fairness", "overload"}
    if profile not in ("reference", "smoke") or not sections or set(sections) - supported:
        raise ValueError("invalid campaign selection")
    smoke = profile == "smoke"
    sizes = [262144] if smoke else [262144, 786432, 1048576]
    repetitions = 1 if smoke else 3
    jobs = []
    def add(kind, **settings):
        jobs.append(dict(id=f"{kind}-{len(jobs):03d}", kind=kind, **settings))
    if "text" in sections:
        for clients in ([4] if smoke else [4, 1, 8, 16]):
            for size in sizes:
                for repetition in range(repetitions):
                    add("text", size=size, clients=clients, repetition=repetition,
                        count=1 if smoke else math.ceil(1000 / clients))
    if "throughput" in sections:
        for size in sizes:
            for repetition in range(repetitions):
                add("throughput", size=size, clients=4, repetition=repetition, duration=1 if smoke else 600)
    if "extraction" in sections:
        for template in ([0, "pdf-text"] if smoke else [0, 1, 2, 3, "pdf-text", "pdf-scan"]):
            for repeated in ([False] if smoke or isinstance(template, str) else [False, True]):
                for clients in ([4] if smoke else [1, 4]):
                    for repetition in range(repetitions):
                        add("extraction", template=template, clients=clients, repetition=repetition,
                            repeated=repeated, count=4 if smoke else 100)
    if "fairness" in sections:
        for size in sizes:
            for repetition in range(repetitions):
                add("fairness", size=size, clients=4, repetition=repetition, count=2 if smoke else 1000)
    if "overload" in sections:
        for size in sizes:
            for repetition in range(repetitions):
                add("overload", size=size, repetition=repetition, duration=1 if smoke else 120,
                    rates=[4] if smoke else [1, 2, 4, 8], max_outstanding=2 if smoke else 16)
    return jobs


def _atomic_manifest(root, manifest):
    fd, temporary = tempfile.mkstemp(prefix=".manifest-", dir=root)
    try:
        with os.fdopen(fd, "w") as stream:
            json.dump(manifest, stream, indent=2, sort_keys=True, allow_nan=False)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, root / "manifest.json")
    finally:
        Path(temporary).unlink(missing_ok=True)


def _report_hash(path):
    if path.is_symlink() or not path.is_file():
        raise ValueError("report is not a regular file")
    return hashlib.sha256(path.read_bytes()).hexdigest()


def _validate_resume(root, manifest, jobs, fingerprint, identity=None):
    if (manifest.get("schema_version") != 1 or manifest.get("fingerprint") != fingerprint or
            manifest.get("identity") != identity or
            [row.get("spec") for row in manifest.get("jobs", [])] != jobs):
        raise ValueError("campaign identity changed")
    for index, row in enumerate(manifest["jobs"]):
        attempts = row.get("attempts", [])
        if row.get("status") not in ("pending", "running", "failed", "succeeded"):
            raise ValueError("invalid campaign state")
        if row["status"] == "succeeded" and (not attempts or attempts[-1].get("exit_code") != 0):
            raise ValueError("invalid completed job")
        for number, attempt in enumerate(attempts, 1):
            expected = f"job-{index:03d}-attempt-{number:03d}.json"
            if attempt.get("report") != expected:
                raise ValueError("invalid report identity")
            digest = attempt.get("sha256")
            if digest is not None and _report_hash(root / expected) != digest:
                raise ValueError("campaign report changed")
            if row["status"] == "succeeded" and number == len(attempts) and digest is None:
                raise ValueError("missing completed report hash")


def run_campaign(root, jobs, fingerprint, execute, *, resume=False, identity=None, observe=None, validate_observation=None):
    """Execute one job at a time; only verified completed jobs may be skipped."""
    root = Path(root)
    if root.is_symlink():
        raise ValueError("campaign directory must not be a symlink")
    if resume:
        if not root.is_dir():
            raise ValueError("campaign directory is missing")
    else:
        root.mkdir(mode=0o700)
    lockfd = os.open(root / ".lock", os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        if not stat.S_ISREG(os.fstat(lockfd).st_mode):
            raise ValueError("invalid campaign lock")
        fcntl.flock(lockfd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        path = root / "manifest.json"
        if resume:
            if path.is_symlink():
                raise ValueError("invalid campaign manifest")
            manifest = json.loads(path.read_text())
            _validate_resume(root, manifest, jobs, fingerprint, identity)
        else:
            manifest = dict(schema_version=1, fingerprint=fingerprint, identity=identity, completed=False,
                            performance_qualified=False,
                            jobs=[dict(spec=job, status="pending", attempts=[]) for job in jobs])
            _atomic_manifest(root, manifest)
        for index, row in enumerate(manifest["jobs"]):
            if row["status"] == "succeeded":
                continue
            filename = f"job-{index:03d}-attempt-{len(row['attempts']) + 1:03d}.json"
            report = root / filename
            if report.exists() or report.is_symlink():
                raise ValueError("attempt report already exists")
            attempt = dict(report=filename, exit_code=None, sha256=None)
            row["attempts"].append(attempt)
            row["status"] = "running"
            manifest["completed"] = False
            _atomic_manifest(root, manifest)
            started = time.monotonic()
            interrupted = None
            try:
                if observe is not None:
                    attempt["environment_before"] = observe()
                    _atomic_manifest(root, manifest)
                    if validate_observation is not None:
                        validate_observation(attempt["environment_before"])
                exit_code = execute(row["spec"], report)
                if type(exit_code) is not int:
                    raise ValueError("invalid runner exit status")
                attempt["exit_code"] = exit_code
                if report.exists() or report.is_symlink():
                    attempt["sha256"] = _report_hash(report)
                if exit_code == 0:
                    if attempt["sha256"] is None or not isinstance(json.loads(report.read_text()), dict):
                        raise ValueError("missing or invalid completed report")
                if observe is not None:
                    attempt["environment_after"] = observe()
                    if validate_observation is not None:
                        validate_observation(attempt["environment_after"])
                row["status"] = "succeeded" if exit_code == 0 else "failed"
            except BaseException as error:
                row["status"] = "failed"
                attempt["error_kind"] = type(error).__name__
                if report.is_file() and not report.is_symlink():
                    try:
                        attempt["sha256"] = _report_hash(report)
                    except OSError:
                        pass
                if not isinstance(error, Exception):
                    interrupted = error
            attempt["elapsed_s"] = time.monotonic() - started
            _atomic_manifest(root, manifest)
            if interrupted is not None:
                raise interrupted
            if row["status"] == "failed":
                return 1
        manifest["completed"] = True
        _atomic_manifest(root, manifest)
        return 0
    finally:
        os.close(lockfd)


def build_command(job, binary, seed_binary, fixtures, report, profile):
    kind = job["kind"]
    scripts = dict(text="performance.py", throughput="performance.py",
                   extraction="extraction_performance.py", fairness="fairness_performance.py",
                   overload="overload_performance.py")
    command = [sys.executable, str(Path(__file__).with_name(scripts[kind])),
               "--binary", str(binary), "--seed-binary", str(seed_binary),
               "--warmup-seconds", "0" if profile == "smoke" else "60",
               "--warmup-count", "4" if profile == "smoke" else "100"]
    if kind == "overload":
        return command + ["--output", str(report), "--sizes", str(job["size"]),
                          "--rates", *map(str, job["rates"]), "--duration", str(job["duration"]),
                          "--max-outstanding", str(job["max_outstanding"]),
                          "--repetition", str(job["repetition"])]
    command += ["--report", str(report), "--repetitions", "1",
                "--repetition-offset", str(job["repetition"])]
    if kind in ("text", "throughput"):
        command += ["--size", str(job["size"]), "--clients", str(job["clients"])]
        return command + (["--duration", str(job["duration"])] if kind == "throughput" else
                          ["--count", str(job["count"])])
    command += ["--fixtures", str(fixtures), "--count", str(job["count"])]
    if kind == "fairness":
        return command + ["--sizes", str(job["size"])]
    command += ["--template", str(job["template"]), "--clients", str(job["clients"])]
    return command + (["--repeated"] if job["repeated"] else [])


def _group_running(pgid):
    try:
        os.killpg(pgid, 0)
    except ProcessLookupError:
        return False
    # Zombies hold no sockets or resources and may await adoption/reaping briefly.
    try:
        result = subprocess.run(["ps", "-axo", "pgid=,stat="], stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE, text=True)
    except OSError:
        return True
    if result.returncode != 0:
        return True
    for line in result.stdout.splitlines():
        fields = line.split()
        if len(fields) == 2 and fields[0] == str(pgid) and not fields[1].startswith("Z"):
            return True
    return False


def _wait_group_exit(process):
    deadline = time.monotonic() + CLEANUP_GRACE
    while True:
        process.poll()  # Reap the leader without confusing its exit with group exit.
        if not _group_running(process.pid):
            return True
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            return False
        time.sleep(min(0.05, remaining))


def _terminate_group(process):
    for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGKILL):
        if not _group_running(process.pid):
            return
        try:
            os.killpg(process.pid, sig)
        except ProcessLookupError:
            return
        if _wait_group_exit(process):
            return
    raise RuntimeError("owned process group did not exit")


def execute_command(command, log_path):
    """Capture child output privately and never leave its live group behind."""
    fd = os.open(log_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "wb") as output:
        process = subprocess.Popen(command, stdout=output, stderr=subprocess.STDOUT,
                                   start_new_session=True, close_fds=True)
        try:
            code = process.wait()
        except BaseException:
            _terminate_group(process)
            raise
        if _group_running(process.pid) and not _wait_group_exit(process):
            _terminate_group(process)
            raise RuntimeError("owned descendants outlived child leader")
        return code


def source_identity():
    root = Path(__file__).resolve().parents[2]
    def git(*args):
        return subprocess.run(["git", "-C", str(root), *args], check=True,
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True).stdout.strip()
    hashes = {path.name: hashlib.sha256(path.read_bytes()).hexdigest()
              for path in sorted(Path(__file__).parent.glob("*.py"))}
    return dict(commit=git("rev-parse", "HEAD"),
                dirty=bool(git("status", "--porcelain", "--untracked-files=normal")),
                script_sha256=hashlib.sha256(json.dumps(hashes, sort_keys=True).encode()).hexdigest())


def campaign_identity(binary, seed_binary, fixtures, jobs, profile):
    return dict(profile=profile, source=source_identity(), python_version=sys.version.split()[0],
                binary_sha256=_report_hash(Path(binary)), seed_binary_sha256=_report_hash(Path(seed_binary)),
                fixture_manifest_sha256=_report_hash(Path(fixtures) / "manifest.json"),
                jobs_sha256=hashlib.sha256(json.dumps(jobs, sort_keys=True).encode()).hexdigest())


def check_reference_environment(info):
    expected = dict(hardware_model="MacBookPro18,3", architecture="arm64", logical_cpus=8,
                    memory_bytes=34359738368, macos_version="26.7.2", go_version="go1.26.11",
                    on_ac=True, low_power=False, competing_work=0, optimized_build=True)
    if any(type(info.get(key)) is not type(value) or info.get(key) != value
           for key, value in expected.items()):
        raise ValueError("reference environment unavailable or changed")


def reference_environment(binary):
    import performance as perf
    info = perf.environment_info(binary)
    def captured(command):
        return subprocess.run(command, check=True, stdout=subprocess.PIPE,
                              stderr=subprocess.PIPE, text=True).stdout
    info["macos_version"] = captured(["sw_vers", "-productVersion"]).strip()
    build = captured(["go", "version", "-m", str(binary)])
    info["optimized_build"] = "-race=true" not in build and "-gcflags=" not in build
    benchmark_scripts = ("performance.py", "extraction_performance.py", "fairness_performance.py",
                         "overload_performance.py", "performance_campaign.py", "extraction_fixtures.py")
    # Put variable-width fields last and request unlimited width. macOS truncates
    # comm in a multi-column layout (for example, Chrome can look like "go").
    arguments = {}
    for line in captured(["ps", "-ww", "-axo", "pid=,args="]).splitlines():
        fields = line.strip().split(None, 1)
        if len(fields) == 2 and fields[0].isdigit():
            arguments[int(fields[0])] = fields[1]
    competing = 0
    for line in captured(["ps", "-ww", "-axo", "pid=,stat=,comm="]).splitlines():
        parts = line.strip().split(None, 2)
        if (len(parts) != 3 or not parts[0].isdigit() or int(parts[0]) == os.getpid() or
                parts[1].startswith("Z")):
            continue
        name = Path(parts[2]).name.lower()
        args = arguments.get(int(parts[0]), "")
        if (name in ("go", "tesseract", "pdftoppm", "pdftotext") or name.startswith("swift") or
                name.endswith(".test") or
                (name.startswith("python") and (any(script in args for script in benchmark_scripts) or
                                                "-m unittest" in args))):
            competing += 1
    info["competing_work"] = competing
    info["build_info_sha256"] = hashlib.sha256(build.encode()).hexdigest()
    return info


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--seed-binary", type=Path, required=True)
    parser.add_argument("--fixtures", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--profile", choices=("smoke", "reference"), default="reference")
    parser.add_argument("--sections", nargs="+", choices=("text", "throughput", "extraction", "fairness", "overload"),
                        default=["text", "throughput", "extraction", "fairness", "overload"])
    parser.add_argument("--resume", action="store_true")
    args = parser.parse_args(argv)
    previous_signal = signal.getsignal(signal.SIGTERM)
    def interrupt(signum, frame):
        raise KeyboardInterrupt()
    signal.signal(signal.SIGTERM, interrupt)
    try:
        binary, seed, fixtures = args.binary.resolve(), args.seed_binary.resolve(), args.fixtures.resolve()
        jobs = make_jobs(args.profile, args.sections)
        identity = campaign_identity(binary, seed, fixtures, jobs, args.profile)
        if args.profile == "reference" and identity["source"]["dirty"]:
            raise ValueError("reference campaign requires a clean source checkout")
        fingerprint = hashlib.sha256(json.dumps(identity, sort_keys=True).encode()).hexdigest()
        def execute(job, report):
            if campaign_identity(binary, seed, fixtures, jobs, args.profile) != identity:
                raise ValueError("campaign inputs changed")
            command = build_command(job, binary, seed, fixtures, report, args.profile)
            print("starting", job["id"], flush=True)
            code = execute_command(command, report.with_suffix(".log"))
            if campaign_identity(binary, seed, fixtures, jobs, args.profile) != identity:
                raise ValueError("campaign inputs changed during measurement")
            print("finished", job["id"], "exit", code, flush=True)
            return code
        code = run_campaign(args.output.absolute(), jobs, fingerprint, execute, resume=args.resume,
                            identity=identity,
                            observe=(lambda: reference_environment(binary)) if args.profile == "reference" else None,
                            validate_observation=check_reference_environment if args.profile == "reference" else None)
        if code == 0:
            print("campaign complete; performance qualification remains pending", flush=True)
        return code
    except KeyboardInterrupt:
        print("campaign interrupted; progress retained", file=sys.stderr)
        return 130
    except Exception:
        print("campaign failed", file=sys.stderr)
        return 1
    finally:
        signal.signal(signal.SIGTERM, previous_signal)


if __name__ == "__main__":
    raise SystemExit(main())
