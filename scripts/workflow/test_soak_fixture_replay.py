import copy
import unittest
from soak_corpus import CorpusSelection
from soak_schedule import plan_request
from test_soak_corpus import descriptors
from soak_fixture_replay import bind_fixture_replay


def phase_report(corpus):
    plan=dict(plan_request(0,7,"fixed"),churn_indices=[],elapsed_s=1.0)
    return dict(completed=True,phase="fixed",schedule=[plan],results=[dict(identity=plan["identity"],request=dict(fixture_evidence=corpus.evidence(plan)))])


class FixtureReplayTests(unittest.TestCase):
    def test_preserves_recorded_selection_on_actual_executor_plan(self):
        corpus=CorpusSelection(descriptors());report=phase_report(corpus);received=[]
        execute=bind_fixture_replay(lambda plan:received.append(plan) or {"completed":True},report,corpus)
        plan=copy.deepcopy(report["schedule"][0]);plan["identity"]=list(plan["identity"])
        self.assertEqual(execute(plan),{"completed":True})
        self.assertEqual(received[0]["fixture_evidence"],report["results"][0]["request"]["fixture_evidence"])
        self.assertNotIn("fixture_evidence",plan)

    def test_missing_changed_duplicate_or_extra_evidence_is_rejected_before_send(self):
        corpus=CorpusSelection(descriptors());base=phase_report(corpus);cases=[]
        for change in ("missing","changed","duplicate","extra"):
            report=copy.deepcopy(base)
            if change=="missing":report["results"][0]["request"].pop("fixture_evidence")
            if change=="changed":report["results"][0]["request"]["fixture_evidence"][0]["sha256"]="f"*64
            if change=="duplicate":report["results"].append(copy.deepcopy(report["results"][0]))
            if change=="extra":report["results"].append(dict(identity=("fixed",0,8),request={}))
            cases.append(report)
        for report in cases:
            with self.assertRaises(ValueError):bind_fixture_replay(lambda plan:self.fail("request sent"),report,corpus)

    def test_unknown_or_changed_plan_cannot_be_sent(self):
        corpus=CorpusSelection(descriptors());report=phase_report(corpus)
        execute=bind_fixture_replay(lambda plan:self.fail("request sent"),report,corpus)
        for plan in [dict(plan_request(1,7,"fixed"),churn_indices=[]),dict(report["schedule"][0],size_bytes=1)]:
            with self.assertRaises(ValueError):execute(plan)

    def test_incomplete_proxy_phase_cannot_start_replay(self):
        corpus=CorpusSelection(descriptors());report=phase_report(corpus);report["completed"]=False
        with self.assertRaises(ValueError):bind_fixture_replay(lambda plan:self.fail("request sent"),report,corpus)
