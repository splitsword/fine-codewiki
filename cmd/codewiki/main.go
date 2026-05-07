package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/splitsword/fine-codewiki/internal/cli"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "generate":
		runGenerate(os.Args[2:])
	case "serve":
		runServe(os.Args[2:])
	case "ask":
		runAsk(os.Args[2:])
	case "config":
		runConfig(os.Args[2:])
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func runGenerate(args []string) {
	fs := flag.NewFlagSet("generate", flag.ExitOnError)
	sourceDir := fs.String("source", ".", "Source code directory to analyze")
	outputDir := fs.String("output", "", "Output directory for wiki files (default: <source>/.codewiki/wiki)")
	lang := fs.String("lang", "", "Language filter: python, javascript (empty = auto-detect)")
	name := fs.String("name", "", "Project name (default: directory name)")
	fs.Parse(args)

	cfg := &cli.Config{
		SourceDir:   *sourceDir,
		OutputDir:   *outputDir,
		Language:    *lang,
		ProjectName: *name,
	}

	if err := cli.RunGenerate(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	dir := fs.String("dir", "", "Wiki directory to serve (default: ./.codewiki/wiki)")
	port := fs.Int("port", 8080, "HTTP server port")
	fs.Parse(args)

	cfg := &cli.Config{
		OutputDir: *dir,
		Port:      *port,
	}

	if err := cli.RunServe(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runAsk(args []string) {
	fs := flag.NewFlagSet("ask", flag.ExitOnError)
	sourceDir := fs.String("source", ".", "Source code directory")
	interactive := fs.Bool("interactive", false, "Enter interactive Q&A session")
	fs.Parse(args)

	question := fs.Arg(0)
	if question == "" && !*interactive {
		fmt.Fprintln(os.Stderr, "Usage: codewiki ask [-source <dir>] [-interactive] <question>")
		os.Exit(1)
	}

	cfg := &cli.Config{
		SourceDir:   *sourceDir,
		Interactive: *interactive,
		Question:    question,
	}

	if err := cli.RunAsk(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runConfig(args []string) {
	fs := flag.NewFlagSet("config", flag.ExitOnError)
	path := fs.String("path", "", "Config file path (default: ~/.codewiki/config.yaml)")
	fs.Parse(args)

	cfg := &cli.Config{}
	if *path != "" {
		cfg.ConfigPath = *path
	}

	if err := cli.RunConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	exe := filepath.Base(os.Args[0])
	fmt.Printf(`fine-codewiki — turn any codebase into an interactive wiki

Usage:
  %s <command> [flags]

Commands:
  generate   Analyze code and generate wiki documentation
  serve      Start a local HTTP server to preview the wiki
  ask        Ask a natural-language question about the codebase
  config     Configure LLM provider and API settings
  help       Show this help message

Generate flags:
  -source string   Source code directory (default ".")
  -output string   Output directory for wiki files
  -lang string     Language filter: python, javascript
  -name string     Project name

Serve flags:
  -dir string      Wiki directory to serve (default "./.codewiki/wiki")
  -port int        HTTP server port (default 8080)

Config flags:
  -path string     Config file path

Examples:
  %s generate -source ./my-project -name "My Project"
  %s serve -port 3000
  %s config
`, exe, exe, exe, exe)
}
