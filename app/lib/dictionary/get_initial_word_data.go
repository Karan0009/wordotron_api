package dictionary

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Karan0009/wordotron_api/app/lib/openai"
)

// Sense is one meaning of a term, as returned by enrichment. It maps
// directly onto models.CreateWordSenseInput at the service boundary.
type Sense struct {
	PartOfSpeech   string   `json:"part_of_speech"`
	Definition     string   `json:"definition"`
	Example        string   `json:"example"`
	Synonyms       []string `json:"synonyms"`
	Antonyms       []string `json:"antonyms"`
	OtherWaysToSay []string `json:"other_ways_to_say"`
}

// sensesEnvelope is the JSON shape the model is asked to produce. Chat
// Completions' json_object mode requires a single JSON object, not a bare
// array, hence the wrapper.
type sensesEnvelope struct {
	Senses []Sense `json:"senses"`
}

const getInitialWordDataSystemPrompt = `You are a lexicographer. Given a term, respond with a JSON object of the shape:
{"senses": [{"part_of_speech": "noun|verb|adjective|adverb|pronoun|preposition|conjunction|interjection|phrase", "definition": "...", "example": "a sentence using the term in this sense", "synonyms": ["..."], "antonyms": ["..."], "other_ways_to_say": ["..."]}]}
List one entry per distinct meaning, most common first. synonyms/antonyms/other_ways_to_say may be empty arrays but must be present. Respond with only the JSON object, nothing else.`

// GetInitialWordData asks the model for every sense of term and returns them
// in the order the model produced them (most common first, per the prompt).
// This is the first enrichment call a new word gets - the "initial" data a
// user sees before any manual editing.
func GetInitialWordData(ctx context.Context, client *openai.Client, term, language string) ([]Sense, error) {
	content, err := client.ChatCompletion(ctx, []openai.ChatMessage{
		{Role: "system", Content: getInitialWordDataSystemPrompt},
		{Role: "user", Content: fmt.Sprintf("Language: %s\nTerm: %s", language, term)},
	}, openai.JSONObjectResponseFormat, 0.3)
	if err != nil {
		return nil, fmt.Errorf("get initial word data: %w", err)
	}

	var envelope sensesEnvelope
	if err := json.Unmarshal([]byte(content), &envelope); err != nil {
		return nil, fmt.Errorf("decode senses: %w", err)
	}

	return envelope.Senses, nil
}
