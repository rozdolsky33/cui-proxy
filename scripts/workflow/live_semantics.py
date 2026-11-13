"""Validate paired live outcomes offline and emit value-free aggregate counts.

This module never calls a provider. Input records are manually classified using
independent semantic oracles; differences do not establish causal model effects.
"""
import json
import re
import sys
from pathlib import Path

CASES = frozenset({"argv", "file_edit", "config_edit", "relationship", "tokenization"})
FIELDS = frozenset({"pair_id", "case", "model", "arm", "status", "semantic_pass"})
IDENTIFIER = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.:/-]{0,127}\Z")
MAX_BYTES = 2 * 1024 * 1024
MAX_RECORDS = 10000


def require(condition):
    if not condition:
        raise ValueError("invalid paired outcome document")


def summarize(document):
    require(isinstance(document, dict) and set(document) == {"schema_version", "records"})
    require(type(document["schema_version"]) is int and document["schema_version"] == 1)
    records = document["records"]
    require(isinstance(records, list) and len(records) <= MAX_RECORDS)
    pairs = {}
    for record in records:
        require(isinstance(record, dict) and set(record) == FIELDS)
        for key in ("pair_id", "model"):
            require(isinstance(record[key], str) and IDENTIFIER.fullmatch(record[key]) is not None)
        require(isinstance(record["case"], str) and record["case"] in CASES)
        require(isinstance(record["arm"], str) and record["arm"] in {"direct", "proxy"})
        require(isinstance(record["status"], str) and record["status"] in {"ok", "proxy_failure", "provider_failure"})
        require(record["status"] != "proxy_failure" or record["arm"] == "proxy")
        if record["status"] == "ok":
            require(type(record["semantic_pass"]) is bool)
        else:
            require(record["semantic_pass"] is None)
        pair = pairs.setdefault(record["pair_id"], {})
        require(record["arm"] not in pair)
        pair[record["arm"]] = record
    result = dict(schema_version=1, measured=bool(pairs), pairs=len(pairs),
                  comparable_pairs=0, both_pass=0, both_fail=0,
                  direct_only_pass=0, proxy_only_pass=0,
                  proxy_failures=0, provider_failures=0, pairs_with_failures=0,
                  cases_measured=0)
    cases = set()
    for pair in pairs.values():
        require(set(pair) == {"direct", "proxy"})
        direct, proxy = pair["direct"], pair["proxy"]
        require(direct["model"] == proxy["model"] and direct["case"] == proxy["case"])
        cases.add(direct["case"])
        failures = [record for record in pair.values() if record["status"] != "ok"]
        result["proxy_failures"] += sum(record["status"] == "proxy_failure" for record in failures)
        result["provider_failures"] += sum(record["status"] == "provider_failure" for record in failures)
        if failures:
            result["pairs_with_failures"] += 1
            continue
        result["comparable_pairs"] += 1
        outcomes = direct["semantic_pass"], proxy["semantic_pass"]
        category = {(True, True): "both_pass", (False, False): "both_fail",
                    (True, False): "direct_only_pass", (False, True): "proxy_only_pass"}[outcomes]
        result[category] += 1
    result["cases_measured"] = len(cases)
    result["cases_unmeasured"] = sorted(CASES - cases)
    return result


def unique_object(pairs):
    value = {}
    for key, item in pairs:
        require(key not in value)
        value[key] = item
    return value


def main(argv=None):
    argv = sys.argv[1:] if argv is None else argv
    if len(argv) != 1:
        print("usage: live_semantics.py OUTCOMES.json", file=sys.stderr)
        return 2
    try:
        with Path(argv[0]).open("rb") as source:
            raw = source.read(MAX_BYTES + 1)
        require(len(raw) <= MAX_BYTES)
        document = json.loads(raw, object_pairs_hook=unique_object)
        result = summarize(document)
    except (OSError, ValueError, TypeError, RecursionError):
        print("paired outcome validation failed", file=sys.stderr)
        return 1
    print(json.dumps(result, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
