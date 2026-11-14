import copy
import unittest
from soak_reload import renamed_policy, verify_policy_records


class ReloadTests(unittest.TestCase):
    def test_only_rule_names_change_and_baseline_is_not_mutated(self):
        baseline = {"policy": {"on_error": "block"}, "rules": [{"name": "one", "type": "regex", "pattern": "[A-Z]+"}, {"name": "two", "type": "dictionary", "file": "private-dictionary"}]}
        saved = copy.deepcopy(baseline)
        changed = renamed_policy(baseline, 2)
        self.assertEqual(baseline, saved)
        self.assertEqual(changed["policy"], baseline["policy"])
        for old, new in zip(baseline["rules"], changed["rules"]):
            self.assertEqual({k: v for k, v in old.items() if k != "name"}, {k: v for k, v in new.items() if k != "name"})
            self.assertEqual(new["name"], "soak-g2-" + old["name"])

    def test_each_request_has_one_generation_and_both_generations_are_observed(self):
        records = [{"rule_ids": ["soak-g0-one", "soak-g0-two"]}, {"rule_ids": ["soak-g1-one"]}]
        self.assertEqual(verify_policy_records(records, {0, 1}), {0: 1, 1: 1})

    def test_mixed_unknown_or_missing_policy_evidence_cannot_pass(self):
        for records in ([{"rule_ids": ["soak-g0-one", "soak-g1-two"]}], [{"rule_ids": ["untagged"]}], [{"rule_ids": []}], [{"rule_ids": ["soak-g2-one"]}], [{"rule_ids": ["soak-g0-one"]}]):
            with self.subTest(records=records), self.assertRaises(ValueError):
                verify_policy_records(records, {0, 1})

    def test_invalid_generation_and_missing_rules_are_rejected(self):
        for generation in (-1, True, "1"):
            with self.assertRaises(ValueError):
                renamed_policy({"rules": [{"name": "one"}]}, generation)
        with self.assertRaises(ValueError):
            renamed_policy({"rules": []}, 0)


class PolicyReloaderTests(unittest.TestCase):
    def test_owned_config_is_replaced_atomically_and_reload_is_requested(self):
        import json
        import tempfile
        from pathlib import Path
        from unittest.mock import Mock, patch
        from soak_reload import PolicyReloader
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory, "config.json")
            baseline = {"rules": [{"name": "one", "type": "regex", "pattern": "[A-Z]+"}], "policy": {"on_error": "block"}}
            path.write_text(json.dumps(baseline))
            env = Mock(directory=Path(directory), ui="http://127.0.0.1:1234")
            env.proxy.args = ["proxy", "serve", "-config", str(path)]
            response = Mock(status=200)
            response.__enter__ = Mock(return_value=response)
            response.__exit__ = Mock(return_value=False)
            with patch("soak_reload.harness.OPENER.open", return_value=response) as request:
                reloader = PolicyReloader(env)
                reloader.reload(0)
                row = reloader.reload(1)
            updated = json.loads(path.read_text())
            self.assertEqual(updated["rules"][0]["name"], "soak-g1-one")
            self.assertEqual(updated["policy"], baseline["policy"])
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            self.assertEqual(request.call_args.args[0].get_method(), "POST")
            self.assertEqual(request.call_args.args[0].full_url, "http://127.0.0.1:1234/api/reload")
            self.assertEqual(row["generation"], 1)
            self.assertEqual(list(Path(directory).iterdir()), [path])

    def test_config_outside_owned_environment_is_rejected(self):
        import tempfile
        from pathlib import Path
        from unittest.mock import Mock
        from soak_reload import PolicyReloader
        with tempfile.TemporaryDirectory() as directory:
            env = Mock(directory=Path(directory), ui="http://127.0.0.1:1234")
            env.proxy.args = ["proxy", "serve", "-config", "/outside/config.json"]
            with self.assertRaises(ValueError):
                PolicyReloader(env)
