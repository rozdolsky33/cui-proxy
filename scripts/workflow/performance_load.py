"""Bounded client schedulers; failures and unsent slots remain explicit."""
import concurrent.futures
import math
import threading
import time


def _attempt(operation,args,clock):
    start=clock()
    try:
        row=operation(*args)
        if not isinstance(row,dict) or "outcome" not in row:
            raise ValueError("invalid_operation_result")
        return row
    except Exception:
        finish=clock()
        return dict(operation_id="attempt-"+"-".join(str(value) for value in args),supported=True,
                    completed=False,outcome="harness_invalid",started_s=start,finished_s=finish,
                    duration_ms=max(0,finish-start)*1000)


def _validate_mixed_settings(text_count, duration):
    if (text_count is None) == (duration is None):
        raise ValueError('choose_count_or_duration')
    if text_count is not None and (type(text_count) is not int or text_count < 1):
        raise ValueError('invalid_text_count')
    if duration is not None and (type(duration) not in (int, float) or not math.isfinite(duration) or duration <= 0):
        raise ValueError('invalid_duration')


def mixed_closed_loop(operation, *, text_count=None, duration=None, clock=time.monotonic, clients=4):
    if type(clients) is not int or clients not in (1, 4, 8, 16):
        raise ValueError('invalid_clients')
    _validate_mixed_settings(text_count, duration)
    stop = threading.Event()
    lock = threading.Lock()
    state = dict(start=None, end=None, aborted=False)
    barrier = threading.Barrier(clients, action=lambda: state.update(start=clock()))
    arms = [[] for _ in range(clients)]

    def finish(aborted=False):
        with lock:
            if state['end'] is None:
                state['end'] = clock()
            state['aborted'] = state['aborted'] or aborted
            stop.set()

    def client(index):
        text_completed = 0
        barrier.wait(timeout=5)
        serial = 0
        while not stop.is_set():
            if duration is not None and clock() >= state['start'] + duration:
                finish()
                break
            operation_index = index if clients != 1 else 0 if serial % 4 == 0 else 1
            operation_serial = serial if clients != 1 else serial // 4 if operation_index == 0 else serial - serial // 4 - 1
            row = _attempt(operation, (operation_index, operation_serial), clock)
            row.update(client=index, workload_kind='text' if operation_index == 0 else 'image')
            arms[index].append(row)
            serial += 1
            if operation_index == 0:
                text_completed += 1
            if row['outcome'] != 'supported_success':
                finish(aborted=True)
                break
            if (index == 0 and text_count is not None and (text_completed >= text_count)) and (clients != 1 or serial % 4 == 0):
                finish()
                break
    with concurrent.futures.ThreadPoolExecutor(max_workers=clients) as pool:
        futures = [pool.submit(client, index) for index in range(clients)]
        for future in futures:
            future.result()
    rows = [row for arm in arms for row in arm]
    window = duration if duration is not None and (not state['aborted']) else state['end'] - state['start']
    text_rows = [row for row in rows if row['workload_kind'] == 'text']
    image_rows = [row for row in rows if row['workload_kind'] == 'image']
    return dict(records=rows, text_records=text_rows, image_records=image_rows, text_unsent=max(0, text_count - len(text_rows)) if text_count is not None else None, window_start_s=state['start'], window_seconds=window, aborted=state['aborted'])


def _validate_open_settings(rate, duration, max_outstanding):
    if type(rate) not in (int, float) or not math.isfinite(rate) or rate <= 0 or (type(duration) not in (int, float)) or (not math.isfinite(duration)) or (duration <= 0) or (type(max_outstanding) is not int) or (not 1 <= max_outstanding <= 16):
        raise ValueError('invalid_open_loop_settings')


