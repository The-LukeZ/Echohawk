package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// buildCache constructs a slice of n normalized strings that look like real
// cached messages, used as the "prev" slice in HandleMessage.
func buildCache(n int, text string) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = normalize(fmt.Sprintf("%s variant %d", text, i))
	}
	return out
}

// countSimilar is the hot loop extracted from HandleMessage so it can be
// benchmarked in isolation without needing a Valkey connection.
func countSimilar(content string, prev []string) int {
	count := 0
	for _, p := range prev {
		if similarity(content, p) >= similarityMin {
			count++
		}
	}
	return count
}

// ── normalize benchmarks ──────────────────────────────────────────────────────

func BenchmarkNormalize_Short(b *testing.B) {
	input := "  HELLO WORLD!  "
	b.ResetTimer()
	for b.Loop() {
		normalize(input)
	}
}

func BenchmarkNormalize_Long(b *testing.B) {
	input := "  " + strings.Repeat("This Is A Sentence. ", 50) + "  "
	b.ResetTimer()
	for b.Loop() {
		normalize(input)
	}
}

func BenchmarkNormalize_Parallel(b *testing.B) {
	input := "  " + strings.Repeat("Concurrent Normalize Test! ", 20) + "  "
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			normalize(input)
		}
	})
}

// ── similarity benchmarks ─────────────────────────────────────────────────────

func BenchmarkSimilarity_Identical(b *testing.B) {
	a := "the quick brown fox jumps over the lazy dog"
	b.ResetTimer()
	for b.Loop() {
		similarity(a, a)
	}
}

func BenchmarkSimilarity_OneDiff(b *testing.B) {
	a := "the quick brown fox jumps over the lazy dog"
	bStr := "the quick brown fox jumps over the lazy log"
	b.ResetTimer()
	for b.Loop() {
		similarity(a, bStr)
	}
}

func BenchmarkSimilarity_Short(b *testing.B) {
	a, bStr := "hello", "hallo"
	b.ResetTimer()
	for b.Loop() {
		similarity(a, bStr)
	}
}

func BenchmarkSimilarity_Medium(b *testing.B) {
	a := strings.Repeat("benchmark test message ", 5)    // ~110 chars
	bStr := strings.Repeat("benchmark text message ", 5) // 1 char diff per word
	b.ResetTimer()
	for b.Loop() {
		similarity(a, bStr)
	}
}

func BenchmarkSimilarity_Long(b *testing.B) {
	a := strings.Repeat("this is a longer repeated spam message please flag it ", 10)    // ~530 chars
	bStr := strings.Repeat("this is a Longer repeated spam message please flag it ", 10) // case diff
	a = normalize(a)
	bStr = normalize(bStr)
	b.ResetTimer()
	for b.Loop() {
		similarity(a, bStr)
	}
}

func BenchmarkSimilarity_CompletelyDifferent(b *testing.B) {
	a := strings.Repeat("abcdefghijklmnop", 10)
	bStr := strings.Repeat("qrstuvwxyz012345", 10)
	b.ResetTimer()
	for b.Loop() {
		similarity(a, bStr)
	}
}

func BenchmarkSimilarity_Parallel(b *testing.B) {
	a := normalize(strings.Repeat("parallel similarity stress test sentence ", 5))
	bStr := normalize(strings.Repeat("parallel similarity stress test sentense ", 5)) // typo
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			similarity(a, bStr)
		}
	})
}

// ── similarity loop benchmarks (mirrors HandleMessage hot path) ───────────────

// BenchmarkSimilarityLoop_NoHits: new message is completely unlike all cached ones.
func BenchmarkSimilarityLoop_NoHits(b *testing.B) {
	content := normalize("totally different content here")
	prev := buildCache(maxCached, "buy cheap products now click here")
	b.ResetTimer()
	for b.Loop() {
		countSimilar(content, prev)
	}
}

