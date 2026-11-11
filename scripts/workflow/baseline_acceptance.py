"""Read-only numerical observations for the version-1 performance contract.

These observations alone cannot qualify a baseline: campaign provenance,
repetition coverage, resources and the remaining workloads are separate gates.
Only fixed field names and numerical summaries leave this module.
"""
import math
import argparse
import tempfile
import hashlib
import json
import os
import re
import stat
from pathlib import Path

from performance_stats import summarize_pairs


NORMAL_SIZES = (262144, 786432, 1048576)


def _number(value):
    return type(value) in (int, float) and math.isfinite(value)


def _integer(value):
    return type(value) is int and value >= 0


def _identity(proxy, bypass):
    for phase, mode in ((proxy, "proxy"), (bypass, "bypass")):
        if not isinstance(phase, dict) or phase.get("mode") != mode:
            return False
        if type(phase.get("clients")) is not int or phase["clients"] != 4:
            return False
        if type(phase.get("size_bytes")) is not int or phase["size_bytes"] not in NORMAL_SIZES:
            return False
        if not _integer(phase.get("repetition")) or phase["repetition"] > 2:
            return False
    return all(proxy[k] == bypass[k] for k in ("clients", "size_bytes", "repetition"))


def _samples(records, field):
    samples = []
    identities = set()
    for record in records:
        if not isinstance(record, dict):
            raise ValueError("invalid_record")
        identity = record.get("operation_id")
        if not isinstance(identity, str) or not identity or identity in identities:
            raise ValueError("invalid_identity")
        identities.add(identity)
        value = record.get(field)
        if field == "first_byte_ms":
            if not _number(value) or value < 0:
                raise ValueError("invalid_timing")
            samples.append(dict(operation_id=identity, first_byte_ms=value))
        else:
            if not isinstance(value, list) or len(value) != 32 or not all(_number(x) for x in value):
                raise ValueError("invalid_relay")
            for index, timing in enumerate(value):
                # Length-prefix the identity, so arbitrary identifiers cannot collide.
                key = str(len(identity)) + ":" + identity + ":" + str(index)
                samples.append(dict(operation_id=key, first_byte_ms=timing))
    return samples


def evaluate_text(proxy, bypass):
    """Evaluate one matched normal four-client text repetition without mutation.

    Sampling insufficiency blocks a numerical pass. Numerical observations are
    still retained when available. Invalid timings/identities are distinguished
    from insufficient sample counts and observed unsuccessful operations.
    """
    result = dict(performance_qualified=False,
                  sampling=dict(state="incomplete"),
                  correctness=dict(state="incomplete"),
                  first_byte=dict(state="incomplete"), relay=dict(state="incomplete"))
    if not _identity(proxy, bypass):
        result["sampling"]["state"] = "invalid"
        return result
    records = [proxy.get("records"), bypass.get("records")]
    if not all(isinstance(r, list) for r in records):
        return result
    if not all(isinstance(r, dict) for arm in records for r in arm):
        result["correctness"]["state"] = "invalid"
        return result
    success = [sum(r.get("supported") is True and r.get("completed") is True
                   and r.get("outcome") == "supported_success" for r in arm)
               for arm in records]
    result["correctness"] = dict(
        state="pass" if all(arm and count == len(arm) for arm, count in zip(records, success)) else "fail",
        proxy_attempted=len(records[0]), proxy_success=success[0],
        bypass_attempted=len(records[1]), bypass_success=success[1])
    enough = all(len(arm) >= 1000 for arm in records)
    invalid_sampling = False
    for phase in (proxy, bypass):
        seconds = phase.get("warmup_seconds")
        count = phase.get("warmup_completed")
        concurrency = phase.get("achieved_client_concurrency")
        if seconds is None or count is None or concurrency is None:
            enough = False
            continue
        if not _number(seconds) or seconds < 0 or not _integer(count) or not _integer(concurrency):
            invalid_sampling = True
        elif seconds < 60 or count < 100 or concurrency != 4:
            enough = False
    result["sampling"]["state"] = "invalid" if invalid_sampling else "pass" if enough else "incomplete"
    ready = result["sampling"]["state"] == "pass" and result["correctness"]["state"] == "pass"
    for gate, field in (("first_byte", "first_byte_ms"), ("relay", "relay_ms")):
        try:
            paired = summarize_pairs(_samples(records[0], field), _samples(records[1], field))
        except (ValueError, TypeError, KeyError):
            result[gate]["state"] = "invalid"
            continue
        observed = dict(count=paired["count"], p50_ms=paired["p50"],
                        p95_ms=paired["p95"], p99_ms=paired["p99"])
        passed = (observed["p95_ms"] <= 150 and observed["p99_ms"] <= 500) if gate == "first_byte" else observed["p99_ms"] <= 100
        observed["state"] = ("pass" if passed else "fail") if ready else "incomplete"
        result[gate] = observed
    return result


