import unittest

from performance_churn import ChurnWorkload, scheduled_offsets
from performance_workload import Workload


class ChurnWorkloadTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.workload = ChurnWorkload()

    def test_initial_population_and_policy_remain_normal(self):
        normal = Workload("normal")
        self.assertEqual(self.workload.initial.pairs, normal.pairs)
        self.assertEqual(self.workload.initial.rules, normal.rules)
        self.assertEqual(self.workload.initial.dictionary, normal.dictionary)
        self.assertEqual(len(self.workload.initial.pairs), 10000)

    def test_fresh_population_is_disjoint_unique_and_same_class(self):
        initial = {tuple(pair) for pair in self.workload.initial.pairs}
        fresh = [tuple(self.workload.new_pair(i)) for i in range(1000)]
        self.assertEqual(len(set(fresh)), 1000)
        self.assertFalse(initial.intersection(fresh))
        self.assertEqual({p[0] for p in fresh}, {p[0] for p in initial})

    def test_each_case_introduces_exactly_one_pair_and_preserves_controls(self):
        initial = {pair[1] for pair in self.workload.initial.pairs}
        for index in (0, 1, 999):
            base = self.workload.initial.case(index, 1048576)
            case = self.workload.case(index, 1048576)
            fresh = self.workload.new_pair(index)[1]
            self.assertEqual(set(case["occurrences"]) - initial, {fresh})
            self.assertEqual(len(case["occurrences"]), len(base["occurrences"]))
            self.assertEqual(case["near_misses"], base["near_misses"])
            self.assertIn(fresh.encode(), case["body"])
            self.assertLessEqual(abs(len(case["body"]) - 1048576), 10485)
            self.assertEqual(case, self.workload.case(index, 1048576))
        self.assertEqual(len(self.workload.initial.pairs), 10000)

    def test_rejects_out_of_range_pair_indices(self):
        for index in (-1, 90000, True, 1.5):
            with self.assertRaises(ValueError):
                self.workload.new_pair(index)

    def test_exact_rate_and_exclusive_window_end(self):
        offsets = scheduled_offsets(600)
        self.assertEqual(len(offsets), 1000)
        self.assertEqual(offsets[0], 0)
        self.assertAlmostEqual(offsets[-1], 599.4)
        for minute in range(10):
            self.assertEqual(sum(60 * minute <= x < 60 * (minute + 1) for x in offsets), 100)
        self.assertEqual(scheduled_offsets(0.6), [0.0])

    def test_rejects_invalid_windows(self):
        for duration in (0, -1, True, float("nan"), float("inf"), 54001):
            with self.assertRaises(ValueError):
                scheduled_offsets(duration)


class DeferredBoundaryTests(unittest.TestCase):
    def request_row(self, digest, valid_stream=True, deferred=True):
        import hashlib
        import json
        from unittest.mock import MagicMock, patch
        import performance
        case = ChurnWorkload().case(0, 262144)
        values = case["occurrences"] if valid_stream else ["wrong-restoration"]
        events = [{"type": "response.output_text.delta", "delta": chunk}
                  for chunk in performance.stream_chunks(values)]
        events.append({"type": "response.completed", "response": {"status": "completed"}})
        wire = b"".join(b"data: " + json.dumps(e).encode() + b"\n\n" for e in events)
        response = MagicMock()
        response.status = 200
        response.getheader.side_effect = lambda name, *args: digest if name == "X-Benchmark-Body-SHA256" else None
        response.read1.side_effect = [wire, b""]
        connection = MagicMock()
        connection.getresponse.return_value = response
        with patch.object(performance.http.client, "HTTPConnection", return_value=connection):
            return performance.request("http://127.0.0.1:1", case, None if deferred else digest, defer_boundary=deferred), case

    def test_existing_immediate_boundary_path_still_verifies_success(self):
        row, _ = self.request_row("a" * 64, deferred=False)
        self.assertEqual(row["outcome"], "supported_success")
        self.assertNotIn("boundary_sha256", row)

    def test_deferred_valid_stream_is_pending_not_success(self):
        row, _ = self.request_row("a" * 64)
        self.assertEqual(row["outcome"], "boundary_pending")
        self.assertEqual(row["boundary_sha256"], "a" * 64)

    def test_invalid_digest_cannot_be_pending(self):
        for digest in (None, "invalid", "g" * 64):
            row, _ = self.request_row(digest)
            self.assertNotEqual(row["outcome"], "boundary_pending")
            self.assertNotEqual(row["outcome"], "supported_success")

    def test_wrong_restoration_cannot_be_pending(self):
        row, _ = self.request_row("a" * 64, False)
        self.assertNotEqual(row["outcome"], "boundary_pending")
        self.assertNotEqual(row["outcome"], "supported_success")

    def test_finalization_requires_complete_correct_mapping_evidence(self):
        import hashlib
        from performance_churn import finalize_boundary
        case = ChurnWorkload().case(0, 262144)
        mappings = {value: "mapped-" + str(i) for i, value in enumerate(case["occurrences"])}
        expected = case["body"]
        for value, replacement in mappings.items():
            expected = expected.replace(value.encode(), replacement.encode())
        pending = dict(outcome="boundary_pending", boundary_sha256=hashlib.sha256(expected).hexdigest())
        self.assertEqual(finalize_boundary(pending, case, mappings)["outcome"], "supported_success")
        self.assertEqual(pending["outcome"], "boundary_pending")
        self.assertNotEqual(finalize_boundary(pending, case, {})["outcome"], "supported_success")
        bad = dict(pending, boundary_sha256="0" * 64)
        self.assertEqual(finalize_boundary(bad, case, mappings)["outcome"], "transformation_error")
        failed = dict(pending, outcome="client_or_model_failure")
        self.assertEqual(finalize_boundary(failed, case, mappings), failed)


