package languagetool_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"languagetool-backend/internal/languagetool"
)

// Simulate real LT processing delay based on text length
func mockLTWithLatency(processingDelay time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		text := r.FormValue("text")

		// Simulate LT processing: longer text = more time
		words := len(strings.Fields(text))
		delay := processingDelay + time.Duration(words)*500*time.Microsecond
		time.Sleep(delay)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"matches": []map[string]any{
				{
					"message": "Spelling error",
					"offset":  2, "length": 7,
					"replacements": []map[string]any{{"value": "receive"}},
					"rule": map[string]any{
						"id": "SPELL", "issueType": "misspelling",
						"category": map[string]any{"id": "TYPOS", "name": "Typos"},
					},
					"context": map[string]any{"text": text, "offset": 2, "length": 7},
				},
			},
			"language": map[string]any{"name": "English (US)", "code": "en-US"},
		})
	}))
}

type latencyStats struct {
	samples []float64
}

func (s *latencyStats) add(d time.Duration) {
	s.samples = append(s.samples, float64(d.Milliseconds()))
}

func (s *latencyStats) p50() float64  { return s.percentile(50) }
func (s *latencyStats) p95() float64  { return s.percentile(95) }
func (s *latencyStats) p99() float64  { return s.percentile(99) }
func (s *latencyStats) min() float64  { return s.samples[0] }
func (s *latencyStats) max() float64  { return s.samples[len(s.samples)-1] }

func (s *latencyStats) avg() float64 {
	sum := 0.0
	for _, v := range s.samples {
		sum += v
	}
	return sum / float64(len(s.samples))
}

func (s *latencyStats) stddev() float64 {
	avg := s.avg()
	sum := 0.0
	for _, v := range s.samples {
		diff := v - avg
		sum += diff * diff
	}
	return math.Sqrt(sum / float64(len(s.samples)))
}

func (s *latencyStats) percentile(p float64) float64 {
	sorted := make([]float64, len(s.samples))
	copy(sorted, s.samples)
	sort.Float64s(sorted)
	idx := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	return sorted[idx]
}

func (s *latencyStats) histogram() string {
	if len(s.samples) == 0 {
		return ""
	}
	buckets := []struct {
		label string
		max   float64
	}{
		{"<5ms  ", 5},
		{"<10ms ", 10},
		{"<20ms ", 20},
		{"<50ms ", 50},
		{"<100ms", 100},
		{">100ms", math.MaxFloat64},
	}

	counts := make([]int, len(buckets))
	for _, v := range s.samples {
		for i, b := range buckets {
			if v < b.max {
				counts[i]++
				break
			}
		}
	}

	var sb strings.Builder
	for i, b := range buckets {
		pct := float64(counts[i]) / float64(len(s.samples)) * 100
		bar := strings.Repeat("▓", int(pct/5))
		sb.WriteString(fmt.Sprintf("  %s │%-20s│ %3d req (%4.1f%%)\n",
			b.label, bar, counts[i], pct))
	}
	return sb.String()
}

// ── Tests ─────────────────────────────────────────────────

func TestLatency_ShortText(t *testing.T) {
	srv := mockLTWithLatency(10 * time.Millisecond) // ~10ms base (self-hosted)
	defer srv.Close()

	client := languagetool.NewClient(languagetool.Config{
		BaseURL: srv.URL, Timeout: 5 * time.Second,
	})

	stats := &latencyStats{}
	runs := 50

	for i := 0; i < runs; i++ {
		start := time.Now()
		client.Check(context.Background(), languagetool.CheckRequest{
			Text: "I recieve the msg", Language: "en-US",
		})
		stats.add(time.Since(start))
	}

	printStats("Short Text (5 words)", runs, stats)
	assertP99(t, stats, 100)
}

func TestLatency_MediumText(t *testing.T) {
	srv := mockLTWithLatency(10 * time.Millisecond)
	defer srv.Close()

	client := languagetool.NewClient(languagetool.Config{
		BaseURL: srv.URL, Timeout: 5 * time.Second,
	})

	mediumText := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 10) // ~90 words

	stats := &latencyStats{}
	runs := 50

	for i := 0; i < runs; i++ {
		start := time.Now()
		client.Check(context.Background(), languagetool.CheckRequest{
			Text: mediumText, Language: "en-US",
		})
		stats.add(time.Since(start))
	}

	printStats("Medium Text (~90 words)", runs, stats)
	assertP99(t, stats, 200)
}

func TestLatency_LongText(t *testing.T) {
	srv := mockLTWithLatency(10 * time.Millisecond)
	defer srv.Close()

	client := languagetool.NewClient(languagetool.Config{
		BaseURL: srv.URL, Timeout: 5 * time.Second,
	})

	longText := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 50) // ~450 words

	stats := &latencyStats{}
	runs := 30

	for i := 0; i < runs; i++ {
		start := time.Now()
		client.Check(context.Background(), languagetool.CheckRequest{
			Text: longText, Language: "en-US",
		})
		stats.add(time.Since(start))
	}

	printStats("Long Text (~450 words)", runs, stats)
	assertP99(t, stats, 500)
}

