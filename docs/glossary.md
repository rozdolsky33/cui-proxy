# Glossary

These terms describe the current proxy. The project name does not imply that
it can identify every kind of Controlled Unclassified Information (CUI).

| Term | Meaning |
|---|---|
| **Adapter** | Protocol-specific code that recognizes routes and decides which request and response fields can be inspected or rewritten. The current adapter supports Codex Responses API traffic |
| **Audit / decision log** | Local, value-free request decision records: forwarded, sanitized, or blocked. A recorded forwarding decision does not prove the upstream completed successfully |
| **Blocking rule** | A rule whose effective action rejects the entire request when it matches |
| **Built-in bundle** | The complete detector set enabled by one `type: builtin` entry; it is not enabled automatically when rules are omitted |
| **CIDR** | An IP address with a prefix length, used to describe a network range. A CIDR-shaped synthetic does not preserve real network relationships |
| **CUI** | Controlled Unclassified Information, a US government information-handling category. Pattern detection alone cannot establish whether information belongs to that category or demonstrate compliance |
| **Detector** | A matcher that finds candidate sensitive spans, using built-in logic, a regex, or a dictionary |
| **Dictionary** | A local file of literal names to detect, matched case-insensitively. It adds matches rather than allowing them |
| **Downgrade** | Retrying an otherwise unrestorable shaped replacement as an opaque `secret` token. It changes the replacement shape, not the configured action |
| **Effective action** | The behavior after combining a rule's optional `action` with the policy profile: sanitize or block |
| **Fail closed** | Refuse a request or stop a stream when the required inspection/restoration cannot be performed safely. The default error policy is block |
| **False negative** | Sensitive content that the configured detectors miss; it can reach the upstream |
| **False positive** | Harmless content that a detector matches; it may be masked or block a request |
| **Fingerprint** | A short keyed identifier for a class/value pair, used by replacement events without storing the original in the event. It is not the original value or an unkeyed hash |
| **Guard** | A check that rejects unsuitable traffic, such as an unknown route, unsupported encoding, invalid metadata, or browser-origin request |
| **JSON** | Structured data using objects, arrays, strings, numbers, booleans, and null. A tool result or argument string can itself contain nested JSON |
| **Loopback / localhost** | The local computer's own network interface. Default proxy and dashboard listeners bind locally; this is a trust boundary, not authentication between local processes |
| **Metadata** | Request context outside ordinary prompt content, such as Codex workspace information, headers, and query parameters. Different fields have different rewrite rules |
| **OCID** | Oracle Cloud Identifier, a structured resource identifier matched by a built-in detector |
| **Opaque field** | A protocol identifier or vendor blob that must remain unchanged. Some opaque request identifiers are checked against blocking rules; encrypted reasoning content remains unscanned |
| **Overlap** | Two detected spans covering some of the same text. The proxy merges their union and preserves any blocking decision |
| **Passthrough** | Context-dependent: the model-listing route forwards without body rewriting; `policy.on_error: passthrough` is a separate unsafe option that can forward an original body after eligible sanitizer failures |
| **Policy profile** | `sanitize` or `strict`, the baseline action for configured rules. Strict blocks detections; it does not make detection complete |
| **Pseudonym / synthetic** | A stand-in sent to the model instead of a detected original. The vault maps it back locally; it is not irreversible anonymization |
| **RE2 / Go regexp** | The regular-expression syntax used by custom regex rules; it supports linear-time matching without lookaround or backreferences |
| **Restoration** | Replacing known synthetics with originals in supported response fields before returning them to the client |
| **Sanitization** | Replacing configured detections in supported outgoing content. Unmatched text remains visible to the model |
| **Span** | A detected range of text. The implementation uses byte offsets; overlapping ranges are resolved before replacement |
| **SSE** | Server-Sent Events, the streaming response format used for model output. A synthetic can cross several event chunks, so restoration keeps a small pending prefix |
| **Strict** | The profile in which every configured rule blocks, explicit sanitize actions are forbidden, and auditing must be enabled and block on failure |
| **Synthesis class (`synth`)** | The replacement shape, such as `hostname`, `email`, or opaque `secret`. It does not define what a custom rule detects |
| **Trust boundary** | A point beyond which data is exposed to another component. Here originals remain in the client/proxy/vault side when detected and successfully sanitized; synthetics go to the model backend |
| **Upstream** | The configured model backend to which the proxy forwards accepted requests, with necessary credentials retained |
| **Vault** | The local mapping store containing originals and synthetics. Memory mode is temporary; file mode persists encrypted mappings and needs its key file |

Follow the [request lifecycle](architecture.md) for the full data path and
[rules](rules.md) for matching and replacement details. Practical setup is in
the [quick start](quickstart.md), [tutorial](tutorial.md), and [Codex guide](codex.md).
For operations, see [configuration](configuration.md), [policy](policy.md),
[recipes](recipes.md), [testing](testing.md), [dashboard](dashboard.md),
[reference](reference.md), [decision log](decision-log.md),
[troubleshooting](troubleshooting.md), and [limitations](limitations.md).
