package serializers

import (
	"time"

	"github.com/google/uuid"

	"github.com/Karan0009/wordotron_api/app/models"
)

// ---------------------------------------------------------------------------
// Requests
// ---------------------------------------------------------------------------

// CreateWordRequest is the body of POST /api/v1/words. Everything beyond the
// term is filled in by enrichment, not supplied by the caller.
type CreateWordRequest struct {
	Term     string `json:"term"     validate:"required,min=1,max=200"`
	Language string `json:"language" validate:"omitempty,bcp47_language_tag"`
}

// ---------------------------------------------------------------------------
// Responses
// ---------------------------------------------------------------------------

// WordMetaResponse is the per-sense lexical metadata.
type WordMetaResponse struct {
	Synonyms       []string `json:"synonyms"`
	Antonyms       []string `json:"antonyms"`
	OtherWaysToSay []string `json:"other_ways_to_say"`
}

// WordSenseResponse is one meaning of a word.
type WordSenseResponse struct {
	ID           uuid.UUID        `json:"id"`
	PartOfSpeech *string          `json:"part_of_speech"`
	Definition   string           `json:"definition"`
	Example      *string          `json:"example"`
	Meta         WordMetaResponse `json:"meta"`
}

// WordResponse is the public projection of a word, senses included.
type WordResponse struct {
	ID            uuid.UUID           `json:"id"`
	Term          string              `json:"term"`
	Language      string              `json:"language"`
	Pronunciation *string             `json:"pronunciation"`
	Tags          []string            `json:"tags"`
	Senses        []WordSenseResponse `json:"senses"`
	CreatedAt     time.Time           `json:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
}

func ToWordSenseResponse(s models.WordSense) WordSenseResponse {
	return WordSenseResponse{
		ID:           s.ID,
		PartOfSpeech: s.PartOfSpeech,
		Definition:   s.Definition,
		Example:      s.Example,
		Meta: WordMetaResponse{
			Synonyms:       s.Meta.Synonyms,
			Antonyms:       s.Meta.Antonyms,
			OtherWaysToSay: s.Meta.OtherWaysToSay,
		},
	}
}

func ToWordResponse(w *models.Word) WordResponse {
	senses := make([]WordSenseResponse, 0, len(w.Senses))
	for _, s := range w.Senses {
		senses = append(senses, ToWordSenseResponse(s))
	}

	return WordResponse{
		ID:            w.ID,
		Term:          w.Term,
		Language:      w.Language,
		Pronunciation: w.Pronunciation,
		Tags:          w.Tags,
		Senses:        senses,
		CreatedAt:     w.CreatedAt,
		UpdatedAt:     w.UpdatedAt,
	}
}

func ToWordResponses(words []models.Word) []WordResponse {
	out := make([]WordResponse, 0, len(words))
	for i := range words {
		out = append(out, ToWordResponse(&words[i]))
	}
	return out
}
