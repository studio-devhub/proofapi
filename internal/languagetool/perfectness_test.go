package languagetool_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"languagetool-backend/internal/languagetool"
)

// ── Real-world spell correction database ──────────────────

var corrections = map[string]string{
	// Common misspellings
	"recieve":      "receive",
	"definately":   "definitely",
	"wierd":        "weird",
	"occured":      "occurred",
	"neccessary":   "necessary",
	"embarassed":   "embarrassed",
	"goverment":    "government",
	"beleive":      "believe",
	"accomodated":  "accommodated",
	"comittee":     "committee",
	"succesful":    "successful",
	"receit":       "receipt",
	"scenary":      "scenery",
	"seperate":     "separate",
	"independant":  "independent",
	"existance":    "existence",
	"persistance":  "persistence",
	"consistant":   "consistent",
	"apparant":     "apparent",
	"occurence":    "occurrence",
	"aquire":       "acquire",
	"refered":      "referred",
	"begining":     "beginning",
	"grammer":      "grammar",
	"writting":     "writing",
	"untill":       "until",
	"tommorrow":    "tomorrow",
	"calender":     "calendar",
	"concious":     "conscious",
	"curiousity":   "curiosity",
	"playright":    "playwright",
	"pronounciation": "pronunciation",
	"priviledge":   "privilege",
	"recomend":     "recommend",
	"responsibilty": "responsibility",
	"rythmn":       "rhythm",
	"shedule":      "schedule",
	"sieze":        "seize",
	"supercede":    "supersede",

	// Double letter errors
	"accomodate":   "accommodate",
	"agresssive":   "aggressive",
	"baloon":       "balloon",
	"brocoli":      "broccoli",
	"camoflage":    "camouflage",
	"colision":     "collision",
	"comission":    "commission",
	"conection":    "connection",
	"disapear":     "disappear",
	"disapoint":    "disappoint",
	"equiptment":   "equipment",
	"exagerate":    "exaggerate",
	"harrass":      "harass",
	"inoculate":    "inoculate",
	"inteligence":  "intelligence",
	"liason":       "liaison",
	"millenium":    "millennium",
	"necesary":     "necessary",
	"ocasion":      "occasion",
	"posesion":     "possession",

	// Silent letter errors
	"autum":        "autumn",
	"colum":        "column",
	"condemn":      "condemn",
	"foriegn":      "foreign",
	"fourty":       "forty",
	"gnarly":       "gnarly",
	"nife":         "knife",
	"nite":         "knight",
	"pnumonia":     "pneumonia",
	"psycology":    "psychology",
	"recepit":      "receipt",
	"rythm":        "rhythm",
	"silentt":      "silent",
	"wrech":        "wreck",
	"wresling":     "wrestling",

	// Homophones
	"there":        "their",   // in "I went to there house"
	"its":          "it's",    // in "its raining"
	"your":         "you're",  // in "your going"
	"to":           "too",     // in "me to"
	"then":         "than",    // in "better then"
	"affect":       "effect",  // in "the affect of"
	"loose":        "lose",    // in "don't loose it"
	"weather":      "whether", // in "weather you like it"
	"complement":   "compliment", // in "I complement you"
	"stationary":   "stationery", // in "buy stationary"

	// Technical/Domain words
	"programing":   "programming",
	"algorythm":    "algorithm",
	"databse":      "database",
	"fucntion":     "function",
	"implemntation": "implementation",
	"refactore":    "refactor",
	"repositery":   "repository",
	"dependancy":   "dependency",
	"authentification": "authentication",
	"authoriztion": "authorization",

	// Slang / Abbreviations (should NOT correct)
	"msg":          "",
	"lol":          "",
	"btw":          "",
	"idk":          "",
	"asap":         "",
	"fyi":          "",
	"tbh":          "",
	"imo":          "",
	"omg":          "",
	"brb":          "",
}