// BenchmarkSimilarityLoop_AllHits: new message is nearly identical to all cached ones.
func BenchmarkSimilarityLoop_AllHits(b *testing.B) {
	base := normalize("buy cheap products now click here")
	content := normalize("buy cheap products now click here!") // tiny diff → still hits
	prev := buildCache(maxCached, "buy cheap products now click here")
	// override all entries with the base so they all match
	for i := range prev {
		prev[i] = base
	}
	b.ResetTimer()
	for b.Loop() {
		countSimilar(content, prev)
	}
}

// BenchmarkSimilarityLoop_HalfHits: 15 hits, 15 misses — typical mid-traffic case.
func BenchmarkSimilarityLoop_HalfHits(b *testing.B) {
	base := normalize("buy cheap products now click here")
	content := normalize("buy cheap products now click here!")
	prev := buildCache(maxCached, "totally different noise message text")
	for i := 0; i < maxCached/2; i++ {
		prev[i] = base
	}
	b.ResetTimer()
	for b.Loop() {
		countSimilar(content, prev)
	}
}

// BenchmarkSimilarityLoop_LongMessages: stress with realistically long Discord messages.
func BenchmarkSimilarityLoop_LongMessages(b *testing.B) {
	base := normalize(strings.Repeat("spam message body text repeated for length ", 10))
	content := normalize(strings.Repeat("spam message body text repeated for Iength ", 10)) // l→I swap
	prev := make([]string, maxCached)
	for i := range prev {
		prev[i] = base
	}
	b.ResetTimer()
	for b.Loop() {
		countSimilar(content, prev)
	}
}

// ── cache-size sweep benchmarks ───────────────────────────────────────────────
//
// These measure how the similarity loop scales as the per-user cache grows
// from the current maxCached (30) up to 100 entries, at both short and long
// message lengths. Run with: go test -bench=BenchmarkCacheSweep -benchmem

func benchCacheSize(b *testing.B, cacheSize int, msgText string) {
	b.Helper()
	base := normalize(msgText)
	content := normalize(msgText + "!")
	prev := make([]string, cacheSize)
	for i := range prev {
		prev[i] = base
	}
	b.ResetTimer()
	for b.Loop() {
		countSimilar(content, prev)
	}
}

const (
	shortMsg = "buy cheap products now click here"
	longMsg  = "spam message body text repeated for length spam message body text repeated for length spam message body text"
)

func BenchmarkCacheSweep_Short_30(b *testing.B)  { benchCacheSize(b, 30, shortMsg) }
func BenchmarkCacheSweep_Short_50(b *testing.B)  { benchCacheSize(b, 50, shortMsg) }
func BenchmarkCacheSweep_Short_75(b *testing.B)  { benchCacheSize(b, 75, shortMsg) }
func BenchmarkCacheSweep_Short_100(b *testing.B) { benchCacheSize(b, 100, shortMsg) }

func BenchmarkCacheSweep_Long_30(b *testing.B)  { benchCacheSize(b, 30, longMsg) }
func BenchmarkCacheSweep_Long_50(b *testing.B)  { benchCacheSize(b, 50, longMsg) }
func BenchmarkCacheSweep_Long_75(b *testing.B)  { benchCacheSize(b, 75, longMsg) }
func BenchmarkCacheSweep_Long_100(b *testing.B) { benchCacheSize(b, 100, longMsg) }

// BenchmarkSimilarityLoop_Parallel: concurrent goroutines each running a full
// message check — closest simulation to real multi-user Discord traffic.
func BenchmarkSimilarityLoop_Parallel(b *testing.B) {
	base := normalize("buy cheap products now click here")
	content := normalize("buy cheap products now click here!")
	prev := make([]string, maxCached)
	for i := range prev {
		prev[i] = base
	}
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			countSimilar(content, prev)
		}
	})
}

