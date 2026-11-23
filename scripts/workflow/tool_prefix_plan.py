"""Associate tool timing with exact recipe prefixes, independent of mapping widths."""
import hashlib
import json
import re
import shlex

from soak_semantics import build_tool_call


_MARKERS = ("CUILogicalValueZeroMarker", "CUILogicalValueOneMarker")
_MARKER = re.compile("(" + "|".join(_MARKERS) + ")")


def _encodings(value):
    # The fixed tool recipes use JSON string escaping and shell quoting. Match
    # only an exact, unambiguous expansion of their independently built result.
    result = {value, shlex.quote(value)}
    for _ in range(3):
        result.update(json.dumps(item, ensure_ascii=ascii_only)[1:-1]
                      for item in tuple(result) for ascii_only in (True, False))
    return sorted(result, key=lambda item: (-len(item), item))


def tool_prefix_plan(scenario, values, call):
    """Return identical logical IDs and local offsets for an exact tool recipe.

    Literal characters each contribute one canonical unit. Each complete mapped
    value occurrence contributes one unit, including its JSON/shell encoding.
    Upstream frames remain the original 32 equal character slices.
    """
    if scenario not in ("W02", "W03", "W04"):
        raise ValueError("tool_prefix_recipe")
    if type(values) not in (list, tuple) or len(values) < 2 or any(
            type(value) is not str or not value for value in values[:2]):
        raise ValueError("tool_prefix_values")
    if call != build_tool_call(scenario, list(values), call.get("call_id")):
        raise ValueError("tool_prefix_recipe")
    template = build_tool_call(scenario, list(_MARKERS), call["call_id"])["arguments"]
    pieces = _MARKER.split(template)
    arguments = call["arguments"]
    choices = {marker: _encodings(values[index]) for index, marker in enumerate(_MARKERS)}
    solutions = set()

    def expand(index, position, spans):
        if len(solutions) > 1:
            return
        if index == len(pieces):
            if position == len(arguments):
                solutions.add(tuple(spans))
            return
        piece = pieces[index]
        if index % 2 == 0:
            if arguments.startswith(piece, position):
                expand(index + 1, position + len(piece), spans)
            return
        for encoded in choices[piece]:
            if arguments.startswith(encoded, position):
                expand(index + 1, position + len(encoded),
                       spans + [(position, position + len(encoded))])

    expand(0, 0, [])
    if len(solutions) != 1:
        raise ValueError("tool_prefix_expansion")
    spans = next(iter(solutions))
    units = []
    position = 0
    for start, end in spans:
        units.extend(range(position + 1, start + 1))
        units.append(end)
        position = end
    units.extend(range(position + 1, len(arguments) + 1))
    count = min(32, len(units))
    coordinates = [(len(units) * index + count - 1) // count
                   for index in range(1, count + 1)]
    lengths = [units[point - 1] for point in coordinates]
    recipe_id = hashlib.sha256((scenario + ":" + template).encode()).hexdigest()
    ends = [len(arguments) * index // 32 for index in range(1, 33)]
    return {"ids": [recipe_id + ":" + str(point) for point in coordinates],
            "lengths": lengths,
            "frames": [next(index for index, end in enumerate(ends) if end >= length)
                       for length in lengths]}
