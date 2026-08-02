package dictionary

import (
	"context"

	"github.com/Karan0009/wordotron_api/app/config"
	"github.com/Karan0009/wordotron_api/app/lib/openai"
)

// Enricher looks up a term and returns its senses. WordService depends on
// this interface, not on openai.Client directly, so the provider can change
// without touching the service layer.
type Enricher interface {
	Enrich(ctx context.Context, term, language string) ([]Sense, error)
}

// OpenAIEnricher implements Enricher by asking the model for a term's
// initial data (see get_initial_word_data.go).
type OpenAIEnricher struct {
	client *openai.Client
}

var _ Enricher = (*OpenAIEnricher)(nil)

// NewOpenAIEnricher builds an Enricher backed by OpenAI. cfg.Enabled() should
// be checked by the caller before wiring this in - it works with an empty
// key, but every call will fail with a 401 from the API.
func NewOpenAIEnricher(cfg config.OpenAI) *OpenAIEnricher {
	return &OpenAIEnricher{client: openai.NewClient(cfg)}
}

func (e *OpenAIEnricher) Enrich(ctx context.Context, term, language string) ([]Sense, error) {
	return GetInitialWordData(ctx, e.client, term, language)
}
