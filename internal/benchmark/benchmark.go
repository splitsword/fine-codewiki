package benchmark

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/splitsword/fine-codewiki/internal/rag"
)

// Case represents a single QA benchmark case.
type Case struct {
	ID                    string   `json:"id"`
	Question              string   `json:"question"`
	Category              string   `json:"category"`
	Difficulty            string   `json:"difficulty"`
	ExpectedAnswerContains []string `json:"expected_answer_contains"`
	MustCiteSource        bool     `json:"must_cite_source"`
	MinSources            int      `json:"min_sources,omitempty"`
	ExactSourceFile       string   `json:"exact_source_file,omitempty"`
}

// Suite holds a collection of benchmark cases.
type Suite struct {
	Name  string `json:"name"`
	Repo  string `json:"repo"`
	Cases []Case `json:"cases"`
}

// Result holds the evaluation outcome for a single case.
type Result struct {
	Case      Case
	Answer    string
	Sources   []rag.Source
	Passed    bool
	Details   string
}

// Metrics aggregates benchmark results.
type Metrics struct {
	Total          int
	Passed         int
	Failed         int
	CitationRate   float64
	ByCategory     map[string]struct{ Total, Passed int }
	ByDifficulty   map[string]struct{ Total, Passed int }
}

// LoadSuite reads a benchmark suite from a JSON file.
func LoadSuite(path string) (*Suite, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read benchmark file: %w", err)
	}
	var suite Suite
	if err := json.Unmarshal(b, &suite); err != nil {
		return nil, fmt.Errorf("parse benchmark file: %w", err)
	}
	return &suite, nil
}

// Evaluate checks a single benchmark case against an answer.
func Evaluate(c Case, answer string, sources []rag.Source) Result {
	var passed bool
	var details []string

	// Check expected content
	if len(c.ExpectedAnswerContains) > 0 {
		allFound := true
		for _, expected := range c.ExpectedAnswerContains {
			if !strings.Contains(strings.ToLower(answer), strings.ToLower(expected)) {
				allFound = false
				details = append(details, fmt.Sprintf("missing expected content: %q", expected))
			}
		}
		passed = allFound
	} else {
		// If no expected content, assume out-of-scope question
		passed = answer == "" || strings.Contains(strings.ToLower(answer), "sorry") || strings.Contains(strings.ToLower(answer), "insufficient")
	}

	// Check source citation
	if c.MustCiteSource {
		if len(sources) == 0 {
			passed = false
			details = append(details, "expected sources but got none")
		} else if c.MinSources > 0 && len(sources) < c.MinSources {
			passed = false
			details = append(details, fmt.Sprintf("expected at least %d sources, got %d", c.MinSources, len(sources)))
		}
		if c.ExactSourceFile != "" {
			found := false
			for _, s := range sources {
				if strings.Contains(s.Filename, c.ExactSourceFile) {
					found = true
					break
				}
			}
			if !found {
				passed = false
				details = append(details, fmt.Sprintf("expected source file %q not found", c.ExactSourceFile))
			}
		}
	}

	detailStr := "passed"
	if !passed {
		detailStr = strings.Join(details, "; ")
	}

	return Result{
		Case:    c,
		Answer:  answer,
		Sources: sources,
		Passed:  passed,
		Details: detailStr,
	}
}

// ComputeMetrics aggregates a slice of results into summary metrics.
func ComputeMetrics(results []Result) Metrics {
	m := Metrics{
		Total:        len(results),
		ByCategory:   make(map[string]struct{ Total, Passed int }),
		ByDifficulty: make(map[string]struct{ Total, Passed int }),
	}
	var cited int
	var totalCitable int

	for _, r := range results {
		if r.Passed {
			m.Passed++
		}
		cat := m.ByCategory[r.Case.Category]
		cat.Total++
		if r.Passed {
			cat.Passed++
		}
		m.ByCategory[r.Case.Category] = cat

		diff := m.ByDifficulty[r.Case.Difficulty]
		diff.Total++
		if r.Passed {
			diff.Passed++
		}
		m.ByDifficulty[r.Case.Difficulty] = diff

		if r.Case.MustCiteSource {
			totalCitable++
			if len(r.Sources) > 0 {
				cited++
			}
		}
	}

	m.Failed = m.Total - m.Passed
	if totalCitable > 0 {
		m.CitationRate = float64(cited) / float64(totalCitable)
	}
	return m
}

// Run executes all cases in a suite using the provided ask function.
// The askFunc should invoke the RAG engine and return the answer with sources.
func (s *Suite) Run(askFunc func(question string) (string, []rag.Source, error)) ([]Result, error) {
	var results []Result
	for _, c := range s.Cases {
		answer, sources, err := askFunc(c.Question)
		if err != nil {
			results = append(results, Result{
				Case:    c,
				Passed:  false,
				Details: fmt.Sprintf("error: %v", err),
			})
			continue
		}
		results = append(results, Evaluate(c, answer, sources))
	}
	return results, nil
}
