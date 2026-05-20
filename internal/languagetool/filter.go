package languagetool

import "strings"

// FilterMatches removes matches whose flagged word is in the user's dictionary.
// Uses rune-based slicing to correctly handle non-ASCII (Unicode) text.
func FilterMatches(text string, matches []Match, wordSet map[string]struct{}) []Match {
	if len(wordSet) == 0 || len(matches) == 0 {
		return matches
	}

	runes := []rune(text)
	filtered := make([]Match, 0, len(matches))

	for _, m := range matches {
		if m.Offset < 0 || m.Offset+m.Length > len(runes) {
			filtered = append(filtered, m)
			continue
		}
		word := strings.ToLower(string(runes[m.Offset : m.Offset+m.Length]))
		if _, ignored := wordSet[word]; !ignored {
			filtered = append(filtered, m)
		}
	}

	return filtered
}
