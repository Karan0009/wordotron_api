package services

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/Karan0009/wordotron_api/app/config"
	"github.com/Karan0009/wordotron_api/app/lib"
	"github.com/Karan0009/wordotron_api/app/lib/dictionary"
	"github.com/Karan0009/wordotron_api/app/models"
	"github.com/Karan0009/wordotron_api/app/repository"
)

// CreateWordInput is the validated payload for adding a term to the
// catalogue. Everything else (definitions, parts of speech, synonyms,
// antonyms) comes from the enricher, not the caller.
type CreateWordInput struct {
	Term     string
	Language string
}

// WordService is the vocabulary catalogue use-case boundary.
type WordService interface {
	// CreateAndEnrich adds a term and fills in its senses via the configured
	// Enricher. Fails if the term already exists or enrichment is disabled.
	CreateAndEnrich(ctx context.Context, actor Actor, in CreateWordInput) (*models.Word, error)
	Get(ctx context.Context, id uuid.UUID) (*models.Word, error)
	List(ctx context.Context, filter models.ListWordsFilter, page models.PageParams) (*models.Page[models.Word], error)
}

type wordService struct {
	store    repository.Store
	enricher dictionary.Enricher
	cfg      *config.Config
	log      *slog.Logger
}

var _ WordService = (*wordService)(nil)

// NewWordService wires the word catalogue use cases. enricher may be nil when
// cfg.OpenAI is not configured; CreateAndEnrich reports a clear error rather
// than panicking if it's called anyway.
func NewWordService(store repository.Store, enricher dictionary.Enricher, cfg *config.Config, log *slog.Logger) WordService {
	return &wordService{
		store:    store,
		enricher: enricher,
		cfg:      cfg,
		log:      log.With(slog.String("component", "word_service")),
	}
}

func (s *wordService) CreateAndEnrich(ctx context.Context, actor Actor, in CreateWordInput) (*models.Word, error) {
	term := strings.TrimSpace(in.Term)
	language := strings.ToLower(strings.TrimSpace(in.Language))
	if language == "" {
		language = "en"
	}

	if _, err := s.store.Words().GetByTerm(ctx, language, term); err == nil {
		return nil, lib.Conflict("This word already exists")
	} else if appErr, ok := lib.As(err); !ok || appErr.Code != lib.CodeNotFound {
		return nil, err
	}

	if !s.cfg.OpenAI.Enabled() || s.enricher == nil {
		return nil, lib.Unavailable("Word enrichment is not configured", nil)
	}

	senses, err := s.enricher.Enrich(ctx, term, language)
	if err != nil {
		return nil, lib.Internal(fmt.Errorf("enrich word: %w", err))
	}
	if len(senses) == 0 {
		return nil, lib.Internal(fmt.Errorf("enrichment returned no senses for %q", term))
	}

	var wordID uuid.UUID
	err = s.store.WithTx(ctx, func(tx repository.Store) error {
		word, err := tx.Words().Create(ctx, models.CreateWordInput{
			Term:      term,
			Language:  language,
			CreatedBy: &actor.ID,
		})
		if err != nil {
			return err
		}
		wordID = word.ID

		for i, sense := range senses {
			var partOfSpeech *string
			if pos := models.PartOfSpeech(sense.PartOfSpeech); pos.Valid() {
				value := string(pos)
				partOfSpeech = &value
			}

			var example *string
			if trimmed := strings.TrimSpace(sense.Example); trimmed != "" {
				example = &trimmed
			}

			definition := strings.TrimSpace(sense.Definition)
			if definition == "" {
				// A sense with no definition is not useful; skip rather than
				// fail the whole word on one bad entry from the model.
				continue
			}

			if _, err := tx.WordSenses().Create(ctx, models.CreateWordSenseInput{
				WordID:       word.ID,
				PartOfSpeech: partOfSpeech,
				Definition:   definition,
				Example:      example,
				Meta: &models.WordMeta{
					Synonyms:       sense.Synonyms,
					Antonyms:       sense.Antonyms,
					OtherWaysToSay: sense.OtherWaysToSay,
				},
				SenseOrder: int16(i),
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	s.log.InfoContext(ctx, "word created and enriched",
		slog.String("word_id", wordID.String()), slog.String("term", term))

	return s.Get(ctx, wordID)
}

func (s *wordService) Get(ctx context.Context, id uuid.UUID) (*models.Word, error) {
	word, err := s.store.Words().GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	senses, err := s.store.WordSenses().ListByWord(ctx, id)
	if err != nil {
		return nil, err
	}
	word.Senses = senses

	return word, nil
}

func (s *wordService) List(ctx context.Context, filter models.ListWordsFilter, page models.PageParams) (*models.Page[models.Word], error) {
	words, total, err := s.store.Words().List(ctx, filter, page)
	if err != nil {
		return nil, err
	}
	return &models.Page[models.Word]{
		Items: words,
		Meta:  models.NewPageMeta(page, total),
	}, nil
}
