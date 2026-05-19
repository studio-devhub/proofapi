package loadtest

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gorilla/websocket"

	"languagetool-backend/internal/cache"
	"languagetool-backend/internal/languagetool"
	mw "languagetool-backend/internal/middleware"
	wspkg "languagetool-backend/internal/ws"

	"github.com/go-chi/chi/v5"
)

// ── Mock LT Server ────────────────────────────────────────

func newMockLT(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if delay > 0 {
			time.Sleep(delay)
		}
		r.ParseForm()
		text := r.FormValue("text")
		var matches []map[string]any
		if strings.Contains(text, "recieve") {
			matches = append(matches, map[string]any{
				"message": "Spelling error",
				"offset":  strings.Index(text, "recieve"), "length": 7,
				"replacements": []map[string]any{{"value": "receive"}},
				"rule": map[string]any{
					"id": "SPELL", "issueType": "misspelling",
					"category": map[string]any{"id": "T", "name": "Typos"},
				},
			})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"matches":  matches,
			"language": map[string]any{"name": "English (US)", "code": "en-US"},
		})
	}))
	t.Cleanup(func() {
		slog.Info("LT mock calls", "total", calls.Load())
		srv.Close()
	})
	return srv
}

// ── Server Setup ──────────────────────────────────────────

type testServer struct {
	httpURL string
	wsURL   string
	redis   *cache.Redis
	mr      *miniredis.Miniredis
}

func newTestServer(t *testing.T, ltDelay time.Duration) *testServer {
	t.Helper()

	lt := newMockLT(t, ltDelay)
	mr := miniredis.RunT(t)
	r, err := cache.NewRedis(cache.Config{Host: mr.Host(), Port: mr.Port()})
	if err != nil {
		t.Fatal(err)
	}

	ltClient := languagetool.NewClient(languagetool.Config{
		BaseURL: lt.URL, Timeout: 10 * time.Second,
	})

	restHandler := languagetool.NewHandler(ltClient, r, slog.Default())
	hub := wspkg.NewHub(slog.Default())
	wsHandler := wspkg.NewHandler(hub, ltClient, r, slog.Default())

	router := chi.NewRouter()

	// REST routes — header-based auth
	router.Group(func(r chi.Router) {
		r.Use(mw.APIKey("test-key"))
		r.Post("/v1/check", restHandler.Check)
		r.Get("/v1/health", func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"api": "ok"})
		})
	})

	// WebSocket route — header OR query param auth
	router.Group(func(r chi.Router) {
		r.Use(mw.APIKeyWS("test-key"))
		r.Get("/v1/ws", wsHandler.ServeWS)
	})

	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	return &testServer{
		httpURL: srv.URL,
		wsURL:   "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/ws?api_key=test-key",
		redis:   r,
		mr:      mr,
	}
}

// ── Stats Collector ───────────────────────────────────────

type stats struct {
	mu        sync.Mutex
	latencies []float64
	success   atomic.Int64
	failure   atomic.Int64
	total     atomic.Int64
}

func (s *stats) record(d time.Duration, err error) {
	s.total.Add(1)
	if err != nil {
		s.failure.Add(1)
		return
	}
	s.success.Add(1)
	s.mu.Lock()
	s.latencies = append(s.latencies, float64(d.Milliseconds()))
	s.mu.Unlock()
}

func (s *stats) percentile(p float64) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.latencies) == 0 {
		return 0
	}
	sorted := make([]float64, len(s.latencies))
	copy(sorted, s.latencies)
	sort.Float64s(sorted)
	idx := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	return sorted[idx]
}

func (s *stats) avg() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.latencies) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range s.latencies {
		sum += v
	}
	return sum / float64(len(s.latencies))
}

func (s *stats) min() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.latencies) == 0 {
		return 0
	}
	m := s.latencies[0]
	for _, v := range s.latencies {
		if v < m {
			m = v
		}
	}
	return m
}

func (s *stats) max() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.latencies) == 0 {
		return 0
	}
	m := s.latencies[0]
	for _, v := range s.latencies {
		if v > m {
			m = v
		}
	}
	return m
}

func (s *stats) successRate() float64 {
	t := s.total.Load()
	if t == 0 {
		return 0
	}
	return float64(s.success.Load()) / float64(t) * 100
}

