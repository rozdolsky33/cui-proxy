# Known limitations / behavior

- Credential headers (`Authorization`, `Chatgpt-Account-Id`, cookies) are
  forwarded unscanned and are never logged or stored.
- Other request headers and the query string are scanned, never rewritten:
  any detected value BLOCKS the request (HTTP 422, code `header`).
  `X-Codex-Turn-Metadata` carries the working directory (cwd; see
  docs/verification.md), so a dictionary entry that matches a part of that
  path blocks every request made from that directory with `header`. Work
  from a directory whose path holds no dictionary term. (Sanitizing that
  header like the body may follow once a real session confirms its shape;
  today it is only scanned.)
- Only streamed (SSE) model responses are supported. Successful non-SSE or
  encoded responses are refused with HTTP 502 (`stream_rewrite`) rather than
  delivering unrestored tool arguments. Ordinary upstream error responses
  and model listings retain their status and body. Unlabelled SSE is recognized
  from a bounded framing probe; unrecognized successful bodies fail closed.
- Numeric literals honor custom rules regardless of length. Detected decimal
  or exponent spellings are refused rather than risking a lossy rewrite;
  exponents outside ±400 are refused. Unmatched numbers keep their spelling.
- Unseparated card-like digit runs and long numeric ids become `REDACTED`
  tokens rather than format-preserving synthetics. So do tcpdump-style
  `addr.port` tokens (`10.20.30.15.443`, `2001:db8::1.443`, port included)
  and values glued where their synthetic would not restore (see
  [Block diagnostics](block-diagnostics.md#block-diagnostics)). Five single-digit dotted
  components (`1.2.3.4.5`) are taken for a version number and not masked.
- Detection is pattern-based: the model receives whatever the patterns and
  your dictionary miss. This reduces exposure; it does not bound it. The
  `strict` profile refuses what is detected; it does not make detection
  complete.
- Hostnames that merely encode an address, such as
  `ip-10-20-30-15.ec2.internal`, are not IP literals and are not detected.
  Add your own regex or dictionary rule for them.
- Usernames and local paths (for example in the `<environment_context>`
  block Codex sends: cwd, shell, home directory) are not detected by the
  builtin set. Put the ones that matter in a dictionary.
- Memory-mode restarts lose all mappings (see Vault).
- Without a blocking rule, `tools[].name` and some protocol identifier fields
  are passed through unscanned so that the protocol keeps working (unknown
  `include` values are scanned, not skipped). When any rule blocks they are
  checked too (see Policy profiles); only the content of `encrypted_content`
  is not scanned (it must look like base64 ciphertext).
- The builtin set has no AWS access key detector; `examples/config.yaml`
  adds one as a regex rule (`aws-keys`, `\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`).
- Upstream 3xx responses other than 304 become 502: a redirect (and its
  `Location`) is never relayed to the client.
- Any sanitize failure blocks the request (fail closed) unless
  `policy.on_error: passthrough`, which forwards the ORIGINAL request only for
  a failure of the sanitizer itself (for example no synthetic left for a
  class). It never forwards unsupported content or a policy block, it is
  refused in the config next to any blocking rule, and it is logged as UNSAFE
  at startup (and by `check-config`).
- Listeners are loopback-only unless `allow_non_loopback: true`; the bound
  address is checked after binding. The proxy API accepts native clients:
  browser Origin/Fetch Metadata headers are refused, and loopback mode rejects
  non-loopback Host names to prevent DNS rebinding. Restored streams are marked
  `Cache-Control: no-store`. Other native processes under the same OS user are
  inside the trust boundary.
- The dashboard is localhost-only and never shows real values: only rule
  names, counts, paths and block reasons.

See [assurance and operations](assurance.md) for the reviewed threat
model, verification evidence, operating requirements, and remaining limits.
