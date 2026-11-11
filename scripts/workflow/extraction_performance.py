"""Real extraction measurements; fixture expectations remain client-side."""
import math


def measured_stages(recent, warm_sessions, count):
    measured=[row for row in recent if row.get("identity",{}).get("session") and row["identity"]["session"] not in warm_sessions]
    if len(measured) != count:
        raise ValueError("stage_coverage_invalid")
    result={}
    for stage in ("extraction_run","extraction_wait"):
        samples=[row.get("stages_ms",{}).get(stage) for row in measured]
        if any(type(value) not in (int,float) or not math.isfinite(value) or value < 0 for value in samples):
            raise ValueError("stage_coverage_invalid")
        result[stage]=samples
    return result


def attachment_case(workload, serial, size, fixture, directory, mappings, session, text_only_reply=False):
    import base64
    import copy
    import hashlib
    import json
    from pathlib import Path
    import performance as perf
    from performance_workload import encode
    case = workload.case(serial,size)
    body = json.loads(case["body"])
    expected = copy.deepcopy(body)
    path = Path(directory) / fixture["file"]
    if path.resolve().parent != Path(directory).resolve() or path.is_symlink():
        raise ValueError("fixture_path_invalid")
    data = path.read_bytes()
    if len(data) != fixture["bytes"] or hashlib.sha256(data).hexdigest() != fixture["sha256"]:
        raise ValueError("fixture_integrity_invalid")
    pdf = "kind" in fixture
    text = "\n".join(fixture["pages"]) if pdf else fixture["expected"]
    mime = "application/pdf" if pdf else "image/"+fixture["format"]
    inline = "data:"+mime+";base64,"+base64.b64encode(data).decode()
    part = dict(type="input_file",filename="reference.pdf",file_data=inline) if pdf else dict(type="input_image",image_url=inline)
    body["input"].append(dict(role="user",content=[part]))
    expected["input"].append(dict(role="user",content=[dict(type="input_text",text=text)]))
    replacements={original:mappings[original] for original in set(case["occurrences"])}
    def replaced(text):
        for original,replacement in replacements.items():
            text=text.replace(original,replacement)
        return text
    for item in expected["input"]:
        if item.get("type")=="function_call_output" and isinstance(item.get("output"),str):
            item["output"]=replaced(item["output"])
        for content in item.get("content",[]):
            if content.get("type")=="input_text" and isinstance(content.get("text"),str):
                content["text"]=replaced(content["text"])
    expected_bytes = encode(expected)
    case.update(body=encode(body),occurrences=case["occurrences"]+[text.splitlines()[0]],benchmark_session=session)
    if text_only_reply:
        case["occurrences"]=case["occurrences"][:-1]
        case["benchmark_text_only"]=True
    return case,perf.text_layout_hash(expected_bytes)


