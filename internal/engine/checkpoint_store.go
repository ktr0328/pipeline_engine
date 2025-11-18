package engine

// StepCheckpointStore is an optional extension a JobStore can implement to
// persist and reload step-level checkpoints for reruns.
type StepCheckpointStore interface {
	SaveCheckpoint(jobID string, stepID StepID, items []ResultItem)
	LoadCheckpoints(jobID string) map[StepID][]ResultItem
	ClearCheckpoints(jobID string)
}

func detectCheckpointStore(store JobStore) StepCheckpointStore {
	if cs, ok := store.(StepCheckpointStore); ok {
		return cs
	}
	return nil
}
