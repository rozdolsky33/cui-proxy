import subprocess
import unittest
from unittest.mock import Mock, patch

from soak_recovery import restart_proxy


class RestartTests(unittest.TestCase):
    def continuation(self):
        return {"outcome": "supported_success", "completed": True, "semantic_verified": True, "boundary_verified": True, "old_substitutions_only": True}

    def env(self):
        old = Mock(args=["proxy", "serve", "-config", "private-config"])
        old.poll.return_value = None
        old.wait.return_value = 0
        env = Mock(proxy=old)
        env.vault_count.side_effect = [10000, 10000]
        env.metrics.return_value = {"runtime": {}}
        env.spawn.return_value.poll.return_value = None
        return env, old

    def test_graceful_restart_reuses_arguments_and_checks_reopened_vault(self):
        env, old = self.env()
        row = restart_proxy(env, 1, continuation=self.continuation)
        old.terminate.assert_called_once()
        old.wait.assert_called_once_with(timeout=30)
        self.assertEqual(env.spawn.call_args.args[0], old.args)
        self.assertIs(env.proxy, env.spawn.return_value)
        self.assertEqual(row["vault_before"], row["vault_after"])
        self.assertTrue(row["graceful_exit"])
        self.assertTrue(row["ready"])

    def test_timeout_or_nonzero_exit_cannot_be_reported_as_graceful(self):
        for failure in (subprocess.TimeoutExpired("proxy", 30), 1):
            env, old = self.env()
            if isinstance(failure, Exception):
                old.wait.side_effect = failure
            else:
                old.wait.return_value = failure
            with self.assertRaises((RuntimeError, subprocess.TimeoutExpired)):
                restart_proxy(env, 1, continuation=self.continuation)
            env.spawn.assert_not_called()

    def test_smaller_reopened_vault_fails(self):
        env, _ = self.env()
        env.vault_count.side_effect = [10000, 9999]
        with self.assertRaises(RuntimeError):
            restart_proxy(env, 1, continuation=self.continuation)

    def test_already_dead_proxy_is_not_a_graceful_restart(self):
        env, old = self.env()
        old.poll.return_value = 1
        with self.assertRaises(RuntimeError):
            restart_proxy(env, 1, continuation=self.continuation)
        old.terminate.assert_not_called()
        env.spawn.assert_not_called()

    def test_vault_count_is_read_after_old_substitution_probe(self):
        env, _ = self.env()
        env.metrics.return_value = {"runtime": {}, "vault_size": 0}
        observed = []
        env.vault_count.side_effect = lambda: observed.append("count") or 10000
        def continuation():
            observed.append("probe")
            return self.continuation()
        restart_proxy(env, 1, continuation=continuation)
        self.assertEqual(observed, ["count", "probe", "count"])

    def test_unsuccessful_or_reseeded_probe_cannot_pass(self):
        for change in ({"outcome": "transformation_error"}, {"old_substitutions_only": False}, {"boundary_verified": False}):
            env, _ = self.env()
            with self.assertRaises(RuntimeError):
                restart_proxy(env, 1, continuation=lambda: dict(self.continuation(), **change))
