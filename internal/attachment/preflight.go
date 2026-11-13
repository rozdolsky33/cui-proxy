package attachment

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

type ToolCheck struct {
	Name   string `json:"name"`
	Code   string `json:"code"`
	Ready  bool   `json:"ready"`
	Remedy string `json:"remedy"`
}

// CheckTools uses the service PATH for discovery and Extractor.run for the
// exact restricted child environment. No paths or tool output are returned.
func CheckTools(ctx context.Context) []ToolCheck {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	checks := make([]ToolCheck, 0, 5)
	for _, name := range []string{"pdfinfo", "pdftotext", "pdftoppm", "tesseract"} {
		_, err := exec.LookPath(name)
		code := "ready"
		if err != nil {
			code = "missing_tool"
		}
		checks = append(checks, ToolCheck{Name: name, Code: code, Ready: err == nil})
	}
	language := ToolCheck{Name: "tesseract_eng", Code: "not_checked"}
	if checks[3].Ready {
		e := New(ctx)
		defer e.Close()
		out, why := e.run("tesseract", nil, 64<<10, "--list-langs")
		language.Code = "probe_failed"
		switch why {
		case success:
			language.Code = "missing_english"
			for _, line := range strings.Split(string(out), "\n") {
				if strings.TrimSpace(line) == "eng" {
					language.Ready = true
					language.Code = "ready"
					break
				}
			}
		case MissingTool:
			language.Code = "missing_tool"
		case Timeout:
			language.Code = "probe_timeout"
		case Busy:
			language.Code = "probe_busy"
		case Limit:
			language.Code = "probe_limit"
		}
	}
	checks = append(checks, language)
	for i := range checks {
		checks[i].Remedy = ToolRemedy(checks[i].Code)
	}
	return checks
}

// ToolRemedy contains only fixed diagnostic wording.
func ToolRemedy(code string) string {
	switch code {
	case "ready":
		return "Dependency available in the running service environment."
	case "missing_tool":
		return "Install Poppler for PDF tools or Tesseract for OCR; ensure the service PATH finds it, then restart the service."
	case "missing_english":
		return "Install Tesseract English language data in its default data directory, then restart the service. Shell TESSDATA_PREFIX is not inherited by extraction."
	case "not_checked":
		return "Repair the missing Tesseract executable, then check English language data again."
	case "probe_busy":
		return "Extraction capacity is busy; reduce concurrent attachments and retry the readiness check with bounded backoff."
	case "probe_timeout":
		return "Check local extractor responsiveness and retry readiness after resolving the timeout."
	case "probe_limit":
		return "The tool returned excessive diagnostic output; repair the local installation before retrying."
	case "probe_failed":
		return "The local tool failed its language probe; repair its installation before retrying."
	}
	return ""
}
