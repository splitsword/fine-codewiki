package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/splitsword/fine-codewiki/internal/cli"
	"github.com/splitsword/fine-codewiki/internal/llm"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printWelcome()
		os.Exit(0)
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
	case "browse":
		runBrowse(os.Args[2:])
	case "export":
		runExport(os.Args[2:])
	case "update":
		runUpdate(os.Args[2:])
	case "version", "-v", "--version":
		fmt.Printf("codewiki version %s\n", version)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printWelcome() {
	exe := filepath.Base(os.Args[0])
	fmt.Println("CodeWiki — 将任意代码仓库转化为交互式学习百科")
	fmt.Println()

	// Detect local wiki
	wikiDir := filepath.Join(".", ".codewiki", "wiki")
	hasWiki := false
	if _, err := os.Stat(filepath.Join(wikiDir, "index.html")); err == nil {
		hasWiki = true
	}

	// Detect config
	hasConfig := false
	if _, err := os.Stat(llm.DefaultConfigPath()); err == nil {
		hasConfig = true
	}

	fmt.Println("快速开始：")
	fmt.Println()
	if !hasConfig {
		fmt.Printf("  1. 配置 LLM      %s config\n", exe)
		fmt.Printf("     （支持 OpenAI / Ollama，也可跳过此步骤使用静态生成）\n")
		fmt.Println()
	}
	fmt.Printf("  %s. 生成文档      %s generate\n", stepNum(2, !hasConfig), exe)
	fmt.Printf("  %s. 本地预览      %s serve\n", stepNum(3, !hasConfig), exe)
	fmt.Printf("  %s. 智能问答      %s ask \"这个项目是做什么的？\"\n", stepNum(4, !hasConfig), exe)
	fmt.Println()

	if hasWiki {
		fmt.Printf("检测到当前目录已有 Wiki，直接运行 `%s serve` 即可预览。\n", exe)
		fmt.Println()
	}

	fmt.Printf("查看完整帮助：%s help\n", exe)
}

func stepNum(n int, configSkipped bool) string {
	if configSkipped {
		return fmt.Sprintf("%d", n-1)
	}
	return fmt.Sprintf("%d", n)
}

