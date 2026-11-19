// detecteval prints the detection-quality tables for docs/detection-quality.md.
// It evaluates the built-in detectors against the published generated corpus
// and writes counts and rates only, never a corpus value.
package main

import (
	"fmt"
	"os"

	"cui-proxy/internal/detecteval"
)

func pct(n, total int) string {
	if total == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(n)/float64(total))
}

func main() {
	r, err := detecteval.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "detecteval:", err)
		os.Exit(1)
	}
	fmt.Printf("Corpus: seed %d, %d values per variant, %d samples.\n\n", r.Seed, r.ValuesPerVariant, r.Samples)

	type sum struct{ total, full, partial, missed int }
	fams, order := map[string]*sum{}, []string{}
	for _, p := range r.Variants {
		if !p.Claimed {
			continue
		}
		if fams[p.Family] == nil {
			fams[p.Family] = &sum{}
			order = append(order, p.Family)
		}
		s := fams[p.Family]
		s.total, s.full, s.partial, s.missed = s.total+p.Total, s.full+p.Full, s.partial+p.Partial, s.missed+p.Missed
	}
	fmt.Println("| Family | Samples | Fully masked | Partly masked | Missed | False-negative rate |")
	fmt.Println("|---|---|---|---|---|---|")
	for _, f := range order {
		s := fams[f]
		fmt.Printf("| `%s` | %d | %d | %d | %d | %s |\n", f, s.total, s.full, s.partial, s.missed, pct(s.partial+s.missed, s.total))
	}

	fmt.Println("\n| Family | Shape | Documented as detected | Samples | Fully masked | Partly masked | Missed |")
	fmt.Println("|---|---|---|---|---|---|---|")
	for _, p := range r.Variants {
		claimed := "yes"
		if !p.Claimed {
			claimed = "no"
		}
		fmt.Printf("| `%s` | %s | %s | %d | %s | %d | %d |\n", p.Family, p.Group, claimed, p.Total, pct(p.Full, p.Total), p.Partial, p.Missed)
	}

	fmt.Println("\n| Family | Context | Samples | Fully masked |")
	fmt.Println("|---|---|---|---|")
	for _, p := range r.Contexts {
		if p.Claimed && p.Full < p.Total {
			fmt.Printf("| `%s` | %s | %d | %s |\n", p.Family, p.Group, p.Total, pct(p.Full, p.Total))
		}
	}

	fmt.Println("\n| Resembles | Shape | Samples | Flagged | False-positive rate |")
	fmt.Println("|---|---|---|---|---|")
	for _, n := range r.Negatives {
		fmt.Printf("| `%s` | %s | %d | %d | %s |\n", n.Family, n.Group, n.Total, n.Flagged, pct(n.Flagged, n.Total))
	}
}
