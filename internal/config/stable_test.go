package config

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// stableSchema is the configuration schema declared stable in
// docs/compatibility.md: every YAML key path with its value kind. Adding a key
// is a compatible change. Removing, renaming or retyping one breaks existing
// configuration files, because unknown keys are rejected, and must follow the
// deprecation rules in that document. Update this list in the same change.
var stableSchema = []string{
	"allow_non_loopback bool",
	"audit.enabled bool",
	"audit.file string",
	"audit.max_size_mb int",
	"audit.on_error string",
	"audit.retention_days int",
	"listen string",
	"observability.correlate bool",
	"policy.attachments string",
	"policy.max_body_bytes int64",
	"policy.on_error string",
	"policy.profile string",
	"policy.unsupported string",
	"rules[].action string",
	"rules[].file string",
	"rules[].name string",
	"rules[].pattern string",
	"rules[].synth string",
	"rules[].type string",
	"rules[].validate string",
	"ui.listen string",
	"upstreams map[string]string",
	"vault.file string",
	"vault.key_file string",
	"vault.mode string",
}

// schemaKeys lists every YAML key path reachable from t with its value kind.
func schemaKeys(t *testing.T, typ reflect.Type, path string, out *[]string) {
	t.Helper()
	switch typ.Kind() {
	case reflect.Pointer:
		schemaKeys(t, typ.Elem(), path, out)
	case reflect.Slice:
		schemaKeys(t, typ.Elem(), path+"[]", out)
	case reflect.Struct:
		for i := range typ.NumField() {
			f := typ.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
			if name == "-" || !f.IsExported() {
				continue
			}
			if name == "" {
				t.Fatalf("%s.%s has no yaml key name: give it an explicit tag", typ, f.Name)
			}
			schemaKeys(t, f.Type, strings.TrimPrefix(path+"."+name, "."), out)
		}
	default:
		*out = append(*out, path+" "+typ.String())
	}
}

func TestStableConfigSchema(t *testing.T) {
	var got []string
	schemaKeys(t, reflect.TypeFor[Config](), "", &got)
	slices.Sort(got)
	want := slices.Clone(stableSchema)
	slices.Sort(want)
	for _, k := range want {
		if !slices.Contains(got, k) {
			t.Errorf("stable config key removed, renamed or retyped: %q (see docs/compatibility.md)", k)
		}
	}
	for _, k := range got {
		if !slices.Contains(want, k) {
			t.Errorf("config key not recorded in stableSchema: %q (adding a key is compatible; record it here)", k)
		}
	}
}