func runGenerate(args []string) {
	fs := flag.NewFlagSet("generate", flag.ExitOnError)
	sourceDir := fs.String("source", ".", "Source code directory to analyze")
	outputDir := fs.String("output", "", "Output directory for wiki files (default: <source>/.codewiki/wiki)")
	lang := fs.String("lang", "", "Language filter: python, javascript, typescript, go, java, rust, c, cpp (empty = auto-detect)")
	name := fs.String("name", "", "Project name (default: directory name)")
	maxFunctions := fs.Int("max-functions", -1, "Max functions for LLM semantic description: -1=auto (30%%), 0=skip, N=cap")
	force := fs.Bool("force", false, "Force full regeneration, ignore checkpoints")
	fs.Parse(args)

	cfg := &cli.Config{
		SourceDir:       *sourceDir,
		OutputDir:       *outputDir,
		Language:        *lang,
		ProjectName:     *name,
		MaxLLMFunctions: *maxFunctions,
		Force:           *force,
	}

	if err := cli.RunGenerate(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	dir := fs.String("dir", "", "Wiki directory to serve")
	port := fs.Int("port", 8080, "HTTP server port")
	source := fs.String("source", "", "Source directory for RAG Q&A (default: current dir)")
	fs.Parse(args)

	cfg := &cli.Config{
		SourceDir: *source,
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
	sourceDir := fs.String("source", ".", "Source code directory for Q&A context")
	interactive := fs.Bool("interactive", false, "Start interactive Q&A session (ask multiple questions)")
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

func runBrowse(args []string) {
	fs := flag.NewFlagSet("browse", flag.ExitOnError)
	sourceDir := fs.String("source", ".", "Source code directory to analyze")
	outputDir := fs.String("output", "", "Output directory for wiki files (default: <source>/.codewiki/wiki)")
	name := fs.String("name", "", "Project name (default: directory name)")
	fs.Parse(args)

	cfg := &cli.Config{
		SourceDir:   *sourceDir,
		OutputDir:   *outputDir,
		ProjectName: *name,
	}
	if err := cli.RunBrowse(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runExport(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: codewiki export pdf [-source <dir>] [-dir <wiki-dir>] [-output <path>]")
		os.Exit(1)
	}
	subCmd := args[0]
	if subCmd != "pdf" {
		fmt.Fprintf(os.Stderr, "Unknown export sub-command: %s\n", subCmd)
		fmt.Fprintln(os.Stderr, "Usage: codewiki export pdf [-source <dir>] [-dir <wiki-dir>] [-output <path>]")
		os.Exit(1)
	}

	fs := flag.NewFlagSet("export pdf", flag.ExitOnError)
	sourceDir := fs.String("source", ".", "Source code directory")
	wikiDir := fs.String("dir", "", "Wiki directory to export (default: <source>/.codewiki/wiki)")
	outPath := fs.String("output", "", "Output PDF file path (default: <project-name>.pdf)")
	lang := fs.String("lang", "", "Language filter")
	name := fs.String("name", "", "Project name (default: directory name)")
	fs.Parse(args[1:])

	cfg := &cli.Config{
		SourceDir:     *sourceDir,
		OutputDir:     *wikiDir,
		Language:      *lang,
		ProjectName:   *name,
		PDFOutputPath: *outPath,
	}
	if err := cli.RunExportPDF(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runUpdate(args []string) {
	cfg := &cli.Config{Version: version}
	if err := cli.RunUpdate(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runConfig(args []string) {
	fs := flag.NewFlagSet("config", flag.ExitOnError)
	path := fs.String("path", "", "Config file path (default: ~/.codewiki/config.yaml). Settings: LLM provider, model, API key, base URL")
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
	// Strip .exe suffix on Windows so examples look consistent across platforms
	exe = strings.TrimSuffix(exe, ".exe")
	fmt.Printf(`fine-codewiki — turn any codebase into an interactive wiki

Usage:
  %s <command> [flags]

Commands:
  generate   Analyze code and generate wiki documentation
  browse     Generate (if needed) and open wiki in browser
  serve      Start a local HTTP server to preview the wiki
  ask        Ask a natural-language question about the codebase
  export     Export wiki to PDF or other formats
  config     Configure LLM provider and API settings
  update     Check for and install the latest version
  version    Print version information
  help       Show this help message

Generate flags:
  -source string   Source code directory (default ".")
  -output string   Output directory for wiki files
  -lang string     Language filter: python, javascript, typescript, go, java, rust, c, cpp
  -name string     Project name
  -max-functions   Max functions for LLM semantic description: -1=auto, 0=skip, N=cap
  -force           Force full regeneration, ignore checkpoints

Browse flags:
  -source string   Source code directory (default ".")
  -output string   Output directory for wiki files
  -name string     Project name

Export pdf flags:
  -source string   Source code directory (default ".")
  -dir string      Wiki directory to export (default "<source>/.codewiki/wiki")
  -output string   Output PDF file path (default "<project-name>.pdf")
  -lang string     Language filter
  -name string     Project name

Serve flags:
  -dir string      Wiki directory to serve (default "./.codewiki/wiki")
  -port int        HTTP server port (default 8080)
  -source string   Source directory for RAG Q&A (default: current dir)

Ask flags:
  -source string   Source code directory (default ".")
  -interactive     Start interactive Q&A session

Config flags:
  -path string     Config file path

Examples:
  %s generate --source ./my-project --name "My Project" --lang go
  %s browse
  %s serve --port 3000
  %s ask "What does the auth module do?"
  %s ask --interactive
  %s config
  %s export pdf --source ./my-project --dir ./my-project/.codewiki/wiki --output ./my-project.pdf
`, exe, exe, exe, exe, exe, exe, exe, exe)
}
