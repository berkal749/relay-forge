package job

import "time"

// Status is the lifecycle state of a job.
type Status string

const (
	StatusPending   Status = "pending"
	StatusScheduled Status = "scheduled"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

var transitions = map[Status][]Status{
	StatusPending:   {StatusScheduled, StatusRunning, StatusCancelled},
	StatusScheduled: {StatusRunning, StatusCancelled},
	StatusRunning:   {StatusSucceeded, StatusFailed, StatusScheduled, StatusCancelled},
	StatusSucceeded: {},
	StatusFailed:    {},
	StatusCancelled: {},
}

func (s Status) Valid() bool {
	_, ok := transitions[s]
	return ok
}

func (s Status) Terminal() bool {
	return s == StatusSucceeded || s == StatusFailed || s == StatusCancelled
}

func (s Status) CanTransitionTo(next Status) bool {
	for _, allowed := range transitions[s] {
		if allowed == next {
			return true
		}
	}
	return false
}

// InitialStatus returns pending for jobs due now and scheduled for future jobs.
func InitialStatus(scheduledAt, now time.Time) Status {
	if scheduledAt.After(now) {
		return StatusScheduled
	}
	return StatusPending
}
