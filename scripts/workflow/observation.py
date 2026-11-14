"""Controlled stage-attribution checks, separate from performance qualification."""

import json
import time

import harness


STAGES = ('ingress_read', 'metadata_inspect', 'body_inspect', 'extraction_wait', 'extraction_run', 'audit_write', 'upstream_wait', 'restore', 'admission_wait', 'mapping_checkpoint')


def check_snapshots(text, image):
    for snapshot in (text, image):
        harness.require(set(snapshot["stages_ms"]).issubset(STAGES), "stage_vocabulary")
        harness.require(all(snapshot[name] == 0 for name in
                            ("active_requests", "extractor_queued", "extractor_active")),
                        "stage_gauge_cleanup")
        harness.require(not snapshot.get("recent_requests"), "correlation_disabled")
    for name in STAGES:
        is_extraction = name.startswith("extraction_")
        harness.require(text["stages_ms"].get(name, {}).get("samples", 0) == (0 if is_extraction else 1),
                        "text_stage_samples")
        harness.require(image["stages_ms"].get(name, {}).get("samples", 0) == (1 if is_extraction else 2),
                        "image_stage_samples")
    harness.require(text["stages_ms"]["upstream_wait"]["p95"] >= 60,
                    "upstream_delay_attribution")
    harness.require(image["stages_ms"]["extraction_run"]["p95"] >= 60,
                    "extraction_delay_attribution")
    harness.require(image["stages_ms"]["body_inspect"]["p95"] <
                    image["stages_ms"]["extraction_run"]["p95"],
                    "extraction_excluded_from_body")
    harness.require(sum(text["request_size_buckets"].values()) == 1 and
                    sum(image["request_size_buckets"].values()) == 2,
                    "request_size_samples")


def snapshot(env):
    # Completion bookkeeping can follow the final response byte. Poll the
    # active gauge instead of relying on a scheduling delay in the client.
    deadline = time.monotonic() + 5
    while True:
        with harness.OPENER.open(env.ui + "/api/metrics", timeout=2) as response:
            value = json.load(response)
        if value.get("active_requests") == 0:
            return value
        harness.require(time.monotonic() < deadline, "stage_completion_timeout")
        time.sleep(0.01)


class DelayedEnvironment(harness.Environment):
    def send(self, body, bypass=False, headers=None):
        headers = dict(headers or {})
        headers["X-Workflow-Delay-MS"] = "80"
        return super().send(body, bypass, headers)


def run(binary):
    with DelayedEnvironment(binary) as env:
        # Add a known run delay to the controlled OCR stand-in. The production
        # extractor, queue and image validation are still exercised unchanged.
        ocr = env.directory / "tools" / "tesseract"
        lines = ocr.read_text().splitlines(keepends=True)
        lines.insert(1, "import time; time.sleep(0.08)\n")
        ocr.write_text("".join(lines))
        result = harness.operation(env, "W01", 0, 1, False)
        harness.require(result["outcome"] == "supported_success", "stage_text_semantics")
        text = snapshot(env)
        result = harness.operation(env, "W06", 0, 2, False)
        harness.require(result["outcome"] == "supported_success", "stage_image_semantics")
        image = snapshot(env)
        check_snapshots(text, image)
        # Only fixed vocabulary and numeric metadata are retained. Recent
        # workload records are deliberately excluded from harness reports.
        return {"controlled_delay_ms": 80, "requests": 2,
                "stages_ms": image["stages_ms"],
                "active_requests": image["active_requests"],
                "extractor_queued": image["extractor_queued"],
                "extractor_active": image["extractor_active"],
                "request_size_buckets": image["request_size_buckets"]}