class ChurnResolutionTests(unittest.TestCase):
    def test_only_sent_verified_fresh_pairs_count_as_observed_growth(self):
        import hashlib
        from performance_churn import resolve_records
        workload = ChurnWorkload()
        rows, mappings = [], {}
        for index in (0, 2):
            case = workload.case(index, 262144)
            for value in case["occurrences"]:
                mappings[value] = "replacement-" + str(len(mappings))
        for index in (0, 2):
            case = workload.case(index, 262144)
            expected = case["body"]
            for value in dict.fromkeys(case["occurrences"]):
                expected = expected.replace(value.encode(), mappings[value].encode())
            rows.append(dict(churn_index=index, outcome="boundary_pending",
                             boundary_sha256=hashlib.sha256(expected).hexdigest()))
        result = resolve_records(rows, workload, 262144, mappings)
        self.assertEqual(result["observed_new_pairs"], 2)
        self.assertEqual(result["verified_new_pairs"], 2)
        self.assertTrue(all(r["outcome"] == "supported_success" for r in result["records"]))
        self.assertTrue(all(r["outcome"] == "boundary_pending" for r in rows))
        failed = [dict(rows[0], outcome="client_or_model_failure")]
        result = resolve_records(failed, workload, 262144, mappings)
        self.assertEqual(result["observed_new_pairs"], 1)
        self.assertEqual(result["verified_new_pairs"], 0)

    def test_duplicate_indices_cannot_inflate_growth(self):
        from performance_churn import resolve_records
        row = dict(churn_index=0, outcome="client_or_model_failure")
        with self.assertRaises(ValueError):
            resolve_records([row, row], ChurnWorkload(), 262144, {})

    def test_missing_index_preserves_failure_and_missing_boundary_evidence(self):
        from performance_churn import resolve_records
        workload = ChurnWorkload()
        rows = [dict(outcome="client_or_model_failure")]
        result = resolve_records(rows, workload, 262144, {})
        self.assertEqual(result["records"], rows)
        self.assertEqual(result["unresolved_indices"], 1)
        self.assertEqual(result["observed_new_pairs"], 0)


class ObservedVaultTests(unittest.TestCase):
    def inspect(self, payload, returncode=0):
        import json
        import tempfile
        from pathlib import Path
        from unittest.mock import patch
        from types import SimpleNamespace
        from performance_churn import inspect_vault
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            def helper(*args, **kwargs):
                self.assertTrue(kwargs["capture_output"])
                self.assertEqual(args[0][1:], ["inspect", str(directory)])
                if payload is not None:
                    (directory / "observed.json").write_text(json.dumps(payload))
                    (directory / "observed.json").chmod(0o600)
                return SimpleNamespace(returncode=returncode)
            with patch("subprocess.run", side_effect=helper):
                return inspect_vault(directory, "/unused/seed")

    def test_valid_private_observation_is_returned(self):
        expected = dict(population=1, mappings={"original": "replacement"})
        self.assertEqual(self.inspect(expected), expected)

    def test_missing_or_invalid_observation_is_rejected(self):
        for payload in (None, {}, dict(population=True, mappings={}),
                        dict(population=2, mappings={"original": "replacement"}),
                        dict(population=1, mappings={"original": "original"}),
                        dict(population=1, mappings={"original": ""})):
            with self.assertRaises(ValueError):
                self.inspect(payload)
        with self.assertRaises(ValueError):
            self.inspect(dict(population=0, mappings={}), returncode=1)

    def test_existing_observation_is_never_reused(self):
        import tempfile
        from pathlib import Path
        from unittest.mock import patch
        from performance_churn import inspect_vault
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            (directory / "observed.json").write_text("{}")
            with patch("subprocess.run") as helper:
                with self.assertRaises(ValueError):
                    inspect_vault(directory, "/unused/seed")
                helper.assert_not_called()


