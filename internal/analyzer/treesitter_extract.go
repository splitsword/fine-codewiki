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
				ci := ClassInfo{Name: d.name, StartLine: int(d.node.StartPoint().Row) + 1}
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
						result.Classes[i].Methods = append(result.Classes[i].Methods, FunctionInfo{Name: d.name, StartLine: int(d.node.StartPoint().Row) + 1})
					}
					assigned = true
					break
				}
			}
			if !assigned {
				if !hasFunction(result.Functions, d.name) {
					result.Functions = append(result.Functions, FunctionInfo{Name: d.name, StartLine: int(d.node.StartPoint().Row) + 1})
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
