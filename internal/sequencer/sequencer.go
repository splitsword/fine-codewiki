package sequencer

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/splitsword/fine-codewiki/internal/analyzer"
)

// FunctionRef identifies a function in the codebase.
type FunctionRef struct {
	Module string
	Name   string
}

// String returns a unique key for the function reference.
func (f FunctionRef) String() string {
	return f.Module + "#" + f.Name
}

// CallEdge is a directed call from one function to another.
type CallEdge struct {
	From FunctionRef
	To   FunctionRef
}

// Sequence represents a linear call path suitable for a sequence diagram.
type Sequence struct {
	Title        string
	Description  string
	Participants []string
	Messages     []Message
}

// Message is a single interaction between two participants.
type Message struct {
	From  string
	To    string
	Label string
}

// BuildCallGraph scans source files to discover inter-function calls.
// sourceDir is the root directory; files should have relative filenames.
func BuildCallGraph(sourceDir string, files []*analyzer.FileResult) ([]CallEdge, error) {
	// 1. Index all known functions: simple name -> list of FunctionRef
	knownFuncs := make(map[string][]FunctionRef)

	for _, f := range files {
		mod := moduleNameFromFilename(f.Filename)
		for _, fn := range f.Functions {
			ref := FunctionRef{Module: mod, Name: fn.Name}
			knownFuncs[fn.Name] = append(knownFuncs[fn.Name], ref)
		}
		for _, cls := range f.Classes {
			for _, m := range cls.Methods {
				fullName := cls.Name + "." + m.Name
				ref := FunctionRef{Module: mod, Name: fullName}
				knownFuncs[fullName] = append(knownFuncs[fullName], ref)
				// Also index by simple method name for loose matching
				knownFuncs[m.Name] = append(knownFuncs[m.Name], ref)
			}
		}
	}

	var edges []CallEdge
	seen := make(map[string]bool)

	for _, f := range files {
		mod := moduleNameFromFilename(f.Filename)
		srcPath := filepath.Join(sourceDir, f.Filename)
		src, err := os.ReadFile(srcPath)
		if err != nil {
			continue // skip files we can't read
		}

		lines := strings.Split(string(src), "\n")

		// Find function definition lines in this file
		funcDefs := findFunctionDefs(f, lines)

		// Scan each line for calls to known functions
		for i, line := range lines {
			stripped := strings.TrimSpace(line)
			if stripped == "" || strings.HasPrefix(stripped, "#") || strings.HasPrefix(stripped, "//") {
				continue
			}

			// Skip function definition lines themselves
			isDefLine := false
			for _, d := range funcDefs {
				if d.Line == i {
					isDefLine = true
					break
				}
			}
			if isDefLine {
				continue
			}

			// Find caller for this line
			callerDef := findNearestPrecedingFunc(funcDefs, i)
			if callerDef.Name == "" {
				continue
			}
			// If call line indent is not deeper than the function def indent,
			// the call is at module/class level, not inside the function body.
			callIndent := len(line) - len(strings.TrimLeft(line, " \t"))
			if callIndent <= callerDef.Indent {
				continue
			}

			// Look for calls to known functions (sort keys for determinism)
			var knownNames []string
			for name := range knownFuncs {
				knownNames = append(knownNames, name)
			}
			sort.Strings(knownNames)
			for _, name := range knownNames {
				refs := knownFuncs[name]

				callPattern := name + "("
				if !strings.Contains(line, callPattern) {
					continue
				}

				// Pick the best matching target
				target := pickTarget(refs, callerDef.Name, mod)

				edge := CallEdge{
					From: FunctionRef{Module: mod, Name: callerDef.Name},
					To:   target,
				}
				key := edge.From.String() + "->" + edge.To.String()
				if !seen[key] {
					seen[key] = true
					edges = append(edges, edge)
				}
			}
		}
	}

	return edges, nil
}

// findFunctionDefs locates function definition lines in source.
type funcDef struct {
	Name   string
	Line   int
	Indent int
}