class ChurnPhaseTests(unittest.TestCase):
    def run_phase(self, bypass=False, observation_fails=False, warmup_fails=False):
        import hashlib
        from contextlib import ExitStack
        from pathlib import Path
        from types import SimpleNamespace
        from unittest.mock import MagicMock, patch
        import performance_churn as churn
        workload = ChurnWorkload()
        mappings = {p[1]: "mapped-" + str(i) for i, p in enumerate(workload.initial.pairs)}
        for index in range(2):
            mappings[workload.new_pair(index)[1]] = "fresh-mapped-" + str(index)
        env = SimpleNamespace(directory=Path("/unused/private"), mappings=dict(mappings),
                              url="http://127.0.0.1:1", upstream_url="http://127.0.0.1:2",
                              metrics=lambda: {}, population={}, vault_count=lambda: 10000)
        environment = MagicMock()
        environment.__enter__.return_value = env
        sampler = MagicMock()
        sampler.__enter__.return_value = SimpleNamespace(samples=[])
        clock = [0.0]
        calls = []
        def request(url, case, expected, **kwargs):
            clock[0] += 1
            deferred = kwargs.get("defer_boundary", False)
            calls.append((url, deferred, case["operation_id"]))
            if deferred:
                body = case["body"]
                for original in dict.fromkeys(case["occurrences"]):
                    body = body.replace(original.encode(), mappings[original].encode())
                return dict(operation_id=case["operation_id"], supported=True, completed=True, duration_ms=1,
                            outcome="boundary_pending", boundary_sha256=hashlib.sha256(body).hexdigest())
            return dict(operation_id=case["operation_id"], supported=True, completed=True, duration_ms=1,
                        outcome="transformation_error" if warmup_fails else "supported_success")
        def load(operation, rate, duration, max_outstanding):
            self.assertEqual(rate, 100 / 60)
            self.assertEqual(max_outstanding, 4)
            return dict(records=[operation(0), operation(1)], unsent_records=[dict(operation_id="unsent")],
                        scheduled=3, sent=2, completed=2, rejected=0, client_unsent=1,
                        window_start_s=clock[0], window_seconds=duration)
        with ExitStack() as stack:
            stack.enter_context(patch("performance.Environment", return_value=environment))
            stack.enter_context(patch("performance.ResourceSampler", return_value=sampler))
            stack.enter_context(patch("performance.request", side_effect=request))
            scheduler = stack.enter_context(patch("performance_load.open_loop", side_effect=load))
            observation = stack.enter_context(patch.object(churn, "inspect_vault", side_effect=ValueError("unavailable") if observation_fails else None,
                                            return_value=dict(population=10002, mappings=mappings)))
            try:
                result = churn.run_phase("/unused/proxy", "/unused/seed", workload,
                                         size=262144, duration=1.8, bypass=bypass,
                                         warmup_seconds=2, warmup_count=4, clock=lambda: clock[0])
            except churn.ChurnPhaseFailure as error:
                return error.report, calls, scheduler.call_count, observation.call_count, True
            return result, calls, scheduler.call_count, observation.call_count, False

    def test_proxy_resolves_boundary_and_keeps_unsent_denominator(self):
        report, calls, scheduled, observed, failed = self.run_phase()
        self.assertFalse(failed)
        self.assertEqual((scheduled, observed), (1, 1))
        self.assertEqual(report["warmup_completed"], 4)
        self.assertGreaterEqual(report["warmup_seconds"], 2)
        self.assertEqual(report["scheduled"], 3)
        self.assertEqual(report["client_unsent"], 1)
        self.assertEqual(report["observed_new_pairs"], 2)
        self.assertEqual(report["verified_new_pairs"], 2)
        self.assertEqual(report["final_vault_population"], 10002)
        self.assertTrue(all(r["outcome"] == "supported_success" for r in report["records"]))
        self.assertEqual(sum(deferred for _, deferred, _ in calls), 2)
        self.assertFalse(report["performance_qualified"])

    def test_bypass_keeps_original_boundary_and_does_not_inspect_growth(self):
        report, calls, _, observed, failed = self.run_phase(bypass=True)
        self.assertFalse(failed)
        self.assertEqual(observed, 0)
        self.assertTrue(all(not deferred for _, deferred, _ in calls))
        self.assertIsNone(report["observed_new_pairs"])

    def test_failed_observation_retains_all_measured_attempts(self):
        report, _, _, _, failed = self.run_phase(observation_fails=True)
        self.assertTrue(failed)
        self.assertEqual(len(report["records"]), 2)
        self.assertEqual(report["client_unsent"], 1)
        self.assertTrue(all(r["outcome"] != "supported_success" for r in report["records"]))

    def test_failed_warmup_never_starts_measurement(self):
        report, _, scheduled, observed, failed = self.run_phase(warmup_fails=True)
        self.assertTrue(failed)
        self.assertEqual((scheduled, observed), (0, 0))
        self.assertFalse(report["performance_qualified"])

    def test_measured_request_exception_remains_in_failure_denominator(self):
        from unittest.mock import patch
        import performance_churn as churn
        original = churn.ChurnWorkload.case
        calls = [0]
        def broken(workload, index, size):
            calls[0] += 1
            if calls[0] > 1:
                raise RuntimeError("measured_case_failed")
            return original(workload, index, size)
        with patch.object(churn.ChurnWorkload, "case", broken):
            report, _, _, _, failed = self.run_phase()
        self.assertTrue(failed)
        self.assertEqual(len(report["records"]), 2)
        self.assertTrue(all(r["outcome"] != "supported_success" for r in report["records"]))
        from performance_stats import summarize_outcomes
        self.assertEqual(summarize_outcomes(report["records"])["failures"], 2)

    def test_invalid_arguments_fail_before_environment(self):
        from unittest.mock import patch
        import performance_churn as churn
        for settings in (dict(duration=0), dict(warmup_count=0), dict(warmup_seconds=float("nan"))):
            with patch("performance.Environment") as environment:
                with self.assertRaises(ValueError):
                    churn.run_phase("/unused/proxy", "/unused/seed", ChurnWorkload(), **settings)
                environment.assert_not_called()