def run_phase(binary,seed_binary,workload,directory,manifest,template,clients=1,count=100,warmup_seconds=60,warmup_count=100,repeated=False):
    from performance_load import StageCollector
    import time
    import performance as perf
    import extraction_fixtures as fixtures
    from performance_stats import summarize_outcomes
    if type(clients) is not int or clients not in (1,4) or type(count) is not int or count < 1 or count > 100 or count % clients or type(warmup_seconds) not in (int,float) or not math.isfinite(warmup_seconds) or warmup_seconds < 0 or type(warmup_count) is not int or warmup_count < 1:
        raise ValueError("invalid_measurement_settings")
    fixtures.validate_coverage(manifest)
    if isinstance(template,int):
        selected=[r for r in manifest["images"] if r["template"] == template]
        warm_fixtures=sorted((r for r in selected if r["index"] < 100000),key=lambda r:r["index"])
        measured_fixtures=sorted((r for r in selected if r["index"] >= 100000),key=lambda r:r["index"])
        if len(measured_fixtures) < (1 if repeated else count):
            raise ValueError("insufficient_unique_fixtures")
    else:
        selected=[r for r in manifest["pdfs"] if r["id"] == template]
        warm_fixtures=measured_fixtures=selected
    if not warm_fixtures or not measured_fixtures:
        raise ValueError("unknown_template")
    records=[]
    try:
        with perf.Environment(binary,seed_binary,workload,262144,correlate=True) as env:
            def send(fixture,serial,session):
                case,digest=attachment_case(workload,serial,262144,fixture,directory,env.mappings,session)
                row=perf.request(env.url,case,expected_layout_hash=digest)
                row["request_bytes"]=len(case["body"])
                return row
            start=time.monotonic()
            warm=[]
            warm_per_client=math.ceil(warmup_count/clients)
            while len(warm)<warmup_count or time.monotonic()-start<warmup_seconds:
                def warm_operation(client,serial):
                    index=len(warm)+client*warm_per_client+serial
                    fixture=measured_fixtures[0] if repeated else warm_fixtures[index % len(warm_fixtures)]
                    return send(fixture,index,"warmup")
                rows,_=perf.closed_loop(warm_operation,clients,per_client=warm_per_client)
                warm.extend(rows)
                if any(row["outcome"]!="supported_success" for row in rows):
                    raise perf.PhaseFailure("warmup",warm)
            warmup_elapsed = time.monotonic() - start
            warm_sessions={row.get("identity",{}).get("session") for row in env.metrics(include_recent=True)["recent_requests"]}
            if not warm_sessions or None in warm_sessions:
                raise ValueError("stage_coverage_invalid")
            per_client=count//clients
            def measured_operation(client,serial):
                index=client*per_client+serial
                fixture=measured_fixtures[0] if repeated or not isinstance(template,int) else measured_fixtures[index]
                return send(fixture,index,"measurement")
            with perf.ResourceSampler(env) as sampler, StageCollector(env, warm_sessions) as collector:
                measured,_=perf.closed_loop(measured_operation,clients,per_client=per_client)
                records.extend(measured)
            # Request accounting is finalized just after the terminal response.
            collector.wait_for(count)
            recent = collector.records
            stages=measured_stages(recent,warm_sessions,count)
            result=dict(stage_poll_errors=collector.errors, records=records,outcomes=summarize_outcomes(records),stage_samples=stages,
                        resources=perf.resource_summary(sampler.samples),warmup_completed=len(warm), warmup_seconds=warmup_elapsed,
                        achieved_concurrency=perf.peak_concurrency(records),performance_qualified=False)
        return result
    except perf.PhaseFailure:
        raise
    except Exception as error:
        context=error.__context__
        seen={id(error)}
        while context is not None and id(context) not in seen:
            if isinstance(context,perf.PhaseFailure):
                raise perf.PhaseFailure(context.phase,context.records) from error
            seen.add(id(context))
            context=context.__context__
        raise perf.PhaseFailure("measurement_collection",records) from error



def record_failure(report,error):
    from performance_stats import summarize_outcomes
    if error.phase == "measurement_collection":
        report["measurements"].append(dict(records=error.records,outcomes=summarize_outcomes(error.records),collection_failed=True))
        report["setup_failure"]="measurement_collection"
    else:
        report["setup_failure"]=error.evidence()



def nonnegative_seconds(value):
    import argparse
    seconds=float(value)
    if not math.isfinite(seconds) or seconds < 0:
        raise argparse.ArgumentTypeError("duration must be finite and nonnegative")
    return seconds


def reserve_report(path):
    import os
    descriptor=os.open(path,os.O_WRONLY | os.O_CREAT | os.O_EXCL,0o600)
    os.close(descriptor)


def write_report(path,report):
    import os
    import tempfile
    from pathlib import Path
    import performance as perf
    path=Path(path)
    descriptor,temporary=tempfile.mkstemp(prefix=".extraction-report-",dir=path.parent)
    os.close(descriptor)
    try:
        perf.write_report(temporary,report)
        os.replace(temporary,path)
    finally:
        Path(temporary).unlink(missing_ok=True)


