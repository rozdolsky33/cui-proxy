import os
import threading
import time
import unittest
from unittest import mock
import performance_load as load


def success(client,serial,started,finished):
    return dict(operation_id=f"{client}-{serial}",supported=True,completed=True,outcome="supported_success",started_s=started,finished_s=finished,duration_ms=(finished-started)*1000)

class LoadTests(unittest.TestCase):
    def test_open_loop_does_not_dispatch_after_partial_final_window(self):
        now = [0.0]
        def sleep(seconds):
            now[0] += seconds + 0.3
        result = load.open_loop(
            lambda *args: {"outcome": "supported_success", "completed": True, "status": 200},
            rate=2, duration=0.75, clock=lambda: now[0], sleep=sleep)
        self.assertEqual(result["sent"], 1)
        self.assertEqual(result["client_unsent"], 1)
        self.assertEqual(result["unsent_records"][0]["reason"], "scheduler_late")


    def test_fairness_rejects_invalid_settings_before_fixture_or_backend_work(self):
        for settings in ({"text_count": 0}, {"duration": float("nan")},
                         {"duration": -1}, {"text_count": True}):
            with self.subTest(settings=settings), mock.patch(
                    "extraction_fixtures.validate_coverage", side_effect=AssertionError("fixture work started")), self.assertRaises(ValueError):
                load.run_fairness_phase(None, None, None, None, None, **settings)


    def test_three_image_clients_remain_active_until_text_count_finishes(self):
        barrier=threading.Barrier(4)
        def operation(client,serial):
            started=time.monotonic()
            if serial==0:barrier.wait(timeout=2)
            time.sleep(0.001 if client==0 else 0.01)
            return success(client,serial,started,time.monotonic())
        result=load.mixed_closed_loop(operation,text_count=3)
        self.assertEqual(len(result["text_records"]),3)
        self.assertFalse(result["aborted"])
        self.assertEqual(result["text_unsent"],0)
        for client in (1,2,3):
            rows=[r for r in result["image_records"] if r["client"]==client]
            self.assertTrue(rows)
            self.assertLessEqual(rows[0]["started_s"],result["text_records"][-1]["finished_s"])

    def test_mixed_operation_exception_is_retained_and_aborts_normal_load(self):
        def operation(client,serial):raise RuntimeError("operation_failed")
        result=load.mixed_closed_loop(operation,text_count=3)
        self.assertTrue(result["aborted"])
        self.assertTrue(result["records"])
        self.assertTrue(all(r["outcome"]=="harness_invalid" for r in result["records"]))
        self.assertEqual(result["text_unsent"]+len(result["text_records"]),3)

    def test_open_loop_preserves_scheduled_sent_and_unsent_counts(self):
        now=[0.0];release=threading.Event()
        def sleep(seconds):
            now[0]+=seconds
            if now[0]>=0.75:release.set()
        def operation(index):
            release.wait(timeout=2)
            return success(0,index,0,now[0])
        result=load.open_loop(operation,rate=4,duration=0.75,max_outstanding=2,clock=lambda:now[0],sleep=sleep)
        self.assertEqual(result["scheduled"],3)
        self.assertEqual(result["sent"],2)
        self.assertEqual(result["client_unsent"],1)
        self.assertEqual(len(result["records"]),2)
        self.assertEqual(result["completed"],2)
        self.assertEqual(result["unsent_records"][0]["reason"],"client_capacity")

    def test_invalid_load_settings_are_rejected(self):
        for rate,duration,cap in [(0,1,16),(1,float("inf"),16),(1,0,16),(1,1,0),(1,1,17)]:
            with self.assertRaises(ValueError):load.open_loop(lambda _:None,rate,duration,cap)
        for count in (0,-1,True):
            with self.assertRaises(ValueError):load.mixed_closed_loop(lambda *_:None,text_count=count)

    def test_open_loop_timeout_is_retained_without_becoming_a_rejection(self):
        now=[0.0]
        def sleep(seconds):now[0]+=seconds
        row=dict(operation_id="timeout",supported=True,completed=False,outcome="unresolved",status=None,started_s=0,finished_s=45,duration_ms=45000)
        result=load.open_loop(lambda _:row,4,0.25,clock=lambda:now[0],sleep=sleep)
        self.assertEqual(result["sent"],1)
        self.assertEqual(result["completed"],0)
        self.assertEqual(result["rejected"],0)
        self.assertEqual(result["records"][0]["outcome"],"unresolved")

    def test_late_slots_are_counted_instead_of_sent_as_a_catchup_burst(self):
        now=[0.0]
        def sleep(seconds):now[0]+=seconds+0.3
        result=load.open_loop(lambda index:success(0,index,0,now[0]),4,0.75,clock=lambda:now[0],sleep=sleep)
        self.assertEqual(result["scheduled"],3)
        self.assertEqual(result["sent"]+result["client_unsent"],3)
        self.assertTrue(any(row["reason"]=="scheduler_late" for row in result["unsent_records"]))

    def test_attachment_oracle_uses_declared_matches_and_preserves_opaque_fields(self):
        import hashlib
        import tempfile
        from pathlib import Path
        from types import SimpleNamespace
        import extraction_performance as extraction
        import performance as perf
        from performance_workload import encode
        body=dict(model="OpaqueOriginal",input=[dict(role="user",content=[dict(type="input_text",text="KnownOriginal")])])
        workload=SimpleNamespace(case=lambda serial,size:dict(operation_id="one",body=encode(body),occurrences=["KnownOriginal"],supported=True))
        with tempfile.TemporaryDirectory() as root:
            root=Path(root);data=b"fixture";(root/"image.png").write_bytes(data)
            fixture=dict(file="image.png",bytes=len(data),sha256=hashlib.sha256(data).hexdigest(),format="png",expected="Reference text")
            _,digest=extraction.attachment_case(workload,0,1,fixture,root,{"KnownOriginal":"AliasKnown","OpaqueOriginal":"AliasOpaque"},"measurement")
        expected=dict(model="OpaqueOriginal",input=[dict(role="user",content=[dict(type="input_text",text="AliasKnown")]),dict(role="user",content=[dict(type="input_text",text="Reference text")])])
        self.assertEqual(digest,perf.text_layout_hash(encode(expected)))

    def test_stage_collector_keeps_samples_after_the_server_ring_rolls_over(self):
        from types import SimpleNamespace
        def row(stamp,session,value):return dict(time=stamp,identity=dict(session=session),stages_ms=dict(extraction_run=value,extraction_wait=0))
        snapshots=iter([dict(recent_requests=[row("0","warm",999),row("1","measure",1)]),dict(recent_requests=[row("1","measure",1),row("2","measure",2)]),dict(recent_requests=[row("3","measure",3)])])
        collector=load.StageCollector(SimpleNamespace(metrics=lambda **_:next(snapshots)),{"warm"})
        for _ in range(3):collector.sample()
        self.assertEqual(len(collector.records),3)
        self.assertEqual([r["stages_ms"]["extraction_run"] for r in collector.records],[1,2,3])
        self.assertEqual(collector.errors,0)

    def test_stage_poll_failure_does_not_erase_retained_samples(self):
        from unittest.mock import Mock
        env=Mock();env.metrics.side_effect=[dict(recent_requests=[dict(time="1",identity=dict(session="measure"),stages_ms=dict(extraction_run=1))]),OSError("metrics_unavailable")]
        collector=load.StageCollector(env,set());collector.sample();collector.sample()
        self.assertEqual(len(collector.records),1)
        self.assertEqual(collector.errors,1)

    @unittest.skipUnless(os.environ.get("CUI_LOAD_PROXY_TEST")=="1","explicit real mixed-load test")
    def test_real_mixed_proxy_and_bypass_preserve_text_and_image_outcomes(self):
        import tempfile
        from pathlib import Path
        import extraction_fixtures as fixtures
        import performance as perf
        from performance_workload import Workload
        with tempfile.TemporaryDirectory() as root:
            root=Path(root);manifest=fixtures.generate(root/"fixtures",count=2)
            runs=[]
            for bypass in (True,False):
                result=load.run_fairness_phase(os.environ["CUI_PROXY_BINARY"],os.environ["CUI_SEED_BINARY"],Workload(),root/"fixtures",manifest,bypass=bypass,text_count=2,warmup_seconds=0,warmup_count=4)
                if bypass:
                    self.assertIsNone(result["request_stage_samples"])
                else:
                    for kind in ("text", "image"):
                        group = result["request_stage_samples"][kind + "_requests"]
                        expected = len(result[kind + "_records"])
                        self.assertEqual(group["requests"], expected)
                        self.assertEqual(group["coverage"]["mapping_checkpoint"], expected)
                self.assertEqual(result["text_outcomes"]["supported_success"],2)
                self.assertGreaterEqual(result["image_outcomes"]["supported_success"],3)
                self.assertFalse(result["aborted"])
                if bypass:self.assertIsNone(result["stage_samples"])
                else:self.assertEqual(len(result["stage_samples"]["extraction_run"]),len(result["image_records"]))
                runs.append(result)
            comparison=perf.compare_runs(runs[1]["text_records"],runs[0]["text_records"])
            self.assertTrue(comparison["relay_samples_valid"])
            perf.write_report(root/"report.json",dict(schema_version=1,measurements=runs,comparison=comparison,performance_qualified=False))

    def test_mixed_single_client_cycles_classes(self):
        import performance_load as load
        seen = []
        def operation(client, serial):
            seen.append((client, serial))
            return dict(operation_id=f"mixed-{client}-{serial}", supported=True,
                        completed=True, outcome="supported_success", duration_ms=1.0)
        result = load.mixed_closed_loop(operation, text_count=3, clients=1)
        self.assertEqual(seen, [(0, 0), (1, 0), (1, 1), (1, 2), (0, 1), (1, 3), (1, 4), (1, 5), (0, 2), (1, 6), (1, 7), (1, 8)])
        self.assertEqual(len(result["text_records"]), 3)
        self.assertEqual(len(result["image_records"]), 9)
        self.assertEqual(len(result["records"]), 12)
        self.assertEqual({row["client"] for row in result["records"]}, {0})
        self.assertEqual(result["text_unsent"], 0)
        self.assertFalse(result["aborted"])

    def test_mixed_capacity_worker_counts_and_default(self):
        import threading
        import time
        import performance_load as load
        for clients in (4, 8, 16):
            with self.subTest(clients=clients):
                first = threading.Barrier(clients, timeout=5)
                lock = threading.Lock()
                active = 0
                peak = 0
                def operation(client, serial):
                    nonlocal active, peak
                    with lock:
                        active += 1
                        peak = max(peak, active)
                    try:
                        if serial == 0:
                            first.wait()
                        time.sleep(0.001)
                        return dict(operation_id=f"mixed-{client}-{serial}", supported=True,
                                    completed=True, outcome="supported_success", duration_ms=1.0)
                    finally:
                        with lock:
                            active -= 1
                settings = {} if clients == 4 else dict(clients=clients)
                result = load.mixed_closed_loop(operation, text_count=2, **settings)
                self.assertEqual(peak, clients)
                self.assertEqual({row["client"] for row in result["records"]}, set(range(clients)))
                self.assertEqual(len(result["text_records"]), 2)
                self.assertGreaterEqual(len(result["image_records"]), clients - 1)
                self.assertTrue(all(row["client"] == 0 for row in result["text_records"]))
                self.assertTrue(all(row["client"] > 0 for row in result["image_records"]))
                self.assertFalse(result["aborted"])

    def test_mixed_single_client_retains_failed_image(self):
        import performance_load as load
        def operation(client, serial):
            if client == 1:
                raise RuntimeError("controlled capacity operation failure")
            return dict(operation_id=f"mixed-{client}-{serial}", supported=True,
                        completed=True, outcome="supported_success", duration_ms=1.0)
        result = load.mixed_closed_loop(operation, text_count=3, clients=1)
        self.assertTrue(result["aborted"])
        self.assertEqual(len(result["records"]), 2)
        self.assertEqual(len(result["text_records"]), 1)
        self.assertEqual(len(result["image_records"]), 1)
        self.assertEqual(result["image_records"][0]["outcome"], "harness_invalid")
        self.assertEqual(result["text_unsent"], 2)

    def test_mixed_rejects_invalid_client_counts(self):
        import performance_load as load
        def operation(client, serial):
            self.fail("Invalid settings must not dispatch work")
        for clients in (0, -1, 2, 3, True, 16.0):
            with self.subTest(clients=clients):
                with self.assertRaises(ValueError):
                    load.mixed_closed_loop(operation, text_count=1, clients=clients)

    def test_mixed_single_client_duration_preserves_window_and_classes(self):
        import performance_load as load
        now = [0.0]
        def operation(client, serial):
            now[0] += 1.0
            return dict(operation_id=f"mixed-{client}-{serial}", supported=True,
                        completed=True, outcome="supported_success", duration_ms=1000.0)
        result = load.mixed_closed_loop(operation, duration=4.0, clients=1,
                                        clock=lambda: now[0])
        self.assertEqual(len(result["records"]), 4)
        self.assertEqual(len(result["text_records"]), 1)
        self.assertEqual(len(result["image_records"]), 3)
        self.assertEqual(result["window_start_s"], 0.0)
        self.assertEqual(result["window_seconds"], 4.0)
        self.assertFalse(result["aborted"])

    def test_mixed_warmup_offsets_cover_capacity_and_single_client_roles(self):
        import threading
        import time
        import performance_load as load
        for clients in (1, 4, 8, 16):
            with self.subTest(clients=clients):
                seen = []
                lock = threading.Lock()
                offsets = None
                for repetition in range(2):
                    first = threading.Barrier(clients, timeout=5)
                    local = threading.local()
                    def operation(client, serial):
                        with lock:
                            seen.append((client, serial))
                        if not getattr(local, "started", False):
                            local.started = True
                            first.wait()
                        time.sleep(0.001)
                        return dict(operation_id=f"warm-{client}-{serial}", supported=True,
                                    completed=True, outcome="supported_success", duration_ms=1.0)
                    batch, offsets = load._mixed_warmup_batch(operation, clients=clients,
                                                              text_count=3, offsets=offsets)
                    self.assertFalse(batch["aborted"])
                    self.assertEqual(len(batch["text_records"]), 3)
                self.assertEqual(len(set(seen)), len(seen))
                self.assertEqual(len(offsets), max(2, clients))
                self.assertEqual(offsets[0], 6)
                if clients == 1:
                    self.assertEqual(offsets, [6, 18])
                    self.assertEqual(len(seen), 24)
                else:
                    self.assertEqual({client for client, _ in seen}, set(range(clients)))

    def test_mixed_identity_separates_capacity_workers_and_warmup(self):
        import performance_load as load
        for clients in (1, 4, 8, 16):
            with self.subTest(clients=clients):
                warm = {load._mixed_operation_identity(client, serial, clients=clients, warmup=True)
                        for client in range(max(2, clients)) for serial in (0, 1, 999)}
                measured = {load._mixed_operation_identity(client, serial, clients=clients, warmup=False)
                            for client in range(max(2, clients)) for serial in (0, 1, 999)}
                self.assertFalse(warm & measured)
                self.assertEqual(len(warm), max(2, clients) * 3)
                self.assertEqual(len(measured), max(2, clients) * 3)
        self.assertEqual(load._mixed_operation_identity(0, 0, clients=4, warmup=True), 10000000)
        self.assertEqual(load._mixed_operation_identity(0, 0, clients=16, warmup=True), 16000000)

    def test_single_client_count_completes_text_image_cycle(self):
        import threading
        import time
        for count in (1, 2, 3):
            with self.subTest(count=count):
                calls = []
                active = 0
                peak = 0
                lock = threading.Lock()
                def operation(role, serial):
                    nonlocal active, peak
                    with lock:
                        active += 1
                        peak = max(peak, active)
                        calls.append((role, serial))
                    try:
                        time.sleep(0.002)
                        return {"operation_id": f"{role}:{serial}",
                                "completed": True, "supported": True,
                                "outcome": "supported_success", "status": 200}
                    finally:
                        with lock:
                            active -= 1
                result = load.mixed_closed_loop(operation, clients=1,
                                                text_count=count)
                self.assertEqual([role for role, _ in calls], [0, 1, 1, 1] * count)
                self.assertEqual(len(result["text_records"]), count)
                self.assertEqual(len(result["image_records"]), 3 * count)
                self.assertEqual(len(set(calls)), 4 * count)
                self.assertEqual(peak, 1)
                self.assertFalse(result["aborted"])

    def test_single_client_small_warmup_batches_include_images(self):
        calls = []
        offsets = None
        def operation(role, serial):
            calls.append((role, serial))
            return {"operation_id": f"{role}:{serial}", "completed": True,
                    "supported": True, "outcome": "supported_success", "status": 200}
        for _ in range(3):
            batch, offsets = load._mixed_warmup_batch(
                operation, clients=1, text_count=1, offsets=offsets)
            self.assertEqual(len(batch["text_records"]), 1)
            self.assertEqual(len(batch["image_records"]), 3)
        self.assertEqual([role for role, _ in calls], [0, 1, 1, 1] * 3)
        self.assertEqual(len(set(calls)), 12)
        self.assertEqual(offsets, [3, 9])

    def test_single_client_count_one_retains_image_failure(self):
        calls = []
        def operation(role, serial):
            calls.append((role, serial))
            return {"operation_id": f"{role}:{serial}", "completed": True,
                    "supported": True, "status": 200,
                    "outcome": "supported_success" if role == 0 else "harness_invalid"}
        result = load.mixed_closed_loop(operation, clients=1, text_count=1)
        self.assertEqual([role for role, _ in calls], [0, 1])
        self.assertEqual(len(result["text_records"]), 1)
        self.assertEqual(len(result["image_records"]), 1)
        self.assertEqual(result["image_records"][0]["outcome"], "harness_invalid")
        self.assertTrue(result["aborted"])