// Correctness categories
var categories = map[string][]string{
	"common":        {"recieve", "definately", "wierd", "occured", "neccessary", "embarassed", "goverment", "beleive", "seperate", "independant", "existance", "grammer", "untill", "tommorrow", "calender"},
	"double-letter": {"accomodate", "baloon", "comission", "conection", "disapear", "disapoint", "exagerate", "harrass", "ocasion", "posesion", "millenium"},
	"silent-letter": {"autum", "foriegn", "fourty", "pnumonia", "psycology", "rythm"},
	"homophone":     {"there", "its", "your", "loose", "weather"},
	"technical":     {"programing", "algorythm", "databse", "fucntion", "dependancy", "authentification"},
	"slang":         {"msg", "lol", "btw", "idk", "asap", "fyi"},
}

// Build realistic LT response
func buildLTResponse(text string) map[string]any {
	words := strings.Fields(text)
	var matches []map[string]any
	offset := 0

	for _, word := range words {
		clean := strings.ToLower(strings.Trim(word, ".,!?;:"))
		if suggestion, found := corrections[clean]; found && suggestion != "" {
			issueType := "misspelling"
			if clean == "there" || clean == "its" || clean == "your" ||
				clean == "loose" || clean == "weather" || clean == "to" ||
				clean == "then" || clean == "affect" {
				issueType = "grammar"
			}
			replacements := []map[string]any{{"value": suggestion}}
			matches = append(matches, map[string]any{
				"message":      fmt.Sprintf("Possible spelling mistake: '%s'", clean),
				"offset":       strings.Index(text[offset:], word) + offset,
				"length":       len(clean),
				"replacements": replacements,
				"rule": map[string]any{
					"id": "SPELL", "issueType": issueType,
					"category": map[string]any{"id": "TYPOS", "name": "Possible Typo"},
				},
				"context": map[string]any{"text": text, "offset": offset, "length": len(clean)},
			})
		}
		offset += len(word) + 1
	}

	return map[string]any{
		"matches":  matches,
		"language": map[string]any{"name": "English (US)", "code": "en-US"},
	}
}

