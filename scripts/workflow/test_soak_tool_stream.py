import math
import json
import unittest

import soak_tool_stream


class ToolStreamTests(unittest.TestCase):
    def setUp(self):
        self.call = {"type": "function_call", "name": "apply_edit", "call_id": "case-1",
                     "arguments": '{"path":"effect.txt","old":"first","new":"second"}'}

    def added(self):
        return {"type": "response.output_item.added", "output_index": 0,
                "item": dict(self.call, id="item-1", arguments="")}

    def delta(self, value, item_id="item-1"):
        return {"type": "response.function_call_arguments.delta", "output_index": 0,
                "item_id": item_id, "delta": value}

    def terminal(self, call=None):
        return {"type": "response.completed", "response": {"status": "completed",
                "output": [dict(self.call if call is None else call, id="item-1")]}}

    def test_incremental_arguments_record_each_logical_prefix(self):
        observer = soak_tool_stream.ToolStreamObserver(self.call)
        observer.observe(self.added(), 0)
        for index, char in enumerate(self.call["arguments"]):
            observer.observe(self.delta(char), index + 1)
        observer.observe(self.terminal(), 100)
        self.assertTrue(observer.valid())
        self.assertEqual(observer.call, self.call)
        self.assertEqual(len(observer.prefix_ms), 32)
        self.assertEqual(observer.prefix_ms, sorted(observer.prefix_ms))
        self.assertLess(observer.prefix_ms[0], observer.prefix_ms[-1])

    def test_buffered_output_retains_its_real_availability_delay(self):
        observer = soak_tool_stream.ToolStreamObserver(self.call)
        observer.observe(self.added(), 0)
        observer.observe(self.delta(self.call["arguments"]), 700)
        observer.observe(self.terminal(), 701)
        self.assertTrue(observer.valid())
        self.assertEqual(observer.prefix_ms, [700] * 32)

    def test_wrong_association_or_restoration_is_not_completion(self):
        for delta in [self.delta(self.call["arguments"], "other-item"), self.delta("wrong")]:
            observer = soak_tool_stream.ToolStreamObserver(self.call)
            observer.observe(self.added(), 0)
            with self.assertRaisesRegex(ValueError, "^soak_stream_invalid$"):
                observer.observe(delta, 1)
            self.assertFalse(observer.valid())

    def test_terminal_cannot_replace_missing_or_wrong_deltas(self):
        observer = soak_tool_stream.ToolStreamObserver(self.call)
        observer.observe(self.added(), 0)
        with self.assertRaisesRegex(ValueError, "^soak_stream_invalid$"):
            observer.observe(self.terminal(), 1)
        self.assertFalse(observer.valid())
        observer = soak_tool_stream.ToolStreamObserver(self.call)
        observer.observe(self.added(), 0)
        observer.observe(self.delta(self.call["arguments"]), 1)
        with self.assertRaisesRegex(ValueError, "^soak_stream_invalid$"):
            observer.observe(self.terminal(dict(self.call, call_id="other-call")), 2)
        self.assertFalse(observer.valid())

    def test_bad_timing_and_duplicate_terminal_are_rejected(self):
        for when in [math.nan, math.inf, -1, True]:
            observer = soak_tool_stream.ToolStreamObserver(self.call)
            with self.assertRaisesRegex(ValueError, "^soak_stream_invalid$"):
                observer.observe(self.added(), when)
        observer = soak_tool_stream.ToolStreamObserver(self.call)
        observer.observe(self.added(), 10)
        with self.assertRaisesRegex(ValueError, "^soak_stream_invalid$"):
            observer.observe(self.delta("{"), 9)
        observer = soak_tool_stream.ToolStreamObserver(self.call)
        observer.observe(self.added(), 0)
        observer.observe(self.delta(self.call["arguments"]), 1)
        observer.observe(self.terminal(), 2)
        with self.assertRaisesRegex(ValueError, "^soak_stream_invalid$"):
            observer.observe(self.terminal(), 3)
        self.assertFalse(observer.valid())

    def test_server_script_has_fixed_deltas_and_independent_items(self):
        events = soak_tool_stream.tool_events(self.call)
        deltas = [event for event in events if event["type"] == "response.function_call_arguments.delta"]
        self.assertEqual(len(deltas), 32)
        self.assertEqual("".join(event["delta"] for event in deltas), self.call["arguments"])
        observer = soak_tool_stream.ToolStreamObserver(self.call)
        for index, event in enumerate(events):
            observer.observe(event, index * 20)
        self.assertTrue(observer.valid())
        events[-2]["item"]["arguments"] = "changed"
        self.assertEqual(events[-1]["response"]["output"][0]["arguments"], self.call["arguments"])

    def test_fragmented_utf8_and_crlf_frames(self):
        call = dict(self.call, arguments=json.dumps({"text": "héllo"}, ensure_ascii=False))
        observer = soak_tool_stream.ToolStreamObserver(call)
        wire = b"".join(b"event: " + event["type"].encode() + b"\r\ndata: " +
                        json.dumps(event, ensure_ascii=False).encode() + b"\r\n\r\n"
                        for event in soak_tool_stream.tool_events(call))
        for index, byte in enumerate(wire):
            observer.feed(bytes([byte]), index)
        self.assertTrue(observer.valid())
        observer.feed(b"data: {", len(wire) + 1)
        self.assertFalse(observer.valid())

    def test_malformed_wire_metadata_is_rejected(self):
        for wire in [b'data: {"type":"response.created","type":"response.in_progress"}\n\n',
                     b'data: {"type":"response.created","bad":NaN}\n\n',
                     b'event: response.created\ndata: {"type":"response.in_progress"}\n\n',
                     b'data: [DONE]\n\n']:
            observer = soak_tool_stream.ToolStreamObserver(self.call)
            with self.assertRaisesRegex(ValueError, "^soak_stream_invalid$"):
                observer.feed(wire, 1)
            self.assertFalse(observer.valid())

    def test_unterminated_frames_have_a_bound(self):
        observer = soak_tool_stream.ToolStreamObserver(self.call)
        with self.assertRaisesRegex(ValueError, "^soak_stream_invalid$"):
            observer.feed(b"data: " + b"x" * (1 << 20), 1)
        self.assertFalse(observer.valid())
