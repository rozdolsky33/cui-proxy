#!/usr/bin/env python3
"""Exercise the published example files with the real CLI; never call upstreams."""

import argparse
import json
from pathlib import Path
import re
import subprocess
import tempfile
import tomllib


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", default="bin/cui-proxy")
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    binary = Path(args.binary).resolve()
    examples = root / "docs" / "examples"
    configs = sorted(examples.glob("*.yaml"))
    assert configs, "No published configuration examples found"

    def run(command, *, sample=None, success=True, expected=None):
        result = subprocess.run(
            [str(binary), *command], input=sample, text=True,
            capture_output=True, check=False,
        )
        assert (result.returncode == 0) == success, (
            f"Unexpected exit {result.returncode}: {' '.join(command)}\n"
            f"{result.stdout}{result.stderr}"
        )
        if expected:
            assert expected in result.stderr + result.stdout, (
                f"Missing expected {expected!r}: {' '.join(command)}"
            )
        return result

    count = 0
    for config in configs:
        run(["check-config", "-config", str(config)])
        strict = config.name == "strict.yaml"
        policy_args = ["policy", "check", "-config", str(config)]
        if strict:
            policy_args.append("-require-strict")
        run(policy_args)
        cmd = ["test-rules", "-config", str(config)]
        clean = "Explain retry backoff"
        assert run(cmd, sample=clean).stdout == clean
        for sample, rule in [
            ("db.demo.example", "service-hosts"),
            ("DB.DEMO.EXAMPLE", "service-hosts"),
            ("203.0.113.42", "builtin.ipv4"),
            ("DEMO-123456", "ticket-ids"),
        ]:
            result = run(cmd, sample=sample, success=not strict, expected=rule)
            assert sample not in result.stdout, f"Unmasked sample: {config.name}"
            count += 1
        for sample in ["DEMO-12345", "xdb.demo.examplex"]:
            assert run(cmd, sample=sample).stdout == sample
            count += 1
        # This is a fabricated access-key-shaped token, not a credential.
        aws_sample = "AKIA" + "A" * 16
        block_key = strict or config.name == "selective-block.yaml"
        result = run(cmd, sample=aws_sample, success=not block_key, expected="aws-keys")
        assert aws_sample not in result.stdout
        count += 1
        if config.name == "selective-block.yaml":
            run(cmd, sample="restricted-demo-project", success=False,
                expected="restricted-names")
            run(cmd, sample="db.demo.example restricted-demo-project", success=False,
                expected="restricted-names")
            count += 2

    # Pin the important invalid combination taught in the strict walkthrough.
    with tempfile.TemporaryDirectory(prefix="cui-docs-invalid-") as directory:
        config = Path(directory) / "invalid.yaml"
        config.write_text(
            "policy:\n  profile: strict\n"
            "rules:\n  - name: builtin\n    type: builtin\n    action: sanitize\n"
        )
        run(["check-config", "-config", str(config)], success=False,
            expected="sanitize")

    for config in examples.glob("*.toml"):
        data = tomllib.loads(config.read_text())
        provider = data["model_providers"][data["model_provider"]]
        assert provider["wire_api"] == "responses"
        assert provider["base_url"] == "http://localhost:8787/v1"
        assert provider["supports_websockets"] is False

    # Check authored public pages, not excluded historical source records.
    for page in (root / "docs").glob("*.md"):
        text = page.read_text()
        assert not re.search(r"/Users/(?!you(?:/|\b))[^/\s]+/", text), (
            f"Personal absolute path in {page.relative_to(root)}"
        )
        fences = re.findall(r"^\s*```", text, re.MULTILINE)
        assert len(fences) % 2 == 0, f"Unbalanced code fences in {page.name}"
    print(json.dumps({"configurations": len(configs), "behavior_checks": count,
                      "toml": "valid", "public_page_checks": "passed"}))


if __name__ == "__main__":
    main()