func findFunctionDefs(f *analyzer.FileResult, lines []string) []funcDef {
	var defs []funcDef

	ext := strings.ToLower(filepath.Ext(f.Filename))

	pyFunc := regexp.MustCompile(`^def\s+(\w+)\s*\(`)
	pyClass := regexp.MustCompile(`^class\s+(\w+)`)
	jsFunc := regexp.MustCompile(`^(?:export\s+(?:default\s+)?)?(?:async\s+)?function\s+(\w+)\s*\(`)
	jsMethod := regexp.MustCompile(`^(?:(?:async|public|private|protected|static)\s+)*(\w+)\s*\([^)]*\)\s*(?::\s*\S+)?\s*\{`)
	jsArrow := regexp.MustCompile(`^(?:export\s+)?(?:const|let|var)\s+(\w+)\s*[:=].*=>`)
	goFunc := regexp.MustCompile(`^func\s+(?:\([^)]*\)\s+)?(\w+)\s*\(`)
	javaClass := regexp.MustCompile(`^(?:public\s+|private\s+|protected\s+)?(?:abstract\s+)?class\s+(\w+)`)
	javaMethod := regexp.MustCompile(`^(?:public\s+|private\s+|protected\s+)?(?:static\s+)?(?:final\s+)?(?:abstract\s+)?\w+(?:<[^>]+>)?\s+(\w+)\s*\(`)

	var currentClass string
	var classIndent int

	for i, line := range lines {
		stripped := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " \t"))

		// Only non-empty, non-comment lines affect class scope exit
		if stripped != "" && !strings.HasPrefix(stripped, "#") && !strings.HasPrefix(stripped, "//") {
			if currentClass != "" && indent <= classIndent {
				currentClass = ""
			}
		}

		switch ext {
		case ".py":
			if m := pyClass.FindStringSubmatch(stripped); m != nil {
				currentClass = m[1]
				classIndent = indent
				continue
			}
			if m := pyFunc.FindStringSubmatch(stripped); m != nil {
				name := m[1]
				if currentClass != "" && indent > classIndent {
					name = currentClass + "." + name
				}
				defs = append(defs, funcDef{Name: name, Line: i, Indent: indent})
				continue
			}
		case ".go":
			if m := goFunc.FindStringSubmatch(stripped); m != nil {
				defs = append(defs, funcDef{Name: m[1], Line: i, Indent: indent})
				continue
			}
		case ".java":
			if m := javaClass.FindStringSubmatch(stripped); m != nil {
				currentClass = m[1]
				classIndent = indent
				continue
			}
			if m := javaMethod.FindStringSubmatch(stripped); m != nil {
				name := m[1]
				if currentClass != "" {
					name = currentClass + "." + name
				}
				defs = append(defs, funcDef{Name: name, Line: i, Indent: indent})
				continue
			}
		case ".js", ".ts", ".tsx":
			if m := jsFunc.FindStringSubmatch(stripped); m != nil {
				defs = append(defs, funcDef{Name: m[1], Line: i, Indent: indent})
				continue
			}
			if m := jsMethod.FindStringSubmatch(stripped); m != nil {
				name := m[1]
				if currentClass != "" {
					name = currentClass + "." + name
				}
				defs = append(defs, funcDef{Name: name, Line: i, Indent: indent})
				continue
			}
			if m := jsArrow.FindStringSubmatch(stripped); m != nil {
				defs = append(defs, funcDef{Name: m[1], Line: i, Indent: indent})
				continue
			}
		}
	}

	return defs
}

func findNearestPrecedingFunc(defs []funcDef, callLine int) funcDef {
	var caller funcDef
	for _, d := range defs {
		if d.Line <= callLine {
			caller = d
		} else {
			break
		}
	}
	return caller
}

func isDefinitionLine(stripped, caller, callee string) bool {
	// If caller is Class.method, check both full and simple name
	callerSimple := caller
	if idx := strings.LastIndex(caller, "."); idx >= 0 {
		callerSimple = caller[idx+1:]
	}
	if callee != callerSimple && callee != caller {
		return false
	}
	// Check if this line defines the caller
	return strings.HasPrefix(stripped, "def ") || strings.HasPrefix(stripped, "function ") ||
		strings.HasPrefix(stripped, "async function ") || strings.HasPrefix(stripped, "export ") ||
		strings.HasPrefix(stripped, "func ") || strings.Contains(stripped, "=>")
}

func pickTarget(refs []FunctionRef, caller, callerMod string) FunctionRef {
	if len(refs) == 0 {
		return FunctionRef{}
	}
	if len(refs) == 1 {
		return refs[0]
	}

	// Prefer same-module target if caller is a method and target is same-class method
	callerClass := ""
	if idx := strings.LastIndex(caller, "."); idx >= 0 {
		callerClass = caller[:idx]
	}

	for _, ref := range refs {
		if ref.Module == callerMod {
			if callerClass != "" && strings.HasPrefix(ref.Name, callerClass+".") {
				return ref
			}
		}
	}

	// Prefer any same-module target
	for _, ref := range refs {
		if ref.Module == callerMod {
			return ref
		}
	}

	// Fallback to first
	return refs[0]
}

