package analyzer

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ImportInfo represents a single import statement.
type ImportInfo struct {
	Module     string `json:"module"`
	Name       string `json:"name"`
	IsRelative bool   `json:"is_relative"`
}

// FunctionInfo represents a function or method definition.
type FunctionInfo struct {
	Name       string   `json:"name"`
	Params     []string `json:"params"`
	ReturnType string   `json:"return_type,omitempty"`
	Decorators []string `json:"decorators,omitempty"`
}

// ClassInfo represents a class definition.
type ClassInfo struct {
	Name       string         `json:"name"`
	Bases      []string       `json:"bases,omitempty"`
	Methods    []FunctionInfo `json:"methods"`
	Decorators []string       `json:"decorators,omitempty"`
}

// FileResult holds all extracted symbols from a single source file.
type FileResult struct {
	Filename  string         `json:"filename"`
	Classes   []ClassInfo    `json:"classes"`
	Functions []FunctionInfo `json:"functions"`
	Imports   []ImportInfo   `json:"imports"`
}

// ---------- Python Parser ----------

var (
	pyImportRegex    = regexp.MustCompile(`^import\s+([\w.]+(?:\s*,\s*[\w.]+)*)`)
	pyFromImportRegex = regexp.MustCompile(`^from\s+([\w.]+)\s+import\s+\(([^)]+)\)|^from\s+([\w.]+)\s+import\s+(.+)`)
	pyClassRegex     = regexp.MustCompile(`^class\s+(\w+)\s*(?:\(([^)]*)\))?\s*:`)
	pyFuncRegex      = regexp.MustCompile(`^def\s+(\w+)\s*\(([^)]*)\)\s*(?:->\s*(\S+))?\s*:`)
	pyDecoratorRegex = regexp.MustCompile(`^@(\w+(?:\.\w+)*)`)
)

// ParsePython parses a Python source file and extracts structural information.
func ParsePython(filename, source string) (*FileResult, error) {
	result := &FileResult{Filename: filename}
	lines := strings.Split(source, "\n")

	var pendingDecorators []string
	var currentClass *ClassInfo
	var classIndent int

	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if stripped == "" || strings.HasPrefix(stripped, "#") {
			continue
		}

		indent := len(line) - len(strings.TrimLeft(line, " \t"))

		// Check if we've exited the current class block
		if currentClass != nil && indent <= classIndent {
			currentClass = nil
		}

		// Decorator
		if m := pyDecoratorRegex.FindStringSubmatch(stripped); m != nil {
			pendingDecorators = append(pendingDecorators, m[1])
			continue
		}

		// Import statement
		if imports := parsePythonImport(stripped); len(imports) > 0 {
			result.Imports = append(result.Imports, imports...)
			continue
		}

		// Class definition
		if m := pyClassRegex.FindStringSubmatch(stripped); m != nil {
			cls := ClassInfo{
				Name:       m[1],
				Decorators: append([]string(nil), pendingDecorators...),
				Methods:    []FunctionInfo{},
			}
			if m[2] != "" {
				cls.Bases = splitAndTrim(m[2])
			}
			result.Classes = append(result.Classes, cls)
			currentClass = &result.Classes[len(result.Classes)-1]
			classIndent = indent
			pendingDecorators = nil
			continue
		}

		// Function definition
		if m := pyFuncRegex.FindStringSubmatch(stripped); m != nil {
			fn := FunctionInfo{
				Name:       m[1],
				Params:     parsePythonParams(m[2]),
				ReturnType: m[3],
				Decorators: append([]string(nil), pendingDecorators...),
			}
			if currentClass != nil && indent > classIndent {
				currentClass.Methods = append(currentClass.Methods, fn)
			} else {
				result.Functions = append(result.Functions, fn)
			}
			pendingDecorators = nil
			continue
		}

		// Reset decorators if line doesn't match anything
		if len(pendingDecorators) > 0 {
			pendingDecorators = nil
		}
	}

	return result, nil
}

