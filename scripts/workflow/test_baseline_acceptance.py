import copy
import importlib.util
import math
import hashlib
import json
import tempfile
import subprocess
import sys
from pathlib import Path
import unittest


class TextAcceptanceTests(unittest.TestCase):
    def evaluator(self):
        self.assertIsNotNone(importlib.util.find_spec("baseline_acceptance"),
                             "baseline acceptance evaluator is missing")
        import baseline_acceptance
        return baseline_acceptance.evaluate_text

    def phases(self):
        phases = []
        for mode, first, relay in (("proxy", 1150.0, 100.0),
                                   ("bypass", 1000.0, 0.0)):
            records = [dict(operation_id=str(i), supported=True, completed=True,
                            outcome="supported_success", first_byte_ms=first,
                            duration_ms=2000.0, prefix_ms=[first] * 32,
                            relay_ms=[relay] * 32, relay_flush_ms=[relay] * 32)
                       for i in range(1000)]
            phases.append(dict(mode=mode, clients=4, size_bytes=262144,
                               repetition=0, records=records,
                               warmup_seconds=60.0, warmup_completed=100,
                               achieved_client_concurrency=4))
        return phases

    def test_exact_limits_pass_but_do_not_qualify_a_partial_baseline(self):
        result = self.evaluator()(*self.phases())
        self.assertEqual(result["first_byte"]["state"], "pass")
        self.assertEqual(result["relay"]["state"], "pass")
        self.assertEqual(result["first_byte"]["p95_ms"], 150.0)
        self.assertEqual(result["relay"]["p99_ms"], 100.0)
        self.assertFalse(result["performance_qualified"])

    def test_pairing_preserves_negative_differences_and_input_order_independence(self):
        proxy, bypass = self.phases()
        for r in proxy["records"]:
            r["first_byte_ms"] = 900.0
        bypass["records"].reverse()
        result = self.evaluator()(proxy, bypass)
        self.assertEqual(result["first_byte"]["p95_ms"], -100.0)
        self.assertEqual(result["first_byte"]["state"], "pass")

    def test_p95_and_p99_exceedances_are_evaluated_independently(self):
        for changed, value in ((60, 151.0), (11, 501.0)):
            with self.subTest(changed=changed):
                proxy, bypass = self.phases()
                for r in proxy["records"][-changed:]:
                    r["first_byte_ms"] = 1000.0 + value
                self.assertEqual(self.evaluator()(proxy, bypass)["first_byte"]["state"], "fail")

    def test_relay_exceedance_is_not_hidden_by_good_first_byte(self):
        proxy, bypass = self.phases()
        for r in proxy["records"]:
            r["relay_ms"] = [101.0] * 32
        result = self.evaluator()(proxy, bypass)
        self.assertEqual(result["relay"]["state"], "fail")
        self.assertEqual(result["first_byte"]["state"], "pass")

    def test_failed_operation_is_retained_and_cannot_be_scored_as_success(self):
        proxy, bypass = self.phases()
        proxy["records"][-1]["outcome"] = "client_cancelled"
        proxy["records"][-1]["completed"] = False
        result = self.evaluator()(proxy, bypass)
        self.assertEqual(result["correctness"]["state"], "fail")
        self.assertEqual(result["correctness"]["proxy_attempted"], 1000)
        self.assertEqual(result["correctness"]["proxy_success"], 999)
        self.assertNotEqual(result["first_byte"]["state"], "pass")

    def test_missing_samples_or_warmup_cannot_pass(self):
        for field in ("records", "warmup_seconds", "warmup_completed"):
            with self.subTest(field=field):
                proxy, bypass = self.phases()
                if field == "records":
                    proxy[field].pop()
                    bypass[field].pop()
                else:
                    proxy.pop(field)
                result = self.evaluator()(proxy, bypass)
                self.assertEqual(result["sampling"]["state"], "incomplete")
                self.assertNotEqual(result["first_byte"]["state"], "pass")

    def test_nonfinite_and_boolean_timings_are_invalid(self):
        for value in (math.nan, math.inf, True):
            with self.subTest(value=value):
                proxy, bypass = self.phases()
                proxy["records"][0]["first_byte_ms"] = value
                self.assertEqual(self.evaluator()(proxy, bypass)["first_byte"]["state"], "invalid")

    def test_duplicate_or_missing_pair_id_is_invalid(self):
        for action in ("duplicate", "missing"):
            with self.subTest(action=action):
                proxy, bypass = self.phases()
                if action == "duplicate":
                    proxy["records"][-1]["operation_id"] = "0"
                else:
                    proxy["records"][-1].pop("operation_id")
                self.assertEqual(self.evaluator()(proxy, bypass)["first_byte"]["state"], "invalid")

    def test_mismatched_or_exploratory_identity_cannot_receive_release_gate(self):
        for field, value in (("clients", 8), ("size_bytes", 65536), ("repetition", 1)):
            with self.subTest(field=field):
                proxy, bypass = self.phases()
                proxy[field] = value
                result = self.evaluator()(proxy, bypass)
                self.assertEqual(result["sampling"]["state"], "invalid")
                self.assertNotEqual(result["first_byte"]["state"], "pass")

    def test_input_is_not_mutated_and_unknown_fields_are_not_copied(self):
        proxy, bypass = self.phases()
        proxy["private_extra"] = {"private_key": "never-copy"}
        before = copy.deepcopy((proxy, bypass))
        result = self.evaluator()(proxy, bypass)
        self.assertEqual((proxy, bypass), before)
        self.assertNotIn("private_extra", str(result))
        self.assertNotIn("never-copy", str(result))


