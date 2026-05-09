package analyzer

import (
	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// GetLanguageForFile detects the language for a given filename using the bundled
// tree-sitter grammars. It returns the loaded Language, the canonical grammar name,
// and a boolean indicating whether a matching grammar was found.
func GetLanguageForFile(filename string) (*gotreesitter.Language, string, bool) {
	entry := grammars.DetectLanguage(filename)
	if entry == nil {
		return nil, "", false
	}
	lang := entry.Language()
	if lang == nil {
		return nil, "", false
	}
	return lang, entry.Name, true
}
