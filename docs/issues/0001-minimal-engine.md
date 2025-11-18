# Issue 1: Implement minimal pipeline engine skeleton

## Summary
- Create the directory layout described in `docs/plans/ImplementationPlan.md`
- Implement the core domain types, engine interface, in-memory store, HTTP server, and Go SDK stub
- Provide a runnable `cmd/pipeline-engine` entrypoint that wires everything together

## Acceptance Criteria
- `go test ./...` succeeds
- `cmd/pipeline-engine` starts an HTTP server on `:8085` (configurable via `PIPELINE_ENGINE_ADDR`)
- The server exposes the endpoints outlined in the implementation plan with dummy job execution
