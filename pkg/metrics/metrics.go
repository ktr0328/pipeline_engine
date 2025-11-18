package metrics

import (
	"expvar"
	"strings"
	"sync"
	"time"
)

var (
	providerCallCount   = expvar.NewMap("provider_call_count")
	providerCallLatency = expvar.NewMap("provider_call_latency_ms")
	providerCallErrors  = expvar.NewMap("provider_call_errors")
	providerChunkCount  = expvar.NewMap("provider_chunk_count")
	mapMu               sync.Mutex
)

// ObserveProviderCall records duration and success/failure of a provider call.
func ObserveProviderCall(kind string, duration time.Duration, err error) {
	key := normalize(kind)
	addInt(providerCallCount, key, 1)
	addInt(providerCallLatency, key, duration.Milliseconds())
	if err != nil {
		addInt(providerCallErrors, key, 1)
	}
}

// ObserveProviderChunks increments chunk counters for streaming output.
func ObserveProviderChunks(kind string, count int) {
	if count <= 0 {
		return
	}
	addInt(providerChunkCount, normalize(kind), int64(count))
}

func normalize(kind string) string {
	if strings.TrimSpace(kind) == "" {
		return "unknown"
	}
	return kind
}

func addInt(m *expvar.Map, key string, delta int64) {
	mapMu.Lock()
	defer mapMu.Unlock()
	if v := m.Get(key); v != nil {
		if iv, ok := v.(*expvar.Int); ok {
			iv.Add(delta)
			return
		}
	}
	iv := new(expvar.Int)
	iv.Set(delta)
	m.Set(key, iv)
}
