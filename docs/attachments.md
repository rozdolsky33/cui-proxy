# Local attachment inspection

!!! info "Available from v0.4.0"
    Upgrade to v0.4.0 or newer and restart the running proxy before enabling
    `policy.attachments`. The v0.3.3 release does not accept this setting.
    Re-run `cui-proxy menubar install` after upgrading to refresh the app and
    its bundled offline manual.

Attachments are blocked by default. To replace supported attachments with locally
extracted, sanitized text, merge the following **partial edit** into the existing
`policy` section of your YAML. Keep the rest of your configuration, validate it
with `check-config`, and reload (or restart) the proxy:

```yaml
policy:
  attachments: extract_text # block is the default
  on_error: block
  max_body_bytes: 20971520  # includes base64 overhead for the whole request
```

`cui-proxy policy check -config PATH` and the dashboard show the attachment mode.
The setting uses the same per-request snapshot as your detector rules. Changing
it does not change requests already being processed. Existing binaries must be
rebuilt/upgraded before this setting is accepted.

For a complete starting configuration, download
[attachment-text.yaml](examples/attachment-text.yaml) and its fabricated
[hosts.txt](examples/hosts.txt) dictionary. Follow the [recipe setup](recipes.md#download-and-validate-a-recipe)
to validate and select the file.

## What reaches the model

Supported attachments are replaced by `input_text` parts before the existing
sanitizer runs. Only extracted text reaches the model. Original bytes, filenames,
image details and file metadata are discarded. The `sanitize` profile masks rule
matches; `strict` and `action: block` rules refuse matches in extracted text.
Conversion mode blocks **all body-processing failures**, even if
`policy.on_error: passthrough` was configured, so a later sanitizer failure cannot
send the original attachment. Unsupported shapes always block in either mode.

This is text extraction, not document redaction or a complete visual inspection.
Layout, diagrams, handwriting and unreadable text may be lost. PDF pages with a
text layer use that layer; embedded images on those pages are not OCRed. OCR uses
English and may misrecognize sensitive text, affecting detection. Review the
applicability of your rules to extracted text. This mode cannot provide the model
with the original image or document layout.

## Supported input and local tools

Direct content parts of unambiguous Responses API **user messages** support:

- `input_file` with `file_data: "data:application/pdf;base64,..."` and optional
  string `filename`.
- `input_image` with `image_url: "data:image/png;base64,..."` or
  `"data:image/jpeg;base64,..."` and optional `detail: auto|low|high`.

Images in **function and custom tool results** are also converted when they
are direct parts of an array-valued `output` in a top-level `input` item of
type `function_call_output` or `custom_tool_call_output`. The item must have
a nonempty string `call_id` and no duplicate fields. The same `input_image`
formats and detail values listed above apply. Each image becomes an
`input_text` part; surrounding text, output order and call identity are
preserved. Extracted tool text follows the same sanitization and blocking
rules as user attachments. All attachments share the request's extraction
budgets. This supports tools returning screenshots without forwarding the
screenshots themselves.

Tool-result PDFs, computer-use output items, images nested in arbitrary
objects, and attachments in assistant messages remain unsupported. Objects
encoded inside text are inspected as text/embedded JSON and do not acquire
attachment conversion privileges. Tool names do not affect eligibility.
This is a protocol-position allowlist, not a restriction to particular tools.

Uploaded `file_id` references, remote URLs, audio, other MIME types, bare base64,
duplicate or unknown fields, and attachments in other positions are refused.
There are no remote downloads or cloud OCR calls.

Install maintained Poppler tools (`pdfinfo`, `pdftotext`, `pdftoppm`) for PDFs and
Tesseract with English language data for images and PDF pages without text.
Executables must be on the proxy process's `PATH`, which may differ from a
terminal's PATH when launched from a menu bar or service. For example, Homebrew
on macOS provides `brew install poppler tesseract`. Restart the proxy after
changing its tool environment. [Tesseract's command-line documentation](https://github.com/tesseract-ocr/tesseract/blob/main/doc/tesseract.1.asc)
describes its local stdin/stdout OCR interface.

A PDF page with no text is rendered locally and OCRed. Every page must yield text;
blank or unreadable pages, encrypted PDFs, missing tools and failed extraction
block the request. Tools run without a shell or inherited credentials; stderr
is discarded. PDF bytes are temporarily stored in a private directory (file mode
0600), removed on normal completion or cancellation. Abrupt process termination
may leave a temporary directory. The tools run as the proxy user, not inside an
OS security sandbox; use maintained installations.

## Limits and diagnostics

Per request: eight attachments, 10 MiB total decoded input, twenty total PDF
pages, 2 MiB extracted text and thirty seconds including time waiting for an
extraction slot. Input images are limited to sixteen million pixels. PDF raster
pages are scaled to at most 4000 pixels on their longest side. Two tool processes
run concurrently across requests, with capped output and cancellation. These
limits do not impose a hard OS memory limit on the external parsers. The normal
request-body size limit also applies before conversion.

Attachment refusals retain `422 reason=sanitize kind=bad_body`, with a safe detail
code and a fixed explanation, for example:

```text
detail=attachment_pdf_missing_tool. A local extraction tool is missing. Install Poppler ...
```

Categories are `pdf`, `image`, `file`, `audio` or `binary`. The cause explains the next step:

| Cause | Meaning | Action |
| --- | --- | --- |
| `disabled` | Attachment processing is off | Enable `extract_text` only for the supported inline formats, or paste text |
| `reference` | A URL or uploaded file ID cannot be inspected locally | Send inline data or paste text; the proxy does not fetch it |
| `unsupported` | Unsupported format or encrypted PDF | Export an unlocked PDF or PNG/JPEG image, or paste text |
| `shape` | Duplicate, unknown or misplaced attachment fields or tool envelope | Use a supported user-message content part or direct function/custom tool-result image |
| `invalid` | Invalid encoding, image or document | Re-export a valid document/image |
| `limit` | A count, size, page, pixel or text limit was exceeded | Split or reduce the input |
| `missing_tool` | Local extractor executable is unavailable | Install Poppler/Tesseract and check the service PATH |
| `failed` | An extractor returned failure | Check tool/language-data installation or re-export the document |
| `empty` | A page or image yielded no readable text | Supply clearer input or paste text |
| `timeout` | Processing exceeded the time budget or was cancelled | Reduce/split the input and retry |
 The code appears in the response, server log and
hash-chained decision log; `cui-proxy explain REQUEST-ID` displays the remedy.
No filenames, URLs, extracted contents or tool error text are logged. Existing
records retain their original hashes; older proxy binaries cannot verify new
records carrying the detail field. Historical records without details cannot
recover an attachment's exact failure cause.

If a client retries the same rejected image history, retrying alone cannot
repair its placement or encoding. Use text output or begin a fresh session
without the unsupported result. Do not disable blocking rules or enable
passthrough to recover. Install the updated proxy binary and restart the
running service to use tool-result image extraction; reloading configuration
alone cannot add support to an older binary.

## Check the running service before attachment work

Run `cui-proxy preflight -config PATH` after starting the service, or use
**Check readiness** in the dashboard. The dashboard checks once on initial load.
This queries the running dashboard's guarded `/api/preflight` endpoint; it does
not substitute dependency discovery from the terminal's environment.

The versioned JSON report identifies the running process (version, Go version,
build revision when available and PID), effective policy enums, and dependency
checks. With `attachments: extract_text`, it discovers `pdfinfo`, `pdftotext`,
`pdftoppm` and `tesseract` through the service PATH, then checks `eng` through
`tesseract --list-langs` using the same restricted child environment as
extraction. Each tool result has a fixed remedy. Tool paths, stderr, language
list contents and credentials are never returned.

Install Poppler for PDF tools and Tesseract with English data for OCR. Ensure
the service PATH finds them and restart the service after installation or
service-environment changes. Extraction does not inherit `TESSDATA_PREFIX` or
other shell variables, so English data must be available through the tool's
default data discovery. A successful check confirms discovery and language
listing, not that every document will extract successfully.

Probes have a two-second deadline, 64 KiB output cap, one concurrent probe per
checker and the existing extraction slot bound. Dependency results are cached
for ten seconds; policy is read fresh each time. Wait ten seconds to recheck a
repaired dependency. With attachments blocked, no extraction tools are required
or probed; readiness means ready for that effective policy, not OCR capability.

The CLI returns exit 0 for ready, 1 for unavailable/not-ready service or
incompatible report, and 2 for usage errors. It accepts only a loopback HTTP
service and never follows redirects. It prints the running build identity;
`dev` or an unavailable revision cannot establish an exact binary revision.
Keep detailed CLI output captured locally as described in verification guidance.
