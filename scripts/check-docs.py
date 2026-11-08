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

    # Cloud CLI guidance uses fabricated INI/env text, never local credentials.
    cloud_config = examples / "cloud-cli.yaml"
    cloud_cmd = ["test-rules", "-config", str(cloud_config)]
    cloud_samples = [
        ("user=ocid1.user.oc1..demouseralpha", "builtin.ocid", "demouseralpha"),
        ("tenancy=ocid1.tenancy.oc1..demotenancyalpha", "builtin.ocid", "demotenancyalpha"),
        ("fingerprint=aa:bb:cc:dd:ee:ff:aa:bb:cc:dd:ee:ff:aa:bb:cc:dd",
         "oci-config-fields", "aa:bb:cc:dd"),
        ("key_file=/demo/.oci/demo-key.pem", "oci-config-fields", "/demo/.oci/demo-key.pem"),
        ("pass_phrase=demo passphrase only", "oci-config-fields", "demo passphrase only"),
        ("security_token_file=/demo/.oci/sessions/demo/token",
         "oci-config-fields", "/demo/.oci/sessions/demo/token"),
        ("aws_secret_access_key=demo-secret-only", "aws-credential-fields", "demo-secret-only"),
        ("aws_session_token=demo-session-only", "aws-credential-fields", "demo-session-only"),
        ("ASIA" + "A" * 16, "aws-keys", "ASIA" + "A" * 16),
        ("-----BEGIN PRIVATE KEY-----\ndemo-key-body\n-----END PRIVATE KEY-----",
         "builtin.secret", "demo-key-body"),
    ]
    for field in ["FINGERPRINT", "KEY_FILE", "PASSPHRASE", "SECURITY_TOKEN_FILE"]:
        cloud_samples.append((f"export OCI_CLI_{field}=demo-env-value",
                              "oci-config-fields", "demo-env-value"))
    for field in ["AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"]:
        cloud_samples.append((f"export {field}=demo-env-value",
                              "aws-credential-fields", "demo-env-value"))
    for sample, rule, value in cloud_samples:
        result = run(cloud_cmd, sample=sample, expected=rule)
        assert value not in result.stdout, f"Unmasked cloud sample: {rule}"
        count += 1
    mixed = "[DEFAULT]\r\n  KEY_FILE = /demo/key.pem\r\nregion=us-ashburn-1\r\n"
    result = run(cloud_cmd, sample=mixed, expected="oci-config-fields")
    assert "/demo/key.pem" not in result.stdout
    assert "[DEFAULT]" in result.stdout and "region=us-ashburn-1" in result.stdout
    count += 1
    # These documented gaps must not be mistaken for assignment coverage.
    for sample in ["region=us-ashburn-1\noutput=json", "key_file=",
                   '{"pass_phrase":"demo-json-only"}', "pass_phrase: demo-yaml-only",
                   "Explain key_file=/demo/key.pem", "demo-unlabeled-secret"]:
        assert run(cloud_cmd, sample=sample).stdout == sample
        count += 1
    with tempfile.TemporaryDirectory(prefix="cui-docs-cloud-") as directory:
        strict_cloud = Path(directory) / "strict.yaml"
        strict_cloud.write_text(cloud_config.read_text().replace(
            "profile: sanitize", "profile: strict").replace(
            "file: hosts.txt", f"file: {examples / 'hosts.txt'}"))
        run(["check-config", "-config", str(strict_cloud)])
        for sample, rule, _ in cloud_samples:
            run(["test-rules", "-config", str(strict_cloud)], sample=sample,
                success=False, expected=rule)
            count += 1

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