MAX_REPORT_BYTES = 256 * 1024 * 1024


def _unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate_json_key")
        result[key] = value
    return result


def _decode(raw):
    try:
        return json.loads(raw, object_pairs_hook=_unique_object,
                          parse_constant=lambda _: (_ for _ in ()).throw(ValueError("nonfinite_json")))
    except (UnicodeError, json.JSONDecodeError, RecursionError) as error:
        raise ValueError("invalid_json") from error


def _private_read(root, name):
    """Read one regular child without following a symlink or reopening its path."""
    if Path(name).name != name or name in ("", ".", ".."):
        raise ValueError("report_path")
    try:
        directory = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            descriptor = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK,
                                 dir_fd=directory)
        finally:
            os.close(directory)
        with os.fdopen(descriptor, "rb") as stream:
            info = os.fstat(stream.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_size > MAX_REPORT_BYTES:
                raise ValueError("report_file")
            raw = stream.read(MAX_REPORT_BYTES + 1)
            if len(raw) > MAX_REPORT_BYTES:
                raise ValueError("report_size")
            return raw
    except OSError as error:
        raise ValueError("report_file") from error


def _digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True).encode()).hexdigest()


def _hash(value):
    return isinstance(value, str) and re.fullmatch(r"[0-9a-f]{64}", value) is not None


def read_campaign(root):
    """Return a manifest snapshot and a lazy stream of hash-verified reports.

    Unfinished attempts are not read, so concurrent atomic report writes cannot
    become successful evidence. Reports are yielded one at a time to bound
    memory use. This checks consistency, not authenticity of externally supplied
    manifests. Reference completeness/provenance are evaluated separately.
    """
    manifest = _decode(_private_read(root, "manifest.json"))
    if (not isinstance(manifest, dict) or type(manifest.get("schema_version")) is not int
            or manifest["schema_version"] != 1 or type(manifest.get("completed")) is not bool):
        raise ValueError("manifest_schema")
    identity = manifest.get("identity")
    jobs = manifest.get("jobs")
    if not isinstance(identity, dict) or not isinstance(jobs, list) or not jobs:
        raise ValueError("manifest_schema")
    if identity.get("profile") not in ("reference", "smoke"):
        raise ValueError("campaign_profile")
    if manifest.get("fingerprint") != _digest(identity):
        raise ValueError("manifest_hash")
    if any(not isinstance(job, dict) or not isinstance(job.get("spec"), dict) for job in jobs):
        raise ValueError("job_schema")
    if identity.get("jobs_sha256") != _digest([job["spec"] for job in jobs]):
        raise ValueError("jobs_hash")
    seen = set()
    statuses = {"pending", "running", "failed", "succeeded"}
    for index, job in enumerate(jobs):
        name = job["spec"].get("id")
        if not isinstance(name, str) or not name or name in seen:
            raise ValueError("job_identity")
        seen.add(name)
        if job.get("status") not in statuses or not isinstance(job.get("attempts"), list):
            raise ValueError("job_schema")
        attempts = job["attempts"]
        if job["status"] == "succeeded" and not attempts:
            raise ValueError("missing_attempt")
        for number, attempt in enumerate(attempts, 1):
            if not isinstance(attempt, dict):
                raise ValueError("attempt_schema")
            expected = f"job-{index:03d}-attempt-{number:03d}.json"
            if attempt.get("report") != expected:
                raise ValueError("report_path")
            digest = attempt.get("sha256")
            if digest is not None and not _hash(digest):
                raise ValueError("report_hash")
        if job["status"] == "succeeded":
            last = attempts[-1]
            if type(last.get("exit_code")) is not int or last["exit_code"] != 0 or not _hash(last.get("sha256")):
                raise ValueError("successful_attempt")
    if manifest["completed"] != all(job["status"] == "succeeded" for job in jobs):
        raise ValueError("campaign_completion")

    def reports():
        for index, job in enumerate(jobs):
            for number, attempt in enumerate(job["attempts"]):
                if attempt.get("sha256") is None:
                    continue
                raw = _private_read(root, attempt["report"])
                if hashlib.sha256(raw).hexdigest() != attempt["sha256"]:
                    raise ValueError("report_hash")
                if job["status"] == "succeeded" and number == len(job["attempts"]) - 1:
                    report = _decode(raw)
                    if not isinstance(report, dict) or type(report.get("schema_version")) is not int or report["schema_version"] != 1:
                        raise ValueError("report_schema")
                    yield index, job["spec"], report
    return manifest, reports()



