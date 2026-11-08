package proxy

// remedy says what would change a block, per reason. The wording follows the
// docs/block-diagnostics.md table and docs/verification.md; it names
// settings and actions, never request data.
var remedy = map[string]string{
	reasonClient: "use a native client with a loopback base URL; the proxy API does not accept browser requests or DNS-rebound host names.",
	reasonPolicy: "a blocking rule matched (the strict profile, or a rule with action: block). Expected: remove the value, " +
		"or use the sanitize profile / a rule without action: block if masking is allowed.",
	reasonHeader: "request metadata could not be safely forwarded. Codex workspaces and cwd are sanitized using the active rules; " +
		"strict/block rules, malformed metadata, and sensitive values in other headers, query strings or unchanged metadata fields still block. " +
		"Use the reported kind and rule to diagnose the failure; never disable detection globally to allow a Git remote.",
	reasonSanitize: "the body could not be sanitized. The proxy log line for this request names a kind and the rule " +
		"(request blocked (reason=sanitize kind=... rule=...)); see docs/block-diagnostics.md for each kind.",
	reasonUnrestorable: "the sanitized text would not restore even after downgrading. Rare: report the rule and kind from the proxy log line.",
	reasonRoute: "Codex called an endpoint the adapter does not know (for example remote compaction). " +
		"Unknown routes are always refused (policy.unsupported: block); see docs/verification.md.",
	reasonTooLarge: "the body is over policy.max_body_bytes. Content the proxy cannot inspect is always refused (policy.unsupported: block).",
	reasonCompressed: "compressed request bodies are not supported: the body could not be scanned. " +
		"Content the proxy cannot inspect is always refused (policy.unsupported: block).",
	reasonBadRequest: "malformed request, or a body on a route that is never scanned. " +
		"Content the proxy cannot inspect is always refused (policy.unsupported: block).",
	reasonInternal: "internal error: check the proxy log line.",
	reasonAudit:    "the decision could not be recorded and audit.on_error is block: fix the decision log's disk or permissions, then run cui-proxy audit verify.",
}

// Explain returns, for a block reason, the message the client was given and
// what would change the outcome. ok is false for an unknown reason.
func Explain(reason string) (message, change string, ok bool) {
	message, ok = blockMessage[reason]
	if !ok {
		return "", "", false
	}
	change, ok = remedy[reason]
	return message, change, ok
}
