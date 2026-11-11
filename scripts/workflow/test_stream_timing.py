import unittest
import performance as perf

class StreamTimingTests(unittest.TestCase):
    def test_observer_pairs_each_prefix_with_its_actual_semantic_chunk(self):
        import itertools
        import json
        values = ["alpha", "longer_beta"] * 20
        chunks = perf.stream_chunks(values)
        observer = perf.StreamObserver(perf.response_text(values),
                                       prefix_lengths=list(itertools.accumulate(map(len, chunks))))
        for tick, chunk in enumerate(chunks, 1):
            event = {"type": "response.output_text.delta", "delta": chunk}
            observer.feed(("data: " + json.dumps(event) + "\n\n").encode(), tick)
        self.assertEqual(observer.prefix_ms, list(range(1, 33)))


    def test_text_only_backend_skips_raw_image_parts(self):
        body = {"input": [
            {"type": "message", "content": [{"type": "input_text", "text": "shared prose"}]},
            {"type": "message", "content": [{"type": "input_image", "image_url": "data:image/png;base64,AA=="}]},
        ]}
        self.assertEqual(perf.values_from_body(body, allow_attachments=True),
                         perf.values_from_body({"input": body["input"][:1]}))


    def test_stream_prefixes_keep_semantic_alignment_when_substitution_lengths_change(self):
        originals=[f"Original{index:02d}End" for index in range(40)]
        substitutes=[f"Replacement{index:02d}End" for index in range(40)]
        originals[0]="ShortEnd";substitutes[0]="VeryLongReplacementIdentifierEnd"
        originals[1]="LongOriginalIdentifierEnd";substitutes[1]="TinyEnd"
        expected=perf.stream_chunks(originals);actual=perf.stream_chunks(substitutes)
        self.assertEqual(len(expected),32);self.assertEqual(len(actual),32)
        self.assertEqual("".join(expected),perf.response_text(originals))
        for index in range(32):
            if index in (0,2): continue
            prefix="".join(actual[:index+1])
            for old,new in zip(substitutes,originals):prefix=prefix.replace(old,new)
            self.assertEqual(prefix,"".join(expected[:index+1]),f"prefix {index}")

    def test_relay_measurement_uses_actual_upstream_write_and_flush_times(self):
        result=perf.relay_timings(10,[10050,11070],[20,21],[20.01,21.02])
        for actual,expected in zip(result["relay_ms"],[50,70]):self.assertAlmostEqual(actual,expected)
        for actual,expected in zip(result["relay_flush_ms"],[40,50]):self.assertAlmostEqual(actual,expected)
        self.assertEqual(result["upstream_send_ms"],[10000,11000])

    def test_missing_or_invalid_send_times_are_unavailable_not_zero(self):
        for starts,ends in [(None,None),([],[]),([20],[19]),([float("nan")],[21]),([20,21],[20.1,21.1])]:
            result=perf.relay_timings(10,[10050],starts,ends)
            self.assertIsNone(result["relay_ms"])

    def test_backend_reports_actual_send_bounds_for_each_logical_prefix(self):
        import math
        from performance_workload import encode
        body=encode(dict(input=[dict(role="user",content=[dict(type="input_text",text="AlphaEnd")]),dict(role="user",content=[dict(type="input_text",text="GammaEnd")])]))
        case=dict(operation_id="timing",body=body,occurrences=["AlphaEnd","GammaEnd"],supported=True)
        with perf.FakeBackend(delay=0,interval=0.001) as backend:
            row=perf.request(backend.url,case)
        self.assertEqual(row["outcome"],"supported_success")
        for key in ["upstream_send_ms","upstream_flush_ms","relay_ms","relay_flush_ms"]:
            self.assertIsInstance(row.get(key),list)
            self.assertEqual(len(row[key]),32)
            self.assertTrue(all(math.isfinite(value) for value in row[key]))
        self.assertTrue(all(end>=start for start,end in zip(row["upstream_send_ms"],row["upstream_flush_ms"])))

    def test_paired_relay_delays_remove_backend_scheduling_jitter(self):
        proxy=[dict(operation_id="one",outcome="supported_success",prefix_ms=[1110,1220],relay_ms=[10,20],relay_flush_ms=[5,15]),dict(operation_id="two",outcome="supported_success",prefix_ms=[100,200],relay_ms=[-1,2],relay_flush_ms=[-2,1])]
        bypass=[dict(operation_id="two",outcome="supported_success",prefix_ms=[100,200],relay_ms=[1,3],relay_flush_ms=[0,2]),dict(operation_id="one",outcome="supported_success",prefix_ms=[107,213],relay_ms=[7,13],relay_flush_ms=[2,8])]
        result=perf.compare_relay(proxy,bypass)
        self.assertTrue(result["relay_samples_valid"])
        self.assertEqual(result["added_relay_ms"],dict(count=4,p50=-1,p95=7,p99=7))
        self.assertEqual(result["added_relay_flush_ms"],dict(count=4,p50=-1,p95=7,p99=7))

    def test_incomplete_relay_samples_cannot_look_like_zero_delay(self):
        import copy
        row=dict(operation_id="one",outcome="supported_success",prefix_ms=[10,20],relay_ms=[1,2],relay_flush_ms=[0,1])
        for mutation in ["missing","short","nan","duplicate","failed"]:
            proxy=[copy.deepcopy(row)];bypass=[copy.deepcopy(row)]
            if mutation=="missing":proxy[0]["relay_ms"]=None
            if mutation=="short":proxy[0]["relay_flush_ms"]=[0]
            if mutation=="nan":proxy[0]["relay_ms"][0]=float("nan")
            if mutation=="duplicate":proxy.append(copy.deepcopy(row))
            if mutation=="failed":proxy[0]["outcome"]="unresolved"
            result=perf.compare_relay(proxy,bypass)
            self.assertFalse(result["relay_samples_valid"])
            self.assertIsNone(result["added_relay_ms"])

    def test_background_image_reply_can_use_only_the_shared_text_workload(self):
        from performance_workload import encode
        values=[f"Value{index:02d}End" for index in range(40)]
        body=encode(dict(input=[dict(role="user",content=[dict(type="input_text",text=value)]) for value in values+["Attachment reference"]]))
        case=dict(operation_id="background",body=body,occurrences=values,supported=True,benchmark_text_only=True)
        with perf.FakeBackend(delay=0,interval=0) as backend:
            result=perf.request(backend.url,case)
        self.assertEqual(result["outcome"],"supported_success")