// FindSequences extracts linear call paths from edges that are worth diagramming.
// A sequence must have at least minEdges and cross at least 2 modules.
func FindSequences(edges []CallEdge, minEdges int) []Sequence {
	if len(edges) == 0 {
		return nil
	}

	// Build adjacency list keyed by From
	adj := make(map[string][]CallEdge)
	for _, e := range edges {
		key := e.From.String()
		adj[key] = append(adj[key], e)
	}

	// Sort adjacency for deterministic output
	for _, list := range adj {
		sort.Slice(list, func(i, j int) bool {
			return list[i].To.String() < list[j].To.String()
		})
	}

	var sequences []Sequence
	seen := make(map[string]bool)

	// Count in-degrees to identify source nodes (no incoming edges).
	// Self-loops are ignored because they shouldn't block a node from being
	// treated as a sequence source (e.g. main() called at module level).
	inDegree := make(map[string]int)
	for _, e := range edges {
		if e.From.String() == e.To.String() {
			continue
		}
		inDegree[e.To.String()]++
	}

	hasSource := false
	for _, e := range edges {
		if inDegree[e.From.String()] == 0 {
			hasSource = true
			break
		}
	}

	var starts []CallEdge
	if !hasSource {
		// All nodes are in cycles; pick the lexicographically smallest
		// starting node to avoid duplicate sequences.
		var best string
		for _, e := range edges {
			key := e.From.String()
			if best == "" || key < best {
				best = key
			}
		}
		for _, e := range edges {
			if e.From.String() == best {
				starts = append(starts, e)
			}
		}
	} else {
		for _, e := range edges {
			if inDegree[e.From.String()] == 0 {
				starts = append(starts, e)
			}
		}
	}

	for _, e := range starts {
		dfsFindSequences([]CallEdge{e}, adj, minEdges, &sequences, seen)
	}

	// Sort sequences by title for determinism
	sort.Slice(sequences, func(i, j int) bool {
		return sequences[i].Title < sequences[j].Title
	})

	return sequences
}

func dfsFindSequences(path []CallEdge, adj map[string][]CallEdge, minEdges int, result *[]Sequence, seen map[string]bool) {
	last := path[len(path)-1]
	lastKey := last.To.String()

	// If path is long enough, record it
	if len(path) >= minEdges && crossesMultipleModules(path) {
		seq := pathToSequence(path)
		key := seq.Title
		if !seen[key] {
			seen[key] = true
			*result = append(*result, seq)
		}
	}

	// Limit depth to prevent explosion
	if len(path) >= 10 {
		return
	}

	for _, next := range adj[lastKey] {
		// Avoid cycles: don't visit same To in this path
		if nodeInPath(path, next.To) {
			continue
		}
		dfsFindSequences(append(path, next), adj, minEdges, result, seen)
	}
}

func crossesMultipleModules(path []CallEdge) bool {
	mods := make(map[string]bool)
	for _, e := range path {
		mods[e.From.Module] = true
		mods[e.To.Module] = true
	}
	return len(mods) >= 2
}

func nodeInPath(path []CallEdge, node FunctionRef) bool {
	// Prevent returning to nodes already visited as a target,
	// but allow cycles for sequence extraction (e.g., a->b->a).
	for _, e := range path {
		if e.To.Module == node.Module && e.To.Name == node.Name {
			return true
		}
	}
	return false
}

func pathToSequence(path []CallEdge) Sequence {
	var participants []string
	participantIndex := make(map[string]int)

	getParticipantIndex := func(mod string) int {
		if idx, ok := participantIndex[mod]; ok {
			return idx
		}
		idx := len(participants)
		participants = append(participants, mod)
		participantIndex[mod] = idx
		return idx
	}

	var messages []Message
	for _, e := range path {
		getParticipantIndex(e.From.Module)
		getParticipantIndex(e.To.Module)
		messages = append(messages, Message{
			From:  e.From.Module,
			To:    e.To.Module,
			Label: e.To.Name,
		})
	}

	// Build title from first and last node
	first := path[0].From
	last := path[len(path)-1].To
	title := fmt.Sprintf("%s.%s -> %s.%s", first.Module, first.Name, last.Module, last.Name)
	desc := generateSequenceDescription(first, last, messages)

	return Sequence{
		Title:        title,
		Description:  desc,
		Participants: participants,
		Messages:     messages,
	}
}

