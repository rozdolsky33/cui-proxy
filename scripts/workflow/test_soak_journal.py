import copy
import unittest

from soak_journal import SoakJournal, replay_requests


class SoakJournalTests(unittest.TestCase):
    def test_claims_are_closed_loop_and_detached_from_saved_metadata(self):
        journal = SoakJournal("fixed", 7200)
        first = journal.claim(0, 0)
        with self.assertRaises(ValueError):
            journal.claim(0, 1)
        first["churn_indices"].append(999)
        journal.complete(first["identity"])
        second = journal.claim(0, 2)
        self.assertEqual(second["identity"], ("fixed", 0, 1))
        with self.assertRaises(ValueError):
            journal.snapshot()
        journal.complete(second["identity"])
        saved = journal.snapshot()
        self.assertEqual(saved[0]["churn_indices"], [])
        self.assertIsNone(journal.claim(1, 7200))
        self.assertEqual(len(journal.snapshot()), 2)

    def test_churn_is_global_contiguous_bounded_and_replayed_per_client(self):
        journal = SoakJournal("churn", 7200)
        for client in range(4):
            row = journal.claim(client, 60)
            journal.complete(row["identity"])
        saved = journal.snapshot()
        indices = [i for row in saved for i in row["churn_indices"]]
        self.assertEqual(indices, list(range(100)))
        self.assertTrue(all(len(row["churn_indices"]) <= 40 for row in saved))
        replay = replay_requests(saved, "churn")
        for client in range(4):
            self.assertEqual(replay[client], [row for row in saved if row["identity"][1] == client])
        replay[0][0]["churn_indices"].clear()
        self.assertEqual(saved[0]["churn_indices"], list(range(40)))

    def test_unknown_or_duplicate_completion_is_rejected(self):
        journal = SoakJournal("fixed", 7200)
        with self.assertRaises(ValueError):
            journal.complete(("fixed", 0, 0))
        row = journal.claim(0, 0)
        journal.complete(row["identity"])
        with self.assertRaises(ValueError):
            journal.complete(row["identity"])

    def test_nonfinite_or_reversed_time_is_rejected(self):
        journal = SoakJournal("fixed", 7200)
        row = journal.claim(0, 5)
        journal.complete(row["identity"])
        for elapsed in (4, -1, float("nan"), float("inf"), True):
            with self.assertRaises(ValueError):
                journal.claim(1, elapsed)

    def test_tampered_replay_cannot_silently_change_workload(self):
        journal = SoakJournal("churn", 7200)
        row = journal.claim(0, 60)
        journal.complete(row["identity"])
        saved = journal.snapshot()
        for field, value in (("scenario", "W12"), ("size_bytes", 1), ("churn_indices", [99]), ("identity", ("churn", 0, 2)), ("elapsed_s", float("nan"))):
            changed = copy.deepcopy(saved)
            changed[0][field] = value
            with self.subTest(field=field), self.assertRaises(ValueError):
                replay_requests(changed, "churn")
        with self.assertRaises(ValueError):
            replay_requests(saved + saved, "churn")

    def test_worker_clock_is_sampled_inside_assignment_lock(self):
        journal = SoakJournal("fixed", 7200)
        def elapsed():
            self.assertTrue(journal.lock.locked())
            return 1
        row = journal.claim(0, elapsed)
        journal.complete(row["identity"])
