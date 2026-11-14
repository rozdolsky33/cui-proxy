"""Exercise fixture selection at the expensive extraction boundary without OCR."""
import unittest
from unittest.mock import patch
from pathlib import Path
from soak_corpus import CorpusSelection
from soak_executor import ScenarioExecutor
from soak_schedule import plan_request
from test_soak_corpus import descriptors


class CorpusExecutorTests(unittest.TestCase):
    def test_image_request_and_evidence_use_same_selected_fixtures(self):
        corpus=CorpusSelection(descriptors())
        runner=ScenarioExecutor("http://127.0.0.1:1", "http://127.0.0.1:2",
                                object(),{},[],Path("."),corpus=corpus)
        def extracted(url,workload,serial,size,fixtures,directory,mappings,session,scenario,**kwargs):
            return dict(outcome="supported_success",completed=True,semantic_verified=True,
                        boundary_verified=True,boundary_sha256="a"*64,
                        operation_id=kwargs["operation_id"],
                        observed_fixture_ids=[x["id"] for x in fixtures])
        with patch("soak_executor.execute_image",side_effect=extracted), patch(
                "soak_executor.backend_evidence",return_value={"complete":True,"count":1,"body_sha256":"a"*64}):
            row=runner(dict(plan_request(0,7,"fixed"),churn_indices=[]))
        self.assertEqual(row.get("observed_fixture_ids"),["image-0-0","image-1-0"])
        self.assertEqual(row.get("fixture_evidence"),corpus.evidence(plan_request(0,7,"fixed")))
        self.assertTrue(row["upstream_receipt_verified"])

    def test_replay_mismatch_cannot_reach_extraction(self):
        for corpus in (CorpusSelection(descriptors()),None):
            runner=ScenarioExecutor("http://127.0.0.1:1","http://127.0.0.1:2",
                                    object(),{},[],Path("."),corpus=corpus)
            plan=dict(plan_request(0,7,"fixed"),churn_indices=[],fixture_evidence=[])
            with patch("soak_executor.execute_image",side_effect=AssertionError("unverified extraction")):
                with self.assertRaises(ValueError):runner(plan)

    def test_warmup_executor_sends_disjoint_partition_and_replays_it(self):
        corpus=CorpusSelection(descriptors())
        runner=ScenarioExecutor("http://127.0.0.1:1","http://127.0.0.1:2",
                                object(),{},[],Path("."),corpus=corpus,warmup=True)
        plan=dict(plan_request(0,7,"fixed"),churn_indices=[])
        plan["fixture_evidence"]=corpus.evidence(plan,warmup=True)
        def extracted(url,workload,serial,size,fixtures,directory,mappings,session,scenario,**kwargs):
            return dict(outcome="supported_success",completed=True,semantic_verified=True,
                        boundary_verified=True,boundary_sha256="a"*64,
                        operation_id=kwargs["operation_id"],
                        observed_fixture_ids=[x["id"] for x in fixtures])
        with patch("soak_executor.execute_image",side_effect=extracted),patch(
                "soak_executor.backend_evidence",return_value={"complete":True,"count":1,"body_sha256":"a"*64}):
            row=runner(plan)
        self.assertEqual(row["observed_fixture_ids"],["image-0-100000","image-1-100000"])
        self.assertEqual(row["operation_id"],"soak-warmup-fixed-0-7")
        self.assertEqual(row["fixture_evidence"],plan["fixture_evidence"])