def _successful(record):
    return (isinstance(record, dict) and record.get("supported") is True
            and record.get("completed") is True and record.get("outcome") == "supported_success")


def _warm(phase):
    seconds, count = phase.get("warmup_seconds"), phase.get("warmup_completed")
    return _number(seconds) and seconds >= 60 and _integer(count) and count >= 100


def evaluate_throughput(proxy, bypass):
    """Count correct completions inside each matched ten-minute window."""
    if not _identity(proxy, bypass):
        return dict(state="invalid")
    for phase in (proxy, bypass):
        window = phase.get("window_seconds")
        if window is None:
            return dict(state="incomplete")
        if not _number(window):
            return dict(state="invalid")
        if window != 600 or not _warm(phase) or phase.get("achieved_client_concurrency") != 4:
            return dict(state="incomplete")
    successes = []
    failure = False
    for phase in (proxy, bypass):
        records = phase.get("records")
        if not isinstance(records, list) or not records:
            return dict(state="incomplete")
        seen = set()
        for record in records:
            if not isinstance(record, dict):
                return dict(state="invalid")
            identity = record.get("operation_id")
            if not isinstance(identity, str) or not identity or identity in seen:
                return dict(state="invalid")
            seen.add(identity)
            start, finish = record.get("started_s"), record.get("finished_s")
            if not _number(start) or not _number(finish) or finish < start:
                return dict(state="invalid")
            failure |= not _successful(record)
        beginning = min(record["started_s"] for record in records)
        successes.append(sum(_successful(record) and record["finished_s"] <= beginning + 600
                             for record in records))
    result = dict(state="incomplete", proxy_window_success=successes[0],
                  bypass_window_success=successes[1], window_seconds=600)
    if successes[1]:
        result["ratio"] = successes[0] / successes[1]
        result["state"] = "pass" if not failure and result["ratio"] >= 0.85 else "fail"
    elif failure:
        result["state"] = "fail"
    return result


def evaluate_resources(phase):
    """Recompute raw peaks when available; label summarized observations."""
    samples = phase.get("resource_samples")
    summary = phase.get("resources")
    summarized = samples is None and isinstance(summary, dict)
    missing = False
    peaks = {}
    if not summarized and (not isinstance(samples, list) or not samples):
        return dict(state="incomplete")
    for field in ("proxy_rss_bytes", "child_rss_bytes"):
        if summarized:
            count_name = "proxy_rss_samples" if field == "proxy_rss_bytes" else "child_rss_samples"
            count = summary.get(count_name)
            if not _integer(count):
                return dict(state="invalid")
            values = [summary.get("peak_" + field)] if count else []
        else:
            if not all(isinstance(sample, dict) for sample in samples):
                return dict(state="invalid")
            values = [sample.get(field) for sample in samples]
        missing |= not values or any(value is None for value in values)
        values = [value for value in values if value is not None]
        if not all(_integer(value) for value in values):
            return dict(state="invalid")
        peaks["peak_" + field] = max(values) if values else None
    exceeded = any(value is not None and value > limit for value, limit in
                   ((peaks["peak_proxy_rss_bytes"], 512 * 1024**2),
                    (peaks["peak_child_rss_bytes"], 1024**3)))
    peaks["state"] = "fail" if exceeded else "incomplete" if missing else "pass"
    peaks["evidence"] = "summary" if summarized else "samples"
    peaks["complete_samples"] = not missing
    return peaks


