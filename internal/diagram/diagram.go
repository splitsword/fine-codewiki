package diagram

import (
	"fmt"
	"sort"
	"strings"

	"github.com/splitsword/fine-codewiki/internal/analyzer"
	"github.com/splitsword/fine-codewiki/internal/grapher"
)

// GenerateArchitectureDiagram generates a Mermaid flowchart (graph TD)
// representing the module dependency structure.
func GenerateArchitectureDiagram(graph *grapher.Graph) (string, error) {
	var b strings.Builder
	b.WriteString("graph TD\n")

	if len(graph.Nodes) == 0 {
		return b.String(), nil
	}

	// Collect standalone nodes (not in any named group) and grouped nodes
	groups := graph.GroupByDirectory()

	// Sort group keys for deterministic output
	var groupKeys []string
	for k := range groups {
		groupKeys = append(groupKeys, k)
	}
	sort.Strings(groupKeys)

	// Track which nodes have been written inside subgraphs
	written := make(map[string]bool)

	// Write subgraphs (skip root-level files)
	for _, dir := range groupKeys {
		nodes := groups[dir]
		if len(nodes) == 0 {
			continue
		}
		if dir == "." || dir == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("    subgraph %s\n", mermaidEscape(dir)))
		for _, n := range nodes {
			nodeID := mermaidEscape(n.Name)
			b.WriteString(fmt.Sprintf("        %s[%s]\n", nodeID, n.Name))
			written[n.Name] = true
		}
		b.WriteString("    end\n")
	}

	// Write standalone nodes (not in any subgraph)
	for _, n := range graph.Nodes {
		if !written[n.Name] {
			nodeID := mermaidEscape(n.Name)
			b.WriteString(fmt.Sprintf("    %s[%s]\n", nodeID, n.Name))
		}
	}

	// Detect cycles for annotation
	cycles := graph.DetectCycles()
	cycleEdges := make(map[string]bool)
	for _, c := range cycles {
		for i := 0; i < len(c.Nodes)-1; i++ {
			key := c.Nodes[i] + "->" + c.Nodes[i+1]
			cycleEdges[key] = true
		}
	}

	// Write edges
	for _, e := range graph.Edges {
		fromID := mermaidEscape(e.From)
		toID := mermaidEscape(e.To)
		if cycleEdges[e.From+"->"+e.To] {
			b.WriteString(fmt.Sprintf("    %s -.-> %s\n", fromID, toID))
		} else {
			b.WriteString(fmt.Sprintf("    %s --> %s\n", fromID, toID))
		}
	}

	return b.String(), nil
}

// GenerateClassDiagram generates a Mermaid classDiagram from the code graph.
func GenerateClassDiagram(graph *grapher.Graph) (string, error) {
	var b strings.Builder
	b.WriteString("classDiagram\n")

	if len(graph.Nodes) == 0 {
		return b.String(), nil
	}

	// Collect all classes and inheritance relationships
	type classRef struct {
		node  *grapher.Node
		class analyzer.ClassInfo
	}
	var classes []classRef
	inheritance := make(map[string][]string) // child -> parents

	for _, n := range graph.Nodes {
		for _, c := range n.Classes {
			classes = append(classes, classRef{node: n, class: c})
			if len(c.Bases) > 0 {
				inheritance[c.Name] = c.Bases
			}
		}
	}

	if len(classes) == 0 {
		return b.String(), nil
	}

	// Sort classes by name for deterministic output
	sort.Slice(classes, func(i, j int) bool {
		return classes[i].class.Name < classes[j].class.Name
	})

	// Write class definitions
	for _, ref := range classes {
		c := ref.class
		classID := mermaidEscape(c.Name)
		b.WriteString(fmt.Sprintf("    class %s {\n", classID))
		for _, m := range c.Methods {
			params := strings.Join(m.Params, ", ")
			// Skip self/cls in display
			params = stripSelfParams(params)
			line := fmt.Sprintf("        +%s(%s)", m.Name, params)
			if m.ReturnType != "" {
				line += " " + m.ReturnType
			}
			b.WriteString(line + "\n")
		}
		b.WriteString("    }\n")
	}

	// Write inheritance relationships
	var childNames []string
	for child := range inheritance {
		childNames = append(childNames, child)
	}
	sort.Strings(childNames)

	for _, child := range childNames {
		parents := inheritance[child]
		sort.Strings(parents)
		for _, parent := range parents {
			childID := mermaidEscape(child)
			parentID := mermaidEscape(parent)
			b.WriteString(fmt.Sprintf("    %s --|> %s\n", childID, parentID))
		}
	}

	return b.String(), nil
}

// mermaidEscape sanitizes a string for use as a Mermaid node/class identifier.
func mermaidEscape(s string) string {
	// Replace characters that break Mermaid identifiers
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, ":", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "--", "__")
	return s
}

// stripSelfParams removes 'self' and 'cls' from parameter lists for cleaner diagrams.
func stripSelfParams(params string) string {
	var parts []string
	for _, p := range strings.Split(params, ", ") {
		if p != "self" && p != "cls" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ", ")
}
