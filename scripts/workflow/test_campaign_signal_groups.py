import os
import subprocess
import sys
import time
from types import SimpleNamespace
import unittest
from unittest import mock

import performance_campaign as campaign


class SignalGroupStateTests(unittest.TestCase):
    group = 12345

    def snapshot(self, output, returncode=0):
        return subprocess.CompletedProcess([], returncode, stdout=output, stderr="")

    @unittest.skipUnless(sys.platform == "darwin", "Darwin signal-group behavior")
    def test_real_zombie_only_group_does_not_replace_cleanup_with_permission_error(self):
        process = subprocess.Popen([sys.executable, "-c", "pass"],
                                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                                   start_new_session=True)
        try:
            deadline = time.monotonic() + 5
            while time.monotonic() < deadline:
                result = subprocess.run(["ps", "-p", str(process.pid), "-o", "stat="],
                                        stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                        text=True, timeout=1)
                if result.stdout.strip().startswith("Z"):
                    break
                time.sleep(0.02)
            else:
                self.fail("owned child did not reach the expected zombie state")
            try:
                os.killpg(process.pid, 0)
            except PermissionError:
                pass
            else:
                self.skipTest("kernel does not reject zombie-only group probes")
            try:
                running = campaign._group_running(process.pid)
            except PermissionError:
                self.fail("zombie-only process group incorrectly treated as denied cleanup")
            self.assertFalse(running)
        finally:
            if process.poll() is None:
                process.kill()
            process.wait(timeout=5)

    def test_permission_probe_with_confirmed_zombie_only_group_is_stopped(self):
        with mock.patch.object(campaign.os, "killpg", side_effect=PermissionError(1, "denied")), \
             mock.patch.object(campaign.subprocess, "run", return_value=self.snapshot("12345 Z\n")):
            try:
                running = campaign._group_running(self.group)
            except PermissionError:
                self.fail("confirmed zombie-only group must not fail liveness detection")
        self.assertFalse(running)

    def test_permission_probe_with_live_member_is_not_ignored(self):
        with mock.patch.object(campaign.os, "killpg", side_effect=PermissionError(1, "denied")), \
             mock.patch.object(campaign.subprocess, "run", return_value=self.snapshot("12345 S\n")):
            with self.assertRaises(PermissionError):
                campaign._group_running(self.group)

    def test_permission_probe_with_unknown_state_is_not_ignored(self):
        for result in (self.snapshot("", 0), self.snapshot("", 1), self.snapshot("12345\n"), self.snapshot("bad data\n")):
            with self.subTest(returncode=result.returncode), \
                 mock.patch.object(campaign.os, "killpg", side_effect=PermissionError(1, "denied")), \
                 mock.patch.object(campaign.subprocess, "run", return_value=result):
                with self.assertRaises(PermissionError):
                    campaign._group_running(self.group)

    def test_unreadable_process_table_preserves_permission_failure(self):
        with mock.patch.object(campaign.os, "killpg", side_effect=PermissionError(1, "denied")), \
             mock.patch.object(campaign.subprocess, "run", side_effect=OSError("unavailable")):
            with self.assertRaises(PermissionError):
                campaign._group_running(self.group)

    def test_malformed_process_table_is_conservatively_running(self):
        with mock.patch.object(campaign.os, "killpg"), \
             mock.patch.object(campaign.subprocess, "run", return_value=self.snapshot("12345\n")):
            self.assertTrue(campaign._group_running(self.group))

    def test_signal_permission_race_accepts_only_confirmed_stopped_group(self):
        process = SimpleNamespace(pid=self.group)
        with mock.patch.object(campaign.os, "killpg", side_effect=PermissionError(1, "denied")), \
             mock.patch.object(campaign, "_group_running", side_effect=[True, False]):
            try:
                campaign._terminate_group(process)
            except PermissionError:
                self.fail("signal race after group exit must not replace interruption")

    def test_signal_permission_with_live_group_still_fails(self):
        process = SimpleNamespace(pid=self.group)
        with mock.patch.object(campaign.os, "killpg", side_effect=PermissionError(1, "denied")), \
             mock.patch.object(campaign, "_group_running", return_value=True):
            with self.assertRaises(PermissionError):
                campaign._terminate_group(process)


if __name__ == "__main__":
    unittest.main()