// ── sustained-throughput tests ────────────────────────────────────────────────
//
// These tests answer "is the pure-compute hot path reliable at N msg/s?"
// They pace message arrivals with a ticker (real wall-clock rate), dispatch
// each message to a goroutine, collect per-message latencies, then report
// p50/p95/p99 and fail if any single message exceeds the hard deadline.
//
// Valkey round-trips are not included — those depend on network/infra.
// The budget here is purely normalize + 30-entry similarity scan.

const (
	throughputDuration = 5 * time.Second      // how long to sustain the load
	hardDeadline       = 2 * time.Millisecond // fail if any single check exceeds this
)

// syntheticMessages is a small pool of varied messages to cycle through,
// mixing spam-like repetition with unrelated content.
var syntheticMessages = []string{
	"buy cheap products now click here",
	"buy cheap products now click here!",
	"buy cheap Products now click here",
	"check out this amazing deal today",
	"hello everyone how is your day going",
	"this is a completely different message",
	strings.Repeat("repeated spam content ", 5),
	strings.Repeat("repeated spam content ", 5) + "!",
}

// runThroughputTest fires msgsPerSec messages every second for throughputDuration,
// handles each on its own goroutine, records latency, then prints a percentile report.
// cacheSize controls how many previous messages each check compares against.
func runThroughputTest(t *testing.T, msgsPerSec, cacheSize int) {
	t.Helper()

	prev := make([]string, cacheSize)
	base := normalize("buy cheap products now click here")
	for i := range prev {
		prev[i] = base
	}

	interval := time.Second / time.Duration(msgsPerSec)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	deadline := time.Now().Add(throughputDuration)

	var (
		mu        sync.Mutex
		latencies []time.Duration
		exceeded  atomic.Int64
		wg        sync.WaitGroup
		msgIdx    atomic.Int64
	)

	for time.Now().Before(deadline) {
		<-ticker.C

		wg.Add(1)
		idx := int(msgIdx.Add(1)-1) % len(syntheticMessages)
		raw := syntheticMessages[idx]

		go func(raw string) {
			defer wg.Done()
			start := time.Now()

			content := normalize(raw)
			countSimilar(content, prev)

			elapsed := time.Since(start)

			mu.Lock()
			latencies = append(latencies, elapsed)
			mu.Unlock()

			if elapsed > hardDeadline {
				exceeded.Add(1)
			}
		}(raw)
	}

	wg.Wait()

	total := len(latencies)
	if total == 0 {
		t.Fatal("no messages were processed")
	}

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	p := func(pct float64) time.Duration {
		idx := int(float64(total) * pct / 100)
		if idx >= total {
			idx = total - 1
		}
		return latencies[idx]
	}

	t.Logf("throughput: %d msg/s × %.1fs = %d messages processed  (cache size: %d)",
		msgsPerSec, throughputDuration.Seconds(), total, cacheSize)
	t.Logf("latency  p50=%-10v  p95=%-10v  p99=%-10v  max=%-10v",
		p(50), p(95), p(99), latencies[total-1])
	t.Logf("messages exceeding %v hard deadline: %d / %d (%.2f%%)",
		hardDeadline, exceeded.Load(), total,
		float64(exceeded.Load())/float64(total)*100)

	if n := exceeded.Load(); n > 0 {
		t.Errorf("%d messages exceeded the %v hard deadline", n, hardDeadline)
	}
}

func TestThroughput_10MsgPerSec(t *testing.T) {
	runThroughputTest(t, 10, maxCached)
}

func TestThroughput_20MsgPerSec(t *testing.T) {
	runThroughputTest(t, 20, maxCached)
}

// TestThroughput_CacheSweep holds the rate at 20 msg/s and increases cache size
// from the current default (30) up to 100 to show the latency cost of each step.
func TestThroughput_CacheSweep(t *testing.T) {
	for _, size := range []int{30, 50, 75, 100} {
		size := size
		t.Run(fmt.Sprintf("cache%d", size), func(t *testing.T) {
			t.Parallel()
			runThroughputTest(t, 20, size)
		})
	}
}
