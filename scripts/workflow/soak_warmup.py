"""Four-client warmup with independent duration and success minimums."""
import copy
import math
import threading
import time
from concurrent.futures import ThreadPoolExecutor
from soak_phase import OperationFailure
from soak_schedule import plan_request


class WarmupFailure(RuntimeError):
    def __init__(self,report):
        super().__init__("soak_warmup_failed")
        self.report=report


def run_warmup(execute,*,minimum_seconds=60,minimum_count=100,clock=time.monotonic,
               record=lambda event:None,stop=None):
    """Use a warmup-enabled executor; retain every started request's outcome.

    Request deadlines belong to the executor. Failure stops new admissions;
    already-running requests finish under those existing bounded deadlines.
    """
    if type(minimum_seconds) not in (int,float) or not math.isfinite(minimum_seconds) or minimum_seconds<=0:
        raise ValueError("warmup_seconds")
    if type(minimum_count) is not int or minimum_count<=0:
        raise ValueError("warmup_count")
    started=clock()
    if not math.isfinite(started):
        raise ValueError("warmup_clock")
    lock=threading.Lock()
    failed=threading.Event()
    external_stop=stop if stop is not None else threading.Event()
    results=[]
    failures=[]
    successes=[0]*4
    last_elapsed=0

    def elapsed():
        nonlocal last_elapsed
        value=clock()-started
        if not math.isfinite(value) or value<last_elapsed:
            raise ValueError("warmup_clock")
        last_elapsed=value
        return value

    def worker(client):
        serial=0
        while True:
            with lock:
                if failed.is_set() or external_stop.is_set():
                    return
                try:
                    now=elapsed()
                    if sum(successes)>=minimum_count and now>=minimum_seconds and all(successes):
                        return
                    plan=plan_request(client,serial,"fixed")
                    while plan["category"] not in ("text","image"):
                        serial+=1
                        plan=plan_request(client,serial,"fixed")
                    serial+=1
                    plan=dict(plan,churn_indices=[])
                    record({"type":"warmup_start","request":copy.deepcopy(plan),"elapsed_seconds":now})
                except Exception as error:
                    failures.append({"stage":"admission","error_kind":type(error).__name__})
                    failed.set()
                    return
            operation_failed=False
            try:
                row=execute(copy.deepcopy(plan))
                if not isinstance(row,dict):
                    raise ValueError("warmup_result")
                row=copy.deepcopy(row)
            except OperationFailure as error:
                operation_failed=True
                row=copy.deepcopy(error.result)
            except Exception as error:
                operation_failed=True
                row={"outcome":"harness_invalid","completed":False,"error_kind":type(error).__name__}
            with lock:
                result={"request":plan,"result":row}
                results.append(result)
                valid=not operation_failed and row.get("outcome")=="supported_success" and all(row.get(key) is True for key in (
                    "completed","semantic_verified","boundary_verified","upstream_receipt_verified"))
                if valid:
                    successes[client]+=1
                else:
                    failures.append({"stage":"operation","identity":plan["identity"]})
                    failed.set()
                try:
                    result["elapsed_seconds"]=elapsed()
                    record({"type":"warmup_complete",**copy.deepcopy(result)})
                except Exception as error:
                    failures.append({"stage":"completion","error_kind":type(error).__name__})
                    failed.set()

    def guarded_worker(client):
        try:
            worker(client)
        except BaseException as error:
            # A dead worker must not leave peers waiting forever for its first
            # success. Preserve only diagnostic metadata, never exception text.
            with lock:
                failures.append({"stage":"worker","client":client,"error_kind":type(error).__name__})
                failed.set()

    with ThreadPoolExecutor(max_workers=4) as pool:
        futures=[pool.submit(guarded_worker,client) for client in range(4)]
        try:
            for future in futures:
                future.result()
        except BaseException:
            failed.set()
            raise
    completed=(not failed.is_set() and not external_stop.is_set()
               and sum(successes)>=minimum_count and last_elapsed>=minimum_seconds and all(successes))
    report=dict(completed=bool(completed),elapsed_seconds=last_elapsed,
                successful=sum(successes),client_successes=successes,results=results,
                failures=failures,cancelled=external_stop.is_set(),
                minimum_seconds=minimum_seconds,minimum_count=minimum_count)
    if not completed:
        raise WarmupFailure(report)
    return report