def open_loop(operation,rate,duration,max_outstanding=16,*,clock=time.monotonic,sleep=time.sleep):
    _validate_open_settings(rate, duration, max_outstanding)
    scheduled=math.ceil(rate*duration)
    unsent=[];futures=[];pending=set()
    with concurrent.futures.ThreadPoolExecutor(max_workers=max_outstanding) as pool:
        start=clock();end=start+duration
        for index in range(scheduled):
            target=start+index/rate
            wait=target-clock()
            if wait>0:sleep(wait)
            pending={future for future in pending if not future.done()}
            now=clock()
            reason="scheduler_late" if now >= min(end, target + 1 / rate) else "client_capacity" if len(pending)>=max_outstanding else None
            if reason:
                unsent.append(dict(operation_id=f"scheduled-{index}",scheduled_s=target,reason=reason))
                continue
            def run(serial=index,scheduled_s=target,dispatched_s=now):
                row=_attempt(operation,(serial,),clock)
                row.update(scheduled_s=scheduled_s,dispatched_s=dispatched_s)
                return row
            future=pool.submit(run);futures.append(future);pending.add(future)
        remaining=end-clock()
        if remaining>0:sleep(remaining)
        rows=[future.result() for future in futures]
    return dict(records=rows,unsent_records=unsent,scheduled=scheduled,sent=len(rows),
                completed=sum(row.get("completed") is True for row in rows),
                rejected=sum(isinstance(row.get("status"),int) and 400<=row["status"]<500 for row in rows),client_unsent=len(unsent),
                window_start_s=start,window_seconds=duration)


class StageCollector:
    """Retain fixed request-stage metadata while the server's recent ring rotates."""
    def __init__(self,env,warm_sessions):
        self.env=env
        self.warm_sessions=set(warm_sessions)
        self._records={}
        self.errors=0
        self.stop=threading.Event()
        self.thread=None

    @property
    def records(self):
        return list(self._records.values())

    def sample(self):
        try:
            recent=self.env.metrics(include_recent=True)["recent_requests"]
            for row in recent:
                session=row.get("identity",{}).get("session")
                if session in self.warm_sessions:continue
                stamp=row.get("time")
                if not isinstance(session,str) or not session or not isinstance(stamp,str) or not isinstance(row.get("stages_ms"),dict):
                    self.errors+=1
                    continue
                self._records[(stamp,session)]=dict(time=stamp,identity=dict(session=session),stages_ms=dict(row["stages_ms"]))
        except (OSError,ValueError,KeyError,TypeError):
            self.errors+=1

    def __enter__(self):
        self.sample()
        def poll():
            while not self.stop.wait(1):self.sample()
        self.thread=threading.Thread(target=poll,daemon=True)
        self.thread.start()
        return self

    def __exit__(self,*_):
        self.stop.set()
        self.thread.join(timeout=3)
        if self.thread.is_alive():raise RuntimeError("stage_sampler_shutdown")
        self.sample()

    def wait_for(self,count):
        deadline=time.monotonic()+2
        while len(self._records)<count and time.monotonic()<deadline:
            self.sample()
            if len(self._records)<count:time.sleep(0.02)
        if len(self._records)!=count:raise ValueError("stage_coverage_invalid")
        return self.records


def _text_case(workload,serial,size,mappings,session):
    import hashlib
    import json
    from performance_workload import encode
    case=workload.case(serial,size)
    body=json.loads(case["body"])
    replacements={original:mappings[original] for original in set(case["occurrences"])}
    def replace(text):
        for original,replacement in replacements.items():text=text.replace(original,replacement)
        return text
    for item in body["input"]:
        if item.get("type")=="function_call_output" and isinstance(item.get("output"),str):item["output"]=replace(item["output"])
        for content in item.get("content",[]):
            if content.get("type")=="input_text" and isinstance(content.get("text"),str):content["text"]=replace(content["text"])
    case["benchmark_session"]=session
    return case,hashlib.sha256(encode(body)).hexdigest(),hashlib.sha256(case["body"]).hexdigest()


def _raise_phase_error(error,records):
    import performance as perf
    context=error.__context__;seen={id(error)}
    while context is not None and id(context) not in seen:
        if isinstance(context,perf.PhaseFailure):raise perf.PhaseFailure(context.phase,context.records) from error
        seen.add(id(context));context=context.__context__
    raise perf.PhaseFailure("measurement_collection",records) from error


def _drained_metrics(env):
    deadline=time.monotonic()+2
    while True:
        metrics=env.metrics(include_recent=True)
        if metrics.get("active_requests")==0:return metrics
        if time.monotonic()>=deadline:raise ValueError("request_drain_incomplete")
        time.sleep(0.02)


