package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/example/pipeline-engine/samples/go/basic/internal"
)

func TestSampleMain(t *testing.T) {
	server := internal.NewMockServer()
	defer server.Close()
	os.Setenv("PIPELINE_ENGINE_ADDR", server.Server.URL)

	r, w, _ := os.Pipe()
	stdout := os.Stdout
	stderr := os.Stderr
	os.Stdout = w
	os.Stderr = w
	defer func() {
		os.Stdout = stdout
		os.Stderr = stderr
		_ = w.Close()
	}()

	main()

	_ = w.Close()
	output, _ := io.ReadAll(r)
	text := string(output)
	if !strings.Contains(text, "Submitting job") {
		t.Fatalf("sample output missing expected text: %s", text)
	}
}
