package diagram

import (
	"testing"

	"github.com/splitsword/fine-codewiki/internal/analyzer"
	"github.com/splitsword/fine-codewiki/internal/grapher"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateMermaidValidGraph(t *testing.T) {
	dsl := `graph TD
    A[Start] --> B[Process]
    B --> C[End]
`
	errs := ValidateMermaid(dsl)
	assert.Empty(t, errs)
}

func TestValidateMermaidValidClassDiagram(t *testing.T) {
	dsl := `classDiagram
    class User {
        +String name
        +login()
    }
    User --|> BaseModel
`
	errs := ValidateMermaid(dsl)
	assert.Empty(t, errs)
}

func TestValidateMermaidEmpty(t *testing.T) {
	errs := ValidateMermaid("")
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0].Message, "empty")
}

func TestValidateMermaidUnclosedBrace(t *testing.T) {
	dsl := `classDiagram
    class User {
        +String name
`
	errs := ValidateMermaid(dsl)
	assert.NotEmpty(t, errs)
	found := false
	for _, e := range errs {
		if e.Message == "unclosed brace block" {
			found = true
		}
	}
	assert.True(t, found, "expected unclosed brace error")
}

func TestValidateMermaidUnclosedSubgraph(t *testing.T) {
	dsl := `graph TD
    subgraph Group
        A --> B
`
	errs := ValidateMermaid(dsl)
	assert.NotEmpty(t, errs)
	found := false
	for _, e := range errs {
		if e.Message == "unclosed subgraph block" {
			found = true
		}
	}
	assert.True(t, found, "expected unclosed subgraph error")
}

func TestValidateMermaidUnmatchedEnd(t *testing.T) {
	dsl := `graph TD
    A --> B
    end
`
	errs := ValidateMermaid(dsl)
	assert.NotEmpty(t, errs)
	found := false
	for _, e := range errs {
		if e.Message == "unmatched 'end' for subgraph" {
			found = true
		}
	}
	assert.True(t, found, "expected unmatched end error")
}

func TestValidateMermaidEdgeMissingNode(t *testing.T) {
	dsl := `graph TD
     --> B
`
	errs := ValidateMermaid(dsl)
	assert.NotEmpty(t, errs)
	found := false
	for _, e := range errs {
		if e.Message == "edge missing source node" {
			found = true
		}
	}
	assert.True(t, found, "expected missing source node error")
}

func TestValidateMermaidUnclosedBracket(t *testing.T) {
	dsl := `graph TD
    A[Start --> B
`
	errs := ValidateMermaid(dsl)
	assert.NotEmpty(t, errs)
	found := false
	for _, e := range errs {
		if e.Message == "unclosed node bracket" {
			found = true
		}
	}
	assert.True(t, found, "expected unclosed bracket error")
}

func TestValidateMermaidSequenceDiagram(t *testing.T) {
	dsl := `sequenceDiagram
    Alice->>Bob: Hello
    Bob-->>Alice: Hi
`
	errs := ValidateMermaid(dsl)
	assert.Empty(t, errs)
}

func TestValidateMermaidGeneratedArchitecture(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "models/user.py",
			Classes:  []analyzer.ClassInfo{{Name: "User"}},
			Imports:  []analyzer.ImportInfo{{Module: ".base", Name: "BaseModel", IsRelative: true}},
		},
		{Filename: "models/base.py", Classes: []analyzer.ClassInfo{{Name: "BaseModel"}}},
	}
	graph := grapher.BuildGraph(files)
	dsl, err := GenerateArchitectureDiagram(graph)
	require.NoError(t, err)
	valErrs := ValidateMermaid(dsl)
	assert.Empty(t, valErrs, "generated architecture diagram should be valid Mermaid")
}