def _mixed_warmup_batch(operation, *, clients, text_count, offsets=None):
    """Advance operation-role offsets without reusing warm-up identities."""
    offsets = [0] * max(2, clients) if offsets is None else list(offsets)
    batch = mixed_closed_loop(
        lambda client, serial: operation(client, offsets[client] + serial),
        text_count=text_count, clients=clients)
    if clients == 1:
        counts = [len(batch["text_records"]), len(batch["image_records"])]
    else:
        counts = [sum(row["client"] == client for row in batch["records"])
                  for client in range(clients)]
    return batch, [offset + count for offset, count in zip(offsets, counts)]


def _mixed_operation_identity(client, serial, *, clients, warmup):
    """Keep warm-up separate while preserving reference four-client IDs."""
    base = max(10, clients) * 1000000 if warmup else 0
    return base + client * 1000000 + serial


def run_fairness_phase(binary, seed_binary, workload, directory, manifest, *, bypass=False, text_count=None, duration=None, text_size=None, warmup_seconds=60, warmup_count=100, clients=4):
    if type(clients) is not int or clients not in (1, 4, 8, 16):
        raise ValueError('invalid_clients')
    import hashlib
    import performance as perf
    import extraction_fixtures as fixtures
    from extraction_performance import attachment_case, measured_stages
    from performance_stats import summarize_outcomes
    _validate_mixed_settings(text_count, duration)
    fixtures.validate_coverage(manifest)
    if type(warmup_seconds) not in (int, float) or not math.isfinite(warmup_seconds) or warmup_seconds < 0 or (type(warmup_count) is not int) or (warmup_count < 1):
        raise ValueError('invalid_warmup_settings')
    if text_size is not None and text_size not in workload.sizes:
        raise ValueError('invalid_text_size')
    if (text_count is None) == (duration is None):
        raise ValueError('choose_count_or_duration')
    warm_fixtures = [row for row in manifest['images'] if row['index'] < 100000]
    measured_fixtures = [row for row in manifest['images'] if row['index'] >= 100000]
    records = []
    try:
        before = perf.environment_info(binary)
        with perf.Environment(binary, seed_binary, workload, 262144, correlate=True) as env:
            case, digest, _ = _text_case(workload, 9000000, workload.sizes[0], env.mappings, '')
            preflight = perf.request(env.url, case, expected_hash=digest)
            if preflight['outcome'] != 'supported_success':
                raise perf.PhaseFailure('preflight', [preflight])
            target = env.upstream_url if bypass else env.url

            def operation(client, serial, phase):
                size = text_size if client == 0 and text_size is not None else workload.sizes[serial % len(workload.sizes)]
                identity = _mixed_operation_identity(client, serial, clients=clients, warmup=phase == 'warmup')
                session = 'warmup' if phase == 'warmup' else 'text-measurement' if client == 0 else 'image-measurement'
                if client == 0:
                    case, digest, raw_digest = _text_case(workload, identity, size, env.mappings, session)
                    row = perf.request(target, case, expected_hash=raw_digest if bypass else digest)
                else:
                    pool = warm_fixtures if phase == 'warmup' else measured_fixtures
                    fixture = pool[(serial * 3 + client - 1) % len(pool)]
                    case, digest = attachment_case(workload, identity, size, fixture, directory, env.mappings, session, text_only_reply=True)
                    raw_digest = hashlib.sha256(case['body']).hexdigest()
                    row = perf.request(target, case, expected_hash=raw_digest if bypass else None, expected_layout_hash=None if bypass else digest)
                row['request_bytes'] = len(case['body'])
                return row
            start = time.monotonic()
            warm = []
            offsets = None
            while len(warm) < warmup_count or time.monotonic() - start < warmup_seconds:
                batch, offsets = _mixed_warmup_batch(lambda client, serial: operation(client, serial, 'warmup'), clients=clients, text_count=max(1, math.ceil((warmup_count - len(warm)) / 4)), offsets=offsets)
                warm.extend(batch['records'])
                if batch['aborted']:
                    raise perf.PhaseFailure('warmup', warm)
            warmup_elapsed = time.monotonic() - start
            metrics = _drained_metrics(env)
            warm_sessions = {row.get('identity', {}).get('session') for row in metrics['recent_requests']}
            if not bypass and (not warm_sessions or None in warm_sessions):
                raise ValueError('warmup_correlation_missing')
            with perf.ResourceSampler(env) as resources, StageCollector(env, warm_sessions) as stages:
                measured = mixed_closed_loop(lambda client, serial: operation(client, serial, 'measurement'), text_count=text_count, duration=duration, clients=clients)
                records.extend(measured['records'])
            if not bypass:
                recent = stages.wait_for(len(records))
                images = [row for row in recent if 'extraction_run' in row['stages_ms']]
                samples = measured_stages(images, set(), len(measured['image_records']))
            else:
                samples = None
            result = dict(measured, mode='bypass' if bypass else 'proxy', population=env.population, text_outcomes=summarize_outcomes(measured['text_records']), image_outcomes=summarize_outcomes(measured['image_records']), outcomes=summarize_outcomes(records), stage_samples=samples, stage_poll_errors=stages.errors, resources=perf.resource_summary(resources.samples), warmup_completed=len(warm), warmup_seconds=warmup_elapsed, achieved_client_concurrency=perf.peak_concurrency(records), correlation_enabled=True, text_completions_per_second=perf.window_successes(measured['text_records'], measured['window_start_s'], measured['window_seconds']) / measured['window_seconds'], final_metrics=env.metrics(), performance_qualified=False, clients=clients)
        result['environment'] = dict(before=before, after=perf.environment_info(binary))
        return result
    except perf.PhaseFailure:
        raise
    except Exception as error:
        _raise_phase_error(error, records)


