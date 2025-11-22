package driftadopt

// GetStep returns the step with the given ID, or nil if not found
func (p *DriftPlan) GetStep(id string) *DriftStep {
	for i := range p.Steps {
		if p.Steps[i].ID == id {
			return &p.Steps[i]
		}
	}
	return nil
}

// GetNextPendingStep returns the first step with status "pending",
// or nil if no pending steps exist
func (p *DriftPlan) GetNextPendingStep() *DriftStep {
	for i := range p.Steps {
		if p.Steps[i].Status == StepPending {
			return &p.Steps[i]
		}
	}
	return nil
}

// CountByStatus returns a map of step status to count
func (p *DriftPlan) CountByStatus() map[StepStatus]int {
	counts := make(map[StepStatus]int)

	for _, step := range p.Steps {
		counts[step.Status]++
	}

	return counts
}

// GetFailedSteps returns all steps with status "failed"
func (p *DriftPlan) GetFailedSteps() []*DriftStep {
	var failed []*DriftStep

	for i := range p.Steps {
		if p.Steps[i].Status == StepFailed {
			failed = append(failed, &p.Steps[i])
		}
	}

	// Return empty slice instead of nil for consistency
	if failed == nil {
		return []*DriftStep{}
	}

	return failed
}
