import copy
import os
import tempfile
from pathlib import Path
import unittest
import performance as perf
from performance_workload import encode

class ExtractionPerformanceTests(unittest.TestCase):
    def test_cli_rejects_changed_fixture_font_before_measurement(self):
        import json
        import sys
        from unittest.mock import patch
        import extraction_performance as extraction
        import extraction_fixtures as fixtures
        from performance_stats import summarize_outcomes
        tools = dict(tool_versions=dict(swift='6.4', tesseract='5.5.4', pdftotext='26.09.0', pdftoppm='26.09.0'), traineddata_sha256='0' * 64)
        manifest = dict(version=fixtures.VERSION, seed=1102026, count=1, images=fixtures.recipes('warmup', 1) + fixtures.recipes('measurement', 1), pdfs=fixtures.pdf_recipes(), **tools, font_name='Unexpected-Font')
        row = dict(operation_id='one', supported=True, completed=True, outcome='supported_success', duration_ms=1)
        result = dict(records=[row], outcomes=summarize_outcomes([row]), stage_samples=dict(extraction_run=[1], extraction_wait=[0]), performance_qualified=False)
        with tempfile.TemporaryDirectory() as root:
            root = Path(root)
            (root / 'manifest.json').write_text(json.dumps(manifest))
            report = root / 'report.json'
            argv = ['extraction_performance', '--binary', 'unused', '--seed-binary', 'unused', '--fixtures', str(root), '--report', str(report), '--template', '0', '--count', '1', '--warmup-count', '1', '--warmup-seconds', '0', '--repetitions', '1']
            with patch.object(sys, 'argv', argv), patch.object(perf, 'environment_info', return_value={}), patch.object(fixtures, 'provenance', return_value=tools), patch.object(extraction, 'run_phase', return_value=result):
                self.assertEqual(extraction.main(), 1)
            written = json.loads(report.read_text())

    def test_long_phase_uses_collected_stages_after_recent_history_expires(self):
        from contextlib import nullcontext
        from types import SimpleNamespace
        from unittest.mock import MagicMock, patch
        import extraction_performance as extraction
        import extraction_fixtures as fixtures
        import performance_load as load
        from performance_workload import Workload
        manifest=dict(version=fixtures.VERSION,seed=1102026,count=1,images=fixtures.recipes("warmup",1)+fixtures.recipes("measurement",1),pdfs=fixtures.pdf_recipes())
        env = MagicMock()
        env.__enter__.return_value = env
        env.metrics.return_value = dict(recent_requests=[dict(identity=dict(session="warm"))])
        row = dict(operation_id="measured", supported=True, completed=True,
                   outcome="supported_success", duration_ms=1, started_s=1, finished_s=2)
        retained = [dict(identity=dict(session="measured"))]
        collector = MagicMock()
        collector.__enter__.return_value = collector
        collector.records = retained
        collector.errors = 0
        with (patch.object(perf, "Environment", return_value=env),
              patch.object(perf, "ResourceSampler", return_value=nullcontext(SimpleNamespace(samples=[]))),
              patch.object(perf, "closed_loop", return_value=([row], None)),
              patch.object(load, "StageCollector", return_value=collector),
              patch.object(extraction, "measured_stages", return_value={}) as stages):
            result = extraction.run_phase(None, None, Workload(), Path("unused"), manifest,
                                          manifest["images"][0]["template"], count=1,
                                          warmup_seconds=0, warmup_count=1)
        collector.wait_for.assert_called_once_with(1)
        self.assertEqual(stages.call_args.args[0], retained)
        self.assertEqual(result["records"], [row])


    def test_layout_hash_normalizes_only_declared_text(self):
        body={"input":[{"role":"user","content":[{"type":"input_text","text":"Alpha\nBeta"}]}],"metadata":{"label":"keep  spaces"}}
        layout=copy.deepcopy(body);layout["input"][0]["content"][0]["text"]="Alpha  Beta\n"
        self.assertEqual(perf.text_layout_hash(encode(body)),perf.text_layout_hash(encode(layout)))
        for field in ("words","structure","opaque"):
            changed=copy.deepcopy(body)
            if field=="words": changed["input"][0]["content"][0]["text"]="Alpha Gamma"
            if field=="structure": changed["input"][0]["content"].append({"type":"input_image","image_url":"unexpected"})
            if field=="opaque": changed["metadata"]["label"]="keep spaces"
            self.assertNotEqual(perf.text_layout_hash(encode(body)),perf.text_layout_hash(encode(changed)))

    def test_backend_boundary_layout_hash_is_enforced(self):
        body=encode({"input":[{"role":"user","content":[{"type":"input_text","text":"Alpha\nBeta"}]},{"role":"user","content":[{"type":"input_text","text":"Gamma\nDelta"}]}]})
        case=dict(operation_id="layout",body=body,occurrences=["Alpha","Gamma"],supported=True)
        with perf.FakeBackend(delay=0,interval=0) as backend:
            good=perf.request(backend.url,case,expected_layout_hash=perf.text_layout_hash(body))
            bad=perf.request(backend.url,case,expected_layout_hash="0"*64)
        self.assertEqual(good["outcome"],"supported_success")
        self.assertEqual(bad["outcome"],"transformation_error")

    def test_text_runs_do_not_pay_for_attachment_layout_hash(self):
        from unittest.mock import patch
        body=encode({"input":[{"role":"user","content":[{"type":"input_text","text":"Alpha"}]},{"role":"user","content":[{"type":"input_text","text":"Gamma"}]}]})
        case=dict(operation_id="plain",body=body,occurrences=["Alpha","Gamma"],supported=True)
        with perf.FakeBackend(delay=0,interval=0) as backend, patch.object(perf,"text_layout_hash",side_effect=AssertionError("unexpected_layout_hash")):
            result=perf.request(backend.url,case)
        self.assertEqual(result["outcome"],"supported_success")

    def test_stage_collection_excludes_warmup_and_rejects_missing_measurements(self):
        import extraction_performance as extraction
        rows=[dict(identity=dict(session="warm"),stages_ms=dict(extraction_run=900)),dict(identity=dict(session="measured"),stages_ms=dict(extraction_run=10,extraction_wait=2)),dict(identity=dict(session="measured"),stages_ms=dict(extraction_run=20,extraction_wait=4))]
        result=extraction.measured_stages(rows,{"warm"},2)
        self.assertEqual(result["extraction_run"],[10,20])
        self.assertEqual(result["extraction_wait"],[2,4])
        with self.assertRaisesRegex(ValueError,"stage_coverage_invalid"):
            extraction.measured_stages(rows,{"warm"},3)
        rows[-1]["stages_ms"].pop("extraction_run")
        with self.assertRaisesRegex(ValueError,"stage_coverage_invalid"):
            extraction.measured_stages(rows,{"warm"},2)

    @unittest.skipUnless(os.environ.get("CUI_EXTRACTION_PROXY_TEST") == "1", "explicit real proxy extraction test")
    def test_real_proxy_image_and_pdf_measurements_have_complete_stages(self):
        import extraction_fixtures as fixtures
        import extraction_performance as extraction
        from performance_workload import Workload
        with tempfile.TemporaryDirectory() as root:
            directory=Path(root)/"fixtures"
            manifest=fixtures.generate(directory,count=2)
            for template in [0,"pdf-scan"]:
                result=extraction.run_phase(os.environ["CUI_PROXY_BINARY"],os.environ["CUI_SEED_BINARY"],Workload(),directory,manifest,template,clients=1,count=2,warmup_seconds=0,warmup_count=1)
                self.assertEqual(result["outcomes"]["supported_success"],2)
                self.assertEqual(len(result["stage_samples"]["extraction_run"]),2)
                self.assertEqual(len(result["records"]),2)
                perf.write_report(Path(root)/"report.json",dict(schema_version=1,measurements=[result],performance_qualified=False))

    def test_collection_failure_retains_completed_measurement(self):
        from types import SimpleNamespace
        from contextlib import nullcontext
        from unittest.mock import MagicMock, patch
        import extraction_performance as extraction
        import extraction_fixtures as fixtures
        from performance_workload import Workload
        manifest = dict(version=fixtures.VERSION, seed=1102026, count=1, images=fixtures.recipes('warmup', 1) + fixtures.recipes('measurement', 1), pdfs=fixtures.pdf_recipes())
        env = MagicMock()
        env.__enter__.return_value = env
        env.metrics.side_effect = [dict(recent_requests=[dict(identity=dict(session='warm'))]), RuntimeError('collection_failure')]

        def completed(*args, **kwargs):
            return dict(operation_id='sample', supported=True, completed=True, outcome='supported_success', duration_ms=1, started_s=1, finished_s=2)
        collector = MagicMock()
        collector.__exit__.side_effect = RuntimeError('stage collection failed')
        with patch.object(perf, 'Environment', return_value=env), patch.object(perf, 'ResourceSampler', return_value=nullcontext(SimpleNamespace(samples=[]))), patch.object(extraction, 'attachment_case', return_value=({'body': b'{}'}, 'digest')), patch.object(perf, 'request', side_effect=completed), patch('performance_load.StageCollector', return_value=collector):
            with self.assertRaises(perf.PhaseFailure) as caught:
                extraction.run_phase('unused', 'unused', Workload(), Path('unused'), manifest, 0, count=1, warmup_seconds=0, warmup_count=1)
        self.assertEqual(len(caught.exception.records), 1)
        self.assertEqual(caught.exception.records[0]['outcome'], 'supported_success')

    def test_failed_phase_keeps_success_and_timeout_in_report_denominator(self):
        import extraction_performance as extraction
        report=dict(measurements=[])
        rows=[dict(operation_id="one",supported=True,completed=True,outcome="supported_success",duration_ms=1),dict(operation_id="two",supported=True,completed=False,outcome="unresolved",duration_ms=45000)]
        extraction.record_failure(report,perf.PhaseFailure("measurement_collection",rows))
        result=report["measurements"][0]
        self.assertEqual(result["outcomes"]["attempted"],2)
        self.assertEqual(result["outcomes"]["supported_success"],1)
        self.assertEqual(result["outcomes"]["failures"],1)
        self.assertTrue(result["collection_failed"])

    def test_cli_writes_complete_metadata_report(self):
        import json
        import sys
        from unittest.mock import patch
        import extraction_performance as extraction
        import extraction_fixtures as fixtures
        from performance_stats import summarize_outcomes
        tools=dict(tool_versions=dict(swift="6.4",tesseract="5.5.4",pdftotext="26.09.0",pdftoppm="26.09.0"),traineddata_sha256="0"*64)
        manifest=dict(version=fixtures.VERSION, seed=1102026, count=1, images=fixtures.recipes('warmup', 1) + fixtures.recipes('measurement', 1), pdfs=fixtures.pdf_recipes(), **tools, font_name='Menlo-Regular')
        row=dict(operation_id="one",supported=True,completed=True,outcome="supported_success",duration_ms=1)
        result=dict(records=[row],outcomes=summarize_outcomes([row]),stage_samples=dict(extraction_run=[1],extraction_wait=[0]),performance_qualified=False)
        with tempfile.TemporaryDirectory() as root:
            root=Path(root);(root/"manifest.json").write_text(json.dumps(manifest));report=root/"report.json"
            argv=["extraction_performance","--binary","unused","--seed-binary","unused","--fixtures",str(root),"--report",str(report),"--template","0","--count","1","--warmup-count","1","--warmup-seconds","0","--repetitions","1"]
            with patch.object(sys,"argv",argv),patch.object(perf,"environment_info",return_value={}),patch.object(fixtures,"provenance",return_value=tools),patch.object(extraction,"run_phase",return_value=result):
                self.assertEqual(extraction.main(),0)
            written=json.loads(report.read_text())
            self.assertEqual(written["settings"]["warmup_count"],1)
            self.assertEqual(written["measurements"][0]["outcomes"]["attempted"],1)
            self.assertFalse(written["performance_qualified"])

    def test_cleanup_failure_preserves_warmup_timeout_evidence(self):
        from unittest.mock import MagicMock,patch
        import extraction_performance as extraction
        import extraction_fixtures as fixtures
        from performance_workload import Workload
        manifest=dict(version=fixtures.VERSION,seed=1102026,count=1,images=fixtures.recipes("warmup",1)+fixtures.recipes("measurement",1),pdfs=fixtures.pdf_recipes())
        env=MagicMock();env.__enter__.return_value=env
        env.__exit__.side_effect=OSError("cleanup_failure")
        timeout=dict(operation_id="warm",supported=True,completed=False,outcome="unresolved",duration_ms=45000,started_s=1,finished_s=46)
        with patch.object(perf,"Environment",return_value=env),patch.object(extraction,"attachment_case",return_value=({"body":b"{}"},"digest")),patch.object(perf,"request",return_value=timeout):
            with self.assertRaises(perf.PhaseFailure) as caught:
                extraction.run_phase("unused","unused",Workload(),Path("unused"),manifest,0,count=1,warmup_seconds=0,warmup_count=1)
        self.assertEqual(caught.exception.phase,"warmup")
        self.assertEqual(len(caught.exception.records),1)
        self.assertEqual(caught.exception.records[0]["outcome"],"unresolved")

    def test_nonfinite_or_negative_warmup_is_rejected_before_work(self):
        import sys
        from unittest.mock import patch
        import extraction_performance as extraction
        for value in ["nan","inf","-1"]:
            argv=["extraction_performance","--binary","unused","--seed-binary","unused","--fixtures","unused","--report","unused","--template","0","--warmup-seconds",value]
            with self.subTest(value=value),patch.object(sys,"argv",argv),patch.object(perf,"environment_info",side_effect=AssertionError("must_not_start")):
                with self.assertRaises(SystemExit) as caught:
                    extraction.main()
                self.assertEqual(caught.exception.code,2)

    def test_report_reservation_preserves_existing_files_and_symlinks(self):
        import extraction_performance as extraction
        with tempfile.TemporaryDirectory() as root:
            root=Path(root);target=root/"target";target.write_bytes(b"previous evidence")
            link=root/"link";link.symlink_to(target)
            for path in [target,link]:
                with self.assertRaises(FileExistsError): extraction.reserve_report(path)
            self.assertEqual(target.read_bytes(),b"previous evidence")
            with self.assertRaises(FileNotFoundError): extraction.reserve_report(root/"missing"/"report")
            fresh=root/"fresh";extraction.reserve_report(fresh)
            self.assertEqual(fresh.stat().st_mode & 0o777,0o600)

    def test_report_write_failure_preserves_previous_evidence(self):
        from unittest.mock import patch
        import extraction_performance as extraction
        with tempfile.TemporaryDirectory() as root:
            root=Path(root);report=root/"report";report.write_bytes(b"previous evidence")
            with patch.object(perf,"write_report",side_effect=OSError("write_failure")):
                with self.assertRaises(OSError): extraction.write_report(report,{})
            self.assertEqual(report.read_bytes(),b"previous evidence")
            self.assertEqual(list(root.iterdir()),[report])
