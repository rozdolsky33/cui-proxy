import unittest
from unittest.mock import patch
import io
import json

from soak_sender import send_tool_stream
from soak_tool_stream import ToolStreamObserver, tool_events
from performance import relay_timings

from soak_semantics import build_tool_call
import tool_prefix_plan as timing


class ToolPrefixPlanTests(unittest.TestCase):
    def plan(self, scenario, values):
        call = build_tool_call(scenario, values, "call")
        return timing.tool_prefix_plan(scenario, values, call), call

    def test_same_logical_ids_survive_mapping_width_and_quoting_changes(self):
        for scenario in ("W02", "W03", "W04"):
            for originals in (("alpha", "beta"), ("space value", "single'quote"),
                              ("unicode \u03bb\nline", 'double"slash\\'),
                              ("repeat", "repeat")):
                with self.subTest(scenario=scenario, originals=originals):
                    original, left = self.plan(scenario, originals)
                    masked, right = self.plan(scenario, ("MASKED_LONG_VALUE_000000", "M2"))
                    self.assertEqual(original["ids"], masked["ids"])
                    self.assertEqual(len(original["ids"]), len(set(original["lengths"])))
                    self.assertEqual(original["lengths"][-1], len(left["arguments"]))
                    self.assertEqual(masked["lengths"][-1], len(right["arguments"]))
                    self.assertTrue(any(a != b for a, b in zip(original["lengths"], masked["lengths"])))

    def test_exact_recipe_is_required(self):
        plan, call = self.plan("W04", ("alpha", "beta"))
        changed = dict(call, arguments=call["arguments"] + " ")
        with self.assertRaises(ValueError):
            timing.tool_prefix_plan("W04", ("alpha", "beta"), changed)
        with self.assertRaises(ValueError):
            timing.tool_prefix_plan("unknown", ("alpha", "beta"), call)

    def test_every_milestone_maps_to_first_source_frame_that_contains_it(self):
        plan, call = self.plan("W04", ("x" * 500, "y" * 100))
        ends = []
        cumulative = 0
        for event in tool_events(call):
            if event["type"] == "response.function_call_arguments.delta":
                cumulative += len(event["delta"])
                ends.append(cumulative)
        for prefix, frame in zip(plan["lengths"], plan["frames"]):
            self.assertGreaterEqual(ends[frame], prefix)
            self.assertTrue(frame == 0 or ends[frame - 1] < prefix)

    def test_mapped_value_is_atomic_but_ordinary_literals_are_measured(self):
        plan, call = self.plan("W04", ("x" * 500, "y" * 500))
        args = call["arguments"]
        for value in ("x" * 500, "y" * 500):
            start = args.index(value)
            end = start + len(value)
            self.assertFalse(any(start < point < end for point in plan["lengths"]))
        self.assertGreaterEqual(len(plan["lengths"]), 10)


    def test_source_frames_follow_emitted_layout_for_widths_unicode_and_escapes(self):
        exact_boundaries = 0
        for scenario in ("W02", "W03", "W04"):
            for width in range(1, 66):
                for suffix in ("", "\u03bb\n\\\"'"):
                    plan, call = self.plan(scenario, ("x" * width + suffix, "y" * (width + 3)))
                    ends = []
                    cumulative = 0
                    for event in tool_events(call):
                        if event["type"] == "response.function_call_arguments.delta":
                            cumulative += len(event["delta"])
                            ends.append(cumulative)
                    for length, frame in zip(plan["lengths"], plan["frames"]):
                        self.assertGreaterEqual(ends[frame], length)
                        self.assertTrue(frame == 0 or ends[frame - 1] < length)
                        exact_boundaries += ends[frame] == length
        self.assertGreater(exact_boundaries, 0)

    def test_inconsistent_or_incomplete_frame_layout_is_rejected(self):
        plan, call = self.plan("W03", ("alpha", "beta"))
        events = tool_events(call)
        delta_index = next(i for i, event in enumerate(events)
                           if event["type"] == "response.function_call_arguments.delta")
        variants = [events[:delta_index] + events[delta_index + 1:]]
        for replacement in (None, "", "incorrect"):
            changed = [dict(event) for event in events]
            changed[delta_index]["delta"] = replacement
            variants.append(changed)
        for changed in variants:
            with self.assertRaises(ValueError):
                timing.source_frames(call["arguments"], plan["lengths"], changed)
        for lengths in ([], [0], [len(call["arguments"]) + 1], [2, 1], [False]):
            with self.assertRaises(ValueError):
                timing.source_frames(call["arguments"], lengths, events)


    def test_sender_rejects_inconsistent_layout_before_writing(self):
        plan, call = self.plan("W03", ("alpha", "beta"))
        events = tool_events(call)
        delta_index = next(i for i, event in enumerate(events)
                           if event["type"] == "response.function_call_arguments.delta")
        truncated = events[:delta_index] + events[delta_index + 1:]
        sink = io.BytesIO()
        with patch("soak_sender.tool_events", return_value=truncated):
            with self.assertRaises(ValueError):
                send_tool_stream(sink, call, 0., prefix_plan=plan)
        self.assertEqual(sink.getvalue(), b"")
        inconsistent_plan = dict(plan, frames=[0] * len(plan["frames"]))
        with self.assertRaises(ValueError):
            send_tool_stream(sink, call, 0., prefix_plan=inconsistent_plan)
        self.assertEqual(sink.getvalue(), b"")