// generateSequenceDescription creates a static scenario description
// based on the entry point and final action of the sequence.
func generateSequenceDescription(first FunctionRef, last FunctionRef, messages []Message) string {
	entryAction := inferAction(first.Name)
	finalAction := inferAction(last.Name)
	entryModule := simplifyModuleName(first.Module)
	finalModule := simplifyModuleName(last.Module)

	// Count intermediate layers
	layers := make(map[string]bool)
	for _, m := range messages {
		layers[simplifyModuleName(m.From)] = true
		layers[simplifyModuleName(m.To)] = true
	}
	layerCount := len(layers)

	desc := fmt.Sprintf("触发条件：调用 %s 的 %s", entryModule, entryAction)
	if layerCount > 2 {
		desc += fmt.Sprintf("；经过 %d 个模块协作", layerCount)
	}
	desc += fmt.Sprintf("；最终由 %s 完成 %s", finalModule, finalAction)
	return desc
}

// inferAction maps common function name patterns to Chinese action descriptions.
func inferAction(name string) string {
	switch {
	case strings.HasPrefix(name, "get_") || strings.HasPrefix(name, "fetch_"):
		return "数据查询"
	case strings.HasPrefix(name, "create_") || strings.HasPrefix(name, "add_") || strings.HasPrefix(name, "new_"):
		return "数据创建"
	case strings.HasPrefix(name, "update_") || strings.HasPrefix(name, "modify_") || strings.HasPrefix(name, "set_"):
		return "数据更新"
	case strings.HasPrefix(name, "delete_") || strings.HasPrefix(name, "remove_"):
		return "数据删除"
	case strings.HasPrefix(name, "validate_") || strings.HasPrefix(name, "check_") || strings.HasPrefix(name, "verify_"):
		return "数据校验"
	case strings.HasPrefix(name, "authenticate") || strings.HasPrefix(name, "login"):
		return "身份认证"
	case strings.HasPrefix(name, "register") || strings.HasPrefix(name, "signup"):
		return "用户注册"
	case strings.HasPrefix(name, "send_") || strings.HasPrefix(name, "notify_"):
		return "消息发送"
	case strings.HasPrefix(name, "process_") || strings.HasPrefix(name, "handle_"):
		return "业务处理"
	case strings.HasPrefix(name, "save_") || strings.HasPrefix(name, "store_") || strings.HasPrefix(name, "write_"):
		return "数据持久化"
	case strings.HasPrefix(name, "load_") || strings.HasPrefix(name, "read_"):
		return "数据加载"
	case strings.HasPrefix(name, "parse_") || strings.HasPrefix(name, "extract_"):
		return "数据解析"
	case strings.HasPrefix(name, "format_") || strings.HasPrefix(name, "render_"):
		return "数据渲染"
	case strings.HasPrefix(name, "run") || strings.HasPrefix(name, "main") || strings.HasPrefix(name, "execute"):
		return "主流程执行"
	default:
		return name + " 操作"
	}
}

// simplifyModuleName extracts the top-level directory or module name.
func simplifyModuleName(module string) string {
	parts := strings.Split(module, "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return module
}

// GenerateSequenceDiagram renders a Sequence as Mermaid sequenceDiagram DSL.
func GenerateSequenceDiagram(seq Sequence) string {
	if len(seq.Messages) == 0 {
		return "%% 时序图：展示关键调用链路的交互顺序\nsequenceDiagram\n"
	}

	var b strings.Builder
	b.WriteString("%% 时序图：展示关键调用链路的交互顺序\n")
	b.WriteString("sequenceDiagram\n")

	// 场景描述注释
	if seq.Description != "" {
		b.WriteString(fmt.Sprintf("    %% %s\n", seq.Description))
	}

	// Declare participants in order of first appearance
	for _, p := range seq.Participants {
		pid := mermaidEscape(p)
		b.WriteString(fmt.Sprintf("    participant %s as %s\n", pid, p))
	}

	// Write messages, deduplicating consecutive identical messages
	var lastMsg *Message
	for _, m := range seq.Messages {
		if lastMsg != nil && lastMsg.From == m.From && lastMsg.To == m.To && lastMsg.Label == m.Label {
			continue
		}
		fromID := mermaidEscape(m.From)
		toID := mermaidEscape(m.To)
		b.WriteString(fmt.Sprintf("    %s->>%s: %s\n", fromID, toID, m.Label))
		lastMsg = &m
	}

	return b.String()
}

func mermaidEscape(s string) string {
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, ":", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, ".", "_")
	return s
}

func moduleNameFromFilename(filename string) string {
	ext := filepath.Ext(filename)
	name := strings.TrimSuffix(filename, ext)
	name = strings.ReplaceAll(name, "\\", "/")
	return name
}