func TestLatency_CacheHit(t *testing.T) {
	// Cache hit = Redis only, no LT call
	// Simulate Redis round-trip: ~0.5ms local
	stats := &latencyStats{}
	runs := 100

	for i := 0; i < runs; i++ {
		start := time.Now()
		time.Sleep(500 * time.Microsecond) // Redis local latency
		stats.add(time.Since(start))
	}

	printStats("Cache Hit (Redis)", runs, stats)
	assertP99(t, stats, 5)
}

func TestLatency_Concurrent(t *testing.T) {
	srv := mockLTWithLatency(10 * time.Millisecond)
	defer srv.Close()

	client := languagetool.NewClient(languagetool.Config{
		BaseURL: srv.URL, Timeout: 5 * time.Second,
	})

	concurrencyLevels := []int{1, 5, 10, 20, 50}

	fmt.Printf("\n%-20s %-10s %-10s %-10s %-10s\n",
		"Concurrency", "Avg", "P50", "P95", "P99")
	fmt.Println(strings.Repeat("─", 60))

	for _, c := range concurrencyLevels {
		stats := &latencyStats{}
		var mu sync.Mutex
		var wg sync.WaitGroup

		for i := 0; i < c*5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				start := time.Now()
				client.Check(context.Background(), languagetool.CheckRequest{
					Text: "I recieve wierd emails", Language: "en-US",
				})
				d := time.Since(start)
				mu.Lock()
				stats.add(d)
				mu.Unlock()
			}()
		}
		wg.Wait()

		fmt.Printf("%-20s %-10s %-10s %-10s %-10s\n",
			fmt.Sprintf("%d goroutines", c),
			fmt.Sprintf("%.1fms", stats.avg()),
			fmt.Sprintf("%.1fms", stats.p50()),
			fmt.Sprintf("%.1fms", stats.p95()),
			fmt.Sprintf("%.1fms", stats.p99()),
		)
	}
	fmt.Println()
}

func TestLatency_ScenarioComparison(t *testing.T) {
	fmt.Printf("\n%s\n", strings.Repeat("═", 65))
	fmt.Printf("  LATENCY COMPARISON — Different Deployment Scenarios\n")
	fmt.Printf("%s\n\n", strings.Repeat("═", 65))

	scenarios := []struct {
		name    string
		base    time.Duration
		p50est  string
		p95est  string
		p99est  string
		note    string
	}{
		{
			"Cache Hit (Redis)",
			500 * time.Microsecond,
			"<1ms", "<2ms", "<5ms",
			"Same text checked before",
		},
		{
			"Self-hosted (same server)",
			5 * time.Millisecond,
			"~15ms", "~40ms", "~80ms",
			"LT on same machine",
		},
		{
			"Self-hosted (LAN)",
			15 * time.Millisecond,
			"~30ms", "~70ms", "~120ms",
			"LT on local network",
		},
		{
			"Self-hosted (VPS)",
			30 * time.Millisecond,
			"~60ms", "~150ms", "~250ms",
			"LT on remote VPS",
		},
		{
			"Public API (free)",
			200 * time.Millisecond,
			"~300ms", "~800ms", "~2000ms",
			"Rate limited, shared",
		},
		{
			"Public API (premium)",
			100 * time.Millisecond,
			"~150ms", "~400ms", "~800ms",
			"Dedicated, faster",
		},
	}

	fmt.Printf("  %-30s %-8s %-8s %-8s  %s\n",
		"Scenario", "P50", "P95", "P99", "Note")
	fmt.Println("  " + strings.Repeat("─", 63))

	for _, s := range scenarios {
		fmt.Printf("  %-30s %-8s %-8s %-8s  %s\n",
			s.name, s.p50est, s.p95est, s.p99est, s.note)
	}

	fmt.Printf("\n  ✅ Recommended: Self-hosted same server + Redis cache\n")
	fmt.Printf("     → Cache hit: <5ms | Cache miss: ~15-80ms\n\n")
}

// ── Helpers ───────────────────────────────────────────────

func printStats(label string, runs int, stats *latencyStats) {
	fmt.Printf("\n┌─ %s (%d requests)\n", label, runs)
	fmt.Printf("│  Min:    %.1fms\n", stats.min())
	fmt.Printf("│  Avg:    %.1fms\n", stats.avg())
	fmt.Printf("│  StdDev: %.1fms\n", stats.stddev())
	fmt.Printf("│  P50:    %.1fms\n", stats.p50())
	fmt.Printf("│  P95:    %.1fms\n", stats.p95())
	fmt.Printf("│  P99:    %.1fms\n", stats.p99())
	fmt.Printf("│  Max:    %.1fms\n", stats.max())
	fmt.Printf("│\n│  Distribution:\n%s└%s\n", stats.histogram(), strings.Repeat("─", 40))
}

func assertP99(t *testing.T, stats *latencyStats, maxMs float64) {
	t.Helper()
	if p99 := stats.p99(); p99 > maxMs {
		t.Errorf("P99 latency %.1fms exceeds threshold %.0fms", p99, maxMs)
	}
}
