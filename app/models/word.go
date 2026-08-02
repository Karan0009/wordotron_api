package models

import (
	"time"

	"github.com/google/uuid"
)

// PartOfSpeech constrains the grammatical category of a sense. The empty
// value is meaningful: not every sense has one (proper nouns, phrases).
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

// WordMeta holds free-form lexical metadata that doesn't warrant its own
// column: synonyms, antonyms, alternate phrasings. These vary by meaning, so
// they live per-sense rather than on the parent word. Stored as JSONB.
type WordMeta struct {
	Synonyms       []string `json:"synonyms,omitempty"`
	Antonyms       []string `json:"antonyms,omitempty"`
	OtherWaysToSay []string `json:"other_ways_to_say,omitempty"`
}

// Word is a term in the shared vocabulary catalogue. It holds no meaning by
// itself - a word can have several senses (WordSense) - and no per-user
// state, which lives on UserWord.
type Word struct {
	ID            uuid.UUID  `json:"id"`
	Term          string     `json:"term"`
	Language      string     `json:"language"`
	Pronunciation *string    `json:"pronunciation"`
	Tags          []string   `json:"tags"`
	CreatedBy     *uuid.UUID `json:"created_by"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`

	// Senses is populated by the service layer when a word is loaded with
	// its meanings; empty when listing words without expanding them.
	Senses []WordSense `json:"senses,omitempty"`
}

// WordSense is one meaning of a word: a part of speech, a definition, and the
// lexical metadata specific to that meaning.
type WordSense struct {
	ID           uuid.UUID `json:"id"`
	WordID       uuid.UUID `json:"word_id"`
	PartOfSpeech *string   `json:"part_of_speech"`
	Definition   string    `json:"definition"`
	Example      *string   `json:"example"`
	Meta         WordMeta  `json:"meta"`
	SenseOrder   int16     `json:"sense_order"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// CreateWordInput is the validated payload for adding a term to the
// catalogue. Senses are created separately (see CreateWordSenseInput) - a
// bare word row can exist before enrichment fills in its meanings.
type CreateWordInput struct {
	Term          string
	Language      string
	Pronunciation *string
	Tags          []string
	CreatedBy     *uuid.UUID
}

// UpdateWordInput is a partial update. A nil pointer leaves the field alone;
// ClearPronunciation is how a caller sets it back to empty, which a nil
// pointer cannot express on its own.
type UpdateWordInput struct {
	Term          *string
	Language      *string
	Pronunciation *string
	Tags          []string

	ClearPronunciation bool
}

// CreateWordSenseInput is the validated payload for adding a sense to a word.
type CreateWordSenseInput struct {
	WordID       uuid.UUID
	PartOfSpeech *string
	Definition   string
	Example      *string
	Meta         *WordMeta
	SenseOrder   int16
}

// UpdateWordSenseInput is a partial update, same nil-means-unchanged rule as
// UpdateWordInput.
type UpdateWordSenseInput struct {
	PartOfSpeech *string
	Definition   *string
	Example      *string
	// Meta replaces the whole blob when set, same as Tags on Word - not a
	// deep merge.
	Meta *WordMeta

	ClearPartOfSpeech bool
	ClearExample      bool
}

// ListWordsFilter narrows the catalogue listing.
type ListWordsFilter struct {
	Search    *string
	Language  *string
	Tags      []string
	CreatedBy *uuid.UUID
}

// TagCount is a tag and how many entries use it.
type TagCount struct {
	Tag  string `json:"tag"`
	Uses int64  `json:"uses"`
}