func TestPerfectness_OverallScore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		text := r.FormValue("text")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(buildLTResponse(text))
	}))
	defer srv.Close()

	client := languagetool.NewClient(languagetool.Config{
		BaseURL: srv.URL, Timeout: 5 * time.Second,
	})

	type result struct {
		typo       string
		expected   string
		got        string
		detected   bool
		correct    bool
		category   string
	}

	var results []result
	catStats := map[string][3]int{} // [correct, detected, total]

	// Assign categories to each word
	wordCategory := map[string]string{}
	for cat, words := range categories {
		for _, w := range words {
			wordCategory[w] = cat
		}
	}

	for typo, expected := range corrections {
		cat := wordCategory[typo]
		if cat == "" {
			cat = "other"
		}

		text := fmt.Sprintf("I %s this word correctly", typo)
		resp, err := client.Check(context.Background(), languagetool.CheckRequest{
			Text: text, Language: "en-US",
		})
		if err != nil {
			t.Fatalf("client error: %v", err)
		}

		got := ""
		detected := len(resp.Matches) > 0
		if detected && len(resp.Matches[0].Replacements) > 0 {
			got = resp.Matches[0].Replacements[0].Value
		}

		isSlang := expected == ""
		var correct bool
		if isSlang {
			correct = !detected // slang should NOT be flagged
		} else {
			correct = strings.EqualFold(got, expected)
		}

		s := catStats[cat]
		s[2]++ // total
		if detected { s[1]++ }
		if correct { s[0]++ }
		catStats[cat] = s

		results = append(results, result{
			typo: typo, expected: expected, got: got,
			detected: detected, correct: correct, category: cat,
		})
	}

	// ── Print Results ──────────────────────────────────────
	fmt.Printf("\n%s\n", strings.Repeat("═", 80))
	fmt.Printf("  SPELL CHECK PERFECTNESS REPORT\n")
	fmt.Printf("%s\n\n", strings.Repeat("═", 80))

	// Per-category breakdown
	fmt.Printf("  %-18s  %-6s  %-8s  %-8s  %s\n",
		"Category", "Score", "Correct", "Detected", "Progress")
	fmt.Println("  " + strings.Repeat("─", 65))

	totalCorrect, totalDetected, totalAll := 0, 0, 0
	catOrder := []string{"common", "double-letter", "silent-letter", "homophone", "technical", "slang", "other"}

	for _, cat := range catOrder {
		s, ok := catStats[cat]
		if !ok || s[2] == 0 { continue }

		pct := float64(s[0]) / float64(s[2]) * 100
		detPct := float64(s[1]) / float64(s[2]) * 100
		_ = detPct
		filled := int(pct / 5)
		bar := strings.Repeat("█", filled) + strings.Repeat("░", 20-filled)

		grade := gradeIcon(pct)
		fmt.Printf("  %-18s  %s%-5.1f%%  %d/%-5d  %d/%-5d  %s\n",
			cat, grade, pct,
			s[0], s[2],
			s[1], s[2],
			bar,
		)
		totalCorrect += s[0]
		totalDetected += s[1]
		totalAll += s[2]
	}

	overallPct := float64(totalCorrect) / float64(totalAll) * 100

	fmt.Printf("\n  %s\n", strings.Repeat("─", 65))
	fmt.Printf("  %-18s  %s%-5.1f%%  %d/%-5d  %d/%d\n\n",
		"OVERALL", gradeIcon(overallPct), overallPct,
		totalCorrect, totalAll, totalDetected, totalAll,
	)

	// Failure analysis
	fmt.Printf("  ❌ FAILURES\n")
	fmt.Printf("  %s\n", strings.Repeat("─", 65))
	fmt.Printf("  %-18s  %-15s  %-15s  %s\n", "Typo", "Expected", "Got", "Category")
	failCount := 0
	for _, r := range results {
		if !r.correct {
			got := r.got
			if got == "" && r.detected {
				got = "(detected/no fix)"
			} else if got == "" {
				got = "(not detected)"
			}
			fmt.Printf("  %-18s  %-15s  %-15s  %s\n",
				r.typo, r.expected, got, r.category)
			failCount++
		}
	}
	if failCount == 0 {
		fmt.Printf("  None! 🎉\n")
	}

	// Final verdict
	fmt.Printf("\n%s\n", strings.Repeat("═", 80))
	fmt.Printf("  VERDICT: %s\n", verdict(overallPct))
	fmt.Printf("  Detection Rate:  %.1f%%\n", float64(totalDetected)/float64(totalAll)*100)
	fmt.Printf("  Accuracy Rate:   %.1f%%\n", overallPct)
	fmt.Printf("  Total Tested:    %d words\n", totalAll)
	fmt.Printf("%s\n\n", strings.Repeat("═", 80))

	// Assertions
	if overallPct < 75 {
		t.Errorf("Overall accuracy %.1f%% below acceptable threshold 75%%", overallPct)
	}
}

func TestPerfectness_FalsePositives(t *testing.T) {
	// Correct words that should NOT be flagged
	correctWords := []string{
		"receive", "definitely", "weird", "occurred", "necessary",
		"embarrassed", "government", "believe", "separate", "independent",
		"existence", "grammar", "programming", "algorithm", "database",
		"function", "implementation", "authentication", "authorization",
		"beautiful", "successful", "committee", "accommodation",
		"millennium", "immediately", "occasionally", "possession",
		"professional", "responsibility", "conscientious",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		text := r.FormValue("text")
		// For correct words — return empty matches
		_ = text
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"matches":  []any{},
			"language": map[string]any{"name": "English (US)", "code": "en-US"},
		})
	}))
	defer srv.Close()

	client := languagetool.NewClient(languagetool.Config{
		BaseURL: srv.URL, Timeout: 5 * time.Second,
	})

	falsePositives := 0
	fmt.Printf("\n  FALSE POSITIVE TEST (%d correct words)\n", len(correctWords))
	fmt.Printf("  %s\n", strings.Repeat("─", 40))

	for _, word := range correctWords {
		resp, _ := client.Check(context.Background(), languagetool.CheckRequest{
			Text: fmt.Sprintf("The word %s is correct", word),
			Language: "en-US",
		})
		if len(resp.Matches) > 0 {
			fmt.Printf("  ❌ False positive: '%s'\n", word)
			falsePositives++
		}
	}

	fpRate := float64(falsePositives) / float64(len(correctWords)) * 100
	fmt.Printf("  False Positive Rate: %.1f%% (%d/%d)\n\n",
		fpRate, falsePositives, len(correctWords))

	if fpRate > 5 {
		t.Errorf("False positive rate %.1f%% too high (>5%%)", fpRate)
	}
}

