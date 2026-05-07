package diagram

import (
	"fmt"
	"strings"
)

// ValidationError describes a single Mermaid syntax problem.
type ValidationError struct {
	Line    int
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("line %d: %s", e.Line, e.Message)
}

// ValidateMermaid performs structural validation on a Mermaid DSL string.
// It returns a slice of validation errors (empty if the DSL is valid).
func ValidateMermaid(dsl string) []ValidationError {
	if strings.TrimSpace(dsl) == "" {
		return []ValidationError{{Line: 0, Message: "empty diagram"}}
	}

	lines := strings.Split(dsl, "\n")
	var errors []ValidationError
	var braceDepth int
	var subgraphDepth int
	diagramType := ""

	for i, raw := range lines {
		lineNum := i + 1
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		// Skip comment lines
		if strings.HasPrefix(line, "%%") {
			continue
		}

		// Detect diagram type from first non-comment line
		if diagramType == "" {
			diagramType = detectDiagramType(line)
			if diagramType == "" {
				errors = append(errors, ValidationError{Line: lineNum, Message: "unrecognized diagram type"})
			}
			continue
		}

		// Track brace depth
		braceDepth += strings.Count(line, "{")
		braceDepth -= strings.Count(line, "}")
		if braceDepth < 0 {
			errors = append(errors, ValidationError{Line: lineNum, Message: "unmatched closing brace"})
			braceDepth = 0
		}

		// Validate line syntax based on diagram type
		switch diagramType {
		case "graph":
			if err := validateGraphLine(line); err != "" {
				errors = append(errors, ValidationError{Line: lineNum, Message: err})
			}
			if strings.HasPrefix(line, "subgraph ") {
				subgraphDepth++
			}
			if line == "end" {
				subgraphDepth--
				if subgraphDepth < 0 {
					errors = append(errors, ValidationError{Line: lineNum, Message: "unmatched 'end' for subgraph"})
					subgraphDepth = 0
				}
			}
		case "classDiagram":
			if err := validateClassDiagramLine(line); err != "" {
				errors = append(errors, ValidationError{Line: lineNum, Message: err})
			}
		case "sequenceDiagram":
			if err := validateSequenceDiagramLine(line); err != "" {
				errors = append(errors, ValidationError{Line: lineNum, Message: err})
			}
		}
	}

	if braceDepth > 0 {
		errors = append(errors, ValidationError{Line: len(lines), Message: "unclosed brace block"})
	}
	if subgraphDepth > 0 {
		errors = append(errors, ValidationError{Line: len(lines), Message: "unclosed subgraph block"})
	}

	return errors
}

func detectDiagramType(line string) string {
	lower := strings.ToLower(line)
	if strings.HasPrefix(lower, "graph ") || lower == "graph" {
		return "graph"
	}
	if strings.HasPrefix(lower, "flowchart ") || lower == "flowchart" {
		return "graph"
	}
	if strings.HasPrefix(lower, "classdiagram") {
		return "classDiagram"
	}
	if strings.HasPrefix(lower, "sequencediagram") {
		return "sequenceDiagram"
	}
	return ""
}

func validateGraphLine(line string) string {
	// Allowed prefixes for graph lines
	if strings.HasPrefix(line, "subgraph ") {
		name := strings.TrimPrefix(line, "subgraph ")
		if name == "" {
			return "subgraph missing name"
		}
		return ""
	}
	if line == "end" {
		return ""
	}

	// Check structural balance first (brackets/parens/braces before edges)
	if strings.Count(line, "[") != strings.Count(line, "]") {
		return "unclosed node bracket"
	}
	if strings.Count(line, "(") != strings.Count(line, ")") {
		return "unclosed node parenthesis"
	}
	if strings.Count(line, "{") != strings.Count(line, "}") {
		return "unclosed node brace"
	}

	// Check for edge syntax
	edgePatterns := []string{"-->", "-.->", "==>", "--x", "--o", "-.-"}
	for _, ep := range edgePatterns {
		if strings.Contains(line, ep) {
			parts := strings.SplitN(line, ep, 2)
			if len(parts) == 2 {
				from := strings.TrimSpace(parts[0])
				to := strings.TrimSpace(parts[1])
				if from == "" {
					return "edge missing source node"
				}
				if to == "" {
					return "edge missing target node"
				}
			}
			return ""
		}
	}

	// Node definition: ID[label] or ID(label) or ID{label} or ID[/label/]
	if strings.Contains(line, "[") {
		return ""
	}
	if strings.Contains(line, "(") {
		return ""
	}
	if strings.Contains(line, "{") {
		return ""
	}

	// Single node ID without shape
	if !strings.ContainsAny(line, "[](){}<>") {
		nodeID := strings.Fields(line)[0]
		if nodeID == "" {
			return "empty node identifier"
		}
		return ""
	}

	return ""
}

func validateClassDiagramLine(line string) string {
	if line == "class" || strings.HasPrefix(line, "class ") {
		rest := strings.TrimSpace(strings.TrimPrefix(line, "class"))
		if rest == "" {
			return "class missing name"
		}
		return ""
	}
	if strings.Contains(line, "--|>") {
		parts := strings.SplitN(line, "--|>", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return "inheritance relation missing class name"
		}
		return ""
	}
	if strings.Contains(line, "<-->") || strings.Contains(line, "--*") || strings.Contains(line, "--o") || strings.Contains(line, "..") {
		return ""
	}
	if line == "{" || line == "}" {
		return ""
	}
	// Method/attribute lines inside class blocks typically start with spaces and +/-/~
	if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") || strings.HasPrefix(line, "~") || strings.HasPrefix(line, "#") {
		return ""
	}
	return ""
}

func validateSequenceDiagramLine(line string) string {
	if strings.HasPrefix(line, "participant ") || strings.HasPrefix(line, "actor ") {
		return ""
	}
	if strings.HasPrefix(line, "Note ") {
		return ""
	}
	if strings.HasPrefix(line, "loop ") || strings.HasPrefix(line, "alt ") || strings.HasPrefix(line, "opt ") || strings.HasPrefix(line, "par ") {
		return ""
	}
	if line == "end" {
		return ""
	}
	// Arrow patterns
	arrowPatterns := []string{"->>", "-->>", "->", "-->", "-x", "--x"}
	for _, ap := range arrowPatterns {
		if strings.Contains(line, ap) {
			return ""
		}
	}
	if strings.Contains(line, ":") {
		return ""
	}
	return ""
}