func parsePythonImport(line string) []ImportInfo {
	var results []ImportInfo

	// from X import Y
	if m := pyFromImportRegex.FindStringSubmatch(line); m != nil {
		module := m[1]
		if module == "" {
			module = m[3]
		}
		names := m[2]
		if names == "" {
			names = m[4]
		}
		isRelative := strings.HasPrefix(module, ".")
		for _, name := range splitAndTrim(names) {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			// Handle "as" alias: "BaseModel as BM"
			if idx := strings.Index(name, " as "); idx > 0 {
				name = strings.TrimSpace(name[idx+4:])
			}
			results = append(results, ImportInfo{
				Module:     module,
				Name:       name,
				IsRelative: isRelative,
			})
		}
		return results
	}

	// import X
	if m := pyImportRegex.FindStringSubmatch(line); m != nil {
		for _, mod := range splitAndTrim(m[1]) {
			mod = strings.TrimSpace(mod)
			if mod == "" {
				continue
			}
			results = append(results, ImportInfo{Module: mod, Name: mod})
		}
		return results
	}

	return nil
}

func parsePythonParams(paramsStr string) []string {
	var params []string
	if paramsStr == "" {
		return params
	}
	for _, p := range strings.Split(paramsStr, ",") {
		p = strings.TrimSpace(p)
		if p == "" || p == "self" || p == "cls" {
			params = append(params, p)
			continue
		}
		// Handle "name: str" or "name: str = default"
		if idx := strings.Index(p, ":"); idx > 0 {
			params = append(params, strings.TrimSpace(p[:idx]))
		} else if idx = strings.Index(p, "="); idx > 0 {
			params = append(params, strings.TrimSpace(p[:idx]))
		} else {
			params = append(params, p)
		}
	}
	return params
}

// ---------- JavaScript/TypeScript Parser ----------

var (
	jsImportRegex = regexp.MustCompile(`^import\s+(?:(\{[^}]+\})|(\w+)|(\*\s+as\s+\w+))\s+from\s+['"]([^'"]+)['"]`)
	jsFuncRegex   = regexp.MustCompile(`^(?:export\s+(?:default\s+)?)?(?:async\s+)?function\s+(\w+)\s*\(([^)]*)\)(?:\s*:\s*(\S+))?`)
	jsArrowRegex  = regexp.MustCompile(`^(?:export\s+)?(?:const|let|var)\s+(\w+)\s*[:=].*=>`)
	jsClassRegex  = regexp.MustCompile(`^class\s+(\w+)\s*(?:extends\s+(\w+))?\s*\{`)
	jsMethodRegex = regexp.MustCompile(`^(?:(?:async|public|private|protected|static)\s+)*(\w+)\s*\(([^)]*)\)(?:\s*:\s*(\S+))?\s*\{`)
)

// ParseJavaScript parses a JavaScript/TypeScript source file.
func ParseJavaScript(filename, source string) (*FileResult, error) {
	result := &FileResult{Filename: filename}
	lines := strings.Split(source, "\n")

	var currentClass *ClassInfo
	var braceDepth int
	var classBraceDepth int

	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if stripped == "" || strings.HasPrefix(stripped, "//") || strings.HasPrefix(stripped, "/*") {
			continue
		}

		// Track brace depth for class scoping
		braceDepth += strings.Count(stripped, "{")
		braceDepth -= strings.Count(stripped, "}")

		if currentClass != nil && braceDepth <= classBraceDepth {
			currentClass = nil
		}

		// Import
		if m := jsImportRegex.FindStringSubmatch(stripped); m != nil {
			module := m[4]
			isRelative := strings.HasPrefix(module, ".")
			var name string
			if m[1] != "" {
				// Named imports: { User, Admin }
				names := strings.Trim(m[1], "{}")
				for _, n := range splitAndTrim(names) {
					n = strings.TrimSpace(n)
					if idx := strings.Index(n, " as "); idx > 0 {
						n = strings.TrimSpace(n[idx+4:])
					}
					result.Imports = append(result.Imports, ImportInfo{
						Module:     module,
						Name:       strings.TrimSpace(n),
						IsRelative: isRelative,
					})
				}
			} else if m[2] != "" {
				name = m[2]
				result.Imports = append(result.Imports, ImportInfo{Module: module, Name: name, IsRelative: isRelative})
			} else if m[3] != "" {
				name = strings.TrimSpace(strings.TrimPrefix(m[3], "* as"))
				result.Imports = append(result.Imports, ImportInfo{Module: module, Name: name, IsRelative: isRelative})
			}
			continue
		}

		// Class definition
		if m := jsClassRegex.FindStringSubmatch(stripped); m != nil {
			cls := ClassInfo{
				Name:    m[1],
				Methods: []FunctionInfo{},
			}
			if m[2] != "" {
				cls.Bases = []string{m[2]}
			}
			result.Classes = append(result.Classes, cls)
			currentClass = &result.Classes[len(result.Classes)-1]
			classBraceDepth = braceDepth - 1
			continue
		}

		// Function definition
		if m := jsFuncRegex.FindStringSubmatch(stripped); m != nil {
			fn := FunctionInfo{
				Name:       m[1],
				Params:     parseJSParams(m[2]),
				ReturnType: m[3],
			}
			result.Functions = append(result.Functions, fn)
			continue
		}

		// Arrow function with const (skip React component patterns)
		if m := jsArrowRegex.FindStringSubmatch(stripped); m != nil {
			if strings.Contains(stripped, "React.") || strings.Contains(stripped, "FC<") {
				continue
			}
			fn := FunctionInfo{
				Name:   m[1],
				Params: []string{}, // Arrow function params are harder to extract reliably
			}
			result.Functions = append(result.Functions, fn)
			continue
		}

		// Class method
		if currentClass != nil {
			if m := jsMethodRegex.FindStringSubmatch(stripped); m != nil {
				fn := FunctionInfo{
					Name:       m[1],
					Params:     parseJSParams(m[2]),
					ReturnType: m[3],
				}
				currentClass.Methods = append(currentClass.Methods, fn)
				continue
			}
		}
	}

	return result, nil
}

