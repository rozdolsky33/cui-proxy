import hashlib
import importlib
import json
import tempfile
import unittest
from pathlib import Path


def _cleanup_probe_kind(stderr):
    if b"probe_caught_runtime" in stderr:
        return "caught_runtime_error"
    if b"probe_caught_interrupt" in stderr:
        return "interrupt_handler_reached"
    # Never echo captured test source or fixture content in assertion output.
    if b"RuntimeError" in stderr and b"_terminate_group" in stderr:
        return "owned_group_cleanup_deadline"
    if b"RuntimeError" in stderr and b"execute_command" in stderr:
        return "owned_descendants_after_exit"
    if b"KeyboardInterrupt" in stderr:
        return "interrupted_probe"
    return "unclassified_probe_exit"


class CampaignTests(unittest.TestCase):
    def _scan_processes(self, combined, commands, arguments):
        from unittest import mock
        import performance as perf
        campaign = importlib.import_module("performance_campaign")
        def run(command, **kwargs):
            if command[0] == "sw_vers":
                output = "26.7.2\n"
            elif command[0] == "go":
                output = "proxy: go1.26.11\n"
            elif command[0] == "ps":
                columns = next(arg for arg in command if arg.startswith("pid="))
                output = {"pid=,comm=,args=": combined,
                          "pid=,stat=,comm=": commands, "pid=,args=": arguments}[columns]
            else:
                raise AssertionError("unexpected probe")
            return campaign.subprocess.CompletedProcess(command, 0, stdout=output, stderr="")
        with (mock.patch.object(perf, "environment_info", return_value={}),
              mock.patch.object(campaign.subprocess, "run", side_effect=run)):
            return campaign.reference_environment("unused")["competing_work"]

    def test_process_scan_preserves_complete_executable_paths_with_spaces(self):
        import os
        pid = os.getpid() + 1000000
        self.assertEqual(self._scan_processes(
            f"{pid} /Applications/Go /Applications/Google Chrome.app/Contents/MacOS/Google Chrome\n",
            f"{pid} S /Applications/Google Chrome.app/Contents/MacOS/Google Chrome\n",
            f"{pid} /Applications/Google Chrome.app/Contents/MacOS/Google Chrome\n"), 0)

    def test_process_scan_counts_live_build_ocr_and_benchmark_but_not_zombies(self):
        import os
        first = os.getpid() + 1000000
        rows = [("S", "/opt/go/bin/go", "go build ./cmd"),
                ("S", "/opt/bin/tesseract", "tesseract input output"),
                ("S", "/usr/bin/python3", "python3 scripts/workflow/performance.py"),
                ("S", "/usr/bin/python3", "python3 idle_service.py"),
                ("Z", "/opt/go/bin/go", "go test ./...")]
        combined = "".join(f"{first+i} {name} {args}\n" for i, (_, name, args) in enumerate(rows))
        commands = "".join(f"{first+i} {state} {name}\n" for i, (state, name, _) in enumerate(rows))
        arguments = "".join(f"{first+i} {args}\n" for i, (_, _, args) in enumerate(rows))
        self.assertEqual(self._scan_processes(combined, commands, arguments), 3)


    def test_real_interrupt_removes_descendant_that_ignores_soft_signals(self):
        self._assert_owned_descendant_cleanup(True)

    def test_successful_leader_cannot_leave_owned_descendant_running(self):
        self._assert_owned_descendant_cleanup(False)


    def _assert_owned_descendant_cleanup(self, interrupt):
        # Use production grace; allow four bounded waits plus launch overhead.
        import os
        import signal
        import subprocess
        import sys
        import time
        child_code = "\n".join([
            "import os, signal, sys, time", "from pathlib import Path",
            "signal.signal(signal.SIGINT, signal.SIG_IGN)",
            "signal.signal(signal.SIGTERM, signal.SIG_IGN)",
            "Path(sys.argv[1]).write_text(str(os.getpid()))", "time.sleep(60)"])
        leader_code = "\n".join([
            "import os, subprocess, sys, time", "from pathlib import Path",
            "Path(sys.argv[1]).write_text(str(os.getpid()))",
            "child = subprocess.Popen([sys.executable, '-c', " + repr(child_code) + ", sys.argv[2]])",
            ("child.wait()" if interrupt else "while not Path(sys.argv[2]).exists(): time.sleep(0.01)")])
        probe_code = '\n'.join([
            'import performance_campaign as campaign',
            'import sys',
            'try:',
            "    campaign.execute_command([sys.executable, '-c', " + repr(leader_code) + ', sys.argv[1], sys.argv[2]], sys.argv[3])',
            'except KeyboardInterrupt:',
            "    sys.stderr.write('probe_caught_interrupt\\n')",
            '    raise SystemExit(130)',
            'except RuntimeError:',
            "    sys.stderr.write('probe_caught_runtime\\n')",
            '    raise SystemExit(1)',
        ])
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            leader, child, log = root / "leader.pid", root / "child.pid", root / "child.log"
            environment = os.environ.copy()
            environment["PYTHONPATH"] = str(Path(__file__).parent.resolve())
            probe = subprocess.Popen([sys.executable, "-c", probe_code, str(leader), str(child), str(log)],
                                     env=environment, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            try:
                deadline = time.monotonic() + 10
                while not child.exists() and probe.poll() is None and time.monotonic() < deadline:
                    time.sleep(0.02)
                self.assertTrue(child.exists(), "owned descendant did not start")
                if interrupt:
                    os.kill(probe.pid, signal.SIGINT)
                _, probe_stderr = probe.communicate(timeout=25)
                self.assertEqual(probe.returncode, 130 if interrupt else 1, msg=_cleanup_probe_kind(probe_stderr))
                status = subprocess.run(["ps", "-o", "stat=", "-p", child.read_text()],
                                        stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True).stdout.strip()
                self.assertTrue(not status or status.startswith("Z"), "owned descendant survived cleanup")
            finally:
                if leader.exists():
                    try:
                        os.killpg(int(leader.read_text()), signal.SIGKILL)
                    except ProcessLookupError:
                        pass
                if probe.poll() is None:
                    probe.kill()
                probe.communicate(timeout=5)


    def test_inputs_changed_during_child_preserve_report_without_completing_job(self):
        from unittest import mock
        campaign = importlib.import_module("performance_campaign")
        before = dict(source=dict(dirty=False), binary_sha256="before")
        after = dict(source=dict(dirty=False), binary_sha256="after")
        def execute(command, log):
            Path(command[command.index("--report") + 1]).write_text("{}")
            return 0
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "campaign"
            args = ["--binary", "unused", "--seed-binary", "unused", "--fixtures", "unused",
                    "--output", str(root), "--profile", "smoke", "--sections", "text"]
            with (mock.patch.object(campaign, "campaign_identity", side_effect=[before, before, after]),
                  mock.patch.object(campaign, "execute_command", side_effect=execute)):
                self.assertEqual(campaign.main(args), 1)
            manifest = json.loads((root / "manifest.json").read_text())
            self.assertFalse(manifest["completed"])
            self.assertEqual(manifest["jobs"][0]["status"], "failed")
            self.assertIsNotNone(manifest["jobs"][0]["attempts"][0]["sha256"])


    def test_extraction_jobs_satisfy_native_phase_client_partition(self):
        campaign = importlib.import_module("performance_campaign")
        for profile in ("smoke", "reference"):
            for job in campaign.make_jobs(profile, sections=("extraction",)):
                self.assertEqual(job["count"] % job["clients"], 0)
                self.assertGreaterEqual(job["count"], job["clients"])


    def test_cli_runs_and_resumes_without_reexecuting_a_completed_job(self):
        from unittest import mock
        campaign = importlib.import_module("performance_campaign")
        def execute(command, log):
            Path(command[command.index("--report") + 1]).write_text("{}")
            return 0
        with tempfile.TemporaryDirectory() as directory:
            args = ["--binary", "unused", "--seed-binary", "unused", "--fixtures", "unused",
                    "--output", str(Path(directory) / "campaign"), "--profile", "smoke", "--sections", "text"]
            with (mock.patch.object(campaign, "campaign_identity", return_value=dict(source=dict(dirty=False))),
                  mock.patch.object(campaign, "execute_command", side_effect=execute) as run):
                self.assertEqual(campaign.main(args), 0)
                self.assertEqual(campaign.main(args + ["--resume"]), 0)
            self.assertEqual(run.call_count, 1)


    def test_reference_guard_rejects_unknown_power_competing_work_and_wrong_build(self):
        campaign = importlib.import_module("performance_campaign")
        valid = dict(hardware_model="MacBookPro18,3", architecture="arm64", logical_cpus=8,
                     memory_bytes=34359738368, macos_version="26.7.2", go_version="go1.26.11",
                     on_ac=True, low_power=False, competing_work=0, optimized_build=True)
        campaign.check_reference_environment(valid)
        for field, value in (("on_ac", None), ("low_power", True), ("competing_work", 1),
                             ("go_version", "go1.26.10"), ("optimized_build", False)):
            with self.subTest(field=field), self.assertRaises(ValueError):
                campaign.check_reference_environment(dict(valid, **{field: value}))

    def test_post_run_condition_failure_retains_report_but_does_not_complete_job(self):
        campaign = importlib.import_module("performance_campaign")
        observations = iter([dict(ready=True), dict(ready=False)])
        def validate(info):
            if not info["ready"]:
                raise ValueError("reference conditions changed")
        def execute(job, report):
            report.write_text("{}")
            return 0
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "campaign"
            code = campaign.run_campaign(root, [dict(id="one")], "identity", execute,
                                         observe=lambda: next(observations), validate_observation=validate)
            self.assertEqual(code, 1)
            manifest = json.loads((root / "manifest.json").read_text())
            attempt = manifest["jobs"][0]["attempts"][0]
            self.assertFalse(manifest["completed"])
            self.assertEqual(manifest["jobs"][0]["status"], "failed")
            self.assertEqual(attempt["exit_code"], 0)
            self.assertIsNotNone(attempt["sha256"])
            self.assertFalse(attempt["environment_after"]["ready"])


    def test_identity_changes_when_binary_or_fixture_manifest_changes(self):
        from unittest import mock
        campaign = importlib.import_module("performance_campaign")
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary, seed, fixtures = root / "binary", root / "seed", root / "fixtures"
            fixtures.mkdir()
            binary.write_bytes(b"version-one")
            seed.write_bytes(b"seed")
            (fixtures / "manifest.json").write_text("{}")
            with mock.patch.object(campaign, "source_identity", return_value=dict(commit="fixed", dirty=False)):
                first = campaign.campaign_identity(binary, seed, fixtures, [], "smoke")
                binary.write_bytes(b"version-two")
                second = campaign.campaign_identity(binary, seed, fixtures, [], "smoke")
                (fixtures / "manifest.json").write_text('{"version":2}')
                third = campaign.campaign_identity(binary, seed, fixtures, [], "smoke")
            self.assertNotEqual(first, second)
            self.assertNotEqual(second, third)


    def test_campaign_lock_prevents_overlapping_resume(self):
        campaign = importlib.import_module("performance_campaign")
        jobs = [dict(id="one")]
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "campaign"
            def execute(job, report):
                with self.assertRaises(BlockingIOError):
                    campaign.run_campaign(root, jobs, "same", execute, resume=True)
                report.write_text("{}")
                return 0
            self.assertEqual(campaign.run_campaign(root, jobs, "same", execute), 0)
            self.assertEqual(root.stat().st_mode & 0o777, 0o700)
            self.assertEqual((root / "manifest.json").stat().st_mode & 0o777, 0o600)

    def test_child_output_is_private_and_not_forwarded(self):
        import sys
        campaign = importlib.import_module("performance_campaign")
        with tempfile.TemporaryDirectory() as directory:
            log = Path(directory) / "child.log"
            self.assertEqual(campaign.execute_command([sys.executable, "-c", "print('diagnostic')"], log), 0)
            self.assertEqual(log.stat().st_mode & 0o777, 0o600)
            self.assertGreater(log.stat().st_size, 0)

    def test_interrupt_cleans_only_the_owned_child_process_group(self):
        import signal
        from unittest import mock
        campaign = importlib.import_module("performance_campaign")
        process = mock.Mock(pid=12345)
        process.wait.side_effect = [KeyboardInterrupt(), 0]
        process.poll.return_value = None
        with tempfile.TemporaryDirectory() as directory:
            with (mock.patch.object(campaign.subprocess, "Popen", return_value=process) as spawn,
                  mock.patch.object(campaign.os, "killpg") as kill,
                  mock.patch.object(campaign, "_group_running", side_effect=[True, False])):
                with self.assertRaises(KeyboardInterrupt):
                    campaign.execute_command(["unused"], Path(directory) / "child.log")
            self.assertTrue(spawn.call_args.kwargs["start_new_session"])
            kill.assert_called_once_with(process.pid, signal.SIGINT)


    def test_commands_keep_reference_repetition_identity_and_arm_order(self):
        campaign = importlib.import_module("performance_campaign")
        for kind in ("text", "throughput", "extraction", "fairness", "overload"):
            job = dict(id="job", kind=kind, size=262144, clients=4, count=250,
                       repetition=1, duration=120, template=0, repeated=False,
                       rates=[1, 2, 4, 8], max_outstanding=16)
            command = campaign.build_command(job, "binary", "seed", "fixtures", "report", "reference")
            flag = "--repetition" if kind == "overload" else "--repetition-offset"
            self.assertEqual(command[command.index(flag) + 1], "1")
            self.assertEqual(command[command.index("--warmup-seconds") + 1], "60")
            self.assertEqual(command[command.index("--warmup-count") + 1], "100")


    def test_reference_text_counts_meet_total_without_multiplying_by_clients(self):
        campaign = importlib.import_module("performance_campaign")
        jobs = campaign.make_jobs("reference", sections=("text",))
        self.assertEqual(len(jobs), 36)
        self.assertEqual(jobs[0]["clients"], 4)
        for job in jobs:
            self.assertGreaterEqual(job["count"] * job["clients"], 1000)
            self.assertLess(job["count"] * job["clients"], 1000 + job["clients"])
        self.assertEqual({j["repetition"] for j in jobs}, {0, 1, 2})

    def test_resume_skips_verified_success_and_keeps_failed_attempt(self):
        campaign = importlib.import_module("performance_campaign")
        jobs = [dict(id="one"), dict(id="two")]
        calls = []
        fail = [True]
        def execute(job, report):
            calls.append(job["id"])
            report.write_text(json.dumps(dict(completed=not (job["id"] == "two" and fail[0]))))
            return 1 if job["id"] == "two" and fail[0] else 0
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "campaign"
            self.assertEqual(campaign.run_campaign(root, jobs, "fingerprint", execute), 1)
            before = json.loads((root / "manifest.json").read_text())
            old_report = root / before["jobs"][1]["attempts"][0]["report"]
            old_hash = hashlib.sha256(old_report.read_bytes()).hexdigest()
            fail[0] = False
            self.assertEqual(campaign.run_campaign(root, jobs, "fingerprint", execute, resume=True), 0)
            self.assertEqual(calls, ["one", "two", "two"])
            after = json.loads((root / "manifest.json").read_text())
            self.assertEqual(len(after["jobs"][1]["attempts"]), 2)
            self.assertEqual(hashlib.sha256(old_report.read_bytes()).hexdigest(), old_hash)
            self.assertTrue(after["completed"])

    def test_resume_rejects_changed_fingerprint_or_completed_report_before_work(self):
        campaign = importlib.import_module("performance_campaign")
        jobs = [dict(id="one")]
        calls = []
        def execute(job, report):
            calls.append(job["id"])
            report.write_text("{}")
            return 0
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "campaign"
            campaign.run_campaign(root, jobs, "original", execute)
            with self.assertRaises(ValueError):
                campaign.run_campaign(root, jobs, "changed", execute, resume=True)
            manifest = json.loads((root / "manifest.json").read_text())
            (root / manifest["jobs"][0]["attempts"][0]["report"]).write_text("tampered")
            with self.assertRaises(ValueError):
                campaign.run_campaign(root, jobs, "original", execute, resume=True)
            self.assertEqual(calls, ["one"])