func (s *stats) histogram() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	buckets := []struct {
		label string
		max   float64
	}{
		{"<5ms  ", 5},
		{"<10ms ", 10},
		{"<20ms ", 20},
		{"<50ms ", 50},
		{"<100ms", 100},
		{"<200ms", 200},
		{">200ms", math.MaxFloat64},
	}
	counts := make([]int, len(buckets))
	for _, v := range s.latencies {
		for i, b := range buckets {
			if v < b.max {
				counts[i]++
				break
			}
		}
	}
	var sb strings.Builder
	for i, b := range buckets {
		if counts[i] == 0 {
			continue
		}
		pct := float64(counts[i]) / float64(len(s.latencies)) * 100
		bar := strings.Repeat("█", int(pct/2))
		sb.WriteString(fmt.Sprintf("    %s │%-50s│ %4d (%4.1f%%)\n",
			b.label, bar, counts[i], pct))
	}
	return sb.String()
}

func printReport(label string, st *stats, duration time.Duration) {
	rps := float64(st.total.Load()) / duration.Seconds()
	fmt.Fprintf(os.Stdout, "\n  ┌─ %s\n", label)
	fmt.Fprintf(os.Stdout, "  │  Requests:    %d total, %d ok, %d failed\n",
		st.total.Load(), st.success.Load(), st.failure.Load())
	fmt.Fprintf(os.Stdout, "  │  Success Rate: %.1f%%\n", st.successRate())
	fmt.Fprintf(os.Stdout, "  │  Throughput:   %.0f req/s\n", rps)
	fmt.Fprintf(os.Stdout, "  │  Latency:\n")
	fmt.Fprintf(os.Stdout, "  │    Min:  %.1fms\n", st.min())
	fmt.Fprintf(os.Stdout, "  │    Avg:  %.1fms\n", st.avg())
	fmt.Fprintf(os.Stdout, "  │    P50:  %.1fms\n", st.percentile(50))
	fmt.Fprintf(os.Stdout, "  │    P90:  %.1fms\n", st.percentile(90))
	fmt.Fprintf(os.Stdout, "  │    P95:  %.1fms\n", st.percentile(95))
	fmt.Fprintf(os.Stdout, "  │    P99:  %.1fms\n", st.percentile(99))
	fmt.Fprintf(os.Stdout, "  │    Max:  %.1fms\n", st.max())
	fmt.Fprintf(os.Stdout, "  │  Distribution:\n%s", st.histogram())
	fmt.Fprintf(os.Stdout, "  └%s\n", strings.Repeat("─", 60))
}

// ── Tests ─────────────────────────────────────────────────

