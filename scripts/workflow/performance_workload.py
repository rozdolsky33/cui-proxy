"""Private deterministic fixtures for workload contract v1; never print bodies."""
import json
import random

SEED = 1102026
TIERS = {
    "small": (50, 1000, 10000, (64 * 1024,)),
    "normal": (50, 1000, 10000, (256 * 1024, 768 * 1024, 1024 * 1024)),
    "large": (50, 1000, 10000, (2 * 1024 * 1024, 8 * 1024 * 1024)),
    "rules": (200, 10000, 10000, (1024 * 1024,)),
    "vault": (50, 1000, 100000, (1024 * 1024,)),
}
PROSE = ("The workshop keeps a tidy notebook of ordinary observations. "
         "Each morning the team reviews the garden paths and updates the drawing. "
         "A careful reader can follow the notes from the first page to the last. ")


def encode(body):
    return json.dumps(body, ensure_ascii=True, separators=(",", ":")).encode()


class Workload:
    def __init__(self, tier="normal", seed=SEED):
        if tier not in TIERS or type(seed) is not int:
            raise ValueError("invalid workload parameters")
        self.tier, self.seed = tier, seed
        self.regex_count, dictionary_count, mappings, self.sizes = TIERS[tier]
        self.dictionary = [f"NotebookTerm{i:06d}End" for i in range(dictionary_count)]
        self.rules = [{"name": "builtin", "type": "builtin"}]
        for i in range(self.regex_count):
            group = i % 50
            if group < 20:
                pattern = rf"field{i:03d}=Memo{i:03d}X[0-9]{{6}}End"
            elif group < 40:
                pattern = rf"\bMemo{i:03d}X[0-9]{{6}}End\b"
            else:
                pattern = rf"\bQuote{i:03d}X[0-9]{{6}}End\b"
            self.rules.append(dict(name=f"benchmark-{i:03d}", type="regex", pattern=pattern, synth="secret"))
        self.rules.append(dict(name="dictionary", type="dictionary", file="dictionary.txt", synth="secret"))
        # Reserve room for regex values even when the dictionary stress population
        # equals the number of live mappings. Not every dictionary entry is timed.
        used_dictionary = min(dictionary_count, mappings // 10)
        self.pairs = [["secret", v] for v in self.dictionary[:used_dictionary]]
        for n in range(mappings - used_dictionary):
            i = n % self.regex_count
            sequence = n // self.regex_count
            if i % 50 < 20:
                value = f"field{i:03d}=Memo{i:03d}X{sequence:06d}End"
            elif i % 50 < 40:
                value = f"Memo{i:03d}X{sequence:06d}End"
            else:
                value = f"Quote{i:03d}X{sequence:06d}End"
            self.pairs.append(["secret", value])

    def metadata(self):
        return dict(generator_version=1, seed=self.seed, tier=self.tier,
                    regex_declarations=self.regex_count, dictionary_entries=len(self.dictionary),
                    mapping_pairs=len(self.pairs), sizes_bytes=list(self.sizes),
                    occurrences_per_request=40, near_misses_per_request=40)

    def case(self, serial, size):
        if type(serial) is not int or serial < 0 or size not in self.sizes:
            raise ValueError("invalid case parameters")
        rng = random.Random(self.seed + serial)
        values = [self.pairs[i][1] for i in rng.sample(range(len(self.pairs)), 20)] * 2
        rng.shuffle(values)
        misses = [f"{value[:-3]}Almost{i:02d}" for i, value in enumerate(values)]
        items = []
        for i, value in enumerate(values):
            text = value + "\n" + misses[i] + "\n"
            if i % 3 == 2:
                items.append(dict(type="function_call_output", call_id=f"call-{i}", output=text))
            else:
                items.append(dict(role="user", content=[dict(type="input_text", text=text)]))
        body = dict(model="benchmark", stream=True, input=items)
        remaining = size - len(encode(body))
        if remaining < 0:
            raise ValueError("case exceeds target size")
        # Distribute ordinary prose across message and tool-result leaves. The
        # fixed corpus is kept outside padding; this must not add any matches.
        share, extra = divmod(remaining, len(items))
        for i, item in enumerate(items):
            length = share + (i < extra)
            padding = (PROSE * (length // len(PROSE) + 1))[:length]
            if "output" in item: item["output"] += padding
            else: item["content"][0]["text"] += padding
        wire = encode(body)
        if len(wire) != size:
            raise ValueError("payload size mismatch")
        return dict(operation_id=f"case-{serial}-size-{size}", body=wire,
                    occurrences=values, near_misses=misses, supported=True)
