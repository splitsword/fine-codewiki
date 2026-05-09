package grapher

import (
	"path/filepath"
	"sort"
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
		// If rest looks like a file path (contains / or \), keep it intact
		// but strip any file extension so it matches moduleNameFromFilename.
		if strings.Contains(rest, "/") || strings.Contains(rest, "\\") {
			ext := filepath.Ext(rest)
			if ext != "" {
				rest = strings.TrimSuffix(rest, ext)
			}
		} else {
			// Package-style import (e.g. Python): a.b.c → a/b/c
			rest = strings.ReplaceAll(rest, ".", "/")
		}
		dir = filepath.Join(dir, rest)
	}

	// Normalize
	dir = strings.ReplaceAll(dir, "\\", "/")
	return dir
}

// PageRank computes PageRank scores for all nodes in the dependency graph.
// Returns a map from node name to its PageRank score (higher = more central).
func (g *Graph) PageRank() map[string]float64 {
	scores := make(map[string]float64)
	if len(g.Nodes) == 0 {
		return scores
	}

	// Build adjacency: out-edges and in-edges
	outEdges := make(map[string][]string)
	inEdges := make(map[string][]string)
	nodeSet := make(map[string]bool)
	for _, n := range g.Nodes {
		nodeSet[n.Name] = true
		scores[n.Name] = 1.0 / float64(len(g.Nodes))
	}
	for _, e := range g.Edges {
		outEdges[e.From] = append(outEdges[e.From], e.To)
		inEdges[e.To] = append(inEdges[e.To], e.From)
	}

	const damping = 0.85
	const epsilon = 1e-6
	numNodes := float64(len(g.Nodes))

	for iter := 0; iter < 100; iter++ {
		newScores := make(map[string]float64)
		var diff float64

		for _, n := range g.Nodes {
			var sum float64
			for _, from := range inEdges[n.Name] {
				outCount := len(outEdges[from])
				if outCount > 0 {
					sum += scores[from] / float64(outCount)
				}
			}
			newScores[n.Name] = (1-damping)/numNodes + damping*sum
			diff += abs(newScores[n.Name] - scores[n.Name])
		}

		scores = newScores
		if diff < epsilon {
			break
		}
	}

	return scores
}

// ModuleRole describes the inferred architectural role of a module.
type ModuleRole struct {
	Name  string
	Role  string // e.g. "核心领域", "入口层", "工具库", "业务模块", "支撑模块"
	Score float64
}

// InferModuleRoles uses PageRank and graph structure to infer each module's role.
func (g *Graph) InferModuleRoles() []ModuleRole {
	if len(g.Nodes) == 0 {
		return nil
	}

	scores := g.PageRank()
	entries := g.EntryPoints()
	entrySet := make(map[string]bool)
	for _, e := range entries {
		entrySet[e.Name] = true
	}

	// Compute score thresholds
	var allScores []float64
	for _, s := range scores {
		allScores = append(allScores, s)
	}
	sort.Float64s(allScores)

	// Thresholds: top 20% = core, bottom 30% = utility
	var coreThreshold, utilityThreshold float64
	if len(allScores) > 0 {
		coreThreshold = allScores[int(float64(len(allScores))*0.8)]
		utilityThreshold = allScores[int(float64(len(allScores))*0.3)]
	}

	var roles []ModuleRole
	for _, n := range g.Nodes {
		score := scores[n.Name]
		dependents := len(g.DependentsOf(n.Name))
		dependencies := len(g.DependenciesOf(n.Name))

		role := "业务模块"
		switch {
		case entrySet[n.Name]:
			role = "入口层"
		case score >= coreThreshold && dependents >= 2:
			role = "核心领域"
		case dependencies >= 2 && dependents == 0 && score <= utilityThreshold:
			role = "工具库"
		case dependents == 0 && dependencies == 0:
			role = "独立模块"
		case dependents >= 1 && dependencies >= 1:
			role = "业务模块"
		default:
			role = "支撑模块"
		}

		roles = append(roles, ModuleRole{
			Name:  n.Name,
			Role:  role,
			Score: score,
		})
	}

	// Sort by score descending
	sort.Slice(roles, func(i, j int) bool {
		return roles[i].Score > roles[j].Score
	})

	return roles
}

func abs(a float64) float64 {
	if a < 0 {
		return -a
	}
	return a
}

func hasEdge(edges []Edge, from, to string) bool {
	for _, e := range edges {
		if e.From == from && e.To == to {
			return true
		}
	}
	return false
}
