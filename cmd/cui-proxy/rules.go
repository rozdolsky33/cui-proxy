package main

import (
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"text/tabwriter"

	"cui-proxy/internal/transform"
	"cui-proxy/internal/vault"
)

// maxRulesInput caps test-rules input.
const maxRulesInput = 8 << 20

// countSink aggregates replacement counts per (rule, class). Fingerprints
// are dropped: the table shows names and counts only.
type countSink struct{ m map[[2]string]int }

func (c *countSink) Record(e transform.Event) { c.m[[2]string{e.Rule, e.Class}] += e.Count }

// runTestRules sanitizes stdin with the configured rules and a throwaway
// vault: the sanitized text goes to stdout, a rule/class/count table to
// stderr. Neither the vault nor the original text is printed.
func runTestRules(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("test-rules", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "path to the YAML config")
	if code, ok := parseFlags(fs, args, stderr, cfgPath); !ok {
		return code
	}
	abs, err := filepath.Abs(*cfgPath)
	if err != nil {
		return fail(stderr, err)
	}
	_, ds, err := loadRules(abs)
	if err != nil {
		return fail(stderr, err)
	}
	in, err := io.ReadAll(io.LimitReader(stdin, maxRulesInput+1))
	if err != nil {
		return fail(stderr, errors.New("reading stdin failed"))
	}
	if len(in) > maxRulesInput {
		return fail(stderr, errors.New("input exceeds 8 MiB"))
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return fail(stderr, errors.New("generating a throwaway vault key failed"))
	}
	sink := &countSink{m: map[[2]string]int{}}
	out, err := transform.NewSanitizer(ds, vault.New(key), sink).Text(string(in))
	if err != nil {
		var se *transform.SpanError
		if errors.Is(err, transform.ErrBlocked) && errors.As(err, &se) {
			return fail(stderr, fmt.Errorf("the input was blocked by policy (rule=%s class=%s); the proxy would refuse the request", cleanName(se.Rule), cleanName(se.Class)))
		}
		if errors.Is(err, transform.ErrUnrestorable) {
			return fail(stderr, errors.New("the input could not be sanitized reversibly (the proxy would block it)"))
		}
		return fail(stderr, errors.New("the input could not be sanitized (the proxy would block it)"))
	}
	if _, err := io.WriteString(stdout, out); err != nil {
		return exitFailure
	}

	keys := make([][2]string, 0, len(sink.m))
	for k := range sink.m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		return keys[i][1] < keys[j][1]
	})
	tw := tabwriter.NewWriter(stderr, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "RULE\tCLASS\tCOUNT")
	for _, k := range keys {
		fmt.Fprintf(tw, "%s\t%s\t%d\n", k[0], k[1], sink.m[k])
	}
	tw.Flush()
	return exitOK
}
