package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Karan0009/wordotron_api/app/lib"
	"github.com/Karan0009/wordotron_api/app/models"
	"github.com/Karan0009/wordotron_api/app/repository/db"
)

// WordRepository is the persistence contract for catalogue entries. It knows
// nothing about senses - that's WordSenseRepository.
type WordRepository interface {
	Create(ctx context.Context, in models.CreateWordInput) (*models.Word, error)
	GetByID(ctx context.Context, id uuid.UUID) (*models.Word, error)
	GetByTerm(ctx context.Context, language, term string) (*models.Word, error)
	Update(ctx context.Context, id uuid.UUID, in models.UpdateWordInput) (*models.Word, error)
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, filter models.ListWordsFilter, page models.PageParams) ([]models.Word, int64, error)
}

type wordRepository struct {
	q db.Querier
}

var _ WordRepository = (*wordRepository)(nil)

func (r *wordRepository) Create(ctx context.Context, in models.CreateWordInput) (*models.Word, error) {
	row, err := r.q.CreateWord(ctx, db.CreateWordParams{
		Term:          in.Term,
		Language:      in.Language,
		Pronunciation: in.Pronunciation,
		Tags:          in.Tags,
		CreatedBy:     uuidPtrToPg(in.CreatedBy),
	})
	if err != nil {
		if lib.IsPgError(err, lib.PgUniqueViolation) {
			return nil, lib.Conflict("This word already exists").Wrap(err)
		}
		return nil, lib.Internal(fmt.Errorf("create word: %w", err))
	}
	return toDomainWord(row), nil
}

func (r *wordRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.Word, error) {
	row, err := r.q.GetWord(ctx, id)
	if err != nil {
		if lib.IsNoRows(err) {
			return nil, lib.NotFound("Word").Wrap(err)
		}
		return nil, lib.Internal(fmt.Errorf("get word: %w", err))
	}
	return toDomainWord(row), nil
}

func (r *wordRepository) GetByTerm(ctx context.Context, language, term string) (*models.Word, error) {
	row, err := r.q.GetWordByTerm(ctx, db.GetWordByTermParams{
		Language: language,
		Term:     term,
	})
	if err != nil {
		if lib.IsNoRows(err) {
			return nil, lib.NotFound("Word").Wrap(err)
		}
		return nil, lib.Internal(fmt.Errorf("get word by term: %w", err))
	}
	return toDomainWord(row), nil
}

func (r *wordRepository) Update(ctx context.Context, id uuid.UUID, in models.UpdateWordInput) (*models.Word, error) {
	row, err := r.q.UpdateWord(ctx, db.UpdateWordParams{
		ID:                 id,
		Term:               stringPtrToPgText(in.Term),
		Language:           in.Language,
		Pronunciation:      in.Pronunciation,
		ClearPronunciation: in.ClearPronunciation,
		Tags:               in.Tags,
	})
	if err != nil {
		if lib.IsNoRows(err) {
			return nil, lib.NotFound("Word").Wrap(err)
		}
		return nil, lib.Internal(fmt.Errorf("update word: %w", err))
	}
	return toDomainWord(row), nil
}

func (r *wordRepository) Delete(ctx context.Context, id uuid.UUID) error {
	affected, err := r.q.DeleteWord(ctx, id)
	if err != nil {
		return lib.Internal(fmt.Errorf("delete word: %w", err))
	}
	if affected == 0 {
		return lib.NotFound("Word")
	}
	return nil
}

func (r *wordRepository) List(ctx context.Context, filter models.ListWordsFilter, page models.PageParams) ([]models.Word, int64, error) {
	rows, err := r.q.ListWords(ctx, db.ListWordsParams{
		Search:     filter.Search,
		Language:   filter.Language,
		Tags:       filter.Tags,
		CreatedBy:  uuidPtrToPg(filter.CreatedBy),
		SortBy:     page.Sort,
		SortOrder:  page.Order.String(),
		PageLimit:  int32(page.Limit),
		PageOffset: int32(page.Offset()),
	})
	if err != nil {
		return nil, 0, lib.Internal(fmt.Errorf("list words: %w", err))
	}

	total, err := r.q.CountWords(ctx, db.CountWordsParams{
		Search:    filter.Search,
		Language:  filter.Language,
		Tags:      filter.Tags,
		CreatedBy: uuidPtrToPg(filter.CreatedBy),
	})
	if err != nil {
		return nil, 0, lib.Internal(fmt.Errorf("count words: %w", err))
	}

	words := make([]models.Word, 0, len(rows))
	for i := range rows {
		words = append(words, *toDomainWord(rows[i]))
	}
	return words, total, nil
}

func toDomainWord(row db.Word) *models.Word {
	return &models.Word{
		ID:            row.ID,
		Term:          row.Term,
		Language:      row.Language,
		Pronunciation: row.Pronunciation,
		Tags:          row.Tags,
		CreatedBy:     pgToUUIDPtr(row.CreatedBy),
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
}

// uuidPtrToPg converts the domain's *uuid.UUID (nil-means-absent) into the
// pgtype.UUID sqlc generates for nullable uuid columns - the uuid override in
// sqlc.yaml only has a non-nullable variant, so nullable uuid falls back to
// pgtype rather than *uuid.UUID.
func uuidPtrToPg(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *id, Valid: true}
}

func pgToUUIDPtr(id pgtype.UUID) *uuid.UUID {
	if !id.Valid {
		return nil
	}
	value := uuid.UUID(id.Bytes)
	return &value
}

// stringPtrToPgText converts the domain's *string (nil-means-unchanged) into
// the pgtype.Text sqlc generates for a nullable narg over a citext column -
// citext has no nullable override in sqlc.yaml (only the non-null variant
// maps to plain string), so it falls back to pgtype rather than *string.
func stringPtrToPgText(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}
