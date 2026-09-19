package agent

import "errors"

var (
	ErrInvalidModel           = errors.New("agent model is invalid")
	ErrInvalidToolExecutor    = errors.New("agent tool executor is invalid")
	ErrInvalidPlanning        = errors.New("agent planning result is invalid")
	ErrPlanningOutputTooLarge = errors.New("agent planning output is too large")
	ErrMultipleToolCalls      = errors.New("agent supports only one tool call")
	ErrInvalidFinalResult     = errors.New("agent final result is invalid")
)
