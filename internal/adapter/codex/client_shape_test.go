package codex

import "testing"

// Codex 0.166.0 sends its tool definitions as an input item of type
// additional_tools holding namespaces, not as a top-level tools list, and
// returns free-form tool output as a list of content parts. The shapes below
// follow the key paths recorded from that client against a local stand-in.

func TestAdditionalToolsInputItemScanned(t *testing.T) {
	e := newEnv(t)
	body := `{"model":"m","stream":true,"input":[
		{"type":"additional_tools","tools":[{"type":"namespace","name":"functions","description":"runs on 10.20.30.15","tools":[
			{"type":"custom","name":"exec","description":"targets 10.20.30.16","format":{"type":"grammar","syntax":"lark","definition":"start: \"10.20.30.17\""}},
			{"type":"function","name":"wait","strict":false,"description":"waits for 10.20.30.18","parameters":{"type":"object","properties":{"cell_id":{"type":"string","description":"see 10.20.30.19"}}}}]}]},
		{"type":"message","role":"user","content":[{"type":"input_text","text":"ping 10.20.30.20"}]},
		{"type":"custom_tool_call","id":"ctc_1","status":"completed","call_id":"call_1","name":"exec","input":"ping 10.20.30.20"},
		{"type":"custom_tool_call_output","call_id":"call_1","output":[{"type":"input_text","text":"reply from 10.20.30.20"}]}]}`
	out := mustSanitize(t, e, []byte(body))
	assertAbsent(t, out, "10.20.30.15", "10.20.30.16", "10.20.30.17", "10.20.30.18", "10.20.30.19", "10.20.30.20")
}

func TestAdditionalToolsSchemaPropertyNamesNotBlocked(t *testing.T) {
	e := newEnv(t)
	body := `{"input":[{"type":"additional_tools","tools":[{"type":"namespace","name":"functions","tools":[
		{"type":"function","name":"view","parameters":{"type":"object","properties":{"image_url":{"type":"string","description":"from 10.20.30.15"},"file_data":{"type":"string"}}}}]}]}]}`
	out := mustSanitize(t, e, []byte(body))
	assertAbsent(t, out, "10.20.30.15")
}

func TestAdditionalToolsBinaryKeyOutsideSchemaBlocked(t *testing.T) {
	e := newEnv(t)
	body := `{"input":[{"type":"additional_tools","tools":[{"type":"namespace","name":"functions","tools":[
		{"type":"function","name":"f","image_url":"data:,"}]}]}]}`
	if out, err := New().SanitizeRequest([]byte(body), e.s); err == nil || out != nil {
		t.Errorf("binary key in an additional_tools item was not blocked")
	}
}

// The schema exemption belongs to additional_tools items only. A tools member
// on any other item, or on an item whose type is ambiguous, gets the binary
// checks like every other unknown field.
func TestToolSchemaExemptionLimitedToAdditionalToolsItems(t *testing.T) {
	e := newEnv(t)
	for name, body := range map[string]string{
		"message item":     `{"input":[{"type":"message","role":"user","tools":[{"type":"function","name":"f","parameters":{"image_url":"data:,"}}]}]}`,
		"untyped item":     `{"input":[{"tools":[{"type":"function","name":"f","parameters":{"image_url":"data:,"}}]}]}`,
		"conflicting":      `{"input":[{"type":"additional_tools","type":"message","tools":[{"type":"function","name":"f","parameters":{"image_url":"data:,"}}]}]}`,
		"nested in part":   `{"input":[{"type":"message","role":"user","content":[{"type":"additional_tools","tools":[{"parameters":{"image_url":"data:,"}}]}]}]}`,
		"outside input":    `{"extra":{"type":"additional_tools","tools":[{"parameters":{"image_url":"data:,"}}]},"input":[]}`,
		"outside schema":   `{"input":[{"type":"additional_tools","tools":[{"type":"namespace","name":"n","tools":[{"type":"function","name":"f","file_data":"x"}]}]}]}`,
		"sibling of tools": `{"input":[{"type":"additional_tools","tools":[],"image_url":"data:,"}]}`,
	} {
		if out, err := New().SanitizeRequest([]byte(body), e.s); err == nil || out != nil {
			t.Errorf("%s: binary key was not blocked", name)
		}
	}
}
