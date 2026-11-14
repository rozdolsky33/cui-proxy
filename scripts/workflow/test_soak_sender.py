import io
import json
import unittest

from soak_semantics import build_tool_call
from soak_tool_stream import ToolStreamObserver
from soak_sender import send_tool_stream


class ToolSenderTests(unittest.TestCase):
    def test_paced_deltas_have_actual_send_and_flush_times(self):
        current = [10.0]
        sleeps = []
        class Sink(io.BytesIO):
            def write(self, data):
                current[0] += .001
                return super().write(data)
            def flush(self):
                current[0] += .002
        sink = Sink()
        def sleep(delay):
            sleeps.append(delay)
            current[0] += delay
        call = build_tool_call("W04", ["first", "second"], "call-1")
        send_tool_stream(sink, call, .02, clock=lambda: current[0], sleep=sleep)
        observer = ToolStreamObserver(call)
        observer.feed(sink.getvalue(), 1000)
        self.assertTrue(observer.valid())
        self.assertEqual(sleeps, [.02] * 32)
        self.assertEqual(len(observer.send_started_s), 32)
        self.assertEqual(len(observer.send_finished_s), 32)
        for start, end in zip(observer.send_started_s, observer.send_finished_s):
            self.assertAlmostEqual(end - start, .003)
        self.assertTrue(all(a < b for a, b in zip(observer.send_started_s, observer.send_started_s[1:])))

    def test_write_failure_does_not_emit_success_terminal(self):
        class Sink(io.BytesIO):
            def write(self, data):
                if b"arguments.delta" in data:
                    raise BrokenPipeError()
                return super().write(data)
        sink = Sink()
        call = build_tool_call("W02", ["first", "second"], "call-2")
        with self.assertRaises(BrokenPipeError):
            send_tool_stream(sink, call, .02, sleep=lambda _: None)
        self.assertNotIn(b"response.completed", sink.getvalue())

    def test_invalid_interval_is_rejected_before_writing(self):
        call = build_tool_call("W02", ["first", "second"], "call-3")
        for delay in (-1, float("nan"), float("inf"), True):
            with self.subTest(delay=delay):
                sink = io.BytesIO()
                with self.assertRaises(ValueError):
                    send_tool_stream(sink, call, delay)
                self.assertEqual(sink.getvalue(), b"")


if __name__ == "__main__":
    unittest.main()
