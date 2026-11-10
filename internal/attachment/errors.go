// Package attachment converts inline documents to text locally. It never returns
// original attachment bytes for forwarding, nor includes input values in errors.
package attachment

import "strings"

// Category and Cause are closed diagnostic vocabularies.
type Category uint8
type Cause uint8

const (
	Binary Category = iota
	PDF
	Image
	File
	Audio
)
const (
	Disabled Cause = iota
	Reference
	Unsupported
	Shape
	Invalid
	Limit
	MissingTool
	Failed
	Empty
	Timeout
)

var categories = [...]string{"binary", "pdf", "image", "file", "audio"}
var causes = [...]string{"disabled", "reference", "unsupported", "shape", "invalid", "limit", "missing_tool", "failed", "empty", "timeout"}

// Error contains only enumerated metadata; tool output and input values are discarded.
type Error struct {
	Category Category
	Cause    Cause
}

func (e *Error) Error() string {
	c, why := e.Category, e.Cause
	if int(c) >= len(categories) {
		c = Binary
	}
	if int(why) >= len(causes) {
		why = Failed
	}
	return "attachment_" + categories[c] + "_" + causes[why]
}

// KnownDetail accepts only generated attachment diagnostic codes.
func KnownDetail(s string) bool {
	for c := range categories {
		for why := range causes {
			if s == (&Error{Category(c), Cause(why)}).Error() {
				return true
			}
		}
	}
	return false
}

// Explain returns fixed, JSON-safe wording for a known code.
func Explain(s string) string {
	if !KnownDetail(s) {
		return ""
	}
	for i, why := range causes {
		if strings.HasSuffix(s, "_"+why) {
			return [...]string{
				"Attachments are blocked by default. Set policy.attachments to extract_text for supported inline PDFs and PNG or JPEG images.",
				"Remote URLs and uploaded file IDs cannot be inspected. Supply an inline base64 PDF or PNG or JPEG image, or paste text.",
				"This attachment format is unsupported. Use an inline PDF or PNG or JPEG image, or paste text.",
				"The attachment has duplicate, unknown, or misplaced fields. Use a supported user-message attachment or function/custom tool-result image, or paste text.",
				"The attachment encoding or document is invalid. Export a valid PDF or PNG or JPEG image, or paste text.",
				"Attachment processing exceeded a size, page, count, pixel, or text limit. Split or reduce the attachment.",
				"A local extraction tool is missing. Install Poppler for PDFs and Tesseract with English data for images or scanned pages, then restart the proxy.",
				"Local attachment extraction failed. Check local Poppler or Tesseract installation, export an unlocked document, or paste text.",
				"No readable text was extracted. Supply clearer text or paste the text directly.",
				"Attachment processing timed out or was cancelled. Reduce or split the attachment and retry.",
			}[i]
		}
	}
	return ""
}
