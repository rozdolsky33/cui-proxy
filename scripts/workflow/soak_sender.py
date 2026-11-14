"""Paced fake-upstream tool events with measured write/flush timestamps."""

import json
import math
import time

from soak_tool_stream import tool_events


def send_tool_stream(sink, call, interval, *, clock=time.monotonic, sleep=time.sleep):
    if isinstance(interval, bool) or not isinstance(interval, (int, float)) or not math.isfinite(interval) or interval < 0:
        raise ValueError("soak_stream_interval")
    starts = []
    finishes = []
    for event in tool_events(call):
        delta = event["type"] == "response.function_call_arguments.delta"
        if event["type"] == "response.completed":
            event = dict(event, benchmark_send_started_s=starts,
                         benchmark_send_finished_s=finishes)
        encoded = b"data: " + json.dumps(event, separators=(",", ":")).encode() + b"\n\n"
        if delta:
            starts.append(clock())
        sink.write(encoded)
        sink.flush()
        if delta:
            finishes.append(clock())
            sleep(interval)