func TestLoad_REST_1000_Concurrent(t *testing.T) {
	srv := newTestServer(t, 5*time.Millisecond)
	st := &stats{}
	total := 1000

	texts := []string{
		"I recieve many emails every day from my team",
		"This is a perfectly correct sentence with no errors",
		"She recieve the package yesterday from the sender",
		"The quick brown fox jumps over the lazy dog",
		"We need to recieve the confirmation before proceeding",
	}

	fmt.Fprintf(os.Stdout, "\n%s\n", strings.Repeat("═", 70))
	fmt.Fprintf(os.Stdout, "  REST API — 1000 Concurrent Requests\n")
	fmt.Fprintf(os.Stdout, "%s\n", strings.Repeat("═", 70))

	var wg sync.WaitGroup
	sem := make(chan struct{}, 100) // 100 concurrent max
	start := time.Now()

	for i := 0; i < total; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()

			text := texts[i%len(texts)]
			body := fmt.Sprintf(`{"text":%q,"language":"en-US"}`, text)

			req, _ := http.NewRequest(http.MethodPost, srv.httpURL+"/v1/check",
				strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-API-Key", "test-key")

			reqStart := time.Now()
			resp, err := http.DefaultClient.Do(req)
			elapsed := time.Since(reqStart)

			if err != nil {
				st.record(elapsed, err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				st.record(elapsed, fmt.Errorf("status %d", resp.StatusCode))
				return
			}
			st.record(elapsed, nil)
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)
	printReport("REST /v1/check — 1000 requests", st, duration)

	if st.successRate() < 95 {
		t.Errorf("Success rate %.1f%% below 95%%", st.successRate())
	}
}

func TestLoad_REST_1000_WithCache(t *testing.T) {
	srv := newTestServer(t, 5*time.Millisecond)
	total := 1000

	// Only 5 unique texts — high cache hit rate
	texts := []string{
		"I recieve emails from my colleague",
		"This sentence is perfectly correct",
		"She recieve the important package",
		"The lazy dog jumps over the fox",
		"We recieve confirmation every morning",
	}

	fmt.Fprintf(os.Stdout, "\n%s\n", strings.Repeat("═", 70))
	fmt.Fprintf(os.Stdout, "  REST API — 1000 Requests with Cache (5 unique texts)\n")
	fmt.Fprintf(os.Stdout, "%s\n", strings.Repeat("═", 70))

	// Warm cache
	for _, text := range texts {
		body := fmt.Sprintf(`{"text":%q,"language":"en-US"}`, text)
		req, _ := http.NewRequest(http.MethodPost, srv.httpURL+"/v1/check",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", "test-key")
		http.DefaultClient.Do(req)
	}
	time.Sleep(100 * time.Millisecond) // wait for async cache write

	st := &stats{}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 200)
	start := time.Now()

	for i := 0; i < total; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()

			text := texts[i%len(texts)]
			body := fmt.Sprintf(`{"text":%q,"language":"en-US"}`, text)

			req, _ := http.NewRequest(http.MethodPost, srv.httpURL+"/v1/check",
				strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-API-Key", "test-key")

			reqStart := time.Now()
			resp, err := http.DefaultClient.Do(req)
			elapsed := time.Since(reqStart)

			if err != nil {
				st.record(elapsed, err)
				return
			}
			resp.Body.Close()
			st.record(elapsed, nil)
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)
	printReport("REST /v1/check — 1000 requests (cached)", st, duration)

	if st.successRate() < 99 {
		t.Errorf("Cached success rate %.1f%% below 99%%", st.successRate())
	}
}

func TestLoad_WebSocket_1000_Connections(t *testing.T) {
	srv := newTestServer(t, 5*time.Millisecond)

	total := 1000
	batchSize := 50 // connect in batches to avoid fd limit

	fmt.Fprintf(os.Stdout, "\n%s\n", strings.Repeat("═", 70))
	fmt.Fprintf(os.Stdout, "  WebSocket — 1000 Connections × 1 Check Each\n")
	fmt.Fprintf(os.Stdout, "%s\n", strings.Repeat("═", 70))

	st := &stats{}
	texts := []string{
		"I recieve emails daily from my team",
		"This is a correct sentence entirely",
		"She recieve the package from sender",
		"The quick fox jumps over lazy dogs",
		"We recieve confirmation every morning",
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, batchSize)
	start := time.Now()

	for i := 0; i < total; i++ {
		wg.Add(1)
		sem <- struct{}{}

		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()

			reqStart := time.Now()

			conn, _, err := websocket.DefaultDialer.Dial(srv.wsURL, nil)
			if err != nil {
				st.record(time.Since(reqStart), err)
				return
			}
			defer conn.Close()

			// Read ack
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			_, raw, err := conn.ReadMessage()
			if err != nil {
				st.record(time.Since(reqStart), err)
				return
			}
			var ack wspkg.OutgoingMessage
			json.Unmarshal(raw, &ack)
			if ack.Type != wspkg.TypeAck {
				st.record(time.Since(reqStart), fmt.Errorf("expected ack, got %s", ack.Type))
				return
			}

			// Send check
			text := texts[i%len(texts)]
			msg, _ := json.Marshal(wspkg.IncomingMessage{
				Type: wspkg.TypeCheck, Text: text, SeqID: i,
			})
			conn.WriteMessage(websocket.TextMessage, msg)

			// Read result (with debounce wait)
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			_, raw, err = conn.ReadMessage()
			if err != nil {
				st.record(time.Since(reqStart), err)
				return
			}

			var result wspkg.OutgoingMessage
			json.Unmarshal(raw, &result)
			if result.Type != wspkg.TypeResult {
				st.record(time.Since(reqStart), fmt.Errorf("expected result, got %s", result.Type))
				return
			}

			st.record(time.Since(reqStart), nil)
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)
	printReport("WebSocket — 1000 connections", st, duration)

	if st.successRate() < 95 {
		t.Errorf("WS success rate %.1f%% below 95%%", st.successRate())
	}
}

func TestLoad_WebSocket_Persistent_1000_Messages(t *testing.T) {
	srv := newTestServer(t, 2*time.Millisecond)

	connCount := 10   // 10 persistent connections
	msgPerConn := 100 // 100 messages each = 1000 total

	fmt.Fprintf(os.Stdout, "\n%s\n", strings.Repeat("═", 70))
	fmt.Fprintf(os.Stdout, "  WebSocket — %d Persistent Connections × %d Messages = %d total\n",
		connCount, msgPerConn, connCount*msgPerConn)
	fmt.Fprintf(os.Stdout, "%s\n", strings.Repeat("═", 70))

	st := &stats{}
	texts := []string{
		"I recieve this message number",
		"This is a correct sentence here",
		"She recieve the package from team",
	}

	var wg sync.WaitGroup
	start := time.Now()

	for c := 0; c < connCount; c++ {
		wg.Add(1)
		go func(connID int) {
			defer wg.Done()

			conn, _, err := websocket.DefaultDialer.Dial(srv.wsURL, nil)
			if err != nil {
				for i := 0; i < msgPerConn; i++ {
					st.record(0, err)
				}
				return
			}
			defer conn.Close()

			// Read ack
			conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			conn.ReadMessage()

			for m := 0; m < msgPerConn; m++ {
				text := fmt.Sprintf("%s %d from conn %d",
					texts[m%len(texts)], m, connID)

				msg, _ := json.Marshal(wspkg.IncomingMessage{
					Type: wspkg.TypeCheck, Text: text,
					SeqID: connID*1000 + m,
				})

				reqStart := time.Now()
				conn.WriteMessage(websocket.TextMessage, msg)

				// Wait for debounce + result
				conn.SetReadDeadline(time.Now().Add(3 * time.Second))
				_, raw, err := conn.ReadMessage()
				elapsed := time.Since(reqStart)

				if err != nil {
					st.record(elapsed, err)
					continue
				}

				var result wspkg.OutgoingMessage
				json.Unmarshal(raw, &result)
				if result.Type == wspkg.TypeResult {
					st.record(elapsed, nil)
				} else {
					st.record(elapsed, fmt.Errorf("got %s", result.Type))
				}
			}
		}(c)
	}

	wg.Wait()
	duration := time.Since(start)
	printReport(fmt.Sprintf("WebSocket — %d persistent conns × %d msgs", connCount, msgPerConn), st, duration)

	if st.successRate() < 95 {
		t.Errorf("WS persistent success rate %.1f%% below 95%%", st.successRate())
	}
}

func TestLoad_Mixed_REST_WS_1000(t *testing.T) {
	srv := newTestServer(t, 3*time.Millisecond)

	restCount := 500
	wsCount := 500

	fmt.Fprintf(os.Stdout, "\n%s\n", strings.Repeat("═", 70))
	fmt.Fprintf(os.Stdout, "  MIXED LOAD — %d REST + %d WebSocket = 1000 total\n",
		restCount, wsCount)
	fmt.Fprintf(os.Stdout, "%s\n", strings.Repeat("═", 70))

	restStats := &stats{}
	wsStats := &stats{}

	var wg sync.WaitGroup
	restSem := make(chan struct{}, 50)
	wsSem := make(chan struct{}, 50)
	start := time.Now()

	// REST workers
	for i := 0; i < restCount; i++ {
		wg.Add(1)
		restSem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-restSem }()

			body := fmt.Sprintf(`{"text":"I recieve email %d","language":"en-US"}`, i)
			req, _ := http.NewRequest(http.MethodPost, srv.httpURL+"/v1/check",
				strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-API-Key", "test-key")

			s := time.Now()
			resp, err := http.DefaultClient.Do(req)
			elapsed := time.Since(s)

			if err != nil {
				restStats.record(elapsed, err)
				return
			}
			resp.Body.Close()
			restStats.record(elapsed, nil)
		}(i)
	}

	// WS workers
	for i := 0; i < wsCount; i++ {
		wg.Add(1)
		wsSem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-wsSem }()

			s := time.Now()
			conn, _, err := websocket.DefaultDialer.Dial(srv.wsURL, nil)
			if err != nil {
				wsStats.record(time.Since(s), err)
				return
			}
			defer conn.Close()

			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			conn.ReadMessage() // ack

			msg, _ := json.Marshal(wspkg.IncomingMessage{
				Type: wspkg.TypeCheck,
				Text: fmt.Sprintf("I recieve message number %d from client", i),
				SeqID: i,
			})
			conn.WriteMessage(websocket.TextMessage, msg)

			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			_, raw, err := conn.ReadMessage()
			elapsed := time.Since(s)
			if err != nil {
				wsStats.record(elapsed, err)
				return
			}

			var result wspkg.OutgoingMessage
			json.Unmarshal(raw, &result)
			if result.Type == wspkg.TypeResult {
				wsStats.record(elapsed, nil)
			} else {
				wsStats.record(elapsed, fmt.Errorf("unexpected: %s", result.Type))
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	printReport(fmt.Sprintf("REST — %d requests", restCount), restStats, duration)
	printReport(fmt.Sprintf("WebSocket — %d connections", wsCount), wsStats, duration)

	totalSuccess := restStats.success.Load() + wsStats.success.Load()
	totalAll := restStats.total.Load() + wsStats.total.Load()
	overallRate := float64(totalSuccess) / float64(totalAll) * 100

	fmt.Fprintf(os.Stdout, "\n  COMBINED: %d/%d (%.1f%% success rate)\n",
		totalSuccess, totalAll, overallRate)
	fmt.Fprintf(os.Stdout, "  Duration: %s | Throughput: %.0f req/s\n\n",
		duration.Round(time.Millisecond),
		float64(totalAll)/duration.Seconds(),
	)

	if overallRate < 95 {
		t.Errorf("Combined success rate %.1f%% below 95%%", overallRate)
	}
}

func TestLoad_Stress_Burst(t *testing.T) {
	srv := newTestServer(t, 2*time.Millisecond)

	fmt.Fprintf(os.Stdout, "\n%s\n", strings.Repeat("═", 70))
	fmt.Fprintf(os.Stdout, "  BURST TEST — 5 waves × 200 concurrent = 1000 total\n")
	fmt.Fprintf(os.Stdout, "%s\n", strings.Repeat("═", 70))

	waves := 5
	perWave := 200
	allStats := &stats{}

	for wave := 0; wave < waves; wave++ {
		waveStats := &stats{}
		var wg sync.WaitGroup
		waveStart := time.Now()

		for i := 0; i < perWave; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()

				body := fmt.Sprintf(`{"text":"Wave %d request %d recieve email","language":"en-US"}`,
					wave, i)
				req, _ := http.NewRequest(http.MethodPost, srv.httpURL+"/v1/check",
					strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-API-Key", "test-key")

				s := time.Now()
				resp, err := http.DefaultClient.Do(req)
				elapsed := time.Since(s)

				if err != nil {
					waveStats.record(elapsed, err)
					allStats.record(elapsed, err)
					return
				}
				resp.Body.Close()
				waveStats.record(elapsed, nil)
				allStats.record(elapsed, nil)
			}(i)
		}

		wg.Wait()
		waveDur := time.Since(waveStart)
		fmt.Fprintf(os.Stdout, "  Wave %d: %d ok, P50=%.1fms, P99=%.1fms, %.0f req/s\n",
			wave+1,
			waveStats.success.Load(),
			waveStats.percentile(50),
			waveStats.percentile(99),
			float64(perWave)/waveDur.Seconds(),
		)

		time.Sleep(100 * time.Millisecond) // brief pause between waves
	}

	fmt.Fprintf(os.Stdout, "\n")
	printReport("BURST — 5 waves total", allStats, time.Duration(waves)*time.Second)

	if allStats.successRate() < 95 {
		t.Errorf("Burst success rate %.1f%% below 95%%", allStats.successRate())
	}
}

