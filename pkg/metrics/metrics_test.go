package metrics

import (
	"testing"
	"time"
)

func TestObserveProviderCall(t *testing.T) {
	ObserveProviderCall("openai", 10*time.Millisecond, nil)
	ObserveProviderCall("openai", 5*time.Millisecond, assertError{})
	if val := providerCallCount.Get("openai"); val == nil || val.String() != "2" {
		 t.Fatalf("expected call count 2, got %v", val)
	}
	if val := providerCallErrors.Get("openai"); val == nil || val.String() != "1" {
		 t.Fatalf("expected error count 1, got %v", val)
	}
}

func TestObserveProviderChunks(t *testing.T) {
	ObserveProviderChunks("ollama", 3)
	if val := providerChunkCount.Get("ollama"); val == nil || val.String() != "3" {
		 t.Fatalf("expected chunk count 3, got %v", val)
	}
	ObserveProviderChunks("ollama", 0)
	if val := providerChunkCount.Get("ollama"); val == nil || val.String() != "3" {
		 t.Fatalf("zero should not modify count; got %v", val)
	}
}

type assertError struct{}

func (assertError) Error() string { return "err" }