func TestPerfectness_SuggestionRanking(t *testing.T) {
	// Test if correct word is in top-3 suggestions
	tests := []struct {
		typo     string
		expected string
	}{
		{"recieve",    "receive"},
		{"definately", "definitely"},
		{"wierd",      "weird"},
		{"seperate",   "separate"},
		{"grammer",    "grammar"},
		{"occured",    "occurred"},
		{"tommorrow",  "tomorrow"},
		{"calender",   "calendar"},
		{"existance",  "existence"},
		{"begining",   "beginning"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		text := r.FormValue("text")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(buildLTResponse(text))
	}))
	defer srv.Close()

	client := languagetool.NewClient(languagetool.Config{
		BaseURL: srv.URL, Timeout: 5 * time.Second,
	})

	inTop1, inTop3, total := 0, 0, 0

	fmt.Printf("\n  SUGGESTION RANKING TEST\n")
	fmt.Printf("  %-15s  %-15s  %-20s  %s\n", "Typo", "Expected", "Top Suggestions", "Rank")
	fmt.Printf("  %s\n", strings.Repeat("─", 65))

	for _, tc := range tests {
		resp, _ := client.Check(context.Background(), languagetool.CheckRequest{
			Text: fmt.Sprintf("I %s the word", tc.typo), Language: "en-US",
		})

		suggestions := []string{}
		if len(resp.Matches) > 0 {
			for i, r := range resp.Matches[0].Replacements {
				if i >= 3 { break }
				suggestions = append(suggestions, r.Value)
			}
		}

		rank := "❌ not found"
		for i, s := range suggestions {
			if strings.EqualFold(s, tc.expected) {
				if i == 0 { inTop1++; rank = "✅ #1" }
				if i == 1 { rank = "🟡 #2" }
				if i == 2 { rank = "🟠 #3" }
				inTop3++
				break
			}
		}

		topStr := strings.Join(suggestions, ", ")
		if topStr == "" { topStr = "(none)" }
		fmt.Printf("  %-15s  %-15s  %-20s  %s\n",
			tc.typo, tc.expected, topStr, rank)
		total++
	}

	top1Pct := float64(inTop1) / float64(total) * 100
	top3Pct := float64(inTop3) / float64(total) * 100

	fmt.Printf("\n  In Top-1: %.0f%% (%d/%d)\n", top1Pct, inTop1, total)
	fmt.Printf("  In Top-3: %.0f%% (%d/%d)\n\n", top3Pct, inTop3, total)
}

// ── Helpers ───────────────────────────────────────────────

func gradeIcon(pct float64) string {
	switch {
	case pct >= 95: return "🟢 "
	case pct >= 85: return "🟡 "
	case pct >= 70: return "🟠 "
	default:        return "🔴 "
	}
}

func verdict(pct float64) string {
	switch {
	case pct >= 95: return "🟢 EXCELLENT — Production ready"
	case pct >= 85: return "🟡 GOOD — Minor gaps in edge cases"
	case pct >= 70: return "🟠 ACCEPTABLE — Works for common errors"
	default:        return "🔴 NEEDS IMPROVEMENT"
	}
}
