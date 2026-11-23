"""Paced fake-upstream tool events with measured write/flush timestamps."""

import json
import math
import time

from tool_prefix_plan import source_frames
from soak_tool_stream import tool_events


def send_tool_stream(sink, call, interval, *, prefix_plan=None, clock=time.monotonic, sleep=time.sleep):
    if isinstance(interval, bool) or not isinstance(interval, (int, float)) or not math.isfinite(interval) or interval < 0:
        raise ValueError("soak_stream_interval")
    starts = []
    finishes = []
    events = tool_events(call)
    if prefix_plan is not None:
        frames = source_frames(call["arguments"], prefix_plan["lengths"], events)
        if frames != prefix_plan["frames"] or len(frames) != len(prefix_plan["ids"]):
            raise ValueError("tool_prefix_frame_layout")
    for event in events:
        delta = event["type"] == "response.function_call_arguments.delta"
        if event["type"] == "response.completed":
            event = dict(event,
                benchmark_send_started_s=starts if prefix_plan is None else [starts[index] for index in frames],
                benchmark_send_finished_s=finishes if prefix_plan is None else [finishes[index] for index in frames])
            if prefix_plan is not None:
                event["benchmark_prefix_ids"] = prefix_plan["ids"]
        encoded = b"data: " + json.dumps(event, separators=(",", ":")).encode() + b"\n\n"
        if delta:
            starts.append(clock())
        sink.write(encoded)
        sink.flush()
        if delta:
            finishes.append(clock())
            sleep(interval)