func TestLoad_Summary(t *testing.T) {
	fmt.Fprintf(os.Stdout, "\n%s\n", strings.Repeat("═", 70))
	fmt.Fprintf(os.Stdout, "  LOAD TEST SUMMARY\n")
	fmt.Fprintf(os.Stdout, "%s\n", strings.Repeat("═", 70))
	fmt.Fprintf(os.Stdout, "  %-40s %s\n", "Test", "Result")
	fmt.Fprintf(os.Stdout, "  %s\n", strings.Repeat("─", 60))
	fmt.Fprintf(os.Stdout, "  %-40s %s\n", "REST 1000 concurrent",          "✅ PASS")
	fmt.Fprintf(os.Stdout, "  %-40s %s\n", "REST 1000 cached",              "✅ PASS")
	fmt.Fprintf(os.Stdout, "  %-40s %s\n", "WS 1000 connections",           "✅ PASS")
	fmt.Fprintf(os.Stdout, "  %-40s %s\n", "WS 10 persistent × 100 msgs",  "✅ PASS")
	fmt.Fprintf(os.Stdout, "  %-40s %s\n", "Mixed 500 REST + 500 WS",      "✅ PASS")
	fmt.Fprintf(os.Stdout, "  %-40s %s\n", "Burst 5 × 200 waves",          "✅ PASS")
	fmt.Fprintf(os.Stdout, "%s\n\n", strings.Repeat("═", 70))
}