func TestValidateMermaidGeneratedClassDiagram(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "models/user.py",
			Classes: []analyzer.ClassInfo{
				{Name: "User", Bases: []string{"BaseModel"}, Methods: []analyzer.FunctionInfo{{Name: "greet"}}},
			},
		},
		{Filename: "models/base.py", Classes: []analyzer.ClassInfo{{Name: "BaseModel"}}},
	}
	graph := grapher.BuildGraph(files)
	dsl, err := GenerateClassDiagram(graph)
	require.NoError(t, err)
	valErrs := ValidateMermaid(dsl)
	assert.Empty(t, valErrs, "generated class diagram should be valid Mermaid")
}

func TestValidateMermaidEdgeMissingTarget(t *testing.T) {
	dsl := `graph TD
    A -->
`
	errs := ValidateMermaid(dsl)
	found := false
	for _, e := range errs {
		if e.Message == "edge missing target node" {
			found = true
		}
	}
	assert.True(t, found, "expected missing target node error")
}

func TestValidateMermaidUnclosedParenthesis(t *testing.T) {
	dsl := `graph TD
    A(Start --> B
`
	errs := ValidateMermaid(dsl)
	found := false
	for _, e := range errs {
		if e.Message == "unclosed node parenthesis" {
			found = true
		}
	}
	assert.True(t, found, "expected unclosed parenthesis error")
}

func TestValidateMermaidUnclosedNodeBrace(t *testing.T) {
	dsl := `graph TD
    A{Start --> B
`
	errs := ValidateMermaid(dsl)
	found := false
	for _, e := range errs {
		if e.Message == "unclosed node brace" {
			found = true
		}
	}
	assert.True(t, found, "expected unclosed node brace error")
}

func TestValidateMermaidFlowchartKeyword(t *testing.T) {
	dsl := `flowchart TD
    A --> B
`
	errs := ValidateMermaid(dsl)
	assert.Empty(t, errs)
}

func TestValidateMermaidSequenceLoop(t *testing.T) {
	dsl := `sequenceDiagram
    loop every minute
        Alice->>Bob: ping
    end
`
	errs := ValidateMermaid(dsl)
	assert.Empty(t, errs)
}

func TestValidateMermaidClassRelation(t *testing.T) {
	dsl := `classDiagram
    class A
    class B
    A <--> B
    A --* C
    A --o D
    A .. E
`
	errs := ValidateMermaid(dsl)
	assert.Empty(t, errs)
}

func TestValidateMermaidClassMissingName(t *testing.T) {
	dsl := `classDiagram
    class
`
	errs := ValidateMermaid(dsl)
	found := false
	for _, e := range errs {
		if e.Message == "class missing name" {
			found = true
		}
	}
	assert.True(t, found, "expected class missing name error")
}

func TestValidateMermaidClassInheritanceMissingName(t *testing.T) {
	dsl := `classDiagram
    --|> B
`
	errs := ValidateMermaid(dsl)
	found := false
	for _, e := range errs {
		if e.Message == "inheritance relation missing class name" {
			found = true
		}
	}
	assert.True(t, found, "expected inheritance missing name error")
}

func TestValidateMermaidDottedEdge(t *testing.T) {
	dsl := `graph TD
    A -.-> B
`
	errs := ValidateMermaid(dsl)
	assert.Empty(t, errs)
}

func TestValidateMermaidCommentLine(t *testing.T) {
	dsl := `graph TD
    %% this is a comment
    A --> B
`
	errs := ValidateMermaid(dsl)
	assert.Empty(t, errs)
}

func TestValidateMermaidEmptyNode(t *testing.T) {
	dsl := `graph TD

`
	errs := ValidateMermaid(dsl)
	assert.Empty(t, errs)
}

func TestValidateMermaidUnrecognizedType(t *testing.T) {
	dsl := `gantt
    title Project
`
	errs := ValidateMermaid(dsl)
	found := false
	for _, e := range errs {
		if e.Message == "unrecognized diagram type" {
			found = true
		}
	}
	assert.True(t, found, "expected unrecognized diagram type error")
}