class ChurnReportingTests(unittest.TestCase):
    def test_pending_and_missing_boundary_are_counted_as_failures(self):
        from performance_stats import summarize_outcomes
        rows = [dict(operation_id=str(i), supported=True, completed=True,
                     duration_ms=1, outcome=outcome) for i, outcome in enumerate(
                         ("boundary_pending", "boundary_evidence_missing"))]
        result = summarize_outcomes(rows)
        self.assertEqual(result["supported_success"], 0)
        self.assertEqual(result["failures"], 2)
        self.assertEqual(result["supported_attempted"], 2)

    def test_private_metadata_report_accepts_churn_fields(self):
        import json
        import tempfile
        from pathlib import Path
        from performance import write_report
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "report.json"
            phase = dict(churn_index=0, boundary_sha256="a" * 64, planned_pairs=1000,
                         observed_new_pairs=1, verified_new_pairs=1, unresolved_indices=0,
                         initial_vault_population=10000, final_vault_population=10001,
                         initial_metrics={}, warmup_records=[], rate=100 / 60)
            write_report(path, dict(schema_version=1, performance_qualified=False, measurements=[phase]))
            self.assertEqual(json.loads(path.read_text())["measurements"], [phase])
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)


class ChurnCLITests(unittest.TestCase):
    def run_cli(self, fail=False, existing=False):
        import contextlib
        import io
        import json
        import tempfile
        from pathlib import Path
        from unittest.mock import patch
        import performance_churn as churn
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "report.json"
            if existing:
                path.write_text("prior-evidence")
            order = []
            def phase(*args, **kwargs):
                order.append(kwargs["bypass"])
                report = dict(mode="bypass" if kwargs["bypass"] else "proxy", records=[],
                              performance_qualified=False, scheduled=1, sent=0, client_unsent=1)
                if fail:
                    raise churn.ChurnPhaseFailure(report)
                return report
            with patch.object(churn, "run_phase", side_effect=phase), patch("performance.environment_info", return_value={}):
                with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
                    code = churn.main(["--binary", "/unused/proxy", "--seed-binary", "/unused/seed",
                                       "--report", str(path), "--repetitions", "3"])
            content = path.read_text()
            return code, order, content if existing else json.loads(content)

    def test_three_repetitions_alternate_matched_arms(self):
        code, order, report = self.run_cli()
        self.assertEqual(code, 0)
        self.assertEqual(order, [False, True, True, False, False, True])
        self.assertEqual(len(report["measurements"]), 6)
        self.assertFalse(report["performance_qualified"])

    def test_phase_failure_is_retained_and_stops_later_arms(self):
        code, order, report = self.run_cli(fail=True)
        self.assertEqual(code, 1)
        self.assertEqual(len(order), 1)
        self.assertEqual(report["measurements"][0]["client_unsent"], 1)
        self.assertFalse(report["performance_qualified"])

    def test_existing_report_is_preserved_before_backend_work(self):
        code, order, content = self.run_cli(existing=True)
        self.assertEqual(code, 1)
        self.assertEqual(order, [])
        self.assertEqual(content, "prior-evidence")