def _p95(values):
    return sorted(values)[math.ceil(len(values) * 0.95) - 1]


def _evaluate_extraction(report, exact_count):
    """Separate image/PDF numerical gates, retaining per-repetition evidence.

    Warm-up duration here is declared runner configuration, not an independently
    recorded elapsed measurement. Thus these observations never establish full
    reference qualification, even when their numerical states pass.
    """
    settings = report.get("settings", {})
    template = str(settings.get("template"))
    if template not in ("0", "1", "2", "3", "pdf-text", "pdf-scan"):
        raise ValueError("extraction_template")
    measurements = report.get("measurements")
    if not isinstance(measurements, list):
        raise ValueError("extraction_measurements")
    results = []
    for phase in measurements:
        result = dict(correctness=dict(state="incomplete"), run=dict(state="incomplete"),
                      queue=dict(state="incomplete"), budget=dict(state="incomplete"),
                      performance_qualified=False)
        results.append(result)
        records = phase.get("records")
        if not isinstance(records, list) or not records:
            continue
        try:
            _record_map(records)
        except ValueError:
            result["correctness"]["state"] = "invalid"
            continue
        successful = sum(_successful(record) for record in records)
        result["correctness"] = dict(state="pass" if successful == len(records) else "fail",
                                     attempted=len(records), supported_success=successful)
        stages = phase.get("stage_samples")
        if not isinstance(stages, dict):
            continue
        run, wait = stages.get("extraction_run"), stages.get("extraction_wait")
        if phase.get("stage_poll_errors") != 0:
            for gate in ("run", "queue", "budget"):
                result[gate]["state"] = "invalid"
            continue
        if not isinstance(run, list) or not isinstance(wait, list):
            continue
        if not all(_number(x) and x >= 0 for x in run + wait):
            for gate in ("run", "queue", "budget"):
                result[gate]["state"] = "invalid"
            continue
        if len(run) != len(records) or len(wait) != len(records):
            continue
        result["run"]["p95_ms"] = _p95(run)
        result["queue"]["p95_ms"] = _p95(wait)
        result["budget"]["maximum_ms"] = max(a + b for a, b in zip(run, wait))
        seconds = settings.get("warmup_seconds")
        count = phase.get("warmup_completed")
        enough = ((len(records) == 100 if exact_count else len(records) >= 100)
                  and successful == len(records) and _integer(count) and count >= 100
                  and _number(seconds) and seconds >= 60 and settings.get("clients") == 4
                  and phase.get("achieved_concurrency") == 4)
        if enough:
            limit = 10000 if template.startswith("pdf-") else 5000
            result["run"]["state"] = "pass" if result["run"]["p95_ms"] <= limit else "fail"
            result["queue"]["state"] = "pass" if result["queue"]["p95_ms"] <= 2000 else "fail"
            result["budget"]["state"] = "pass" if result["budget"]["maximum_ms"] <= 30000 else "fail"
    return results



def evaluate_extraction(report):
    return _evaluate_extraction(report, exact_count=True)


def _record_map(records):
    if not isinstance(records, list):
        raise ValueError("records_missing")
    result = {}
    for record in records:
        if not isinstance(record, dict):
            raise ValueError("invalid_record")
        identity = record.get("operation_id")
        if not isinstance(identity, str) or not identity or identity in result:
            raise ValueError("invalid_identity")
        result[identity] = record
    return result


def evaluate_fairness(proxy, bypass):
    """Keep text and all image operations in their own denominators."""
    if not _identity(proxy, bypass):
        return dict(state="invalid", performance_qualified=False)
    try:
        for phase in (proxy, bypass):
            combined = _record_map(phase.get("records"))
            text = _record_map(phase.get("text_records"))
            images = _record_map(phase.get("image_records"))
            if text.keys() & images.keys() or combined != {**text, **images}:
                raise ValueError("partition_mismatch")
    except ValueError:
        return dict(state="invalid", performance_qualified=False)
    text_arms = [dict(phase, records=phase["text_records"]) for phase in (proxy, bypass)]
    image_phase = dict(proxy, records=proxy["image_records"],
                       achieved_concurrency=proxy.get("achieved_client_concurrency"))
    image_report = dict(settings=dict(template="0", clients=4,
                                      warmup_seconds=proxy.get("warmup_seconds")),
                        measurements=[image_phase])
    images = _evaluate_extraction(image_report, exact_count=False)[0]
    bypass_success = sum(_successful(record) for record in bypass["image_records"])
    images["bypass_attempted"] = len(bypass["image_records"])
    images["bypass_success"] = bypass_success
    if bypass_success != len(bypass["image_records"]) or not bypass_success:
        images["correctness"]["state"] = "fail"
        for gate in ("run", "queue", "budget"):
            if images[gate]["state"] == "pass":
                images[gate]["state"] = "incomplete"
    return dict(performance_qualified=False, text=evaluate_text(*text_arms), images=images,
                resources=evaluate_resources(proxy))


