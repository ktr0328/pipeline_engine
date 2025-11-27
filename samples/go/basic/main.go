package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/example/pipeline-engine/internal/engine"
	gosdk "github.com/example/pipeline-engine/pkg/sdk/go"
)

func main() {
	addr := os.Getenv("PIPELINE_ENGINE_ADDR")
	if addr == "" {
		addr = "http://127.0.0.1:8085"
	}
	client := gosdk.NewClient(addr)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	req := engine.JobRequest{
		PipelineType: "openai.summarize.v1",
		Input: engine.JobInput{
			Sources: []engine.Source{
				{Kind: engine.SourceKindNote, Content: "この文章を 3 行でまとめて"},
			},
		},
		Mode: "sync",
	}

	fmt.Println("Submitting job to", addr)
	job, err := client.CreateJob(ctx, req)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Job %s status: %s\n", job.ID, job.Status)

	fmt.Println("Streaming events (async mode example)...")
	streamReq := req
	streamReq.Mode = "async"
	streamReq.Input.Sources[0].Content = "ストリームで結果を確認したいテキスト"
	events, acceptedJob, err := client.StreamJobs(ctx, streamReq)
	if err != nil {
		panic(err)
	}
	fmt.Println("Accepted job:", acceptedJob.ID)
	for evt := range events {
		fmt.Printf("[%s] %v\n", evt.Event, evt.Data)
		if evt.Event == "stream_finished" {
			break
		}
	}
}
