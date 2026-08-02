package models

import (
	"time"

	"github.com/google/uuid"
)

// LearningStatus is where a word sits in one person's study cycle.
type LearningStatus string

const (
	StatusLearning LearningStatus = "learning"
	StatusKnown    LearningStatus = "known"
	StatusArchived LearningStatus = "archived"
)

// LearningStatuses mirrors the CHECK constraint on user_words.
var LearningStatuses = []LearningStatus{StatusLearning, StatusKnown, StatusArchived}

// Valid reports whether the value is one the database will accept.
func (s LearningStatus) Valid() bool {
	for _, candidate := range LearningStatuses {
		if s == candidate {
			return true
		}
	}
	return false
}

// MaxBox is the top Leitner box. A word reaching it is considered known.
const MaxBox = 5

// UserWord is one person's relationship with one catalogue entry: their
// progress, their private note, and when it is next due.
type UserWord struct {
	UserID         uuid.UUID      `json:"user_id"`
	WordID         uuid.UUID      `json:"word_id"`
	Status         LearningStatus `json:"status"`
	Notes          *string        `json:"notes"`
	IsFavourite    bool           `json:"is_favourite"`
	ReviewCount    int32          `json:"review_count"`
	CorrectCount   int32          `json:"correct_count"`
	Box            int16          `json:"box"`
	LastReviewedAt *time.Time     `json:"last_reviewed_at"`
	DueAt          time.Time      `json:"due_at"`
	AddedAt        time.Time      `json:"added_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// Accuracy is the share of reviews answered correctly, 0–1. Zero reviews gives
// zero rather than a divide by zero, and callers render that as "not yet".
func (u UserWord) Accuracy() float64 {
	if u.ReviewCount == 0 {
		return 0
	}
	return float64(u.CorrectCount) / float64(u.ReviewCount)
}

// IsDue reports whether the word is ready to be reviewed.
func (u UserWord) IsDue(now time.Time) bool {
	return u.Status != StatusArchived && !u.DueAt.After(now)
}

// UserWordEntry is a mapping joined to its catalogue entry, which is what the
// list endpoints return: the client needs both to render a single row.
type UserWordEntry struct {
	UserWord
	Word Word `json:"word"`
}

// AddWordInput is the payload for putting a catalogue entry into a list.
type AddWordInput struct {
	WordID      uuid.UUID
	Status      *string
	Notes       *string
	IsFavourite *bool
}

// UpdateUserWordInput is a partial update of the personal state only. The
// shared catalogue entry is untouched by this path.
type UpdateUserWordInput struct {
	Status      *string
	Notes       *string
	IsFavourite *bool

	ClearNotes bool
}

// ListUserWordsFilter narrows a person's list.
type ListUserWordsFilter struct {
	Search      *string
	Status      *string
	Language    *string
	Tags        []string
	IsFavourite *bool
	// DueOnly selects the review queue: due now and not archived.
	DueOnly bool
}

// UserWordStats is the progress summary for one person.
type UserWordStats struct {
	Total      int64 `json:"total"`
	Learning   int64 `json:"learning"`
	Known      int64 `json:"known"`
	Archived   int64 `json:"archived"`
	Favourites int64 `json:"favourites"`
	Due        int64 `json:"due"`
	Reviews    int64 `json:"reviews"`
	Correct    int64 `json:"correct"`
}

// Accuracy is the overall share of correct answers, 0–1.
func (s UserWordStats) Accuracy() float64 {
	if s.Reviews == 0 {
		return 0
	}
	return float64(s.Correct) / float64(s.Reviews)
}