def evaluate_overload(phase):
    """Preserve all scheduled slots and observe the 45-second request bound."""
    fields = ("scheduled", "sent", "completed", "rejected", "client_unsent")
    if not all(_integer(phase.get(field)) for field in fields):
        return dict(state="invalid")
    try:
        records = _record_map(phase.get("records"))
        unsent = _record_map(phase.get("unsent_records"))
    except ValueError:
        return dict(state="invalid")
    if (records.keys() & unsent.keys() or len(records) != phase["sent"]
            or len(unsent) != phase["client_unsent"]
            or phase["scheduled"] != len(records) + len(unsent)
            or phase["completed"] > phase["sent"] or phase["rejected"] > phase["sent"]):
        return dict(state="invalid")
    result = {field: phase[field] for field in fields}
    result["supported_success"] = sum(_successful(record) for record in records.values())
    durations = [record.get("duration_ms") for record in records.values()]
    if not all(_number(value) and value >= 0 for value in durations):
        return dict(result, state="invalid")
    if phase.get("collection_failed") is not False or not durations:
        return dict(result, state="incomplete")
    maximum = max(durations)
    failure = any(not _successful(record) and record.get("outcome") != "proxy_overload"
                  for record in records.values())
    return dict(result, state="fail" if failure or maximum > 45000 else "pass",
                maximum_duration_ms=maximum, performance_qualified=False)


def _coverage(manifest):
    from performance_campaign import make_jobs
    expected = make_jobs(profile="reference")
    by_id = {job["id"]: job for job in expected}
    jobs = manifest["jobs"]
    valid = all(job["spec"] == by_id.get(job["spec"]["id"]) for job in jobs)
    complete = valid and len(jobs) == len(expected) and manifest["completed"]
    return dict(state="invalid" if not valid and manifest["identity"]["profile"] == "reference"
                else "pass" if complete and manifest["identity"]["profile"] == "reference" else "incomplete",
                required_jobs=len(expected), planned_jobs=len(jobs))


def _provenance(manifest):
    from performance_campaign import check_reference_environment
    identity = manifest["identity"]
    source = identity.get("source")
    if not isinstance(source, dict):
        return dict(state="incomplete")
    if (type(source.get("dirty")) is not bool
            or (identity["profile"] == "reference" and source["dirty"])
            or not isinstance(source.get("commit"), str)
            or re.fullmatch(r"[0-9a-f]{40}", source["commit"]) is None
            or not _hash(source.get("script_sha256"))):
        return dict(state="invalid")
    if not all(_hash(identity.get(field)) for field in
               ("binary_sha256", "seed_binary_sha256", "fixture_manifest_sha256")):
        return dict(state="invalid")
    incomplete = identity["profile"] != "reference"
    verified = 0
    for job in manifest["jobs"]:
        if job["status"] != "succeeded":
            incomplete = True
            continue
        attempt = job["attempts"][-1]
        observations = [attempt.get("environment_before"), attempt.get("environment_after")]
        if not all(isinstance(value, dict) for value in observations):
            incomplete = True
            continue
        try:
            for observation in observations:
                check_reference_environment(observation)
        except (ValueError, TypeError, KeyError):
            return dict(state="invalid", verified_jobs=verified)
        if observations[0].get("build_info_sha256") != observations[1].get("build_info_sha256"):
            return dict(state="invalid", verified_jobs=verified)
        verified += 1
    return dict(state="incomplete" if incomplete else "pass", verified_jobs=verified)


