package scheduler

import "strings"

// TaskError is a failed schtasks.exe call, with its output for the log.
type TaskError struct {
	Out string
	Err error
}

// Error includes schtasks' own message.
func (e *TaskError) Error() string {
	return "schtasks: " + e.Err.Error() + ": " + strings.TrimSpace(e.Out)
}

// Unwrap returns the process error.
func (e *TaskError) Unwrap() error { return e.Err }
