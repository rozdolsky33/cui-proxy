"""Distinguish policy snapshots without changing matching behavior."""

import copy
import re
import json
import os
from pathlib import Path
import tempfile
import urllib.request
import urllib.parse

import harness


def renamed_policy(baseline, generation):
    if type(generation) is not int or generation < 0:
        raise ValueError("soak_policy_generation")
    rules = baseline.get("rules")
    if not isinstance(rules, list) or not rules:
        raise ValueError("soak_policy_rules")
    updated = copy.deepcopy(baseline)
    for rule in updated["rules"]:
        if not isinstance(rule.get("name"), str) or not rule["name"]:
            raise ValueError("soak_policy_rules")
        rule["name"] = f"soak-g{generation}-" + rule["name"]
    return updated


def verify_policy_records(records, generations):
    if not generations or any(type(generation) is not int or generation < 0 for generation in generations):
        raise ValueError("soak_policy_generations")
    counts = {generation: 0 for generation in generations}
    for record in records:
        rules = record.get("rule_ids")
        if not isinstance(rules, list) or not rules:
            raise ValueError("soak_policy_evidence_missing")
        observed = set()
        for rule in rules:
            match = re.fullmatch(r"soak-g([0-9]+)-[A-Za-z0-9_.-]+", rule) if isinstance(rule, str) else None
            if match is None:
                raise ValueError("soak_policy_evidence_unknown")
            observed.add(int(match.group(1)))
        if len(observed) != 1 or not observed.issubset(generations):
            raise ValueError("soak_policy_snapshot_mixed")
        counts[observed.pop()] += 1
    if any(count == 0 for count in counts.values()):
        raise ValueError("soak_policy_generation_unobserved")
    return counts


class PolicyReloader:
    def __init__(self, env):
        self.env = env
        address = urllib.parse.urlsplit(env.ui)
        if (address.scheme != "http" or address.hostname not in ("127.0.0.1", "localhost")
                or not address.port or address.username or address.password
                or address.path not in ("", "/") or address.query or address.fragment):
            raise ValueError("soak_reload_url")
        self.reload_url = env.ui.rstrip("/") + "/api/reload"
        args = list(env.proxy.args)
        try:
            path = Path(args[args.index("-config") + 1])
        except (ValueError, IndexError):
            raise ValueError("soak_reload_config") from None
        if path.is_symlink() or not path.resolve().is_relative_to(Path(env.directory).resolve()):
            raise ValueError("soak_reload_config")
        self.path = path.resolve()
        self.baseline = json.loads(self.path.read_text())

    def reload(self, generation):
        updated = renamed_policy(self.baseline, generation)
        pending = None
        try:
            with tempfile.NamedTemporaryFile(mode="w", dir=self.path.parent, prefix=".soak-policy-", delete=False) as stream:
                pending = Path(stream.name)
                os.fchmod(stream.fileno(), 0o600)
                json.dump(updated, stream)
                stream.flush()
                os.fsync(stream.fileno())
            os.replace(pending, self.path)
            pending = None
        finally:
            if pending is not None:
                pending.unlink(missing_ok=True)
        request = urllib.request.Request(self.reload_url,
                                         data=b"{}", method="POST",
                                         headers={"Content-Type": "application/json"})
        with harness.OPENER.open(request, timeout=30) as response:
            if response.status != 200:
                raise RuntimeError("soak_reload_rejected")
            response.read(4096)
        return {"generation": generation, "reload_returned_success": True,
                "rule_count": len(updated["rules"])}
