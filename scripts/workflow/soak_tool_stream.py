"""Decoded-event collection for controlled structured-argument streams.

Transport framing and request deadlines belong to the caller. No payload is
included in failure messages or numeric timing evidence.
"""

import math
import json

_FIELDS = ("type", "name", "call_id", "arguments")


def _call(item):
    if type(item) is not dict:
        raise ValueError("soak_stream_invalid")
    call = {key: item[key] for key in _FIELDS}
    if (call["type"] != "function_call"
            or any(type(call[key]) is not str or not call[key] for key in ("name", "call_id"))
            or type(call["arguments"]) is not str):
        raise ValueError("soak_stream_invalid")
    return call


def tool_events(call):
    """Build a fixed 32-delta script from an already constructed upstream call."""
    try:
        call = _call(call)
        if not call["arguments"]:
            raise ValueError
        item = dict(call, id="item-1")
        events = [{"type": "response.output_item.added", "output_index": 0,
                   "item": dict(item, arguments="")}]
        arguments = call["arguments"]
        start = 0
        for index in range(32):
            end = (len(arguments) * (index + 1) + 31) // 32
            events.append({"type": "response.function_call_arguments.delta", "output_index": 0,
                           "item_id": item["id"], "delta": arguments[start:end]})
            start = end
        events.append({"type": "response.function_call_arguments.done", "output_index": 0,
                       "item_id": item["id"], "arguments": arguments, "name": call["name"]})
        events.append({"type": "response.output_item.done", "output_index": 0, "item": dict(item)})
        events.append({"type": "response.completed", "response": {
            "status": "completed", "output": [dict(item)]}})
        return events
    except (KeyError, TypeError, ValueError):
        raise ValueError("soak_stream_invalid") from None


class ToolStreamObserver:
    """Preserve logical-prefix availability even when a proxy buffers deltas."""

    def __init__(self, expected_call):
        try:
            self.expected = _call(expected_call)
            if not self.expected["arguments"]:
                raise ValueError
        except (KeyError, TypeError, ValueError):
            raise ValueError("soak_stream_invalid") from None
        length = len(self.expected["arguments"])
        self.prefix_lengths = [(length * index + 31) // 32 for index in range(1, 33)]
        self.prefix_ms = []
        self.arguments = ""
        self.item_id = None
        self.call = None
        self.last_ms = -1
        self.terminal = False
        self.failed = False
        self.notifications = set()
        self.pending = b""
        self.send_started_s = None
        self.send_finished_s = None

    def _index(self, event):
        if type(event.get("output_index")) is not int or event["output_index"] != 0:
            raise ValueError

    def _item_reference(self, event):
        self._index(event)
        if self.item_id is None or event.get("item_id") != self.item_id:
            raise ValueError

    def _complete_item(self, item):
        if _call(item) != self.expected or item.get("id", self.item_id) != self.item_id:
            raise ValueError

    def observe(self, event, elapsed_ms):
        try:
            if (self.failed or self.terminal or type(event) is not dict
                    or type(elapsed_ms) not in (int, float) or not math.isfinite(elapsed_ms)
                    or elapsed_ms < 0 or elapsed_ms < self.last_ms):
                raise ValueError
            self.last_ms = elapsed_ms
            kind = event["type"]
            if kind in ("response.created", "response.in_progress"):
                return
            if kind == "response.output_item.added":
                self._index(event)
                item = event["item"]
                call = _call(item)
                if (self.item_id is not None or call != dict(self.expected, arguments="")
                        or type(item.get("id")) is not str or not item["id"]):
                    raise ValueError
                self.item_id = item["id"]
            elif kind == "response.function_call_arguments.delta":
                self._item_reference(event)
                if type(event.get("delta")) is not str or self.notifications:
                    raise ValueError
                self.arguments += event["delta"]
                if not self.expected["arguments"].startswith(self.arguments):
                    raise ValueError
                while (len(self.prefix_ms) < 32
                       and len(self.arguments) >= self.prefix_lengths[len(self.prefix_ms)]):
                    self.prefix_ms.append(elapsed_ms)
            elif kind == "response.function_call_arguments.done":
                self._item_reference(event)
                if (kind in self.notifications or event.get("arguments") != self.arguments
                        or self.arguments != self.expected["arguments"]
                        or event.get("name", self.expected["name"]) != self.expected["name"]):
                    raise ValueError
                self.notifications.add(kind)
            elif kind == "response.output_item.done":
                self._index(event)
                if self.item_id is None or kind in self.notifications:
                    raise ValueError
                self._complete_item(event["item"])
                if self.arguments != self.expected["arguments"]:
                    raise ValueError
                self.notifications.add(kind)
            elif kind == "response.completed":
                response = event["response"]
                if (self.item_id is None or type(response) is not dict
                        or response.get("status") != "completed"
                        or type(response.get("output")) is not list or len(response["output"]) != 1
                        or self.arguments != self.expected["arguments"]):
                    raise ValueError
                self._complete_item(response["output"][0])
                self.send_started_s = event.get("benchmark_send_started_s")
                self.send_finished_s = event.get("benchmark_send_finished_s")
                self.call = _call(response["output"][0])
                self.terminal = True
            else:
                raise ValueError
        except (KeyError, TypeError, ValueError, OverflowError):
            self.failed = True
            raise ValueError("soak_stream_invalid") from None

    def feed(self, data, elapsed_ms):
        """Accept fragmented SSE bytes, including split UTF-8 and CRLF framing."""
        try:
            if type(data) is not bytes or self.failed:
                raise ValueError
            self.pending += data
            # Normalize the full pending buffer so split CRLF boundaries survive.
            self.pending = self.pending.replace(b"\r\n", b"\n")
            while b"\n\n" in self.pending:
                frame, self.pending = self.pending.split(b"\n\n", 1)
                if len(frame) > 1 << 20:
                    raise ValueError
                payload, event_name = [], None
                for line in frame.splitlines():
                    if line.startswith(b"data:"):
                        payload.append(line[5:].lstrip(b" "))
                    elif line.startswith(b"event:"):
                        event_name = line[6:].strip().decode("utf-8")
                if not payload:
                    continue
                raw = b"\n".join(payload)
                if raw.strip() == b"[DONE]":
                    if not self.terminal:
                        raise ValueError
                    continue
                event = json.loads(raw, object_pairs_hook=_unique, parse_constant=_constant)
                if (type(event) is not dict
                        or event_name is not None and event.get("type") != event_name):
                    raise ValueError
                self.observe(event, elapsed_ms)
            if len(self.pending) > 1 << 20:
                raise ValueError
        except (ValueError, TypeError, UnicodeError, RecursionError):
            self.failed = True
            raise ValueError("soak_stream_invalid") from None

    def valid(self):
        return (not self.failed and not self.pending.strip() and self.terminal and self.call is not None
                and self.arguments == self.expected["arguments"] and len(self.prefix_ms) == 32)


def _unique(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("soak_stream_invalid")
        result[key] = value
    return result


def _constant(_value):
    raise ValueError("soak_stream_invalid")
