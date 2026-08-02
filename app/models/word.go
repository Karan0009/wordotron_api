package models

import (
	"time"

	"github.com/google/uuid"
)

// PartOfSpeech constrains the grammatical category of an entry. The empty
// value is meaningful: not every entry has one (proper nouns, phrases).
type PartOfSpeech string

const (
	PartNoun         PartOfSpeech = "noun"
	PartVerb         PartOfSpeech = "verb"
	PartAdjective    PartOfSpeech = "adjective"
	PartAdverb       PartOfSpeech = "adverb"
	PartPronoun      PartOfSpeech = "pronoun"
	PartPreposition  PartOfSpeech = "preposition"
	PartConjunction  PartOfSpeech = "conjunction"
	PartInterjection PartOfSpeech = "interjection"
	PartPhrase       PartOfSpeech = "phrase"
)

// PartsOfSpeech is the allow-list, mirroring the CHECK constraint on the table.
var PartsOfSpeech = []PartOfSpeech{
	PartNoun, PartVerb, PartAdjective, PartAdverb, PartPronoun,
	PartPreposition, PartConjunction, PartInterjection, PartPhrase,
}

// Valid reports whether the value is one the database will accept.
func (p PartOfSpeech) Valid() bool {
	for _, candidate := range PartsOfSpeech {
		if p == candidate {
			return true
		}
	}
	return false
}

// Word is an entry in the shared vocabulary catalogue. It holds no per-user
// state; that lives on UserWord.
type Word struct {
	ID            uuid.UUID  `json:"id"`
	Term          string     `json:"term"`
	Language      string     `json:"language"`
	PartOfSpeech  *string    `json:"part_of_speech"`
	Definition    string     `json:"definition"`
	Example       *string    `json:"example"`
	Pronunciation *string    `json:"pronunciation"`
	Tags          []string   `json:"tags"`
	CreatedBy     *uuid.UUID `json:"created_by"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// CreateWordInput is the validated payload for adding to the catalogue.
type CreateWordInput struct {
	Term          string
	Language      string
	PartOfSpeech  *string
	Definition    string
	Example       *string
	Pronunciation *string
	Tags          []string
	CreatedBy     *uuid.UUID
}

// UpdateWordInput is a partial update. A nil pointer leaves the field alone;
// the Clear flags are how a caller sets an optional field back to empty, which
// a nil pointer cannot express on its own.
type UpdateWordInput struct {
	Term          *string
	Language      *string
	PartOfSpeech  *string
	Definition    *string
	Example       *string
	Pronunciation *string
	Tags          []string

	ClearPartOfSpeech  bool
	ClearExample       bool
	ClearPronunciation bool
}

// ListWordsFilter narrows the catalogue listing.
type ListWordsFilter struct {
	Search       *string
	Language     *string
	PartOfSpeech *string
	Tags         []string
	CreatedBy    *uuid.UUID
}

// TagCount is a tag and how many entries use it.
type TagCount struct {
	Tag  string `json:"tag"`
	Uses int64  `json:"uses"`
}
