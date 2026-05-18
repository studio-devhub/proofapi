package languagetool_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"context"

	"languagetool-backend/internal/languagetool"
)

// Real LanguageTool response simulation for known words
var spellCorrections = map[string][]string{
	"recieve":     {"receive"},
	"definately":  {"definitely"},
	"wierd":       {"weird"},
	"occured":     {"occurred"},
	"neccessary":  {"necessary"},
	"embarassed":  {"embarrassed"},
	"goverment":   {"government"},
	"beleive":     {"believe"},
	"accomodated": {"accommodated"},
	"comittee":    {"committee"},
	"succesful":   {"successful"},
	"receit":      {"receipt"},
	"scenary":     {"scenery"},
	"there":       {"their"},   // homophone (context: "went to there house")
	"Its":         {"It's"},    // homophone
	"Your":        {"You're"},  // homophone
	"They're":     {"There"},   // homophone
	"msg":         {},          // slang — no suggestion
	"b":           {},          // slang — no suggestion
	"Thx":         {"Thanks"},  // slang
}

// Mock LT server that simulates real spell check
func mockSpellServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		text := r.FormValue("text")
		words := strings.Fields(text)

		var matches []map[string]any
		offset := 0

		for _, word := range words {
			clean := strings.Trim(word, ".,!?")
			if suggestions, found := spellCorrections[clean]; found {
				replacements := []map[string]any{}
				for _, s := range suggestions {
					replacements = append(replacements, map[string]any{"value": s})
				}
				issueType := "misspelling"
				if clean == "there" || clean == "Its" || clean == "Your" || clean == "They're" {
					issueType = "grammar"
				}
				matches = append(matches, map[string]any{
					"message":      fmt.Sprintf("Possible spelling mistake: '%s'", clean),
					"offset":       strings.Index(text, word),
					"length":       len(clean),
					"replacements": replacements,
					"rule": map[string]any{
						"id":        "SPELL_001",
						"issueType": issueType,
						"category":  map[string]any{"id": "TYPOS", "name": "Possible Typo"},
					},
					"context": map[string]any{
						"text":   text,
						"offset": offset,
						"length": len(clean),
					},
				})
			}
			offset += len(word) + 1
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"matches":  matches,
			"language": map[string]any{"name": "English (US)", "code": "en-US"},
		})
	}))
}

type spellCase struct {
	input      string
	typo       string
	expected   string
	category   string
	shouldFind bool
}

var spellTestCases = []spellCase{
	// ── Common Misspellings ──────────────────────────────
	{"I recieve emails daily", "recieve", "receive", "common", true},
	{"She is definately sure", "definately", "definitely", "common", true},
	{"Its a wierd situation", "wierd", "weird", "common", true},
	{"This is neccessary work", "neccessary", "necessary", "common", true},
	{"She was embarassed", "embarassed", "embarrassed", "common", true},
	{"The goverment decided", "goverment", "government", "common", true},
	{"I beleive you now", "beleive", "believe", "common", true},
	{"It occured yesterday", "occured", "occurred", "common", true},

	// ── Double Letters ───────────────────────────────────
	{"She was accomodated well", "accomodated", "accommodated", "double-letter", true},
	{"The comittee agreed", "comittee", "committee", "double-letter", true},
	{"It was succesful launch", "succesful", "successful", "double-letter", true},

	// ── Homophones ───────────────────────────────────────
	{"I went to there house", "there", "their", "homophone", true},
	{"Its raining outside", "Its", "It's", "homophone", true},
	{"Your going to love this", "Your", "You're", "homophone", true},

	// ── Silent Letters ───────────────────────────────────
	{"He wrote a receit", "receit", "receipt", "silent-letter", true},
	{"The scenary was nice", "scenary", "scenery", "silent-letter", true},

	// ── Slang (should NOT suggest) ───────────────────────
	{"Send me a msg please", "msg", "", "slang", false},
	{"I will b there soon", "b", "", "slang", false},

	// ── Correct Words (no false positives) ───────────────
	{"The cat sat on the mat", "", "", "no-error", false},
	{"She received the letter", "", "", "no-error", false},
	{"The government is responsible", "", "", "no-error", false},
}

func TestSpelling_Suggestions(t *testing.T) {
	srv := mockSpellServer()
	defer srv.Close()

	client := languagetool.NewClient(languagetool.Config{
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
	})

	type result struct {
		tc      spellCase
		got     string
		detected bool
		pass    bool
	}

	results := []result{}
	correct, total := 0, 0

	categoryStats := map[string][2]int{}

	for _, tc := range spellTestCases {
		resp, err := client.Check(context.Background(), languagetool.CheckRequest{
			Text:     tc.input,
			Language: "en-US",
			Level:    "default",
		})

		if err != nil {
			t.Fatalf("client error: %v", err)
		}

		got := ""
		detected := len(resp.Matches) > 0

		if detected && len(resp.Matches[0].Replacements) > 0 {
			got = resp.Matches[0].Replacements[0].Value
		}

		var pass bool
		if tc.category == "no-error" {
			pass = !detected
		} else if !tc.shouldFind {
			pass = !detected || got == ""
		} else {
			pass = strings.EqualFold(got, tc.expected)
		}

		if pass {
			correct++
		}
		total++

		s := categoryStats[tc.category]
		if pass {
			categoryStats[tc.category] = [2]int{s[0] + 1, s[1] + 1}
		} else {
			categoryStats[tc.category] = [2]int{s[0], s[1] + 1}
		}

		results = append(results, result{tc, got, detected, pass})
	}

	// ── Print Results ──────────────────────────────────
	fmt.Printf("\n")
	fmt.Printf("%-38s %-14s %-14s %-14s %s\n",
		"Input", "Expected", "Got", "Category", "Status")
	fmt.Println(strings.Repeat("─", 90))

	for _, r := range results {
		status := "✅ PASS"
		if !r.pass {
			status = "❌ FAIL"
		}

		expected := r.tc.expected
		if expected == "" {
			expected = "(none)"
		}
		got := r.got
		if got == "" && r.detected {
			got = "detected/no fix"
		} else if got == "" {
			got = "(none)"
		}

		fmt.Printf("%-38s %-14s %-14s %-14s %s\n",
			truncateStr(r.tc.input, 37),
			expected,
			got,
			r.tc.category,
			status,
		)
	}

	fmt.Println(strings.Repeat("─", 90))
	pct := float64(correct) / float64(total) * 100
	fmt.Printf("\n📊 Overall: %d/%d correct (%.1f%%)\n\n", correct, total, pct)

	fmt.Println("📂 By Category:")
	categories := []string{"common", "double-letter", "homophone", "silent-letter", "slang", "no-error"}
	for _, cat := range categories {
		s, ok := categoryStats[cat]
		if !ok {
			continue
		}
		catPct := float64(s[0]) / float64(s[1]) * 100
		bar := strings.Repeat("█", s[0]) + strings.Repeat("░", s[1]-s[0])
		fmt.Printf("  %-14s %s  %d/%d (%.0f%%)\n", cat, bar, s[0], s[1], catPct)
	}
	fmt.Println()

	// Assertion
	if pct < 70 {
		t.Errorf("Overall accuracy %.1f%% below threshold 70%%", pct)
	}
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