def run_overload_phase(binary, seed_binary, workload, *, size, rate, duration,
                       max_outstanding=16, bypass=False, warmup_seconds=60, warmup_count=100):
    """Run diagnostic text load, retaining every offered slot on collection failure."""
    import performance as perf
    from performance_stats import summarize_outcomes
    _validate_open_settings(rate, duration, max_outstanding)
    if (type(size) is not int or size <= 0 or
            type(warmup_seconds) not in (int, float) or not math.isfinite(warmup_seconds) or
            warmup_seconds < 0 or type(warmup_count) is not int or warmup_count < 1):
        raise ValueError("invalid overload phase settings")
    if size not in workload.sizes:
        raise ValueError("unsupported workload size")
    before = perf.environment_info(binary)
    measured = None
    records = []
    warm = []
    try:
        with perf.Environment(binary, seed_binary, workload, size) as env:
            target = env.upstream_url if bypass else env.url
            case, transformed_hash, _ = _text_case(workload, 9000000, size, env.mappings, "preflight")
            preflight = perf.request(env.url, case, expected_hash=transformed_hash)
            if preflight.get("outcome") != "supported_success":
                raise perf.PhaseFailure("preflight", [preflight])
            def operation(serial, session="measurement"):
                case, transformed_hash, raw_hash = _text_case(
                    workload, serial, size, env.mappings, session)
                row = perf.request(target, case, expected_hash=raw_hash if bypass else transformed_hash)
                row["request_bytes"] = len(case["body"])
                return row
            warm_start = time.monotonic()
            while len(warm) < warmup_count or time.monotonic() - warm_start < warmup_seconds:
                warm.append(operation(10000000 + len(warm), "warmup"))
                if warm[-1].get("outcome") != "supported_success":
                    raise perf.PhaseFailure("warmup", warm)
            _drained_metrics(env)
            with perf.ResourceSampler(env) as resources:
                measured = open_loop(operation, rate, duration, max_outstanding)
                records.extend(measured["records"])
                measured.update(mode="bypass" if bypass else "proxy", size_bytes=size,
                                population=env.population, warmup_completed=len(warm),
                                outcomes=summarize_outcomes(records),
                                achieved_client_concurrency=perf.peak_concurrency(records),
                                performance_qualified=False, collection_failed=False)
            measured["resources"] = perf.resource_summary(resources.samples)
            _drained_metrics(env)
            measured["final_metrics"] = env.metrics()
            measured["environment"] = dict(before=before, after=perf.environment_info(binary))
        return measured
    except Exception as error:
        if measured is not None:
            # Preserve the complete scheduler denominator even if sampling or cleanup fails.
            measured.update(performance_qualified=False, collection_failed=True,
                            collection_error_kind=type(error).__name__)
            return measured
        _raise_phase_error(error, records)
