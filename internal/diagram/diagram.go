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
	b.WriteString("%% 架构图：展示项目模块间的依赖关系与层级结构\n")
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

	// Add click handlers for interactive navigation
	for _, n := range graph.Nodes {
		nodeID := mermaidEscape(n.Name)
		b.WriteString(fmt.Sprintf("    click %s \"javascript:navigateToModule('%s')\"\n", nodeID, n.Name))
	}

	// Inject semantic role-based styling
	writeRoleStyling(&b, graph)

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
	b.WriteString("%% 类图：展示项目中类的定义、属性及继承/实现关系\n")
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

	// Deduplicate by class name, keeping the one with the most methods.
	classMap := make(map[string]classRef)
	for _, ref := range classes {
		c := ref.class
		if existing, ok := classMap[c.Name]; !ok || len(c.Methods) > len(existing.class.Methods) {
			classMap[c.Name] = ref
		}
	}
	classes = classes[:0]
	for _, ref := range classMap {
		classes = append(classes, ref)
	}

	// Sort classes by name for deterministic output
	sort.Slice(classes, func(i, j int) bool {
		return classes[i].class.Name < classes[j].class.Name
	})

	// Write class definitions
	for _, ref := range classes {
		c := ref.class
		classID := mermaidEscape(c.Name)
		if ref.node != nil {
			b.WriteString("    %% " + c.Name + " 来自 " + ref.node.Name + " 模块\n")
		}
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

// GenerateDependencyDiagram generates a standalone dependency diagram (graph LR)
// showing the full import dependency graph without subgraphs.
func GenerateDependencyDiagram(graph *grapher.Graph) (string, error) {
	var b strings.Builder
	b.WriteString("%% 依赖图：展示模块间的完整导入依赖关系\n")
	b.WriteString("graph LR\n")

	if len(graph.Nodes) == 0 {
		return b.String(), nil
	}

	// Sort nodes for deterministic output
	sortedNodes := make([]*grapher.Node, len(graph.Nodes))
	copy(sortedNodes, graph.Nodes)
	sort.Slice(sortedNodes, func(i, j int) bool {
		return sortedNodes[i].Name < sortedNodes[j].Name
	})

	// Write node declarations
	for _, n := range sortedNodes {
		nodeID := mermaidEscape(n.Name)
		b.WriteString(fmt.Sprintf("    %s[%s]\n", nodeID, n.Name))
	}

	// Inject semantic role-based styling
	writeRoleStyling(&b, graph)

	// Detect cycles for annotation
	cycles := graph.DetectCycles()
	cycleEdges := make(map[string]bool)
	for _, c := range cycles {
		for i := 0; i < len(c.Nodes)-1; i++ {
			key := c.Nodes[i] + "->" + c.Nodes[i+1]
			cycleEdges[key] = true
		}
	}

	// Sort edges for deterministic output
	sortedEdges := make([]grapher.Edge, len(graph.Edges))
	copy(sortedEdges, graph.Edges)
	sort.Slice(sortedEdges, func(i, j int) bool {
		if sortedEdges[i].From == sortedEdges[j].From {
			return sortedEdges[i].To < sortedEdges[j].To
		}
		return sortedEdges[i].From < sortedEdges[j].From
	})

	// Write edges
	for _, e := range sortedEdges {
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

// roleStyleMap maps inferred module roles to Mermaid classDef names and CSS styles.
var roleStyleMap = map[string]struct {
	className string
	style     string
}{
	"入口层":   {"entry", "fill:#e1f5fe,stroke:#01579b,stroke-width:2px"},
	"核心领域": {"core", "fill:#fff3e0,stroke:#e65100,stroke-width:2px"},
	"工具库":   {"util", "fill:#f3e5f5,stroke:#4a148c,stroke-width:1px"},
	"支撑模块": {"support", "fill:#e8f5e9,stroke:#1b5e20,stroke-width:1px"},
	"业务模块": {"business", "fill:#f5f5f5,stroke:#424242,stroke-width:1px"},
	"独立模块": {"independent", "fill:#fffde7,stroke:#f57f17,stroke-width:1px"},
}

// writeRoleStyling appends Mermaid classDef / class statements that colour
// nodes according to their inferred architectural role.
func writeRoleStyling(b *strings.Builder, graph *grapher.Graph) {
	roles := graph.InferModuleRoles()
	if len(roles) == 0 {
		return
	}

	seen := make(map[string]bool)
	var assignments []struct{ nodeID, className string }
	for _, r := range roles {
		info, ok := roleStyleMap[r.Role]
		if !ok {
			continue
		}
		seen[info.className] = true
		assignments = append(assignments, struct{ nodeID, className string }{
			nodeID:    mermaidEscape(r.Name),
			className: info.className,
		})
	}
	if len(assignments) == 0 {
		return
	}

	sort.Slice(assignments, func(i, j int) bool {
		return assignments[i].nodeID < assignments[j].nodeID
	})

	b.WriteString("\n")
	b.WriteString("    %% 节点角色标注：按模块职责着色\n")
	for _, key := range []string{"entry", "core", "util", "support", "business", "independent"} {
		if seen[key] {
			fmt.Fprintf(b, "    classDef %s %s\n", key, roleStyleMap[classNameToRole(key)].style)
		}
	}
	for _, a := range assignments {
		fmt.Fprintf(b, "    class %s %s\n", a.nodeID, a.className)
	}
}

// classNameToRole reverses the classDef name back to a role key so we can look
// up its style string deterministically.
func classNameToRole(name string) string {
	for role, info := range roleStyleMap {
		if info.className == name {
			return role
		}
	}
	return ""
}

// mermaidEscape sanitizes a string for use as a Mermaid node/class identifier.
func mermaidEscape(s string) string {
	// Replace characters that break Mermaid identifiers
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, ":", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "--", "__")
	// TypeScript generics and type parameters
	s = strings.ReplaceAll(s, "<", "_")
	s = strings.ReplaceAll(s, ">", "_")
	s = strings.ReplaceAll(s, ",", "_")
	s = strings.ReplaceAll(s, ".", "_")
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

// GenerateTopLevelDiagram generates a coarsened architecture diagram where
// each top-level directory is collapsed into a single node. Suitable for
// the overview article to give a high-level picture without overwhelming detail.
func GenerateTopLevelDiagram(graph *grapher.Graph) (string, error) {
	top := graph.TopLevelGraph()
	if len(top.Nodes) == 0 {
		return "", nil
	}
	var b strings.Builder
	b.WriteString("%% 顶层架构概览：展示各模块包之间的依赖关系\n")
	b.WriteString("graph TD\n")
	for _, n := range top.Nodes {
		nodeID := mermaidEscape(n.Name)
		b.WriteString(fmt.Sprintf("    %s[%s]\n", nodeID, n.Name))
	}
	for _, e := range top.Edges {
		fromID := mermaidEscape(e.From)
		toID := mermaidEscape(e.To)
		b.WriteString(fmt.Sprintf("    %s --> %s\n", fromID, toID))
	}
	return b.String(), nil
}

// GenerateSubArchDiagram generates an architecture diagram for a sub-graph
// (e.g. a single directory or a group of related modules).
func GenerateSubArchDiagram(subGraph *grapher.Graph, title string) (string, error) {
	if len(subGraph.Nodes) == 0 {
		return "", nil
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%%%% %s\n", title))
	b.WriteString("graph TD\n")
	for _, n := range subGraph.Nodes {
		nodeID := mermaidEscape(n.Name)
		label := n.Name
		if idx := strings.LastIndex(label, "/"); idx >= 0 {
			label = label[idx+1:]
		}
		b.WriteString(fmt.Sprintf("    %s[%s]\n", nodeID, label))
	}
	for _, e := range subGraph.Edges {
		fromID := mermaidEscape(e.From)
		toID := mermaidEscape(e.To)
		b.WriteString(fmt.Sprintf("    %s --> %s\n", fromID, toID))
	}
	return b.String(), nil
}

// GenerateSubClassDiagram generates a class diagram for a filtered set of classes.
func GenerateSubClassDiagram(classes []analyzer.ClassInfo, title string) (string, error) {
	if len(classes) == 0 {
		return "", nil
	}

	// Deduplicate by class name
	classMap := make(map[string]analyzer.ClassInfo)
	for _, c := range classes {
		if existing, ok := classMap[c.Name]; !ok || len(c.Methods) > len(existing.Methods) {
			classMap[c.Name] = c
		}
	}
	var deduped []analyzer.ClassInfo
	for _, c := range classMap {
		deduped = append(deduped, c)
	}
	sort.Slice(deduped, func(i, j int) bool {
		return deduped[i].Name < deduped[j].Name
	})

	var b strings.Builder
	b.WriteString(fmt.Sprintf("%%%% %s\n", title))
	b.WriteString("classDiagram\n")

	inheritance := make(map[string][]string)
	for _, c := range deduped {
		classID := mermaidEscape(c.Name)
		b.WriteString(fmt.Sprintf("    class %s {\n", classID))
		for _, m := range c.Methods {
			params := strings.Join(m.Params, ", ")
			params = stripSelfParams(params)
			line := fmt.Sprintf("        +%s(%s)", m.Name, params)
			if m.ReturnType != "" {
				line += " " + m.ReturnType
			}
			b.WriteString(line + "\n")
		}
		b.WriteString("    }\n")
		if len(c.Bases) > 0 {
			inheritance[c.Name] = c.Bases
		}
	}

	var childNames []string
	for child := range inheritance {
		childNames = append(childNames, child)
	}
	sort.Strings(childNames)
	for _, child := range childNames {
		for _, parent := range inheritance[child] {
			childID := mermaidEscape(child)
			parentID := mermaidEscape(parent)
			b.WriteString(fmt.Sprintf("    %s --|> %s\n", childID, parentID))
		}
	}

	return b.String(), nil
}
