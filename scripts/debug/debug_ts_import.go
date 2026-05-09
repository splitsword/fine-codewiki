//go:build ignore

package main

import (
	"fmt"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"github.com/splitsword/fine-codewiki/internal/analyzer"
)

func main() {
	src := []byte(`import type { AgentOptions } from './types.js'
import { QueryEngine } from './engine.js'
export { Agent, createAgent } from './agent.js'
export * from './session.js'
`)

	entry := grammars.DetectLanguage("test.ts")
	if entry == nil {
		panic("no ts language")
	}
	lang := entry.Language()
	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse(src)
	if err != nil {
		panic(err)
	}

	fmt.Println("Walking AST for import nodes:")
	gotreesitter.Walk(tree.RootNode(), func(node *gotreesitter.Node, depth int) gotreesitter.WalkAction {
		nodeType := node.Type(lang)
		if nodeType == "import_statement" || nodeType == "import_declaration" || nodeType == "export_statement" {
			fmt.Printf("  %s: %q\n", nodeType, node.Text(src))
		}
		return gotreesitter.WalkContinue
	})

	fmt.Println("\n=== analyzer result ===")
	res, err := analyzer.NewTreeSitterParser(lang, nil).Parse("test.ts", string(src))
	if err != nil {
		panic(err)
	}
	fmt.Printf("Imports: %+v\n", res.Imports)
}
