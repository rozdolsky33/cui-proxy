"""Metadata-only selection tests; no file reads, subprocesses or OCR."""
import copy
import unittest
from soak_corpus import CorpusSelection
from soak_schedule import plan_request


def descriptors():
    return [dict(id=f"image-{t}-{i}", template=t, index=i,
                 format="png" if t % 2 == 0 else "jpeg",
                 width=1024 if t < 2 else 1920,
                 height=768 if t < 2 else 1080, expected="reference text",
                 file=f"images/{t}-{i}.png", bytes=100,
                 sha256=f"{t * 200 + j:064x}")
            for t in range(4)
            for j, i in enumerate(list(range(100)) + list(range(100000,100100)))]


class CorpusSelectionTests(unittest.TestCase):
    def test_selects_distinct_templates_and_disjoint_warmup(self):
        selector = CorpusSelection(descriptors())
        plan = plan_request(0, 7, "fixed")
        measured = selector.select(plan)
        warmup = selector.select(plan, warmup=True)
        self.assertEqual([(x["template"], x["index"]) for x in measured], [(0,0),(1,0)])
        self.assertEqual([(x["template"], x["index"]) for x in warmup], [(0,100000),(1,100000)])

    def test_all_measured_images_are_reached_deterministically(self):
        rows = descriptors()
        selector = CorpusSelection(rows)
        reverse = CorpusSelection(list(reversed(rows)))
        seen = set()
        for serial in range(500):
            if serial % 10 not in (7,8):
                continue
            for client in range(4):
                plan = plan_request(client, serial, "fixed")
                selected = selector.select(plan)
                self.assertEqual(selected, reverse.select(plan))
                self.assertNotEqual(selected[0]["template"], selected[1]["template"])
                seen.update(x["id"] for x in selected)
        self.assertEqual(len(seen),400)

    def test_caller_mutation_cannot_change_selection(self):
        rows=descriptors(); selector=CorpusSelection(rows)
        plan=plan_request(0,7,"fixed")
        rows[0]["expected"]="changed"
        first=selector.select(plan);first[0]["expected"]="changed"
        self.assertEqual(selector.select(plan)[0]["expected"],"reference text")

    def test_rejects_incomplete_duplicate_and_unsafe_descriptors(self):
        invalid=[]
        rows=descriptors();invalid.append(rows[:-1])
        for field,value in [("id","image-0-1"),("sha256",f"{1:064x}"),
                            ("file","../escape.png"),("file","/absolute.png"),
                            ("bytes",True),("index",True),("format","pdf"),
                            ("sha256","bad"),("expected",None)]:
            rows=descriptors();rows[0][field]=value;invalid.append(rows)
        for n,rows in enumerate(invalid):
            with self.subTest(case=n),self.assertRaises(ValueError):
                CorpusSelection(rows)

    def test_rejects_nonimage_or_tampered_schedule(self):
        selector=CorpusSelection(descriptors())
        for plan in [plan_request(0,0,"fixed"),dict(plan_request(0,7,"fixed"),scenario="W06")]:
            with self.assertRaises(ValueError):selector.select(plan)

    def test_churn_selection_is_reproducible_and_bounded(self):
        selector=CorpusSelection(descriptors())
        plan=plan_request(3,100008,"churn")
        self.assertEqual(selector.select(plan),selector.select(copy.deepcopy(plan)))
        self.assertTrue(all(0<=x["index"]<100 for x in selector.select(plan)))

    def test_replay_resolves_only_exact_ordered_selection(self):
        selector=CorpusSelection(descriptors())
        plan=plan_request(0,7,"fixed")
        evidence=selector.evidence(plan)
        self.assertEqual(evidence,[{"id":"image-0-0","sha256":f"{0:064x}"},
                                   {"id":"image-1-0","sha256":f"{200:064x}"}])
        replay=copy.deepcopy(plan);replay["identity"]=list(replay["identity"])
        self.assertEqual(selector.resolve(replay,evidence),selector.select(plan))
        changed=copy.deepcopy(evidence);changed[0]["sha256"]="f"*64
        extra=copy.deepcopy(evidence);extra[0]["unexpected"]=True
        for invalid in [evidence[::-1],evidence[:1],evidence+[evidence[0]],changed,extra,None]:
            with self.subTest(kind=type(invalid).__name__),self.assertRaises(ValueError):
                selector.resolve(plan,invalid)

    def test_replay_rejects_wrong_partition_or_request_identity(self):
        selector=CorpusSelection(descriptors())
        plan=plan_request(0,7,"fixed")
        evidence=selector.evidence(plan)
        for other,warmup in [(plan,True),(plan_request(1,7,"fixed"),False),
                             (plan_request(0,7,"churn"),False)]:
            with self.assertRaises(ValueError):selector.resolve(other,evidence,warmup=warmup)
