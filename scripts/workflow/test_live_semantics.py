"""Outcome accounting contracts; no live providers or sensitive fixtures."""
import copy
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

import live_semantics


class LiveSemanticsTests(unittest.TestCase):
    def document(self):
        return {"schema_version": 1, "records": [
            {"pair_id": "pair-1", "case": "argv", "model": "test-model",
             "arm": arm, "status": "ok", "semantic_pass": True}
            for arm in ("direct", "proxy")
        ]}

    def test_agreement_and_denominator(self):
        result = live_semantics.summarize(self.document())
        self.assertEqual(result["pairs"], 1)
        self.assertEqual(result["comparable_pairs"], 1)
        self.assertEqual(result["both_pass"], 1)
        self.assertEqual(result["proxy_failures"], 0)

    def test_model_outcome_difference_is_not_proxy_failure(self):
        doc = self.document()
        doc["records"][1]["semantic_pass"] = False
        result = live_semantics.summarize(doc)
        self.assertEqual(result["direct_only_pass"], 1)
        self.assertEqual(result["proxy_failures"], 0)
        self.assertEqual(result["comparable_pairs"], 1)

    def test_proxy_failure_stays_in_total_denominator(self):
        doc = self.document()
        doc["records"][1].update(status="proxy_failure", semantic_pass=None)
        result = live_semantics.summarize(doc)
        self.assertEqual(result["pairs"], 1)
        self.assertEqual(result["comparable_pairs"], 0)
        self.assertEqual(result["proxy_failures"], 1)
        self.assertEqual(result["direct_only_pass"], 0)

    def test_provider_failure_is_separate(self):
        doc = self.document()
        doc["records"][0].update(status="provider_failure", semantic_pass=None)
        self.assertEqual(live_semantics.summarize(doc)["provider_failures"], 1)

    def test_all_outcome_combinations(self):
        for direct, proxy, key in [(True, True, "both_pass"), (True, False, "direct_only_pass"),
                                   (False, True, "proxy_only_pass"), (False, False, "both_fail")]:
            doc = self.document()
            doc["records"][0]["semantic_pass"] = direct
            doc["records"][1]["semantic_pass"] = proxy
            self.assertEqual(live_semantics.summarize(doc)[key], 1)

    def test_empty_is_unmeasured(self):
        result = live_semantics.summarize({"schema_version": 1, "records": []})
        self.assertFalse(result["measured"])
        self.assertEqual(result["pairs"], 0)

    def test_invalid_records_are_rejected(self):
        mutations = [
            lambda d: d["records"].pop(),
            lambda d: d["records"].append(copy.deepcopy(d["records"][0])),
            lambda d: d["records"][1].update(model="other-model"),
            lambda d: d["records"][1].update(case="file_edit"),
            lambda d: d["records"][1].update(semantic_pass=1),
            lambda d: d["records"][1].update(status="proxy_failure"),
            lambda d: d["records"][0].update(status="proxy_failure", semantic_pass=None),
            lambda d: d["records"][1].update(prompt="unapproved free text"),
            lambda d: d.update(extra="unapproved free text"),
            lambda d: d.update(schema_version=True),
            lambda d: d.update(records={}),
            lambda d: d["records"][1].update(case="unknown"),
            lambda d: d["records"][1].update(arm="unknown"),
            lambda d: d["records"][1].update(pair_id=""),
            lambda d: d["records"][1].update(status="unknown"),
        ]
        for mutate in mutations:
            doc = self.document()
            mutate(doc)
            with self.subTest(mutation=mutations.index(mutate)):
                with self.assertRaises(ValueError):
                    live_semantics.summarize(doc)

    def test_cli_outputs_only_aggregates_and_safe_errors(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / "input.json"
            doc = self.document()
            source.write_text(json.dumps(doc))
            command = [sys.executable, str(Path(live_semantics.__file__)), str(source)]
            result = subprocess.run(command, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0)
            self.assertEqual(json.loads(result.stdout)["pairs"], 1)
            self.assertNotIn("test-model", result.stdout)
            self.assertNotIn("pair-1", result.stdout)
            source.write_text('{"private-content":')
            result = subprocess.run(command, capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertNotIn("private-content", result.stderr + result.stdout)
            self.assertNotIn(str(source), result.stderr + result.stdout)


if __name__ == "__main__":
    unittest.main()
