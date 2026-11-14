import copy
import unittest

from soak_schedule import plan_request
from soak_window_acceptance import evaluate_text_windows


def phase(first_byte):
    report = {"phase": "fixed", "completed": True, "duration_s": 7200,
              "elapsed_s": 7201, "started_s": 10000.0, "schedule": [], "results": []}
    for window in range(8):
        for client in range(4):
            for offset in range(3):
                plan = plan_request(client=client, serial=window * 100 + offset, phase="fixed")
                plan["elapsed_s"] = window * 900 + 1 + offset
                report["schedule"].append(plan)
                row = {"operation_id": f"request-{client}-{window}-{offset}", "category": "text",
                       "scenario": plan["scenario"], "supported": True, "completed": True,
                       "outcome": "supported_success", "semantic_verified": True,
                       "boundary_verified": True, "upstream_receipt_verified": True,
                       "first_byte_ms": first_byte, "duration_ms": 2000.0,
                       "prefix_ms": [first_byte + 20.0 * i for i in range(1, 33)],
                       "relay_ms": [0.0] * 32, "relay_flush_ms": [0.0] * 32}
                report["results"].append({"identity": plan["identity"], "request": row})
    return report


class TextWindowAcceptanceTests(unittest.TestCase):
    def test_every_window_and_size_is_required_without_claiming_full_qualification(self):
        result = evaluate_text_windows(phase(100.0), phase(0.0))
        self.assertEqual(result["state"], "pass")
        self.assertEqual(len(result["windows"]), 8)
        self.assertTrue(all(len(window["sizes"]) == 3 for window in result["windows"]))
        self.assertFalse(result["performance_qualified"])

    def test_negative_paired_jitter_is_retained(self):
        result = evaluate_text_windows(phase(100.0), phase(150.0))
        self.assertEqual(result["windows"][0]["sizes"][0]["added_first_byte_ms"]["p95"], -50.0)

    def test_one_bad_size_window_cannot_hide_in_pooled_statistics(self):
        proxy, bypass = phase(100.0), phase(0.0)
        target = next(plan for plan in proxy["schedule"] if plan["elapsed_s"] >= 900 and plan["size_bytes"] == 1048576)
        row = next(entry["request"] for entry in proxy["results"] if entry["identity"] == target["identity"])
        row["first_byte_ms"] = 600.0
        row["prefix_ms"] = [600.0 + 20.0 * i for i in range(1, 33)]
        result = evaluate_text_windows(proxy, bypass)
        self.assertEqual(result["state"], "fail")

    def test_missing_client_in_one_size_window_is_incomplete(self):
        proxy, bypass = phase(100.0), phase(0.0)
        for report in (proxy, bypass):
            removed = report["schedule"].pop(0)
            report["results"] = [entry for entry in report["results"] if entry["identity"] != removed["identity"]]
        result = evaluate_text_windows(proxy, bypass)
        self.assertEqual(result["state"], "incomplete")

    def test_duplicate_or_unmatched_identities_are_rejected(self):
        for mutate in (lambda r: r["results"].append(copy.deepcopy(r["results"][0])),
                       lambda r: r["results"].pop()):
            proxy = phase(100.0)
            mutate(proxy)
            with self.assertRaises(ValueError):
                evaluate_text_windows(proxy, phase(0.0))

    def test_unverified_semantics_and_nonfinite_timings_are_rejected(self):
        for key, value in (("semantic_verified", False), ("first_byte_ms", float("nan")),
                           ("first_byte_ms", True), ("upstream_receipt_verified", False)):
            proxy = phase(100.0)
            proxy["results"][0]["request"][key] = value
            with self.assertRaises(ValueError):
                evaluate_text_windows(proxy, phase(0.0))

    def test_short_smoke_cannot_satisfy_original_phase_duration(self):
        proxy, bypass = phase(100.0), phase(0.0)
        proxy["duration_s"] = bypass["duration_s"] = 150
        with self.assertRaises(ValueError):
            evaluate_text_windows(proxy, bypass)

    def test_relay_and_request_deadline_failures_are_not_hidden(self):
        for field, value in (("relay_ms", [101.0] * 32), ("duration_ms", 45001.0)):
            proxy = phase(100.0)
            proxy["results"][0]["request"][field] = value
            self.assertEqual(evaluate_text_windows(proxy, phase(0.0))["state"], "fail")

    def test_bypass_cannot_substitute_an_unmatched_operation(self):
        bypass = phase(0.0)
        bypass["results"][0]["request"]["operation_id"] = "unmatched"
        with self.assertRaises(ValueError):
            evaluate_text_windows(phase(100.0), bypass)
