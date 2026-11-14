import copy
import hashlib
import unittest
from soak_schedule import plan_request
from soak_warmup_acceptance import evaluate_warmup


class WarmupAcceptanceTests(unittest.TestCase):
    def report(self):
        results, serials = [], [0]*4
        for _ in range(25):
            for client in range(4):
                while True:
                    plan = plan_request(client=client, serial=serials[client], phase="fixed")
                    serials[client] += 1
                    if plan["category"] in ("text", "image"):
                        break
                plan["churn_indices"] = []
                row = dict(operation_id=f"warm-{client}-{serials[client]}", outcome="supported_success",
                           completed=True, supported=True, semantic_verified=True, boundary_verified=True,
                           upstream_receipt_verified=True, duration_ms=1000, category=plan["category"],
                           scenario=plan["scenario"])
                if plan["category"] == "image":
                    row["fixture_evidence"] = [dict(id=f"warm-image-{client}-{serials[client]}-{i}",
                        sha256=hashlib.sha256(f"{client}-{serials[client]}-{i}".encode()).hexdigest()) for i in range(2)]
                results.append(dict(request=plan, result=row))
        return dict(completed=True, elapsed_seconds=60.1, successful=100, client_successes=[25]*4,
                    results=results, failures=[], cancelled=False, minimum_seconds=60, minimum_count=100)

    def test_original_warmup_has_all_clients_and_real_images(self):
        result = evaluate_warmup(self.report(), mode="proxy")
        self.assertEqual(result["state"], "pass")
        self.assertEqual(result["successful"], 100)
        self.assertGreater(result["image_operations"], 0)
        self.assertFalse(result["performance_qualified"])

    def test_short_or_contradictory_warmup_is_rejected(self):
        mutations = [lambda r: r.update(elapsed_seconds=59.9),
                     lambda r: r.update(minimum_seconds=1),
                     lambda r: r.update(minimum_count=8),
                     lambda r: r.update(successful=99),
                     lambda r: r.update(client_successes=[0, 25, 25, 50]),
                     lambda r: r.update(elapsed_seconds=float("nan")),
                     lambda r: r.update(cancelled=True),
                     lambda r: r["results"].append(r["results"][0]),
                     lambda r: r["results"][0]["result"].update(boundary_verified=False)]
        for mutate in mutations:
            report = self.report()
            mutate(report)
            with self.assertRaises(ValueError):
                evaluate_warmup(report, mode="proxy")

    def test_warmup_cannot_reuse_measured_request_or_image_identity(self):
        report = self.report()
        operation = report["results"][0]["result"]["operation_id"]
        with self.assertRaises(ValueError):
            evaluate_warmup(report, mode="proxy", measured_operation_ids={operation})
        image = next(e["result"] for e in report["results"] if e["request"]["category"] == "image")
        with self.assertRaises(ValueError):
            evaluate_warmup(report, mode="proxy", measured_image_ids={image["fixture_evidence"][0]["id"]})

    def test_canonical_plan_and_deadline_are_required(self):
        for field, value in [("duration_ms", 45001), ("operation_id", ""), ("semantic_verified", False)]:
            report = self.report()
            report["results"][0]["result"][field] = value
            with self.assertRaises(ValueError):
                evaluate_warmup(report, mode="bypass")
        report = self.report()
        report["results"][0]["request"]["category"] = "image"
        with self.assertRaises(ValueError):
            evaluate_warmup(report, mode="proxy")
