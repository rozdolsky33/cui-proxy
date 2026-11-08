# Attachment text inspection

The user authorized the recommended safe approach: an opt-in configuration that replaces attachments with sanitized extracted text, and clearer block errors. Original attachment bytes never go upstream. Work stays in the current clean checkout.

`policy.attachments` accepts `block` (default) or `extract_text`. Only inline Responses API user input parts are converted: PDF `input_file` and PNG/JPEG `input_image`. Uploaded IDs, remote URLs, audio, ambiguous/duplicate fields and unknown shapes block. Schemas and text containing JSON retain their existing treatment.

Local Poppler tools extract PDF text, with Tesseract OCR for pages lacking text; Tesseract handles images. Only text is retained, so layout, nontext visuals, and unreadable text may be lost. This is text extraction, not certification that all attachment content was recognized. Rules sanitize or block the extracted text using the existing profile. Extractor errors always block, even under passthrough policy.

Limits per request: eight attachments, 10 MiB decoded bytes, twenty PDF pages, 2 MiB extracted text, thirty seconds processing; images at most sixteen million pixels. Two extraction processes may run concurrently globally. Commands run locally without a shell, with bounded captured output and no persisted diagnostic values. PDF temporary files live in private temporary directories and are removed on completion. Commands require maintained local Poppler/Tesseract installations.

Diagnostics use closed codes for attachment category and rejection cause, never filenames, URLs, contents, or command error text. Existing `bad_body` remains the kind for compatibility; a safe detail and actionable explanation appear in the response and server log. Decision logs retain safe details so request IDs can be explained later.

Approaches considered: forwarding scanned originals was rejected because extraction cannot establish that originals are safe; full PDF/image redaction was rejected because it needs a separate trustworthy reconstruction pipeline. Text substitution is the selected approach.
