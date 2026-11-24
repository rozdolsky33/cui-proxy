from contextlib import redirect_stdout
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from soak_acceptance_cli import main
from soak_growth_review import GrowthReview, evaluator_module_sha256
import test_soak_growth_review as review_cases


class GrowthReviewCLITests(unittest.TestCase):
    def run_cli(self, review=True, dirty=False, module_hash=True):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary).resolve()
            root = directory / "sealed"
            root.mkdir()
            fixture = review_cases.GrowthReviewTests()
            fixture.setUp()
            identity = directory / "expected.json"
            identity.write_text(json.dumps(fixture.expected))
            output = directory / "acceptance.json"
            args = ["--root", str(root), "--expected-identity", str(identity), "--output", str(output)]
            if review:
                review_file = fixture.files(directory)
                args += ["--growth-review", str(review_file)]
            source = {"commit": "e" * 40, "dirty": dirty,
                      "script_sha256": "0" * 64}

            def evaluate(campaign, expected, *, growth_review=None):
                self.assertEqual(campaign, fixture.campaign)
                self.assertEqual(expected, fixture.expected)
                if review:
                    self.assertIsInstance(growth_review, GrowthReview)
                else:
                    self.assertIsNone(growth_review)
                gate = fixture.evaluate(growth_review)
                return {"state": gate["state"], "soak_qualified": gate["state"] == "pass",
                        "performance_qualified": False, "gates": {"growth_review": gate}}

            with patch("soak_acceptance_cli.evaluator_module_sha256", return_value=module_hash, side_effect=None if module_hash else ValueError("missing evaluator module provenance")), patch("soak_acceptance_cli.read_completed_campaign", return_value=fixture.campaign), \
                 patch("soak_acceptance_cli.evaluate_completed_soak", side_effect=evaluate), \
                 patch("soak_acceptance_cli.source_identity", return_value=source), redirect_stdout(io.StringIO()):
                status = main(args)
            return status, json.loads(output.read_text())

    def test_optional_review_reaches_evaluator_and_retains_only_bound_metadata(self):
        status, output = self.run_cli()
        self.assertEqual(status, 0)
        self.assertTrue(output["soak_qualified"])
        self.assertEqual(len(output["gates"]["growth_review"]["review_sha256"]), 64)
        self.assertIn("soak_growth_review.py", output["evaluator_source"]["module_sha256"])

    def test_absent_flag_preserves_incomplete_positive_growth(self):
        status, output = self.run_cli(review=False)
        self.assertEqual(status, 1)
        self.assertFalse(output["soak_qualified"])
        self.assertEqual(output["gates"]["growth_review"]["state"], "incomplete")

    def test_dirty_or_missing_new_module_provenance_never_qualifies(self):
        for options in ({"dirty": True}, {"module_hash": False}):
            with self.subTest(options=options):
                status, output = self.run_cli(**options)
                self.assertEqual(status, 1)
                self.assertFalse(output["soak_qualified"])


if __name__ == "__main__":
    unittest.main()
