# Task 11 native cancellation verification

The original-warmup, three-trial native cancellation diagnostic passed on 2026-11-14. This verifies the cancellation gate only; full performance, fairness, throughput, stress, recovery and four-hour soak qualification remain separate requirements.

## Tested identities

- Harness: `eed6b4936973543d82bc710a4e6a469ebfe05f74`, clean checkout.
- Runtime: `5d7112e47063a65efee62f2f326c7060e5df9cf6`.
- Runtime SHA-256: `ee512576a8d47aefb6a63759b4ba48b675fdd3c3984234f2324c8a4bfc22196b`.
- Original extraction and fail-closed policy remained enabled. The diagnostic gated the actual OCR subprocess; it did not substitute successful OCR results.

## Observed results

Warmup completed 112 successful requests in 61.422 seconds, exceeding the original minimum of 100 requests and 60 seconds.

| Trial | Queued cancellation upper bound | Active-slot recovery upper bound |
| --- | ---: | ---: |
| 1 | 64.403 ms | 222.095 ms |
| 2 | 64.507 ms | 170.651 ms |
| 3 | 69.404 ms | 175.126 ms |

All three trials met the original 1-second queued-cancellation, 2-second active-slot recovery and 45-second request limits. Each trial bound observations to the actual proxy and helper process identities, verified termination of the cancelled helper, continued execution of the survivor, and admission of a replacement helper. The five operations per trial included unaffected text and image recovery with semantic and upstream-receipt verification. Cancelled operations had no upstream receipt and uniquely attributable blocking audit records.

Finalization verified all 15 worker threads joined, nine helper identities terminal, both owned backend/proxy processes stopped, closed logs, released gates and restored HTTP connection handling. Privacy verification checked five files against 20,000 protected values without publishing their contents.

The evaluator reports `cancellation_qualified=true`, `privacy_verified=true`, `cleanup_verified=true`, and deliberately retains `performance_qualified=false`.

## Reproduction and retained evidence

Run `scripts/workflow/cancellation_campaign.py` with `--binary`, `--seed-binary`, `--fixtures`, `--build-manifest`, `--binary-source`, `--binary-commit` and a fresh `--output` path. Use the current seed helper so its inspection interface matches the harness. The CLI preserves the original warmup and three-trial requirements. Capture stdout and stderr locally as described in [verification guidance](../verification.md).

The successful private artifact root is `/private/tmp/cui-task11-cancellation-native-11`. Raw reports and diagnostic logs remain local because they can contain fixture or policy data. The complete expected identity is independently attested and retained separately from the original base identity.

| Artifact suffix | SHA-256 |
| --- | --- |
| `.cancellation.json` | `5db6b4966968971c62bd8d0726d1a8b53f39fd7493f485bcf70470a247d28890` |
| `.cancellation-evaluation.json` | `bff35e11a78758ac5ce145f51754b6ed09db2f6cf7e37599dec1461d26f779be` |
| `.expected-complete-identity.json` | `90145093c9b9da5f3cad2c1a8426e9bccc0704226e2e0f1f25d6025675264f14` |

Earlier failed diagnostic attempts remain retained and unqualified. Their fixes cover CLI exception propagation, current seed inspection compatibility, unrelated unknown process states, worker failure propagation, complete fixed-plan assignments, receipt operation IDs, native tuple/list and headerless-status representations, and independently attested corpus provenance. None relaxed the original numerical or fail-closed gates.

Local workflow regressions passed before the successful native run. Final branch regression, documentation checks, independent review and CI are recorded with the integration PR.

## Final review follow-up

The independent branch review confirmed the retained three-trial artifact and identified two failure-path gaps. Failed trials now preserve available operation outcomes after worker cleanup, including supported-operation failures. Failed campaigns scan their available logs, audit, partial report, failure report and traceback, and retain a separate metadata-only privacy result. A failed privacy scan preserves the original trial failure and cannot publish qualification. Targeted regressions for these paths passed; required CI reruns the full suite. These changes do not alter the measured timing thresholds or the successful native evidence above.
