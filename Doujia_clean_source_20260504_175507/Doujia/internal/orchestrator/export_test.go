package orchestrator

import (
	"context"

	"devflow/internal/core"
)

func (s *Service) AdvanceActiveRefForTest(ctx context.Context, runID core.RunID) error {
	run, err := s.runs.Get(ctx, runID)
	if err != nil {
		return err
	}
	return s.advanceActiveRef(ctx, run)
}
