package analyzer

import (
	"fmt"
	"strings"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// extractFromTreeSitter parses source with tree-sitter and extracts classes,
// functions, and imports. It uses the grammar's tags query for definitions and
// a lightweight AST walk for imports.
func extractFromTreeSitter(lang *gotreesitter.Language, source []byte, filename string) (*FileResult, error) {
	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse(source)
	if err != nil {
		return nil, fmt.Errorf("tree-sitter parse: %w", err)
	}

	result := &FileResult{Filename: filename}

	// Use tags query to extract definitions
	entry := grammars.DetectLanguage(filename)
	if entry != nil {
		tagsQuery := grammars.ResolveTagsQuery(*entry)
		if tagsQuery != "" {
			extractDefinitionsWithQuery(tree, lang, source, tagsQuery, result)
		}
	}

	// Walk AST to extract imports
	extractImportsWithWalk(tree.RootNode(), lang, source, result)

	return result, nil
}

// extractDefinitionsWithQuery runs a tree-sitter tags query and populates
// classes and functions in the result.  It also associates methods with the
// class they are defined inside by comparing node byte ranges.
func extractDefinitionsWithQuery(tree *gotreesitter.Tree, lang *gotreesitter.Language, source []byte, queryText string, result *FileResult) {
	query, err := gotreesitter.NewQuery(queryText, lang)
	if err != nil {
		return
	}

	type def struct {
		name string
		kind string
		node *gotreesitter.Node
	}

	var defs []def

	matches := query.Execute(tree)
	for _, match := range matches {
		var name, kind string
		var node *gotreesitter.Node
		for _, cap := range match.Captures {
			if cap.Name == "name" {
				name = cap.Text(source)
			}
			if kind == "" && strings.HasPrefix(cap.Name, "definition.") {
				kind = cap.Name
				if cap.Node != nil {
					node = cap.Node
				}
			}
		}
		if name == "" || node == nil {
			continue
		}
		defs = append(defs, def{name: name, kind: kind, node: node})
	}

	// First pass: create classes and remember their definition nodes.
	type classNode struct {
		info ClassInfo
		node *gotreesitter.Node
	}
	var classNodes []classNode

	for _, d := range defs {
		switch d.kind {
		case "definition.class", "definition.interface", "definition.type":
			if !hasClass(result.Classes, d.name) {
				ci := ClassInfo{Name: d.name, StartLine: int(d.node.StartPoint().Row) + 1, EndLine: int(d.node.EndPoint().Row) + 1}
				result.Classes = append(result.Classes, ci)
				classNodes = append(classNodes, classNode{info: ci, node: d.node})
			}
		}
	}

	// Second pass: assign functions/methods to classes or top-level functions.
	for _, d := range defs {
		switch d.kind {
		case "definition.function", "definition.method", "definition.constructor":
			assigned := false
			for i := range classNodes {
				if isNodeInsideClass(d.node, classNodes[i].node) {
					if !hasFunction(result.Classes[i].Methods, d.name) {
						result.Classes[i].Methods = append(result.Classes[i].Methods, FunctionInfo{Name: d.name, StartLine: int(d.node.StartPoint().Row) + 1, EndLine: int(d.node.EndPoint().Row) + 1})
					}
					assigned = true
					break
				}
			}
			if !assigned {
				if !hasFunction(result.Functions, d.name) {
					result.Functions = append(result.Functions, FunctionInfo{Name: d.name, StartLine: int(d.node.StartPoint().Row) + 1, EndLine: int(d.node.EndPoint().Row) + 1})
				}
			}
		}
	}
}

// isNodeInsideClass returns true if child is contained within parent by byte range.
func isNodeInsideClass(child, parent *gotreesitter.Node) bool {
	if child == nil || parent == nil {
		return false
	}
	return child.StartByte() >= parent.StartByte() && child.EndByte() <= parent.EndByte()
}

// extractImportsWithWalk traverses the AST looking for import-like nodes.
func extractImportsWithWalk(root *gotreesitter.Node, lang *gotreesitter.Language, source []byte, result *FileResult) {
	gotreesitter.Walk(root, func(node *gotreesitter.Node, depth int) gotreesitter.WalkAction {
		nodeType := node.Type(lang)
		if isImportNodeType(nodeType) {
			text := node.Text(source)
			imp := parseImportFromText(text, nodeType)
			if imp.Module != "" {
				result.Imports = append(result.Imports, imp)
			}
		}
		return gotreesitter.WalkContinue
	})
}

// isImportNodeType returns true for common tree-sitter import node types.
func isImportNodeType(nodeType string) bool {
	switch nodeType {
	case "import_statement", "import_from_statement", "import_declaration",
		"using_declaration", "preproc_include", "use_declaration",
		"import_directive", "namespace_use_clause":
		return true
	}
	return strings.Contains(nodeType, "import") || strings.Contains(nodeType, "export") || strings.Contains(nodeType, "include") || strings.Contains(nodeType, "use")
}

// parseImportFromText attempts to extract a module name from an import node's text.
func parseImportFromText(text, nodeType string) ImportInfo {
	text = strings.TrimSpace(text)

	// Go import_spec: bare quoted string like `"github.com/foo/bar"` or `alias "pkg"`
	if nodeType == "import_spec" {
		mod := extractQuotedString(text)
		if mod != "" {
			return ImportInfo{Module: mod}
		}
		return ImportInfo{}
	}

	// Go import_declaration: the whole `import ( ... )` block — extract individual
	// specs from each line rather than only the first quoted string.
	if nodeType == "import_declaration" {
		// Return empty here; individual import_spec children will be visited
		// separately by the tree walk.
		return ImportInfo{}
	}

	// JS/TS: import/export ... from "module"
	// Covers: import type { A } from "m", import { A } from "m",
	//         import * as foo from "m", import foo from "m",
	//         export { A } from "m", export * from "m"
	if strings.HasPrefix(text, "import") || strings.HasPrefix(text, "export") {
		if mod := extractFromClause(text); mod != "" {
			return ImportInfo{Module: mod, IsRelative: isRelativeModulePath(mod)}
		}
		// Side-effect import: import "module"
		if strings.HasPrefix(text, "import") {
			mod := extractQuotedString(text)
			if mod != "" {
				return ImportInfo{Module: mod, IsRelative: isRelativeModulePath(mod)}
			}
		}
	}

	switch {
	case strings.HasPrefix(text, "from"):
		// Python: from foo import bar
		parts := strings.Fields(text)
		if len(parts) >= 2 {
			mod := parts[1]
			return ImportInfo{Module: mod, IsRelative: isRelativeModulePath(mod)}
		}
	case strings.HasPrefix(text, "#include"):
		// C/C++: #include <foo> or #include "foo"
		content := strings.TrimPrefix(text, "#include")
		content = strings.TrimSpace(content)
		content = strings.Trim(content, "<>")
		content = strings.Trim(content, `"`)
		return ImportInfo{Module: content, IsRelative: isRelativeModulePath(content)}
	case strings.HasPrefix(text, "using"):
		// C#: using Foo;
		parts := strings.Fields(text)
		if len(parts) >= 2 {
			mod := strings.TrimSuffix(parts[1], ";")
			return ImportInfo{Module: mod, IsRelative: isRelativeModulePath(mod)}
		}
	case strings.HasPrefix(text, "use"):
		// Rust: use foo::bar;
		parts := strings.Fields(text)
		if len(parts) >= 2 {
			mod := strings.TrimSuffix(parts[1], ";")
			return ImportInfo{Module: mod, IsRelative: isRelativeModulePath(mod)}
		}
	case strings.HasPrefix(text, "require"):
		// JS: require("foo")
		mod := extractQuotedString(text)
		if mod != "" {
			return ImportInfo{Module: mod, IsRelative: isRelativeModulePath(mod)}
		}
	}
	return ImportInfo{}
}

// isRelativeModulePath returns true for paths that start with ./ or ../
// (or a leading dot in Python relative imports).
func isRelativeModulePath(mod string) bool {
	return strings.HasPrefix(mod, ".")
}

// extractFromClause pulls the module path after "from" in import/export statements.
func extractFromClause(text string) string {
	idx := strings.LastIndex(text, " from ")
	if idx == -1 {
		idx = strings.LastIndex(text, "\tfrom\t")
	}
	if idx == -1 {
		return ""
	}
	mod := strings.TrimSpace(text[idx+len(" from "):])
	mod = strings.TrimSuffix(mod, ";")
	mod = strings.Trim(mod, "`")
	mod = strings.Trim(mod, "'")
	mod = strings.Trim(mod, `"`)
	return mod
}

// extractQuotedString returns the first single/double-quoted string in text.
func extractQuotedString(text string) string {
	for _, q := range []byte{'"', '\''} {
		start := strings.IndexByte(text, q)
		if start == -1 {
			continue
		}
		end := strings.IndexByte(text[start+1:], q)
		if end != -1 {
			return text[start+1 : start+1+end]
		}
	}
	return ""
}

// CallSite represents a function or method invocation discovered in source code.
type CallSite struct {
	Callee string // name of the called function or method
	Line   int    // 1-based line number
}

// ExtractCallSites parses source with tree-sitter and returns all call sites.
// It works for any language with a bundled grammar (Go, Python, JS/TS, Java,
// Rust, C/C++, Ruby, C#).  Calls inside comments or strings are naturally
// excluded because tree-sitter does not parse them as call nodes.
func ExtractCallSites(source []byte, filename string) []CallSite {
	entry := grammars.DetectLanguage(filename)
	if entry == nil {
		return nil
	}
	lang := entry.Language()
	if lang == nil {
		return nil
	}

	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse(source)
	if err != nil {
		return nil
	}

	var sites []CallSite
	gotreesitter.Walk(tree.RootNode(), func(node *gotreesitter.Node, depth int) gotreesitter.WalkAction {
		if isCallNode(node, lang) {
			callee := extractCalleeName(node, lang, source)
			if callee != "" {
				line := int(node.StartPoint().Row) + 1
				sites = append(sites, CallSite{Callee: callee, Line: line})
			}
		}
		return gotreesitter.WalkContinue
	})
	return sites
}

func isCallNode(node *gotreesitter.Node, lang *gotreesitter.Language) bool {
	if node == nil {
		return false
	}
	t := node.Type(lang)
	switch t {
	case "call_expression", "call", "method_invocation", "function_call",
		"macro_invocation", "command", "invocation_expression":
		return true
	}
	return strings.Contains(t, "call") || strings.Contains(t, "invocation")
}

func extractCalleeName(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte) string {
	// Find the argument-list child; the callee expression is the meaningful
	// child just before it (skipping separators like '.' or '::').
	argIdx := -1
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		t := child.Type(lang)
		if t == "argument_list" || t == "arguments" || t == "parameters" {
			argIdx = i
			break
		}
	}

	var funcNode *gotreesitter.Node
	if argIdx > 0 {
		for i := argIdx - 1; i >= 0; i-- {
			child := node.Child(i)
			if child == nil {
				continue
			}
			t := child.Type(lang)
			if t == "." || t == "::" || t == "->" {
				continue
			}
			funcNode = child
			break
		}
	}
	if funcNode == nil && node.ChildCount() > 0 {
		funcNode = node.Child(0)
	}
	if funcNode == nil {
		return ""
	}
	return extractNameFromNode(funcNode, lang, source)
}

func extractNameFromNode(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte) string {
	if node == nil {
		return ""
	}
	t := node.Type(lang)
	// These node types are the "rightmost" name in a member access.
	switch t {
	case "field_identifier", "property_identifier", "attribute_name":
		return node.Text(source)
	}
	// For composite expressions, find the deepest identifier-like child.
	var name string
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		n := extractNameFromNode(child, lang, source)
		if n != "" {
			name = n
		}
	}
	// If no deeper name found, use this identifier's text.
	if name == "" && t == "identifier" {
		return node.Text(source)
	}
	return name
}

func hasClass(classes []ClassInfo, name string) bool {
	for _, c := range classes {
		if c.Name == name {
			return true
		}
	}
	return false
}

func hasFunction(funcs []FunctionInfo, name string) bool {
	for _, f := range funcs {
		if f.Name == name {
			return true
		}
	}
	return false
}
