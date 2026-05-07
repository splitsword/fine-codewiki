package grapher

import (
	"path/filepath"
	"strings"

	"github.com/splitsword/fine-codewiki/internal/analyzer"
)

// Node represents a file/module in the dependency graph.
type Node struct {
	Name       string              `json:"name"`
	Filename   string              `json:"filename"`
	Classes    []analyzer.ClassInfo    `json:"classes,omitempty"`
	Functions  []analyzer.FunctionInfo `json:"functions,omitempty"`
	IsExternal bool                `json:"is_external,omitempty"`
}

// Edge represents a dependency between two nodes.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"` // "import", "call", "inherit"
}

// Graph holds the complete dependency graph of a codebase.
type Graph struct {
	Nodes []*Node `json:"nodes"`
	Edges []Edge  `json:"edges"`
}

// BuildGraph constructs a dependency graph from parsed file results.
func BuildGraph(files []*analyzer.FileResult) *Graph {
	graph := &Graph{
		Nodes: make([]*Node, 0, len(files)),
		Edges: make([]Edge, 0),
	}

	// Build a set of all internal module names for filtering external deps
	internalModules := make(map[string]bool)
	for _, f := range files {
		modName := moduleNameFromFilename(f.Filename)
		internalModules[modName] = true
	}

	// Create nodes
	for _, f := range files {
		modName := moduleNameFromFilename(f.Filename)
		node := &Node{
			Name:      modName,
			Filename:  f.Filename,
			Classes:   f.Classes,
			Functions: f.Functions,
		}
		graph.Nodes = append(graph.Nodes, node)
	}

	// Create edges from imports
	for _, f := range files {
		fromModule := moduleNameFromFilename(f.Filename)
		for _, imp := range f.Imports {
			targetModule := resolveImport(f.Filename, imp)
			if targetModule == "" {
				continue
			}
			// Only create edges to internal modules
			if !internalModules[targetModule] {
				// Fallback: absolute import may be relative to the importing file's directory
				// e.g. src/central.py imports "orchestrator" -> src/orchestrator
				if !imp.IsRelative {
					dir := filepath.Dir(f.Filename)
					dir = strings.ReplaceAll(dir, "\\", "/")
					candidate := dir + "/" + targetModule
					candidate = strings.TrimPrefix(candidate, "./")
					if internalModules[candidate] {
						targetModule = candidate
					} else {
						continue
					}
				} else {
					continue
				}
			}
			// Avoid duplicate edges
			if !hasEdge(graph.Edges, fromModule, targetModule) {
				graph.Edges = append(graph.Edges, Edge{
					From: fromModule,
					To:   targetModule,
					Type: "import",
				})
			}
		}
	}

	return graph
}

// GroupByDirectory groups nodes by their parent directory.
func (g *Graph) GroupByDirectory() map[string][]*Node {
	groups := make(map[string][]*Node)
	for _, n := range g.Nodes {
		dir := filepath.Dir(n.Filename)
		dir = strings.TrimSuffix(dir, filepath.Ext(dir))
		// Normalize path separators
		dir = strings.ReplaceAll(dir, "\\", "/")
		groups[dir] = append(groups[dir], n)
	}
	return groups
}

// EntryPoints returns nodes that have no incoming edges (no other module depends on them)
// but have outgoing edges (they depend on others). These are typically application entry points.
func (g *Graph) EntryPoints() []*Node {
	// Count incoming edges for each node
	incoming := make(map[string]int)
	for _, e := range g.Edges {
		incoming[e.To]++
	}

	var entries []*Node
	for _, n := range g.Nodes {
		// Node has no incoming edges but has outgoing edges
		if incoming[n.Name] == 0 {
			for _, e := range g.Edges {
				if e.From == n.Name {
					entries = append(entries, n)
					break
				}
			}
		}
	}
	return entries
}

// DependenciesOf returns all modules that the given module directly depends on.
func (g *Graph) DependenciesOf(moduleName string) []string {
	var deps []string
	for _, e := range g.Edges {
		if e.From == moduleName {
			deps = append(deps, e.To)
		}
	}
	return deps
}

// DependentsOf returns all modules that directly depend on the given module.
func (g *Graph) DependentsOf(moduleName string) []string {
	var deps []string
	for _, e := range g.Edges {
		if e.To == moduleName {
			deps = append(deps, e.From)
		}
	}
	return deps
}

