package types

import "time"

type IssueStatus string

const (
	IssueTodo       IssueStatus = "todo"
	IssueInProgress IssueStatus = "in_progress"
	IssueReview     IssueStatus = "review"
	IssueDone       IssueStatus = "done"
)

func (s IssueStatus) IsValid() bool {
	switch s {
	case IssueTodo, IssueInProgress, IssueReview, IssueDone:
		return true
	}
	return false
}

type Issue struct {
	ID          string      `json:"id"`
	ProjectID   string      `json:"project_id"`
	Title       string      `json:"title"`
	Description string      `json:"description,omitempty"`
	Status      IssueStatus `json:"status"`
	Priority    int         `json:"priority"`
	Tags        []string    `json:"tags"`
	SortOrder   int         `json:"sort_order"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}
