import copy
import hashlib
import http.client
import io
import json
import time
import unittest
from unittest import mock

import performance
import soak_tool_stream


class MemorySocket:
    def __init__(self, wire):
        self.wire = wire

    def makefile(self, *args, **kwargs):
        return io.BytesIO(self.wire)

    def settimeout(self, timeout):
        pass

    def close(self):
        pass


class MemoryConnection:
    events = []
    instances = []
    response_class = http.client.HTTPResponse

    def __init__(self, *args, **kwargs):
        self.closed = False
        self.sock = MemorySocket(b"")
        self.instances.append(self)

    def connect(self):
        pass

    def request(self, method, url, body=None, headers=None):
        self.method, self.body = method, body

    def getresponse(self):
        events = copy.deepcopy(self.events)
        stamp = time.monotonic()
        for event in events:
            if event.get("type") == "response.completed":
                event["benchmark_send_started_s"] = [stamp] * 32
                event["benchmark_send_finished_s"] = [stamp] * 32
        payload = b"".join(b"data: " + json.dumps(event).encode() + b"\n\n" for event in events)
        headers = ("HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\n"
                   "Content-Length: " + str(len(payload)) + "\r\n"
                   "X-Benchmark-Body-SHA256: " + hashlib.sha256(self.body).hexdigest() + "\r\n\r\n")
        self.sock = MemorySocket(headers.encode() + payload)
        response = self.response_class(self.sock, method=self.method)
        response.begin()
        return response

    def close(self):
        self.closed = True


class SoakRequestTests(unittest.TestCase):
    def setUp(self):
        self.case = {"body": b"{}", "operation_id": "case-1", "occurrences": ["first", "second"]}
        self.expected = {"type": "function_call", "name": "apply_edit", "call_id": "case-1",
                         "arguments": '{"path":"effect.txt","old":"first","new":"second"}'}
        MemoryConnection.instances = []

    def request(self, events, **kwargs):
        MemoryConnection.events = events
        with mock.patch.object(performance.http.client, "HTTPConnection", MemoryConnection):
            result = performance.request("http://127.0.0.1:1/v1/responses", self.case,
                                         hashlib.sha256(self.case["body"]).hexdigest(), **kwargs)
        self.assertTrue(all(connection.closed for connection in MemoryConnection.instances))
        return result

    def test_structured_observer_completes_through_the_real_http_reader(self):
        observers = []
        def factory():
            observer = soak_tool_stream.ToolStreamObserver(self.expected)
            observers.append(observer)
            return observer
        result = self.request(soak_tool_stream.tool_events(self.expected), observer_factory=factory)
        self.assertEqual(result["outcome"], "supported_success")
        self.assertEqual(len(result["prefix_ms"]), 32)
        self.assertIsNotNone(result["first_byte_ms"])
        self.assertEqual(observers[0].call, self.expected)

    def test_missing_terminal_is_never_supported_success(self):
        result = self.request(soak_tool_stream.tool_events(self.expected)[:-1],
                              observer_factory=lambda: soak_tool_stream.ToolStreamObserver(self.expected))
        self.assertNotEqual(result["outcome"], "supported_success")

    def test_default_text_observer_keeps_existing_behavior(self):
        events = [{"type": "response.output_text.delta", "delta": chunk}
                  for chunk in performance.stream_chunks(self.case["occurrences"])]
        events.append({"type": "response.completed"})
        result = self.request(events)
        self.assertEqual(result["outcome"], "supported_success")
        self.assertEqual(len(result["prefix_ms"]), 32)
