package rules

import (
	"sort"

	"perfadvisor/internal/collect"
)

type Severity int

const (
	Info Severity = iota
	Medium
	High
)

func (s Severity) String() string {
	switch s {
	case High:
		return "High"
	case Medium:
		return "Medium"
	default:
		return "Info"
	}
}

// Finding is one ranked observation with concrete advice.
type Finding struct {
	ID       string
	Title    string
	Severity Severity
	Summary  string
	Evidence []string
	Advice   []string
}

type rule func(*collect.Snapshot) *Finding

// Evaluate runs every rule against the snapshot, highest severity first.
func Evaluate(s *collect.Snapshot) []Finding {
	var out []Finding
	for _, r := range allRules {
		if f := r(s); f != nil {
			out = append(out, *f)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Severity > out[j].Severity })
	return out
}

// Score is a rough health number: 100 minus a weight per finding, floor 5.
func Score(findings []Finding) int {
	score := 100
	for _, f := range findings {
		switch f.Severity {
		case High:
			score -= 15
		case Medium:
			score -= 7
		default:
			score -= 2
		}
	}
	if score < 5 {
		score = 5
	}
	return score
}
