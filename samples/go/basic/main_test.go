package main

import (
	"bytes"
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

	var buf bytes.Buffer
	stdout := os.Stdout
	stderr := os.Stderr
	os.Stdout = &buf
	os.Stderr = &buf
	defer func() {
		os.Stdout = stdout
		os.Stderr = stderr
	}()

	main()

	output, _ := io.ReadAll(&buf)
	text := string(output)
	if !strings.Contains(text, "Submitting job") {
		t.Fatalf("sample output missing expected text: %s", text)
	}
}
