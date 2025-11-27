package engine

import "time"

type ProviderKind string

const (
	ProviderOpenAI ProviderKind = "openai"
	ProviderOllama ProviderKind = "ollama"
	ProviderImage  ProviderKind = "image"
	ProviderLocal  ProviderKind = "local_tool"
)

type ProviderProfileID string

type ProviderProfile struct {
	ID           ProviderProfileID `json:"id"`
	Kind         ProviderKind      `json:"kind"`
	BaseURI      string            `json:"base_uri"`
	APIKey       string            `json:"api_key,omitempty"`
	DefaultModel string            `json:"default_model,omitempty"`
	Extra        map[string]any    `json:"extra,omitempty"`
}

type ContentType string

const (
	ContentText      ContentType = "text"
	ContentMarkdown  ContentType = "markdown"
	ContentJSON      ContentType = "json"
	ContentImage     ContentType = "image"
	ContentEmbedding ContentType = "embedding"
	ContentTable     ContentType = "table"
	ContentBinary    ContentType = "binary"
)

type OutputFormat string

const (
	OutputFormatText       OutputFormat = "text"
	OutputFormatJSONStrict OutputFormat = "json_strict"
	OutputFormatJSONLoose  OutputFormat = "json_loose"
)

type PromptTemplate struct {
	System string         `json:"system,omitempty"`
	User   string         `json:"user,omitempty"`
	Meta   map[string]any `json:"meta,omitempty"`
}

type PipelineType string

type StepKind string

const (
	StepKindLLM    StepKind = "llm"
	StepKindImage  StepKind = "image"
	StepKindMap    StepKind = "map"
	StepKindReduce StepKind = "reduce"
	StepKindCustom StepKind = "custom"
)

type StepMode string

const (
	StepModeSingle  StepMode = "single"
	StepModeFanOut  StepMode = "fanout"
	StepModePerItem StepMode = "per_item"
)

type StepID string

type StepDef struct {
	ID                StepID            `json:"id"`
	Name              string            `json:"name"`
	Kind              StepKind          `json:"kind"`
	Mode              StepMode          `json:"mode,omitempty"`
	DependsOn         []StepID          `json:"depends_on"`
	ProviderProfileID ProviderProfileID `json:"provider_profile_id"`
	ProviderOverride  map[string]any    `json:"provider_override,omitempty"`
	Prompt            *PromptTemplate   `json:"prompt,omitempty"`
	OutputType        ContentType       `json:"output_type"`
	OutputFormat      OutputFormat      `json:"output_format,omitempty"`
	Config            map[string]any    `json:"config,omitempty"`
	Export            bool              `json:"export,omitempty"`
	ExportTag         string            `json:"export_tag,omitempty"`
}

type PipelineDef struct {
	Type    PipelineType `json:"type"`
	Version string       `json:"version"`
	Steps   []StepDef    `json:"steps"`
}

type SourceKind string

const (
	SourceKindLog  SourceKind = "log"
	SourceKindCode SourceKind = "code"
	SourceKindNote SourceKind = "note"
	SourceKindRaw  SourceKind = "raw"
)

type Source struct {
	Kind     SourceKind     `json:"kind"`
	Label    string         `json:"label"`
	Content  string         `json:"content"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type JobOptions struct {
	MaxTokens   int    `json:"max_tokens,omitempty"`
	DetailLevel string `json:"detail_level,omitempty"`
	Language    string `json:"language,omitempty"`
}

type JobInput struct {
	Sources []Source    `json:"sources"`
	Options *JobOptions `json:"options,omitempty"`
}

type ContentTypeAlias = ContentType

type ResultItem struct {
	ID          string      `json:"id"`
	Label       string      `json:"label"`
	StepID      StepID      `json:"step_id"`
	ShardKey    *string     `json:"shard_key,omitempty"`
	IsPrimary   bool        `json:"is_primary,omitempty"`
	Kind        string      `json:"kind"`
	Tag         string      `json:"tag,omitempty"`
	ContentType ContentType `json:"content_type"`
	Data        any         `json:"data"`
}

type JobResult struct {
	Items []ResultItem   `json:"items"`
	Meta  map[string]any `json:"meta,omitempty"`
}

type JobStatus string

const (
	JobStatusQueued    JobStatus = "queued"
	JobStatusRunning   JobStatus = "running"
	JobStatusSucceeded JobStatus = "succeeded"
	JobStatusFailed    JobStatus = "failed"
	JobStatusCancelled JobStatus = "cancelled"
)

type JobError struct {
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Details interface{} `json:"details,omitempty"`
}

type StepExecutionStatus string

const (
	StepExecPending   StepExecutionStatus = "pending"
	StepExecRunning   StepExecutionStatus = "running"
	StepExecSuccess   StepExecutionStatus = "success"
	StepExecFailed    StepExecutionStatus = "failed"
	StepExecSkipped   StepExecutionStatus = "skipped"
	StepExecCancelled StepExecutionStatus = "cancelled"
)

type StepExecution struct {
	StepID     StepID              `json:"step_id"`
	Status     StepExecutionStatus `json:"status"`
	StartedAt  *time.Time          `json:"started_at,omitempty"`
	FinishedAt *time.Time          `json:"finished_at,omitempty"`
	Error      *JobError           `json:"error,omitempty"`
	Chunks     []StepChunk         `json:"chunks,omitempty"`
}

type StepChunk struct {
	StepID  StepID `json:"step_id"`
	Index   int    `json:"index"`
	Content string `json:"content"`
}

type Job struct {
	ID              string          `json:"id"`
	PipelineType    PipelineType    `json:"pipeline_type"`
	PipelineVersion string          `json:"pipeline_version"`
	Status          JobStatus       `json:"status"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	Input           JobInput        `json:"input"`
	Result          *JobResult      `json:"result,omitempty"`
	Error           *JobError       `json:"error,omitempty"`
	StepExecutions  []StepExecution `json:"step_executions,omitempty"`
	ParentJobID     *string         `json:"parent_job_id,omitempty"`
	Mode            string          `json:"mode,omitempty"`
	RerunFromStep   *StepID         `json:"rerun_from_step,omitempty"`
	ReuseUpstream   bool            `json:"reuse_upstream,omitempty"`
}

type StepCheckpoint struct {
	JobID    string     `json:"job_id"`
	StepID   StepID     `json:"step_id"`
	ShardKey *string    `json:"shard_key,omitempty"`
	Result   ResultItem `json:"result"`
}

type StreamingEvent struct {
	Seq   uint64      `json:"seq,omitempty"`
	Event string      `json:"event"`
	JobID string      `json:"job_id"`
	Data  interface{} `json:"data"`
}
