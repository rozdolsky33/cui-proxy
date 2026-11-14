# Bounded scan short mixed probe

Status: short diagnostic passed; full qualification remains open.

Candidate source `5d7112e`; Go 1.26.11; four clients; 768 KiB text; 80 paired text samples; one proxy-first repetition; warmup minimum 5 seconds and 10 requests. These short settings deliberately do not satisfy the original 60-second/100-request warmup or 1,000-sample/three-repetition acceptance requirements.

Added first-byte p95: 135.841750 ms; p99: 137.340124 ms. Added logical-prefix relay p99: 63.940375 ms. Paired and relay samples valid.

Proxy: 80 text plus 219 image requests, all 299 supported successes. Bypass: 80 text plus 237 image requests, all 317 supported successes. No aborted run, unsent text, outcome failures or stage-poll errors. Controller completed in 348.506 seconds.

The previous full candidate failure remains preserved. This result justifies proceeding to the original full pair after checking production dense/late-Unicode fallback cost; it does not substitute for that pair or the remaining roadmap matrix, soak and release envelope.

Private evidence:
- `/private/tmp/cui-task11-bounded-mixed-768.json` SHA-256 `7a07f0fee73b3d51e8d00a35f87a55e4efd51c54731e6f6cfe33bd8e2e8a75fa`.
- `/private/tmp/cui-task11-bounded-mixed-768.provenance.json` SHA-256 `a3e2656376c6d0a813087369500666101829895f8b630f33add3c620075f8d20`.
- `/private/tmp/cui-task11-bounded-final-build.json` SHA-256 `ca67b69b4b2c1d73ff0c55ebc1c00128a4753634e28982770691a3bc43ee3f7c`.
