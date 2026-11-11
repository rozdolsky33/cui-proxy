import json
import unittest


class WorkloadTests(unittest.TestCase):
    def generator(self, tier="normal"):
        try:
            from performance_workload import Workload
        except ImportError:
            self.fail("workload generator missing")
        return Workload(tier=tier)

    def test_policy_and_state_populations(self):
        for tier, regexes, entries, mappings in [("normal", 50, 1000, 10000),
                ("rules", 200, 10000, 10000), ("vault", 50, 1000, 100000)]:
            w = self.generator(tier)
            self.assertEqual(len([r for r in w.rules if r["type"] == "regex"]), regexes)
            self.assertEqual(len(w.dictionary), entries)
            self.assertEqual(len(set(s.casefold() for s in w.dictionary)), entries)
            self.assertEqual(len(w.pairs), mappings)
            self.assertEqual(len(set(tuple(p) for p in w.pairs)), mappings)
            self.assertEqual(len([r for r in w.rules if r["type"] == "builtin"]), 1)

    def test_payloads_have_exact_occurrences_and_near_misses(self):
        w = self.generator()
        for size in (256 * 1024, 768 * 1024, 1024 * 1024):
            case = w.case(7, size)
            self.assertEqual(len(case["body"]), size)
            self.assertEqual(len(case["occurrences"]), 40)
            self.assertEqual(len(set(case["occurrences"])), 20)
            self.assertEqual(len(case["near_misses"]), 40)
            body = json.loads(case["body"])
            self.assertGreaterEqual(len(body["input"]), 40)
            self.assertEqual(sum(case["body"].count(v.encode()) for v in set(case["occurrences"])), 40)
            self.assertTrue(all(case["body"].count(v.encode()) == 1 for v in case["near_misses"]))

    def test_reproducible_cases_and_stable_pair_keys(self):
        w = self.generator()
        self.assertEqual(w.case(9, 262144), self.generator().case(9, 262144))
        self.assertNotEqual(w.case(9, 262144)["operation_id"], w.case(10, 262144)["operation_id"])
        self.assertNotEqual(w.case(9, 262144)["body"], w.case(10, 262144)["body"])

    def test_unknown_tier_and_invalid_size_rejected(self):
        with self.assertRaises(ValueError): self.generator("unknown")
        with self.assertRaises(ValueError): self.generator().case(0, 100)

    def test_metadata_has_no_private_corpus(self):
        w = self.generator()
        manifest = json.dumps(w.metadata())
        self.assertTrue(all(value not in manifest for _, value in w.pairs))
        self.assertEqual(w.metadata()["generator_version"], 1)
