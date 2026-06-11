package daemon

import (
	"fmt"

	"github.com/s22625/orch/internal/model"
)

func newRunSnapshot(run *model.Run) *RunSnapshot {
	if run == nil {
		return nil
	}
	return &RunSnapshot{
		IssueID:           run.IssueID,
		RunID:             run.RunID,
		Status:            run.Status,
		Phase:             run.Phase,
		Agent:             run.Agent,
		Profile:           run.Profile,
		Model:             run.Model,
		ModelVariant:      run.ModelVariant,
		Branch:            run.Branch,
		WorktreePath:      run.WorktreePath,
		Target:            run.Target,
		TargetHost:        run.TargetHost,
		TargetWorkerID:    run.TargetWorkerID,
		SessionName:       run.SessionName,
		Multiplexer:       run.Multiplexer,
		ServerPort:        run.ServerPort,
		OpenCodeSessionID: run.OpenCodeSessionID,
		ContinuedFrom:     run.ContinuedFrom,
	}
}

func modelRunFromSnapshot(snapshot *RunSnapshot) (*model.Run, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("run_snapshot missing")
	}
	if snapshot.IssueID == "" || snapshot.RunID == "" {
		return nil, fmt.Errorf("run_snapshot missing issue_id or run_id")
	}
	status, err := model.NormalizeStatus(string(snapshot.Status))
	if err != nil {
		return nil, fmt.Errorf("run_snapshot %s#%s invalid status: %w", snapshot.IssueID, snapshot.RunID, err)
	}
	return &model.Run{
		IssueID:           snapshot.IssueID,
		RunID:             snapshot.RunID,
		Status:            status,
		Phase:             snapshot.Phase,
		Agent:             snapshot.Agent,
		Profile:           snapshot.Profile,
		Model:             snapshot.Model,
		ModelVariant:      snapshot.ModelVariant,
		Branch:            snapshot.Branch,
		WorktreePath:      snapshot.WorktreePath,
		Target:            snapshot.Target,
		TargetHost:        snapshot.TargetHost,
		TargetWorkerID:    snapshot.TargetWorkerID,
		SessionName:       snapshot.SessionName,
		Multiplexer:       snapshot.Multiplexer,
		ServerPort:        snapshot.ServerPort,
		OpenCodeSessionID: snapshot.OpenCodeSessionID,
		ContinuedFrom:     snapshot.ContinuedFrom,
	}, nil
}