def _report_observations(spec, report):
    kind = spec["kind"]
    if kind == "extraction":
        return dict(repetitions=evaluate_extraction(report))
    if kind == "overload":
        return dict(phases=[evaluate_overload(phase) for phase in report.get("measurements", [])])
    measurements = report.get("measurements")
    if not isinstance(measurements, list) or len(measurements) != 2:
        return dict(state="incomplete", reason="matched_arms_missing")
    if not all(isinstance(phase, dict) for phase in measurements):
        return dict(state="invalid", reason="matched_arms_invalid")
    arms = {phase.get("mode"): phase for phase in measurements}
    if set(arms) != {"proxy", "bypass"}:
        return dict(state="invalid", reason="matched_arms_invalid")
    for phase in measurements:
        if any(phase.get(field) != spec[key] for field, key in
               (("size_bytes", "size"), ("clients", "clients"), ("repetition", "repetition"))):
            return dict(state="invalid", reason="phase_identity")
    if kind == "fairness":
        return evaluate_fairness(arms["proxy"], arms["bypass"])
    if kind == "throughput":
        return dict(throughput=evaluate_throughput(arms["proxy"], arms["bypass"]),
                    resources=evaluate_resources(arms["proxy"]))
    if spec["clients"] == 4:
        return dict(text=evaluate_text(arms["proxy"], arms["bypass"]),
                    resources=evaluate_resources(arms["proxy"]))
    observations = dict(scope="capacity", performance_qualified=False)
    for gate, field in (("first_byte", "first_byte_ms"), ("relay", "relay_ms")):
        paired = summarize_pairs(_samples(arms["proxy"]["records"], field),
                                 _samples(arms["bypass"]["records"], field))
        observations[gate] = {key: paired[key] for key in ("count", "p50", "p95", "p99")}
    return observations


def summarize_campaign(root):
    """Build a fixed-field summary; successful collection is not qualification."""
    manifest, reports = read_campaign(root)
    result = dict(schema_version=1, performance_qualified=False,
                  coverage=_coverage(manifest), provenance=_provenance(manifest),
                  counts={state: 0 for state in ("pending", "running", "failed", "succeeded")},
                  evaluated_jobs=[],
                  outstanding=["stress_and_churn", "component_profiles", "cancellation_evidence",
                               "cross_workload_acceptance", "complete_reference_evidence"])
    for job in manifest["jobs"]:
        result["counts"][job["status"]] += 1
    for index, spec, report in reports:
        kind = spec.get("kind")
        if kind not in ("text", "throughput", "extraction", "fairness", "overload"):
            raise ValueError("job_kind")
        entry = dict(index=index, kind=kind)
        try:
            entry["observations"] = _report_observations(spec, report)
        except (ValueError, TypeError, KeyError, AttributeError, OverflowError):
            entry["observations"] = dict(state="invalid", reason="report_invalid")
        source = report.get("source")
        if source is not None and (not isinstance(source, dict)
                                   or type(source.get("dirty")) is not bool
                                   or source.get("dirty") != manifest["identity"]["source"]["dirty"]
                                   or source.get("commit") != manifest["identity"]["source"]["commit"]
                                   or report.get("executable_sha256") != manifest["identity"]["binary_sha256"]):
            entry["observations"] = dict(state="invalid", reason="source_identity")
        result["evaluated_jobs"].append(entry)
    return result


def _write_summary(destination, report):
    destination = Path(destination)
    descriptor, temporary = tempfile.mkstemp(prefix=".baseline-summary-", dir=destination.parent)
    try:
        with os.fdopen(descriptor, "w") as stream:
            json.dump(report, stream, allow_nan=False, sort_keys=True)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
        # Hard-link publication is atomic and refuses an existing destination.
        os.link(temporary, destination)
    finally:
        os.unlink(temporary)


def main():
    parser = argparse.ArgumentParser(description="Summarize private baseline evidence without qualifying it")
    parser.add_argument("campaign", type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    try:
        report = summarize_campaign(args.campaign)
        _write_summary(args.output, report)
    except (OSError, ValueError, TypeError, KeyError, AttributeError, OverflowError):
        print("baseline summary failed; evidence retained")
        return 1
    print("baseline summary written; evaluated_jobs=" + str(len(report["evaluated_jobs"]))
          + "; performance_qualified=false")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
