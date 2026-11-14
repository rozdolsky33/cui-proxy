import unittest

from soak_evidence_bindings import evaluate_evidence_bindings


class EvidenceBindingsTests(unittest.TestCase):
    def evidence(self):
        reports, phases, events = {}, {}, []
        for mode in ("proxy", "bypass"):
            for phase in ("fixed", "churn"):
                name = mode + "-" + phase
                assigned = dict(event="assigned", identity=[phase, 0, 0], elapsed_s=0,
                                category="text", scenario="W01", size_bytes=262144,
                                churn_indices=[])
                completed = dict(event="completed", identity=[phase, 0, 0],
                                 request=dict(completed=True))
                phases[name] = dict(events=[assigned, completed], run_completed=True,
                                    framing_complete=True, trailing_bytes=0)
                reports[name + "-summary.json"] = dict(
                    phase=phase, completed=True,
                    schedule=[{k: v for k, v in assigned.items() if k != "event"}],
                    results=[{k: v for k, v in completed.items() if k != "event"}])
                events.extend([dict(event="phase_start", mode=mode, phase=phase),
                               dict(event="phase_end", mode=mode, phase=phase)])
            events.append(dict(event="mode_finalized", mode=mode,
                               identity_verified=True, corpus_verified=True))
        stage = dict(session="test", observed_s=2, observation_lower_s=1)
        reports["proxy-stage-records.json"] = dict(records=[stage], poll_errors=0)
        events.append(dict(event="request_stages", mode="proxy", request=dict(session="test"),
                           observed_s=2, observation_lower_s=1))
        reports["proxy-controls.json"] = dict(restarts=[], reloads=[])
        reports["proxy-mode-summary.json"] = dict(resource_samples=1)
        events.append(dict(event="resource", mode="proxy", slot=0))
        return dict(reports=reports, phases=phases, events=events)

    def test_exact_sealed_summary_bindings_pass(self):
        result = evaluate_evidence_bindings(self.evidence())
        self.assertEqual(result["state"], "pass")
        self.assertEqual(result["phases_verified"], 4)
        self.assertFalse(result["performance_qualified"])

    def test_changed_summary_or_missing_completion_is_rejected(self):
        for key in ("schedule", "results"):
            c = self.evidence()
            c["reports"]["proxy-fixed-summary.json"][key] = []
            with self.assertRaises(ValueError): evaluate_evidence_bindings(c)
        c = self.evidence()
        c["phases"]["proxy-fixed"]["events"].append(dict(event="unexpected"))
        with self.assertRaises(ValueError): evaluate_evidence_bindings(c)

    def test_stage_control_and_resource_summary_must_match_journal(self):
        for name, key, value in (("proxy-stage-records.json", "records", []),
                                  ("proxy-stage-records.json", "poll_errors", 1),
                                  ("proxy-controls.json", "restarts", [dict(index=1)]),
                                  ("proxy-mode-summary.json", "resource_samples", 0)):
            c = self.evidence()
            c["reports"][name][key] = value
            with self.subTest(name=name, key=key), self.assertRaises(ValueError):
                evaluate_evidence_bindings(c)

    def test_finalization_identity_and_phase_order_are_required(self):
        c = self.evidence()
        next(e for e in c["events"] if e["event"] == "mode_finalized")["identity_verified"] = False
        with self.assertRaises(ValueError): evaluate_evidence_bindings(c)
        c = self.evidence()
        c["events"][0], c["events"][1] = c["events"][1], c["events"][0]
        with self.assertRaises(ValueError): evaluate_evidence_bindings(c)

    def test_deferred_churn_boundary_requires_proof_and_no_other_changes(self):
        c = self.evidence()
        journal = c["phases"]["proxy-churn"]["events"]
        report = c["reports"]["proxy-churn-summary.json"]
        journal[0]["churn_indices"] = [0]
        report["schedule"][0]["churn_indices"] = [0]
        pending = dict(completed=True, semantic_verified=True,
                       upstream_receipt_verified=True, outcome="boundary_pending")
        journal[1]["request"] = pending
        report["results"][0]["request"] = dict(pending, outcome="supported_success",
                                                boundary_verified=True)
        report["mapping_evidence"] = dict(unverified_text_boundaries=0, unmet_requirements=[])
        self.assertEqual(evaluate_evidence_bindings(c)["state"], "pass")
        report["results"][0]["request"]["duration_ms"] = 1
        with self.assertRaises(ValueError): evaluate_evidence_bindings(c)

    def test_duplicate_identity_cannot_be_hidden_by_summary_ordering(self):
        c = self.evidence()
        journal = c["phases"]["proxy-fixed"]["events"]
        journal.extend(list(journal))
        with self.assertRaises(ValueError): evaluate_evidence_bindings(c)

    def test_proxy_phase_order_is_already_bound_by_ordered_child_seals(self):
        c = self.evidence()
        c["events"] = [e for e in c["events"] if not (
            e.get("mode") == "proxy" and e.get("event") in ("phase_start", "phase_end"))]
        self.assertEqual(evaluate_evidence_bindings(c)["state"], "pass")
