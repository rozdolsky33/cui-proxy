import threading
import unittest
from soak_warmup import WarmupFailure,run_warmup


class Clock:
    def __init__(self):self.value=0;self.lock=threading.Lock()
    def __call__(self):
        with self.lock:return self.value
    def advance(self):
        with self.lock:self.value+=1


def success():
    return dict(outcome="supported_success",completed=True,semantic_verified=True,
                boundary_verified=True,upstream_receipt_verified=True)


class WarmupTests(unittest.TestCase):
    def test_waits_for_both_minimums_with_four_clients_and_all_outcomes(self):
        for seconds,count in [(60,100),(20,8)]:
            clock=Clock();barrier=threading.Barrier(4);seen=set();lock=threading.Lock();events=[]
            def execute(plan):
                client=plan["identity"][1]
                with lock:first=client not in seen;seen.add(client)
                if first:barrier.wait(timeout=2)
                clock.advance();return success()
            report=run_warmup(execute,minimum_seconds=seconds,minimum_count=count,
                              clock=clock,record=events.append)
            self.assertTrue(report["completed"])
            self.assertGreaterEqual(report["elapsed_seconds"],seconds)
            self.assertGreaterEqual(report["successful"],count)
            self.assertTrue(all(n>0 for n in report["client_successes"]))
            self.assertEqual(len(events),2*len(report["results"]))
            self.assertTrue(all(row["request"]["category"] in ("text","image") for row in report["results"]))

    def test_failed_semantics_retains_failure_and_stops_warmup(self):
        clock=Clock()
        def execute(plan):
            clock.advance();row=success();row["semantic_verified"]=False;return row
        with self.assertRaises(WarmupFailure) as caught:
            run_warmup(execute,minimum_seconds=60,minimum_count=100,clock=clock)
        report=caught.exception.report
        self.assertFalse(report["completed"])
        self.assertEqual(report["successful"],0)
        self.assertGreaterEqual(len(report["results"]),1)

    def test_record_failure_never_sends_an_unrecorded_request(self):
        calls=[]
        def broken(event):raise OSError("record failed")
        with self.assertRaises(WarmupFailure) as caught:
            run_warmup(lambda plan:calls.append(plan),minimum_seconds=1,minimum_count=1,record=broken)
        self.assertEqual(calls,[])
        self.assertFalse(caught.exception.report["completed"])

    def test_invalid_minimums_and_cancellation_are_not_success(self):
        for value in [True,0,-1,float("nan"),float("inf")]:
            with self.assertRaises(ValueError):run_warmup(lambda plan:success(),minimum_seconds=value)
        stop=threading.Event();stop.set()
        with self.assertRaises(WarmupFailure):run_warmup(lambda plan:success(),stop=stop)

    def test_explicit_operation_failure_cannot_be_promoted_by_attached_fields(self):
        from soak_phase import OperationFailure
        clock=Clock()
        def execute(plan):
            clock.advance();raise OperationFailure(success())
        with self.assertRaises(WarmupFailure) as caught:
            run_warmup(execute,minimum_seconds=1,minimum_count=1,clock=clock)
        self.assertEqual(caught.exception.report["successful"],0)

    def test_unexpected_worker_exit_is_retained_as_failed_warmup(self):
        stop=threading.Event()
        def execute(plan):
            stop.set();raise SystemExit(1)
        with self.assertRaises(WarmupFailure) as caught:
            run_warmup(execute,minimum_seconds=1,minimum_count=1,stop=stop)
        self.assertFalse(caught.exception.report["completed"])
        self.assertTrue(caught.exception.report["failures"])
