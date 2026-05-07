package benchmark

import (
	"path/filepath"
	"testing"

	"github.com/splitsword/fine-codewiki/internal/rag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadSuite(t *testing.T) {
	suitePath := filepath.Join("..", "..", "benchmark", "qa_bench.json")
	suite, err := LoadSuite(suitePath)
	require.NoError(t, err)
	require.NotNil(t, suite)

	assert.Equal(t, "CodeWiki QA Benchmark v1", suite.Name)
	assert.Len(t, suite.Cases, 3)
	assert.Equal(t, "arch-001", suite.Cases[0].ID)
	assert.Equal(t, "architecture", suite.Cases[0].Category)
}

func TestEvaluatePassed(t *testing.T) {
	c := Case{
		ID:                     "test-001",
		Question:               "What is User?",
		ExpectedAnswerContains: []string{"class", "BaseModel"},
		MustCiteSource:         true,
		MinSources:             1,
	}
	answer := "User is a class that extends BaseModel."
	sources := []rag.Source{{Filename: "models/user.py", Name: "User"}}

	result := Evaluate(c, answer, sources)
	assert.True(t, result.Passed)
	assert.Equal(t, "passed", result.Details)
}

func TestEvaluateMissingContent(t *testing.T) {
	c := Case{
		ID:                     "test-002",
		Question:               "What is User?",
		ExpectedAnswerContains: []string{"class", "BaseModel", "password"},
	}
	answer := "User is a class that extends BaseModel."

	result := Evaluate(c, answer, nil)
	assert.False(t, result.Passed)
	assert.Contains(t, result.Details, "missing expected content: \"password\"")
}

func TestEvaluateOutOfScope(t *testing.T) {
	c := Case{
		ID:       "test-003",
		Question: "What should I eat?",
		ExpectedAnswerContains: []string{},
		MustCiteSource:         false,
	}
	answer := "Sorry, I cannot answer that."

	result := Evaluate(c, answer, nil)
	assert.True(t, result.Passed)
}

func TestEvaluateCitationRequired(t *testing.T) {
	c := Case{
		ID:               "test-004",
		Question:         "What is User?",
		MustCiteSource:   true,
		MinSources:       2,
		ExpectedAnswerContains: []string{"User"},
	}
	answer := "User is a class."
	sources := []rag.Source{{Filename: "models/user.py", Name: "User"}}

	result := Evaluate(c, answer, sources)
	assert.False(t, result.Passed)
	assert.Contains(t, result.Details, "expected at least 2 sources, got 1")
}

func TestEvaluateExactSourceFile(t *testing.T) {
	c := Case{
		ID:                "test-005",
		Question:          "What is User?",
		MustCiteSource:    true,
		ExactSourceFile:   "services/user_service.py",
		ExpectedAnswerContains: []string{"User"},
	}
	answer := "User is a class."
	sources := []rag.Source{{Filename: "models/user.py", Name: "User"}}

	result := Evaluate(c, answer, sources)
	assert.False(t, result.Passed)
	assert.Contains(t, result.Details, "expected source file")
}

func TestComputeMetrics(t *testing.T) {
	results := []Result{
		{Case: Case{Category: "architecture", Difficulty: "easy", MustCiteSource: true}, Passed: true, Sources: []rag.Source{{Filename: "a.py"}}},
		{Case: Case{Category: "api_reference", Difficulty: "easy", MustCiteSource: true}, Passed: false, Sources: []rag.Source{{Filename: "b.py"}}},
		{Case: Case{Category: "architecture", Difficulty: "hard", MustCiteSource: false}, Passed: true},
	}

	m := ComputeMetrics(results)
	assert.Equal(t, 3, m.Total)
	assert.Equal(t, 2, m.Passed)
	assert.Equal(t, 1, m.Failed)
	assert.InDelta(t, 1.0, m.CitationRate, 0.001)

	arch := m.ByCategory["architecture"]
	assert.Equal(t, 2, arch.Total)
	assert.Equal(t, 2, arch.Passed)

	api := m.ByCategory["api_reference"]
	assert.Equal(t, 1, api.Total)
	assert.Equal(t, 0, api.Passed)
}

func TestSuiteRun(t *testing.T) {
	suite := &Suite{
		Cases: []Case{
			{ID: "q1", Question: "What is X?", ExpectedAnswerContains: []string{"X"}},
			{ID: "q2", Question: "What is Y?", ExpectedAnswerContains: []string{"Y"}},
		},
	}

	askFunc := func(question string) (string, []rag.Source, error) {
		if question == "What is X?" {
			return "X is a variable.", []rag.Source{{Filename: "x.py"}}, nil
		}
		return "Z is a constant.", nil, nil
	}

	results, err := suite.Run(askFunc)
	require.NoError(t, err)
	require.Len(t, results, 2)

	assert.True(t, results[0].Passed)
	assert.False(t, results[1].Passed)
}

func TestLoadSuiteMissingFile(t *testing.T) {
	_, err := LoadSuite("/nonexistent/path/bench.json")
	assert.Error(t, err)
}
