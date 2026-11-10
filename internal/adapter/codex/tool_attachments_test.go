package codex

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cui-proxy/internal/adapter"
	"cui-proxy/internal/config"
	"cui-proxy/internal/detect"
	"cui-proxy/internal/transform"
	"cui-proxy/internal/vault"
)

// Only OCR is replaced: image decoding, budgets, walking and sanitization are real.
func toolImageEnv(t *testing.T, profile string) (*transform.Sanitizer, *transform.Restorer, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tesseract"), []byte("#!/bin/sh\nprintf '%s' 'hiddenlabel'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	ds, err := detect.BuildForProfile([]config.Rule{{Name: "label", Type: "regex", Pattern: "hiddenlabel", Synth: "secret"}}, "", profile)
	if err != nil {
		t.Fatal(err)
	}
	v := vault.New(bytes.Repeat([]byte{7}, 32))
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	part := fmt.Sprintf(`{"type":"input_image","image_url":"data:image/png;base64,%s","detail":"high"}`, base64.StdEncoding.EncodeToString(b.Bytes()))
	return transform.NewSanitizer(ds, v, nil), transform.NewRestorer(v), part
}

func TestToolImageExtraction(t *testing.T) {
	for _, typ := range []string{"function_call_output", "custom_tool_call_output"} {
		t.Run(typ, func(t *testing.T) {
			s, r, part := toolImageEnv(t, config.ProfileSanitize)
			body := []byte(fmt.Sprintf(`{"input":[{"type":%q,"call_id":"call-test","id":"item-test","output":[{"type":"input_text","text":"before"},%s,%s,{"type":"input_text","text":"after"}]}]}`, typ, part, part))
			out, err := New().SanitizeRequestWithPolicy(context.Background(), body, s, "extract_text")
			if err != nil {
				t.Fatalf("supported tool images refused: %v", err)
			}
			if bytes.Contains(out, []byte("hiddenlabel")) || bytes.Contains(out, []byte("image_url")) || bytes.Contains(out, []byte("data:image")) {
				t.Fatal("original image or unsanitized OCR text retained")
			}
			var got struct {
				Input []struct {
					Type   string
					CallID string `json:"call_id"`
					ID     string
					Output []struct{ Type, Text string }
				}
			}
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatal(err)
			}
			if len(got.Input) != 1 || got.Input[0].CallID != "call-test" || got.Input[0].ID != "item-test" || got.Input[0].Type != typ {
				t.Fatal("tool envelope changed")
			}
			parts := got.Input[0].Output
			if len(parts) != 4 || parts[0].Text != "before" || parts[3].Text != "after" {
				t.Fatal("output order changed")
			}
			for _, i := range []int{1, 2} {
				if parts[i].Type != "input_text" || parts[i].Text == "hiddenlabel" || r.Text(parts[i].Text) != "hiddenlabel" {
					t.Fatal("OCR text was not sanitized and restorable")
				}
			}
		})
	}
}

func TestToolImagesRejectAmbiguousPlacement(t *testing.T) {
	s, _, part := toolImageEnv(t, config.ProfileSanitize)
	cases := []string{
		`{"type":"custom_tool_call_output","output":[%s]}`,
		`{"type":"custom_tool_call_output","call_id":"","output":[%s]}`,
		`{"type":"custom_tool_call_output","call_id":1,"output":[%s]}`,
		`{"type":"custom_tool_call_output","call_id":"a","call_id":"b","output":[%s]}`,
		`{"type":"custom_tool_call_output","type":"custom_tool_call_output","call_id":"a","output":[%s]}`,
		`{"type":"custom_tool_call_output","call_id":"a","output":[],"output":[%s]}`,
		`{"type":"unknown_output","call_id":"a","output":[%s]}`,
		`{"type":"custom_tool_call_output","call_id":"a","output":{"nested":%s}}`,
		`{"type":"custom_tool_call_output","call_id":"a","output":[{"nested":%s}]}`,
		`{"type":"message","role":"assistant","content":[%s]}`,
		`{"type":"custom_tool_call_output","call_id":"a","output":[{"type":"input_text","text":%s}]}`,
	}
	for i, envelope := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			out, err := New().SanitizeRequestWithPolicy(context.Background(), []byte(`{"input":[`+fmt.Sprintf(envelope, part)+`]}`), s, "extract_text")
			if !errors.Is(err, adapter.ErrBadBody) || out != nil {
				t.Fatal("ambiguous or misplaced attachment accepted")
			}
		})
	}
}