def main():
    import argparse
    import hashlib
    import json
    from pathlib import Path
    import performance as perf
    import extraction_fixtures as fixtures
    from performance_workload import Workload
    parser=argparse.ArgumentParser(description="Measure isolated real extraction without claiming full qualification")
    parser.add_argument("--binary",required=True,type=Path)
    parser.add_argument("--seed-binary",required=True,type=Path)
    parser.add_argument("--fixtures",required=True,type=Path)
    parser.add_argument("--report",required=True,type=Path)
    parser.add_argument("--template",required=True,choices=["0","1","2","3","pdf-text","pdf-scan"])
    parser.add_argument("--clients",type=int,choices=[1,4],default=1)
    parser.add_argument("--count",type=int,default=100)
    parser.add_argument("--warmup-seconds",type=nonnegative_seconds,default=60)
    parser.add_argument("--warmup-count",type=int,default=100)
    parser.add_argument("--repetitions",type=int,default=3)
    parser.add_argument("--repeated",action="store_true")
    parser.add_argument("--repetition-offset", type=int, default=0)
    args=parser.parse_args()
    if args.repetition_offset < 0:
        parser.error("repetition offset must be nonnegative")
    if args.repetitions < 1:
        parser.error("repetitions must be positive")
    try:
        reserve_report(args.report)
    except OSError:
        print("report destination unavailable; no measurement started")
        return 1
    workload=Workload()
    report=dict(schema_version=1,settings=dict(template=args.template,clients=args.clients,count=args.count,
                warmup_seconds=args.warmup_seconds,warmup_count=args.warmup_count,repetitions=args.repetitions,repeated=args.repeated),
                environment=None,workload=workload.metadata(),measurements=[],comparison=None,
                performance_qualified=False,unmet_requirements=["full_workload_qualification_not_implemented"])
    success=True
    try:
        report["environment"]=perf.environment_info(args.binary)
        manifest=json.loads((args.fixtures / "manifest.json").read_text())
        fixtures.validate_coverage(manifest)
        if manifest.get("font_name") != "Menlo-Regular":
            raise ValueError("fixture font mismatch")
        current=fixtures.provenance()
        if any(manifest.get(k)!=v for k,v in current.items()):
            raise ValueError("fixture_provenance_changed")
        report["workload"].update(fixture_generator_version=manifest["version"],tool_versions=current["tool_versions"])
        for name,file in (("extraction_generator_sha256","extraction_fixtures.py"),("extraction_runner_sha256","extraction_performance.py"),("renderer_sha256","render_extraction.swift")):
            report["workload"][name]=hashlib.sha256(Path(__file__).with_name(file).read_bytes()).hexdigest()
        template=int(args.template) if args.template.isdigit() else args.template
        for repetition in range(args.repetition_offset, args.repetition_offset + args.repetitions):
            result=run_phase(args.binary,args.seed_binary,workload,args.fixtures,manifest,template,
                             args.clients,args.count,args.warmup_seconds,args.warmup_count,args.repeated)
            result["repetition"]=repetition
            report["measurements"].append(result)
            write_report(args.report,report)
            if result["outcomes"]["supported_success"] != args.count:
                success=False
                break
    except perf.PhaseFailure as error:
        record_failure(report,error)
        success=False
    except (OSError,ValueError,RuntimeError):
        report["setup_failure"]="extraction_measurement_failed"
        success=False
    try:
        write_report(args.report,report)
    except (OSError,ValueError):
        print("report write failed; previous completed report preserved")
        return 1
    print("extraction measurement completed; performance_qualified=false" if success else "extraction measurement failed; evidence retained")
    return 0 if success else 1


if __name__ == "__main__":
    raise SystemExit(main())