func parseJSParams(paramsStr string) []string {
	var params []string
	if paramsStr == "" {
		return params
	}
	paramsStr = strings.TrimSpace(paramsStr)
	// Handle single destructuring block: { a, b }
	if strings.HasPrefix(paramsStr, "{") && strings.HasSuffix(paramsStr, "}") {
		inner := strings.TrimSpace(strings.Trim(paramsStr, "{}"))
		for _, name := range strings.Split(inner, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				params = append(params, name)
			}
		}
		return params
	}

	// Split by comma, but merge destructuring blocks that span multiple parts
	parts := strings.Split(paramsStr, ",")
	var merged []string
	for i := 0; i < len(parts); i++ {
		p := parts[i]
		if strings.Contains(p, "{") && !strings.Contains(p, "}") {
			// Start of a multi-part destructuring block
			block := p
			for j := i + 1; j < len(parts); j++ {
				block += "," + parts[j]
				if strings.Contains(parts[j], "}") {
					i = j
					break
				}
			}
			merged = append(merged, block)
		} else {
			merged = append(merged, p)
		}
	}

	for _, p := range merged {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Handle destructuring: { user }
		if strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}") {
			inner := strings.TrimSpace(strings.Trim(p, "{}"))
			for _, name := range strings.Split(inner, ",") {
				name = strings.TrimSpace(name)
				if name != "" {
					params = append(params, name)
				}
			}
			continue
		}
		// Handle "name: string" or "name = default"
		if idx := strings.Index(p, ":"); idx > 0 {
			params = append(params, strings.TrimSpace(p[:idx]))
		} else if idx = strings.Index(p, "="); idx > 0 {
			params = append(params, strings.TrimSpace(p[:idx]))
		} else {
			params = append(params, p)
		}
	}
	return params
}

// ---------- Directory Parser ----------

// ParseDirectory recursively parses all source files in a directory.
func ParseDirectory(dir string, lang string) ([]*FileResult, error) {
	var results []*FileResult

	exts := map[string]bool{
		".py":  lang == "python" || lang == "",
		".js":  lang == "javascript" || lang == "",
		".ts":  lang == "javascript" || lang == "",
		".tsx": lang == "javascript" || lang == "",
	}

	file, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	entries, err := file.Readdir(-1)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(dir, name)

		if entry.IsDir() {
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "__pycache__" || name == "vendor" {
				continue
			}
			subResults, err := ParseDirectory(path, lang)
			if err != nil {
				return nil, fmt.Errorf("parse %s: %w", path, err)
			}
			results = append(results, subResults...)
			continue
		}

		ext := filepath.Ext(path)
		if !exts[ext] {
			continue
		}

		src, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}

		var result *FileResult
		var parseErr error
		switch ext {
		case ".py":
			result, parseErr = ParsePython(path, string(src))
		case ".js", ".ts", ".tsx":
			result, parseErr = ParseJavaScript(path, string(src))
		}

		if parseErr != nil {
			return nil, fmt.Errorf("parse %s: %w", path, parseErr)
		}
		results = append(results, result)
	}

	return results, nil
}

// ---------- Utilities ----------

func splitAndTrim(s string) []string {
	var parts []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

// ScanLines is a helper to read a file line by line.
func ScanLines(filename string) ([]string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}