func TestToolImagesHonorPolicyAndBudgets(t *testing.T) {
	for _, profile := range []string{config.ProfileSanitize, config.ProfileStrict} {
		t.Run(profile, func(t *testing.T) {
			s, _, part := toolImageEnv(t, profile)
			body := []byte(`{"input":[{"type":"custom_tool_call_output","call_id":"a","output":[` + part + `]}]}`)
			if out, err := New().SanitizeRequestWithPolicy(context.Background(), body, s, "block"); out != nil || !errors.Is(err, adapter.ErrBadBody) {
				t.Fatal("block mode accepted image")
			}
			if profile == config.ProfileStrict {
				if out, err := New().SanitizeRequestWithPolicy(context.Background(), body, s, "extract_text"); out != nil || !errors.Is(err, transform.ErrBlocked) {
					t.Fatal("OCR bypassed blocking rule")
				}
			}
		})
	}
	s, _, part := toolImageEnv(t, config.ProfileSanitize)
	user := `{"role":"user","content":[` + part + `]}`
	tool := `{"type":"function_call_output","call_id":"a","output":[` + strings.TrimSuffix(strings.Repeat(part+",", 8), ",") + `]}`
	out, err := New().SanitizeRequestWithPolicy(context.Background(), []byte(`{"input":[`+user+`,`+tool+`]}`), s, "extract_text")
	if out != nil || err == nil || !strings.Contains(err.Error(), "attachment_image_limit") {
		t.Fatal("user/tool attachments did not share count budget")
	}
}

func TestToolImageExtractionFailures(t *testing.T) {
	s, _, part := toolImageEnv(t, config.ProfileSanitize)
	for _, tc := range []struct{ name, script, detail string }{
		{"empty", "#!/bin/sh\nexit 0\n", "attachment_image_empty"},
		{"failed", "#!/bin/sh\nexit 1\n", "attachment_image_failed"},
		{"missing", "", "attachment_image_missing_tool"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.script != "" {
				if err := os.WriteFile(filepath.Join(dir, "tesseract"), []byte(tc.script), 0700); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", dir)
			body := []byte(`{"input":[{"type":"custom_tool_call_output","call_id":"a","output":[` + part + `]}]}`)
			out, err := New().SanitizeRequestWithPolicy(context.Background(), body, s, "extract_text")
			if out != nil || err == nil || !strings.Contains(err.Error(), tc.detail) {
				t.Fatal("extraction failure did not refuse with expected diagnostic")
			}
		})
	}
}

func TestToolImagePartValidation(t *testing.T) {
	s, _, part := toolImageEnv(t, config.ProfileSanitize)
	for i, bad := range []string{
		strings.Replace(part, `"detail":"high"`, `"detail":"original"`, 1),
		strings.Replace(part, `"detail":"high"`, `"detail":"high","extra":"ignored"`, 1),
		strings.Replace(part, `"detail":"high"`, `"detail":"high","detail":"high"`, 1),
		`{"type":"input_image","image_url":"https://example.invalid/image.png"}`,
		`{"type":"input_image","image_url":"data:image/png;base64,invalid"}`,
		`{"type":"input_image","image_url":null}`,
		`{"type":"input_file","file_data":"data:application/pdf;base64,invalid"}`,
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			body := []byte(`{"input":[{"type":"function_call_output","call_id":"a","output":[` + bad + `]}]}`)
			if out, err := New().SanitizeRequestWithPolicy(context.Background(), body, s, "extract_text"); out != nil || !errors.Is(err, adapter.ErrBadBody) {
				t.Fatal("invalid tool attachment accepted")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	body := []byte(`{"input":[{"type":"custom_tool_call_output","call_id":"a","output":[` + part + `]}]}`)
	if out, err := New().SanitizeRequestWithPolicy(ctx, body, s, "extract_text"); out != nil || err == nil || !strings.Contains(err.Error(), "attachment_image_timeout") {
		t.Fatal("cancelled extraction was accepted")
	}
}

func TestToolEnvelopeInTextDoesNotGrantExtraction(t *testing.T) {
	s, _, part := toolImageEnv(t, config.ProfileSanitize)
	inner := `{"input":[{"type":"custom_tool_call_output","call_id":"a","output":[` + part + `]}]}`
	text, _ := json.Marshal(inner)
	body := []byte(`{"input":[{"role":"user","content":[{"type":"input_text","text":` + string(text) + `}]}]}`)
	out, err := New().SanitizeRequestWithPolicy(context.Background(), body, s, "extract_text")
	if err != nil {
		t.Fatal("ordinary embedded JSON was refused")
	}
	if !bytes.Equal(out, body) {
		t.Fatal("embedded JSON granted attachment extraction privileges")
	}
}