// DetectCommunities finds communities in the dependency graph using
// deterministic label propagation. Returns a map from community label to nodes.
func (g *Graph) DetectCommunities() map[string][]*Node {
	// Build undirected adjacency list
	adj := make(map[string]map[string]bool)
	nodeMap := make(map[string]*Node)
	for _, n := range g.Nodes {
		adj[n.Name] = make(map[string]bool)
		nodeMap[n.Name] = n
	}
	for _, e := range g.Edges {
		adj[e.From][e.To] = true
		adj[e.To][e.From] = true
	}

	// Initialize each node with its own label
	labels := make(map[string]string)
	for _, n := range g.Nodes {
		labels[n.Name] = n.Name
	}

	// Propagate labels deterministically
	changed := true
	for iter := 0; iter < 100 && changed; iter++ {
		changed = false
		for _, n := range g.Nodes {
			neighbors := adj[n.Name]
			if len(neighbors) == 0 {
				continue
			}
			counts := make(map[string]int)
			for neighbor := range neighbors {
				counts[labels[neighbor]]++
			}
			bestLabel := labels[n.Name]
			bestCount := -1
			for label, count := range counts {
				if count > bestCount || (count == bestCount && label < bestLabel) {
					bestCount = count
					bestLabel = label
				}
			}
			if bestLabel != labels[n.Name] {
				labels[n.Name] = bestLabel
				changed = true
			}
		}
	}

	// Group nodes by final label
	communities := make(map[string][]*Node)
	for _, n := range g.Nodes {
		label := labels[n.Name]
		communities[label] = append(communities[label], nodeMap[n.Name])
	}
	return communities
}

// Cycle represents a circular dependency path.
type Cycle struct {
	Nodes []string
}

// DetectCycles finds all simple cycles in the dependency graph using DFS.
func (g *Graph) DetectCycles() []Cycle {
	adj := make(map[string][]string)
	for _, e := range g.Edges {
		adj[e.From] = append(adj[e.From], e.To)
	}

	var cycles []Cycle
	visited := make(map[string]bool)

	var dfs func(node string, path []string, pathSet map[string]bool)
	dfs = func(node string, path []string, pathSet map[string]bool) {
		visited[node] = true
		path = append(path, node)
		pathSet[node] = true

		for _, next := range adj[node] {
			if pathSet[next] {
				// Found a cycle: extract cycle portion from path
				cycleStart := 0
				for i, n := range path {
					if n == next {
						cycleStart = i
						break
					}
				}
				cycle := append([]string(nil), path[cycleStart:]...)
				cycle = append(cycle, next) // close the loop
				cycles = append(cycles, Cycle{Nodes: cycle})
				continue
			}
			dfs(next, path, pathSet)
		}

		pathSet[node] = false
	}

	for _, n := range g.Nodes {
		if !visited[n.Name] {
			dfs(n.Name, nil, make(map[string]bool))
		}
	}

	// Deduplicate cycles that are rotations of each other
	return deduplicateCycles(cycles)
}

func deduplicateCycles(cycles []Cycle) []Cycle {
	seen := make(map[string]bool)
	var result []Cycle
	for _, c := range cycles {
		key := cycleKey(c.Nodes)
		if !seen[key] {
			seen[key] = true
			result = append(result, c)
		}
	}
	return result
}

func cycleKey(nodes []string) string {
	if len(nodes) == 0 {
		return ""
	}
	// Find lexicographically smallest rotation
	s := strings.Join(nodes, ">")
	min := s
	n := len(s)
	for i := 1; i < n; i++ {
		rot := s[i:] + s[:i]
		if rot < min {
			min = rot
		}
	}
	return min
}

// moduleNameFromFilename converts a filename to a module name (without extension).
func moduleNameFromFilename(filename string) string {
	// Remove extension
	ext := filepath.Ext(filename)
	name := strings.TrimSuffix(filename, ext)
	// Normalize path separators
	name = strings.ReplaceAll(name, "\\", "/")
	return name
}

// resolveImport resolves an import statement to a target module name.
func resolveImport(fromFile string, imp analyzer.ImportInfo) string {
	if imp.IsRelative {
		return resolveRelativeImport(fromFile, imp.Module)
	}
	// Absolute import within project: module.path.file -> module/path/file
	return strings.ReplaceAll(imp.Module, ".", "/")
}

// resolveRelativeImport resolves a relative import path.
func resolveRelativeImport(fromFile, module string) string {
	// Count leading dots
	dotCount := 0
	for i := 0; i < len(module); i++ {
		if module[i] == '.' {
			dotCount++
		} else {
			break
		}
	}

	// Get directory of source file
	dir := filepath.Dir(fromFile)
	// Go up 'dotCount' directories
	for i := 0; i < dotCount-1; i++ {
		dir = filepath.Dir(dir)
	}

	// Append the rest of the module path
	rest := module[dotCount:]
	if rest != "" {
		rest = strings.ReplaceAll(rest, ".", "/")
		dir = filepath.Join(dir, rest)
	}

	// Normalize
	dir = strings.ReplaceAll(dir, "\\", "/")
	return dir
}

func hasEdge(edges []Edge, from, to string) bool {
	for _, e := range edges {
		if e.From == from && e.To == to {
			return true
		}
	}
	return false
}
