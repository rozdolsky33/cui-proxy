import copy
from dataclasses import replace
import hashlib
import json
import math
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from soak_acceptance import REQUIRED_GATES
from soak_growth_review import (
    GrowthReview, evaluate_growth_review, growth_review_context, load_growth_review,
)


class GrowthReviewTests(unittest.TestCase):
    def setUp(self):
        self.campaign = {"root_events_sha256": "a" * 64, "root_seal_sha256": "b" * 64,
                         "identity": {"binary_sha256": "c" * 64, "source": {"commit": "d" * 40}}}
        self.expected = copy.deepcopy(self.campaign["identity"])
        self.resources = {"state": "pass", "availability": {"state": "pass"},
                          "positive_growth_requires_review": True,
                          "growth": {"rss_median_bytes": .04, "live_heap_median_bytes": -.01},
                          "windows": [{"start_s": index * 900., "samples": 900,
                                       "rss_median_bytes": 100000 + index * 500,
                                       "live_heap_median_bytes": 10000 - index * 10}
                                      for index in range(8)], "performance_qualified": False}
        self.required = REQUIRED_GATES - {"growth_review"}
        self.gates = {name: {"state": "pass"} for name in self.required}

    def documents(self):
        context = growth_review_context(self.campaign, self.expected, self.resources)
        producer = {"kind": "manual", "identity": "investigation author",
                    "provenance": "Manual analysis using the committed evaluator context API."}
        investigation = dict(schema_version=1, scope="fixed_resource_growth_investigation",
                             **context, producer=producer,
                             conclusions={"rss_median_bytes": "bounded_positive_growth",
                                          "live_heap_median_bytes": "nonpositive_growth"},
                             analysis_summary="Window and mapping observations support a bounded plateau.",
                             limitations=["Finite observation cannot establish unlimited-duration behavior."])
        data = json.dumps(investigation, sort_keys=True).encode()
        review = {"schema_version": 1, "scope": "fixed_resource_growth_review",
                  "bindings": context["bindings"], "decision": "accept_bounded_growth",
                  "reviewer": {"identity": "independent reviewer", "independent_of_collection": True,
                               "independence_basis": "Reviewed completed sealed evidence independently of collection."},
                  "producer": dict(producer, identity="manual review author"),
                  "investigation": {"path": "investigation.json", "bytes": len(data),
                                    "sha256": hashlib.sha256(data).hexdigest()}}
        return review, investigation

    def bundle(self, review=None, investigation=None):
        original_review, original_investigation = self.documents()
        review = copy.deepcopy(original_review if review is None else review)
        investigation = original_investigation if investigation is None else investigation
        data = json.dumps(investigation, sort_keys=True).encode()
        review["investigation"].update(bytes=len(data), sha256=hashlib.sha256(data).hexdigest())
        return GrowthReview(json.dumps(review, sort_keys=True).encode(), data)

    def evaluate(self, review):
        return evaluate_growth_review(self.campaign, self.expected, self.resources, review,
                                      prerequisites=self.gates, required_prerequisites=self.required)

    def test_absent_review_preserves_incomplete_positive_growth(self):
        self.assertEqual(self.evaluate(None)["state"], "incomplete")

    def test_nonpositive_growth_without_review_preserves_pass(self):
        self.resources["growth"] = {name: 0. for name in self.resources["growth"]}
        self.resources["positive_growth_requires_review"] = False
        self.assertEqual(self.evaluate(None)["state"], "pass")

    def test_bound_explicit_review_closes_only_growth_gate_without_echoing_manual_text(self):
        result = self.evaluate(self.bundle())
        self.assertEqual(result["state"], "pass")
        self.assertFalse(result["performance_qualified"])
        self.assertEqual(len(result["review_sha256"]), 64)
        self.assertEqual(len(result["investigation_sha256"]), 64)
        public = json.dumps(result)
        for private in ("independent reviewer", "investigation author", "bounded plateau", "Finite observation"):
            self.assertNotIn(private, public)

    def test_failed_unavailable_or_over_limit_fixed_resources_cannot_be_overridden(self):
        for change in ({"state": "fail"}, {"state": "incomplete"}, {"availability": {"state": "fail"}},
                       {"growth": {"rss_median_bytes": .100001, "live_heap_median_bytes": 0.}}):
            prior = self.resources
            self.resources = dict(prior, **change)
            try:
                with self.assertRaises(ValueError):
                    self.evaluate(self.bundle())
            finally:
                self.resources = prior

    def test_every_other_required_gate_remains_absolute(self):
        bundle = self.bundle()
        for name in self.required:
            for state in ("fail", "incomplete"):
                self.gates[name] = {"state": state}
                with self.subTest(gate=name, state=state), self.assertRaises(ValueError):
                    self.evaluate(bundle)
            self.gates[name] = {"state": "pass"}
            del self.gates[name]
            with self.subTest(missing=name), self.assertRaises(ValueError):
                self.evaluate(bundle)
            self.gates[name] = {"state": "pass"}

    def test_other_campaign_identity_or_changed_recomputed_window_rejects_replay(self):
        bundle = self.bundle()
        for field in ("root_events_sha256", "root_seal_sha256"):
            previous = self.campaign[field]
            self.campaign[field] = "e" * 64
            with self.subTest(field=field), self.assertRaises(ValueError):
                self.evaluate(bundle)
            self.campaign[field] = previous
        self.campaign["identity"]["source"]["commit"] = "f" * 40
        with self.assertRaises(ValueError):
            self.evaluate(bundle)
        self.campaign["identity"] = copy.deepcopy(self.expected)
        self.expected["binary_sha256"] = "e" * 64
        with self.assertRaises(ValueError):
            self.evaluate(bundle)
        self.expected = copy.deepcopy(self.campaign["identity"])
        self.resources["windows"][-1]["rss_median_bytes"] += 1
        with self.assertRaises(ValueError):
            self.evaluate(bundle)

    def test_analysis_conclusions_limitations_and_observed_context_are_mandatory(self):
        review, investigation = self.documents()
        changes = [{"analysis_summary": ""}, {"limitations": []},
                   {"observed_growth": {"rss_median_bytes": .01, "live_heap_median_bytes": -.01}},
                   {"conclusions": {"rss_median_bytes": "nonpositive_growth",
                                    "live_heap_median_bytes": "nonpositive_growth"}},
                   {"unexpected": "ignored content is forbidden"}]
        for change in changes:
            with self.subTest(fields=list(change)), self.assertRaises(ValueError):
                self.evaluate(self.bundle(review, dict(investigation, **change)))
        malformed = copy.deepcopy(investigation)
        malformed["window_medians"][0]["start_s"] = False
        with self.assertRaises(ValueError):
            self.evaluate(self.bundle(review, malformed))

    def test_reviewer_independence_and_honest_manual_provenance_are_required(self):
        review, investigation = self.documents()
        for key, value in (("identity", ""), ("independent_of_collection", False), ("independence_basis", "")):
            changed = copy.deepcopy(review)
            changed["reviewer"][key] = value
            with self.subTest(reviewer=key), self.assertRaises(ValueError):
                self.evaluate(self.bundle(changed, investigation))
        for producer in ({}, {"kind": "automatic_approval", "identity": "author", "provenance": "source"},
                         {"kind": "manual", "identity": "author", "provenance": ""}):
            with self.assertRaises(ValueError):
                self.evaluate(self.bundle(dict(review, producer=producer), investigation))

    def test_unknown_review_fields_nonfinite_and_mutated_artifact_bytes_are_rejected(self):
        review, investigation = self.documents()
        with self.assertRaises(ValueError):
            self.evaluate(self.bundle(dict(review, extra=True), investigation))
        for value in (math.nan, math.inf, -math.inf):
            invalid = copy.deepcopy(investigation)
            invalid["observed_growth"]["rss_median_bytes"] = value
            with self.assertRaises(ValueError):
                self.evaluate(self.bundle(review, invalid))
        with self.assertRaises(ValueError):
            self.evaluate(replace(self.bundle(), investigation_bytes=b"{}"))

    def files(self, directory):
        bundle = self.bundle()
        review_path = directory / "review.json"
        review_path.write_bytes(bundle.review_bytes)
        (directory / "investigation.json").write_bytes(bundle.investigation_bytes)
        return review_path

    def test_capture_verifies_external_hash_bound_artifact(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary).resolve()
            campaign_root = directory / "campaign"
            campaign_root.mkdir()
            path = self.files(directory)
            self.assertEqual(self.evaluate(load_growth_review(path, campaign_root))["state"], "pass")
            (directory / "investigation.json").write_bytes(b"{}")
            with self.assertRaises(ValueError):
                load_growth_review(path, campaign_root)

    def test_capture_rejects_campaign_internal_symlinked_escaped_missing_or_oversized_files(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary).resolve()
            path = self.files(directory)
            with self.assertRaises(ValueError):
                load_growth_review(path, directory)
            root = directory / "campaign"
            root.mkdir()
            linked = directory / "linked-review.json"
            linked.symlink_to(path)
            with self.assertRaises(ValueError):
                load_growth_review(linked, root)
            review, _ = self.documents()
            for value in ("../outside.json", "/outside.json", "missing.json"):
                changed = copy.deepcopy(review)
                changed["investigation"]["path"] = value
                path.write_text(json.dumps(changed))
                with self.subTest(path=value), self.assertRaises(ValueError):
                    load_growth_review(path, root)
            path.write_bytes(b" " * (65536 + 1))
            with self.assertRaises(ValueError):
                load_growth_review(path, root)

    def test_capture_rejects_duplicate_fields_and_changes_during_read(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary).resolve()
            root = directory / "campaign"
            root.mkdir()
            path = self.files(directory)
            path.write_bytes(b'{"schema_version":1,"schema_version":1}')
            with self.assertRaises(ValueError):
                load_growth_review(path, root)
            path = self.files(directory)
            original_read = os.read
            changed = [False]

            def mutate(descriptor, size):
                data = original_read(descriptor, size)
                if not changed[0]:
                    changed[0] = True
                    with path.open("ab") as output:
                        output.write(b" ")
                return data

            with patch("soak_growth_review.os.read", side_effect=mutate):
                with self.assertRaises(ValueError):
                    load_growth_review(path, root)


if __name__ == "__main__":
    unittest.main()