class CampaignEvidenceTests(unittest.TestCase):
    def loader(self):
        import baseline_acceptance
        self.assertTrue(hasattr(baseline_acceptance, "read_campaign"),
                        "campaign reader is missing")
        return baseline_acceptance.read_campaign

    def campaign(self, root, status="succeeded"):
        report = {"schema_version": 1, "measurements": [], "performance_qualified": False}
        raw = json.dumps(report).encode()
        (root / "job-000-attempt-001.json").write_bytes(raw)
        job = dict(spec=dict(id="text-000", kind="text", clients=4,
                             size=262144, repetition=0, count=250),
                   status=status,
                   attempts=[dict(report="job-000-attempt-001.json", exit_code=0,
                                  sha256=hashlib.sha256(raw).hexdigest())])
        identity = dict(profile="reference", source=dict(commit="a" * 40, dirty=False,
                                                        script_sha256="b" * 64),
                        binary_sha256="c" * 64, seed_binary_sha256="d" * 64,
                        fixture_manifest_sha256="e" * 64, python_version="3.14",
                        jobs_sha256="")
        manifest = dict(schema_version=1, completed=status == "succeeded",
                        performance_qualified=False, identity=identity,
                        fingerprint="", jobs=[job])
        self.save(root, manifest)
        return manifest

    def save(self, root, manifest):
        manifest["identity"]["jobs_sha256"] = hashlib.sha256(json.dumps(
            [j["spec"] for j in manifest["jobs"]], sort_keys=True).encode()).hexdigest()
        manifest["fingerprint"] = hashlib.sha256(json.dumps(
            manifest["identity"], sort_keys=True).encode()).hexdigest()
        (root / "manifest.json").write_text(json.dumps(manifest))

    def test_verified_report_is_read_without_changing_files(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.campaign(root)
            before = {p.name: p.read_bytes() for p in root.iterdir()}
            manifest, reports = self.loader()(root)
            self.assertEqual(len(list(reports)), 1)
            self.assertTrue(manifest["completed"])
            self.assertEqual(before, {p.name: p.read_bytes() for p in root.iterdir()})

    def test_tampered_report_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.campaign(root)
            (root / "job-000-attempt-001.json").write_text("{}")
            with self.assertRaisesRegex(ValueError, "report_hash"):
                _, reports = self.loader()(root)
                list(reports)

    def test_escaped_and_symlink_report_paths_are_rejected(self):
        for mode in ("escaped", "symlink"):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                manifest = self.campaign(root)
                target = root / "job-000-attempt-001.json"
                if mode == "escaped":
                    manifest["jobs"][0]["attempts"][0]["report"] = "../private.json"
                    self.save(root, manifest)
                else:
                    target.rename(root / "other.json")
                    target.symlink_to(root / "other.json")
                with self.assertRaises(ValueError):
                    _, reports = self.loader()(root)
                    list(reports)

    def test_duplicate_job_identity_and_false_completion_are_rejected(self):
        for mode in ("duplicate", "completion"):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                manifest = self.campaign(root)
                if mode == "duplicate":
                    manifest["jobs"].append(copy.deepcopy(manifest["jobs"][0]))
                else:
                    manifest["jobs"][0]["status"] = "pending"
                self.save(root, manifest)
                with self.assertRaises(ValueError):
                    _, reports = self.loader()(root)
                    list(reports)

    def test_running_attempt_is_not_mistaken_for_verified_evidence(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            manifest = self.campaign(root, "running")
            manifest["jobs"][0]["attempts"][0].update(exit_code=None, sha256=None)
            self.save(root, manifest)
            snapshot, reports = self.loader()(root)
            self.assertFalse(snapshot["completed"])
            self.assertEqual(list(reports), [])

    def test_manifest_hash_and_duplicate_json_keys_are_rejected(self):
        for mode in ("hash", "duplicate_keys"):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                manifest = self.campaign(root)
                if mode == "hash":
                    manifest["fingerprint"] = "f" * 64
                    (root / "manifest.json").write_text(json.dumps(manifest))
                else:
                    (root / "manifest.json").write_text('{"schema_version":1,"schema_version":1}')
                with self.assertRaises(ValueError):
                    self.loader()(root)

    def test_missing_success_report_and_failed_exit_are_rejected(self):
        for mode in ("missing", "exit"):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                manifest = self.campaign(root)
                if mode == "missing":
                    (root / "job-000-attempt-001.json").unlink()
                else:
                    manifest["jobs"][0]["attempts"][0]["exit_code"] = 1
                    self.save(root, manifest)
                with self.assertRaises(ValueError):
                    _, reports = self.loader()(root)
                    list(reports)


class ThroughputAcceptanceTests(unittest.TestCase):
    def evaluate(self, proxy, bypass):
        import baseline_acceptance
        self.assertTrue(hasattr(baseline_acceptance, "evaluate_throughput"),
                        "throughput evaluator is missing")
        return baseline_acceptance.evaluate_throughput(proxy, bypass)

    def phases(self):
        proxy, bypass = TextAcceptanceTests().phases()
        for phase in (proxy, bypass):
            phase["window_seconds"] = 600.0
            phase["window_start_s"] = 0.0
            for i, record in enumerate(phase["records"]):
                record.update(started_s=float(i) / 10, finished_s=599.0)
        for record in proxy["records"][850:]:
            record["finished_s"] = 601.0
        return proxy, bypass

    def test_exact_ratio_passes_and_late_completions_are_excluded(self):
        result = self.evaluate(*self.phases())
        self.assertEqual(result["state"], "pass")
        self.assertEqual(result["ratio"], 0.85)
        self.assertEqual(result["proxy_window_success"], 850)
        self.assertEqual(result["bypass_window_success"], 1000)

    def test_ratio_below_target_fails(self):
        proxy, bypass = self.phases()
        proxy["records"][849]["finished_s"] = 601.0
        self.assertEqual(self.evaluate(proxy, bypass)["state"], "fail")

    def test_unexpected_failure_is_not_hidden_by_throughput(self):
        proxy, bypass = self.phases()
        proxy["records"][-1].update(completed=False, outcome="transformation_error")
        self.assertEqual(self.evaluate(proxy, bypass)["state"], "fail")

    def test_short_or_mismatched_windows_cannot_pass(self):
        for duration in (None, 599.0, 601.0, True, math.nan):
            with self.subTest(duration=duration):
                proxy, bypass = self.phases()
                proxy["window_seconds"] = duration
                self.assertNotEqual(self.evaluate(proxy, bypass)["state"], "pass")

    def test_empty_or_zero_success_bypass_is_incomplete(self):
        proxy, bypass = self.phases()
        for record in bypass["records"]:
            record["finished_s"] = 601.0
        self.assertEqual(self.evaluate(proxy, bypass)["state"], "incomplete")

    def test_invalid_finish_or_duplicate_operations_are_rejected(self):
        for mode in ("timing", "duplicate"):
            with self.subTest(mode=mode):
                proxy, bypass = self.phases()
                if mode == "timing":
                    proxy["records"][0]["finished_s"] = math.nan
                else:
                    proxy["records"][-1]["operation_id"] = "0"
                self.assertEqual(self.evaluate(proxy, bypass)["state"], "invalid")


class ResourceAcceptanceTests(unittest.TestCase):
    def evaluate(self, phase):
        import baseline_acceptance
        self.assertTrue(hasattr(baseline_acceptance, "evaluate_resources"),
                        "resource evaluator is missing")
        return baseline_acceptance.evaluate_resources(phase)

    def phase(self):
        return dict(resource_samples=[dict(proxy_rss_bytes=512 * 1024**2,
                                           child_rss_bytes=1024**3)])

    def test_peak_limits_pass_and_exceedance_fails(self):
        self.assertEqual(self.evaluate(self.phase())["state"], "pass")
        for field in ("proxy_rss_bytes", "child_rss_bytes"):
            phase = self.phase()
            phase["resource_samples"][0][field] += 1
            self.assertEqual(self.evaluate(phase)["state"], "fail")

    def test_sample_peaks_are_recomputed_instead_of_trusting_summary(self):
        phase = self.phase()
        phase["peak_proxy_rss_bytes"] = 1
        phase["resource_samples"].append(dict(proxy_rss_bytes=600 * 1024**2, child_rss_bytes=1))
        result = self.evaluate(phase)
        self.assertEqual(result["state"], "fail")
        self.assertEqual(result["peak_proxy_rss_bytes"], 600 * 1024**2)

    def test_missing_measurements_are_incomplete_not_zero(self):
        for phase in ({}, dict(resource_samples=[]), dict(resource_samples=[dict(proxy_rss_bytes=None, child_rss_bytes=None)])):
            self.assertEqual(self.evaluate(phase)["state"], "incomplete")

    def test_extraction_summary_requires_observed_samples(self):
        phase = dict(resources=dict(peak_proxy_rss_bytes=100, peak_child_rss_bytes=200,
                                    proxy_rss_samples=5, child_rss_samples=5))
        self.assertEqual(self.evaluate(phase)["state"], "pass")
        phase["resources"]["child_rss_samples"] = 0
        self.assertEqual(self.evaluate(phase)["state"], "incomplete")

    def test_known_peak_failure_is_not_hidden_by_another_missing_sample(self):
        phase = self.phase()
        phase["resource_samples"][0]["proxy_rss_bytes"] = 600 * 1024**2
        phase["resource_samples"].append(dict(proxy_rss_bytes=None, child_rss_bytes=None))
        self.assertEqual(self.evaluate(phase)["state"], "fail")

    def test_invalid_measurements_are_invalid(self):
        for value in (-1, True, math.inf, "0"):
            phase = self.phase()
            phase["resource_samples"][0]["proxy_rss_bytes"] = value
            self.assertEqual(self.evaluate(phase)["state"], "invalid")


class ExtractionAcceptanceTests(unittest.TestCase):
    def evaluate(self, report):
        import baseline_acceptance
        self.assertTrue(hasattr(baseline_acceptance, "evaluate_extraction"),
                        "extraction evaluator is missing")
        return baseline_acceptance.evaluate_extraction(report)

    def report(self):
        records = [dict(operation_id=str(i), supported=True, completed=True,
                        outcome="supported_success") for i in range(100)]
        return dict(settings=dict(template="0", clients=4, count=100,
                                  warmup_seconds=60.0, warmup_count=100,
                                  repetitions=1, repeated=False),
                    measurements=[dict(records=records, warmup_completed=100, warmup_seconds=60.0,
                                       repetition=0, stage_poll_errors=0,
                                       achieved_concurrency=4,
                                       stage_samples=dict(extraction_run=[5000.0] * 100,
                                                          extraction_wait=[2000.0] * 100))])

    def test_image_thresholds_and_total_budget_are_independent(self):
        result = self.evaluate(self.report())[0]
        self.assertEqual(result["run"]["state"], "pass")
        self.assertEqual(result["queue"]["state"], "pass")
        self.assertEqual(result["budget"]["state"], "pass")
        report = self.report()
        report["measurements"][0]["stage_samples"]["extraction_run"][-1] = 29000.0
        result = self.evaluate(report)[0]
        self.assertEqual(result["run"]["state"], "pass")
        self.assertEqual(result["budget"]["state"], "fail")

    def test_pdf_has_separate_run_threshold(self):
        report = self.report()
        report["settings"]["template"] = "pdf-text"
        report["measurements"][0]["stage_samples"]["extraction_run"] = [10000.0] * 100
        self.assertEqual(self.evaluate(report)[0]["run"]["state"], "pass")
        report["measurements"][0]["stage_samples"]["extraction_run"] = [10001.0] * 100
        self.assertEqual(self.evaluate(report)[0]["run"]["state"], "fail")

    def test_failure_and_missing_stage_samples_cannot_pass(self):
        report = self.report()
        report["measurements"][0]["records"][-1]["outcome"] = "dependency_failure"
        result = self.evaluate(report)[0]
        self.assertEqual(result["correctness"]["state"], "fail")
        self.assertNotEqual(result["run"]["state"], "pass")
        report = self.report()
        report["measurements"][0]["stage_samples"]["extraction_run"].pop()
        self.assertEqual(self.evaluate(report)[0]["run"]["state"], "incomplete")

    def test_poll_errors_and_nonfinite_timings_invalidate_evidence(self):
        for mode in ("poll", "timing"):
            with self.subTest(mode=mode):
                report = self.report()
                phase = report["measurements"][0]
                if mode == "poll":
                    phase["stage_poll_errors"] = 1
                else:
                    phase["stage_samples"]["extraction_run"][0] = math.nan
                self.assertEqual(self.evaluate(report)[0]["run"]["state"], "invalid")

    def test_duplicate_image_identity_cannot_supply_sample_count(self):
        report = self.report()
        report["measurements"][0]["records"][-1]["operation_id"] = "0"
        self.assertEqual(self.evaluate(report)[0]["correctness"]["state"], "invalid")
        self.assertNotEqual(self.evaluate(report)[0]["run"]["state"], "pass")

    def test_short_smoke_is_incomplete_and_one_client_is_capacity_only(self):
        for mode in ("smoke", "one_client"):
            report = self.report()
            if mode == "smoke":
                report["settings"]["warmup_seconds"] = 1.0
                report["measurements"][0]["warmup_seconds"] = 1.0
            else:
                report["settings"]["clients"] = 1
                report["measurements"][0]["achieved_concurrency"] = 1
            self.assertNotEqual(self.evaluate(report)[0]["run"]["state"], "pass")


class SummaryAcceptanceTests(unittest.TestCase):
    campaign = CampaignEvidenceTests.campaign
    save = CampaignEvidenceTests.save
    def summarizer(self):
        import baseline_acceptance
        self.assertTrue(hasattr(baseline_acceptance, "summarize_campaign"),
                        "campaign summary is missing")
        return baseline_acceptance.summarize_campaign

    def test_partial_reference_cannot_pass_coverage_or_qualification(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.campaign(root)
            summary = self.summarizer()(root)
            self.assertEqual(summary["coverage"]["state"], "incomplete")
            self.assertEqual(summary["coverage"]["required_jobs"], 123)
            self.assertEqual(summary["counts"]["succeeded"], 1)
            self.assertFalse(summary["performance_qualified"])

    def test_running_job_and_pending_jobs_are_preserved(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            manifest = self.campaign(root, "running")
            manifest["jobs"][0]["attempts"][0].update(exit_code=None, sha256=None)
            self.save(root, manifest)
            summary = self.summarizer()(root)
            self.assertEqual(summary["counts"]["running"], 1)
            self.assertEqual(summary["evaluated_jobs"], [])
            self.assertFalse(summary["performance_qualified"])

    def test_changed_workload_is_invalid_reference_coverage(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            manifest = self.campaign(root)
            manifest["jobs"][0]["spec"]["count"] = 2
            self.save(root, manifest)
            self.assertEqual(self.summarizer()(root)["coverage"]["state"], "invalid")

    def test_dirty_smoke_with_consistent_source_keeps_diagnostic_metrics(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            manifest = self.campaign(root)
            manifest["identity"].update(profile="smoke")
            manifest["identity"]["source"]["dirty"] = True
            report = dict(schema_version=1, source=dict(commit="a" * 40, dirty=True),
                          executable_sha256="c" * 64,
                          measurements=TextAcceptanceTests().phases())
            raw = json.dumps(report).encode()
            (root / "job-000-attempt-001.json").write_bytes(raw)
            manifest["jobs"][0]["attempts"][0]["sha256"] = hashlib.sha256(raw).hexdigest()
            self.save(root, manifest)
            summary = self.summarizer()(root)
            self.assertEqual(summary["provenance"]["state"], "incomplete")
            self.assertIn("text", summary["evaluated_jobs"][0]["observations"])
            self.assertFalse(summary["performance_qualified"])

    def test_private_fields_never_reach_summary(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            manifest = self.campaign(root)
            manifest["identity"]["private_extra"] = "never-copy-this-value"
            manifest["jobs"][0]["private_extra"] = "never-copy-this-value"
            self.save(root, manifest)
            output = json.dumps(self.summarizer()(root))
            self.assertNotIn("private_extra", output)
            self.assertNotIn("never-copy-this-value", output)
            self.assertNotIn(str(root), output)

    def test_missing_reference_observations_are_incomplete(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.campaign(root)
            summary = self.summarizer()(root)
            self.assertEqual(summary["provenance"]["state"], "incomplete")

    def test_cli_writes_private_output_and_refuses_overwrite(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.campaign(root)
            destination = root / "summary.json"
            script = Path(__file__).with_name("baseline_acceptance.py")
            command = [sys.executable, str(script), str(root), "--output", str(destination)]
            first = subprocess.run(command, capture_output=True, text=True)
            self.assertEqual(first.returncode, 0)
            self.assertTrue(destination.is_file())
            self.assertEqual(destination.stat().st_mode & 0o777, 0o600)
            saved = destination.read_bytes()
            second = subprocess.run(command, capture_output=True, text=True)
            self.assertNotEqual(second.returncode, 0)
            self.assertEqual(destination.read_bytes(), saved)
            self.assertNotIn(str(root), first.stdout + first.stderr + second.stdout + second.stderr)

    def test_cli_invalid_input_reports_no_private_error_text(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "manifest.json").write_text('{"private_extra":"never-copy-this-value"')
            script = Path(__file__).with_name("baseline_acceptance.py")
            result = subprocess.run([sys.executable, str(script), str(root), "--output", str(root / "summary.json")], capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertNotIn("never-copy-this-value", result.stdout + result.stderr)
            self.assertNotIn("Traceback", result.stdout + result.stderr)


class FairnessAcceptanceTests(unittest.TestCase):
    def phases(self):
        phases = TextAcceptanceTests().phases()
        for phase in phases:
            phase["text_records"] = phase.pop("records")
            phase["image_records"] = [dict(operation_id="image-" + str(i), supported=True,
                                           completed=True, outcome="supported_success")
                                      for i in range(100)]
            phase["records"] = phase["text_records"] + phase["image_records"]
            phase["stage_poll_errors"] = 0
            phase["stage_samples"] = dict(extraction_run=[5000.0] * 100,
                                          extraction_wait=[2000.0] * 100)
        return phases

    def evaluate(self, proxy, bypass):
        import baseline_acceptance
        self.assertTrue(hasattr(baseline_acceptance, "evaluate_fairness"),
                        "fairness evaluator is missing")
        return baseline_acceptance.evaluate_fairness(proxy, bypass)

    def test_text_and_image_gates_are_both_required(self):
        result = self.evaluate(*self.phases())
        self.assertEqual(result["text"]["first_byte"]["state"], "pass")
        self.assertEqual(result["images"]["run"]["state"], "pass")
        self.assertFalse(result["performance_qualified"])

    def test_image_failure_cannot_be_hidden_by_successful_text(self):
        proxy, bypass = self.phases()
        proxy["image_records"][-1]["outcome"] = "transformation_error"
        result = self.evaluate(proxy, bypass)
        self.assertEqual(result["text"]["first_byte"]["state"], "pass")
        self.assertEqual(result["images"]["correctness"]["state"], "fail")
        self.assertNotEqual(result["images"]["run"]["state"], "pass")

    def test_image_queue_exceedance_is_reported(self):
        proxy, bypass = self.phases()
        proxy["stage_samples"]["extraction_wait"] = [2001.0] * 100
        self.assertEqual(self.evaluate(proxy, bypass)["images"]["queue"]["state"], "fail")

    def test_partition_mismatch_and_duplicate_ids_are_invalid(self):
        for mode in ("missing", "duplicate"):
            with self.subTest(mode=mode):
                proxy, bypass = self.phases()
                if mode == "missing":
                    proxy["records"].pop()
                else:
                    proxy["image_records"][-1]["operation_id"] = "0"
                self.assertEqual(self.evaluate(proxy, bypass)["state"], "invalid")

    def test_long_fairness_image_population_is_not_truncated(self):
        proxy, bypass = self.phases()
        for phase in (proxy, bypass):
            for i in range(100, 300):
                phase["image_records"].append(dict(operation_id="image-" + str(i), supported=True,
                                                   completed=True, outcome="supported_success"))
            phase["records"] = phase["text_records"] + phase["image_records"]
            phase["stage_samples"] = dict(extraction_run=[5000.0] * 300,
                                          extraction_wait=[2000.0] * 300)
        result = self.evaluate(proxy, bypass)
        self.assertEqual(result["images"]["correctness"]["attempted"], 300)
        self.assertEqual(result["images"]["run"]["state"], "pass")

    def test_missing_duration_evidence_keeps_fairness_incomplete(self):
        proxy, bypass = self.phases()
        proxy.pop("warmup_seconds")
        self.assertEqual(self.evaluate(proxy, bypass)["text"]["sampling"]["state"], "incomplete")


class OverloadAcceptanceTests(unittest.TestCase):
    def evaluate(self, phase):
        import baseline_acceptance
        self.assertTrue(hasattr(baseline_acceptance, "evaluate_overload"),
                        "overload evaluator is missing")
        return baseline_acceptance.evaluate_overload(phase)

    def phase(self):
        return dict(scheduled=4, sent=2, completed=2, rejected=0, client_unsent=2,
                    collection_failed=False, records=[
                        dict(operation_id="0", supported=True, completed=True,
                             outcome="supported_success", duration_ms=45000.0),
                        dict(operation_id="1", supported=True, completed=True,
                             outcome="supported_success", duration_ms=1000.0)],
                    unsent_records=[dict(operation_id="2"), dict(operation_id="3")])

    def test_unsent_work_stays_in_denominator(self):
        result = self.evaluate(self.phase())
        self.assertEqual(result["state"], "pass")
        self.assertEqual(result["scheduled"], 4)
        self.assertEqual(result["supported_success"], 2)
        self.assertEqual(result["client_unsent"], 2)

    def test_unbounded_duration_and_unexpected_failure_fail(self):
        for mode in ("duration", "failure"):
            with self.subTest(mode=mode):
                phase = self.phase()
                if mode == "duration":
                    phase["records"][0]["duration_ms"] = 45000.1
                else:
                    phase["records"][0]["outcome"] = "transformation_error"
                self.assertEqual(self.evaluate(phase)["state"], "fail")

    def test_accounting_mismatch_or_duplicate_slot_is_invalid(self):
        for mode in ("count", "duplicate"):
            with self.subTest(mode=mode):
                phase = self.phase()
                if mode == "count":
                    phase["scheduled"] = 5
                else:
                    phase["unsent_records"][-1]["operation_id"] = "0"
                self.assertEqual(self.evaluate(phase)["state"], "invalid")

    def test_collection_failure_never_passes(self):
        phase = self.phase()
        phase["collection_failed"] = True
        self.assertEqual(self.evaluate(phase)["state"], "incomplete")

    def test_nonfinite_duration_is_invalid(self):
        phase = self.phase()
        phase["records"][0]["duration_ms"] = math.nan
        self.assertEqual(self.evaluate(phase)["state"], "invalid")


class ReviewRegressionTests(unittest.TestCase):
    def test_text_exceedance_survives_missing_warmup(self):
        from baseline_acceptance import evaluate_text
        proxy, bypass = TextAcceptanceTests().phases()
        proxy.pop("warmup_seconds")
        for record in proxy["records"]:
            record["first_byte_ms"] = 1151.0
        result = evaluate_text(proxy, bypass)
        self.assertEqual(result["sampling"]["state"], "incomplete")
        self.assertEqual(result["first_byte"]["state"], "fail")

    def test_extraction_exceedance_survives_missing_observed_warmup(self):
        from baseline_acceptance import evaluate_extraction
        report = ExtractionAcceptanceTests().report()
        report["measurements"][0].pop("warmup_seconds")
        report["settings"].pop("warmup_seconds")
        report["measurements"][0]["stage_samples"]["extraction_run"] = [5001.0] * 100
        result = evaluate_extraction(report)[0]
        self.assertEqual(result["run"]["state"], "fail")
        self.assertEqual(result["queue"]["state"], "incomplete")

    def test_configured_extraction_warmup_is_not_observed_evidence(self):
        from baseline_acceptance import evaluate_extraction
        report = ExtractionAcceptanceTests().report()
        report["measurements"][0].pop("warmup_seconds")
        result = evaluate_extraction(report)[0]
        for gate in ("run", "queue", "budget"):
            self.assertEqual(result[gate]["state"], "incomplete")

    def test_extraction_report_is_bound_to_its_job(self):
        from baseline_acceptance import _report_observations
        spec = dict(kind="extraction", template=0, clients=4, count=100, repeated=False, repetition=0)
        for field, value in (("template", "pdf-text"), ("clients", 1), ("repeated", True), ("count", 99), ("repetition", 2)):
            with self.subTest(field=field):
                report = ExtractionAcceptanceTests().report()
                if field == "repetition":
                    report["measurements"][0][field] = value
                else:
                    report["settings"][field] = value
                self.assertEqual(_report_observations(spec, report).get("state"), "invalid")

    def test_overload_report_is_bound_to_rate_window_and_repetition(self):
        from baseline_acceptance import _report_observations
        spec = dict(kind="overload", size=262144, repetition=0, rates=[1], duration=120, max_outstanding=16)
        for field, value in (("offered_rate", 8), ("requested_duration_s", 1), ("repetition", 2), ("size_bytes", 65536), ("max_outstanding", 2)):
            with self.subTest(field=field):
                phases = []
                for mode in ("proxy", "bypass"):
                    phase = OverloadAcceptanceTests().phase()
                    phase.update(mode=mode, offered_rate=1, requested_duration_s=120,
                                 window_seconds=120, repetition=0, size_bytes=262144,
                                 max_outstanding=16)
                    phases.append(phase)
                phases[0][field] = value
                self.assertEqual(_report_observations(spec, dict(measurements=phases)).get("state"), "invalid")

    def test_overload_completed_and_rejected_must_match_records(self):
        from baseline_acceptance import evaluate_overload
        for field, value in (("completed", 0), ("rejected", 2)):
            phase = OverloadAcceptanceTests().phase()
            phase[field] = value
            self.assertEqual(evaluate_overload(phase)["state"], "invalid")

    def test_overload_rejection_count_uses_http_status(self):
        from baseline_acceptance import evaluate_overload
        phase = OverloadAcceptanceTests().phase()
        phase["records"][0].update(status=429, outcome="proxy_overload", completed=False)
        phase.update(completed=1, rejected=1)
        self.assertEqual(evaluate_overload(phase)["state"], "pass")

    def test_extraction_resources_are_reported(self):
        from baseline_acceptance import _report_observations
        spec = dict(kind="extraction", template=0, clients=4, count=100, repeated=False, repetition=0)
        report = ExtractionAcceptanceTests().report()
        report["measurements"][0]["resources"] = dict(
            peak_proxy_rss_bytes=600 * 1024**2, peak_child_rss_bytes=2 * 1024**3,
            proxy_rss_samples=10, child_rss_samples=10)
        result = _report_observations(spec, report)["repetitions"][0]
        self.assertEqual(result.get("resources", {}).get("state"), "fail")

    def test_throughput_uses_actual_origin_not_first_operation(self):
        from baseline_acceptance import evaluate_throughput
        proxy, bypass = ThroughputAcceptanceTests().phases()
        for phase in (proxy, bypass):
            for record in phase["records"]:
                record["started_s"] += 5.0
        result = evaluate_throughput(proxy, bypass)
        self.assertEqual(result["proxy_window_success"], 850)
        self.assertEqual(result["ratio"], 0.85)

    def test_missing_throughput_origin_is_incomplete(self):
        from baseline_acceptance import evaluate_throughput
        proxy, bypass = ThroughputAcceptanceTests().phases()
        proxy.pop("window_start_s")
        self.assertEqual(evaluate_throughput(proxy, bypass)["state"], "incomplete")

    def test_nonfinite_outcome_duration_is_invalid(self):
        from baseline_acceptance import evaluate_text
        proxy, bypass = TextAcceptanceTests().phases()
        proxy["records"][0]["duration_ms"] = math.inf
        result = evaluate_text(proxy, bypass)
        self.assertEqual(result["correctness"]["state"], "invalid")
        self.assertNotEqual(result["first_byte"]["state"], "pass")

    def test_json_numeric_overflow_is_rejected(self):
        from baseline_acceptance import _decode
        with self.assertRaises(ValueError):
            _decode(b'{"duration_ms":1e999}')


if __name__ == "__main__":
    unittest.main()
