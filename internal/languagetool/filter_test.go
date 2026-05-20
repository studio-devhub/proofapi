package languagetool_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"languagetool-backend/internal/languagetool"
)

func TestFilterMatches_EmptyWordSet(t *testing.T) {
	matches := []languagetool.Match{
		{Offset: 0, Length: 4, Message: "spelling error"},
	}
	result := languagetool.FilterMatches("test text", matches, nil)
	assert.Len(t, result, 1)
}

func TestFilterMatches_WordInDictionary(t *testing.T) {
	matches := []languagetool.Match{
		{Offset: 0, Length: 5, Message: "unknown word"},
	}
	wordSet := map[string]struct{}{"tulvo": {}}
	result := languagetool.FilterMatches("Tulvo is great", matches, wordSet)
	assert.Empty(t, result)
}

func TestFilterMatches_CaseInsensitive(t *testing.T) {
	matches := []languagetool.Match{
		{Offset: 0, Length: 10, Message: "unknown word"},
	}
	wordSet := map[string]struct{}{"kubernetes": {}}
	// "Kubernetes" at offset 0, length 10 should be filtered
	result := languagetool.FilterMatches("Kubernetes is a tool", matches, wordSet)
	assert.Empty(t, result)
}

func TestFilterMatches_WordNotInDictionary(t *testing.T) {
	matches := []languagetool.Match{
		{Offset: 0, Length: 5, Message: "unknown word"},
	}
	wordSet := map[string]struct{}{"other": {}}
	result := languagetool.FilterMatches("hello world", matches, wordSet)
	assert.Len(t, result, 1)
}

func TestFilterMatches_MultipleMatches_SomeFiltered(t *testing.T) {
	text := "Tulvo kubernetes platform"
	matches := []languagetool.Match{
		{Offset: 0, Length: 5},  // "Tulvo"
		{Offset: 6, Length: 10}, // "kubernetes"
		{Offset: 17, Length: 8}, // "platform"
	}
	wordSet := map[string]struct{}{"tulvo": {}, "kubernetes": {}}
	result := languagetool.FilterMatches(text, matches, wordSet)
	assert.Len(t, result, 1)
	assert.Equal(t, 17, result[0].Offset)
}

func TestFilterMatches_UnicodeText(t *testing.T) {
	// Bengali text — LT offset is character-based, not byte-based
	text := "বাংলা টেক্সট"
	matches := []languagetool.Match{
		{Offset: 0, Length: 5}, // "বাংলা"
	}
	wordSet := map[string]struct{}{"বাংলা": {}}
	result := languagetool.FilterMatches(text, matches, wordSet)
	assert.Empty(t, result)
}

func TestFilterMatches_OutOfBoundsOffset(t *testing.T) {
	matches := []languagetool.Match{
		{Offset: 999, Length: 5}, // out of bounds — should be kept, not panicked
	}
	result := languagetool.FilterMatches("short", matches, map[string]struct{}{"word": {}})
	assert.Len(t, result, 1) // kept unchanged
}

func TestFilterMatches_EmptyMatches(t *testing.T) {
	result := languagetool.FilterMatches("hello world", nil, map[string]struct{}{"hello": {}})
	assert.Empty(t, result)
}