class ToolPrefixAssociationTests(unittest.TestCase):
    def stream(self, buffered=False):
        values = ("x" * 500, "y" * 120)
        masked_values = ("MAPPED_VALUE_0", "MAPPED_VALUE_1")
        original = build_tool_call("W04", values, "call")
        masked = build_tool_call("W04", masked_values, "call")
        left = timing.tool_prefix_plan("W04", values, original)
        right = timing.tool_prefix_plan("W04", masked_values, masked)
        sink = io.BytesIO()
        now = [10.0]
        recorded_clock = []
        def clock():
            now[0] += .001
            recorded_clock.append(now[0])
            return now[0]

        send_tool_stream(sink, masked, .025, prefix_plan=right,
                         clock=clock, sleep=lambda delay: now.__setitem__(0, now[0] + delay))
        finished_ms = (now[0] - 10.) * 1000.
        upstream = [json.loads(frame.split(b"data: ", 1)[1])
                    for frame in sink.getvalue().split(b"\n\n") if frame]
        terminal = next(e for e in upstream if e["type"] == "response.completed")
        cumulative = 0
        emitted_ends = []
        for event in upstream:
            if event["type"] == "response.function_call_arguments.delta":
                cumulative += len(event["delta"])
                emitted_ends.append(cumulative)
        self.assertEqual(len(emitted_ends), 32)
        for index, (length, frame) in enumerate(zip(right["lengths"], right["frames"])):
            self.assertGreaterEqual(emitted_ends[frame], length)
            self.assertTrue(frame == 0 or emitted_ends[frame - 1] < length)
            self.assertEqual(terminal["benchmark_send_started_s"][index], recorded_clock[2 * frame])
            self.assertEqual(terminal["benchmark_send_finished_s"][index], recorded_clock[2 * frame + 1])
        observer = ToolStreamObserver(original, prefix_plan=left)
        skeleton = tool_events(original)
        observer.observe(skeleton[0], 0.)
        position = 0
        for frame in range(32):
            ends = [length for length, source_frame in zip(left["lengths"], right["frames"])
                    if source_frame <= frame]
            end = max(ends, default=position)
            if end == position:
                continue
            event = dict(next(e for e in skeleton if e["type"] == "response.function_call_arguments.delta"),
                         delta=original["arguments"][position:end])
            source_ms = (terminal["benchmark_send_started_s"][right["frames"].index(frame)] - 10.) * 1000.
            observer.observe(event, finished_ms + 150. if buffered else source_ms + 5.)
            position = end
        for event in skeleton[1:]:
            if event["type"] == "response.function_call_arguments.delta":
                continue
            if event["type"] == "response.completed":
                event = dict(event, **{key: value for key, value in terminal.items()
                                      if key.startswith("benchmark_")})
            observer.observe(event, finished_ms + 155.)
        return observer, terminal

    def test_atomic_mapping_hold_uses_actual_required_source_frame(self):
        observer, terminal = self.stream()
        self.assertTrue(observer.valid())
        delays = relay_timings(10., observer.prefix_ms, observer.send_started_s, observer.send_finished_s)
        self.assertTrue(any(finish > start for start, finish in zip(
            observer.send_started_s, observer.send_finished_s)))
        self.assertLess(len(set(zip(observer.send_started_s, observer.send_finished_s))), len(observer.prefix_ms))
        row = dict(prefix_ms=observer.prefix_ms, **delays)
        for key in ("upstream_send_ms", "upstream_flush_ms", "relay_ms", "relay_flush_ms"):
            self.assertIsInstance(row[key], list)
            self.assertEqual(len(row[key]), len(row["prefix_ms"]))
        self.assertLess(max(delays["relay_ms"]), 10.)
        self.assertEqual(len(observer.prefix_ms), len(terminal["benchmark_prefix_ids"]))

    def test_buffering_ordinary_literals_still_exceeds_unchanged_gate(self):
        observer, _ = self.stream(buffered=True)
        self.assertTrue(observer.valid())
        delays = relay_timings(10., observer.prefix_ms, observer.send_started_s, observer.send_finished_s)
        self.assertGreater(max(delays["relay_ms"]), 100.)

    def test_qualified_observer_rejects_missing_wrong_and_malformed_ids(self):
        for change in (None, [], ["unknown"], [False]):
            observer, terminal = self.stream()
            call = build_tool_call("W04", ("x" * 500, "y" * 120), "call")
            plan = timing.tool_prefix_plan("W04", ("x" * 500, "y" * 120), call)
            candidate = ToolStreamObserver(call, prefix_plan=plan)
            for event in tool_events(call):
                if event["type"] == "response.completed":
                    event = dict(event, **{key: value for key, value in terminal.items()
                                          if key.startswith("benchmark_")})
                    event.pop("benchmark_prefix_ids", None)
                    if change is not None:
                        event["benchmark_prefix_ids"] = change
                candidate.observe(event, 900.)
            self.assertFalse(candidate.valid())


    def test_qualified_observer_rejects_malformed_time_arrays(self):
        _, terminal = self.stream()
        for field in ("benchmark_send_started_s", "benchmark_send_finished_s"):
            for malformed in (None, [], [False] * 32, [float("nan")] * 32, [-.5] * 32,
                              [1.] * 31 + [0.]):
                call = build_tool_call("W04", ("x" * 500, "y" * 120), "call")
                plan = timing.tool_prefix_plan("W04", ("x" * 500, "y" * 120), call)
                observer = ToolStreamObserver(call, prefix_plan=plan)
                for event in tool_events(call):
                    if event["type"] == "response.completed":
                        event = dict(event, **{key: value for key, value in terminal.items()
                                              if key.startswith("benchmark_")})
                        event[field] = malformed
                    observer.observe(event, 900.)
                self.assertFalse(observer.valid())


    def test_distinct_overlapping_intervals_cannot_produce_report_samples(self):
        for starts, finishes in (([10.01, 10.0105], [10.011, 10.012]),
                                 ([10.01, 10.01], [10.011, 10.012]),
                                 ([10.01, 10.0105], [10.011, 10.011]),
                                 ([10.01, 10.009], [10.011, 10.010])):
            row = relay_timings(10., [30., 30.], starts, finishes)
            self.assertTrue(all(value is None for value in row.values()))


if __name__ == "__main__":
    unittest.main()
