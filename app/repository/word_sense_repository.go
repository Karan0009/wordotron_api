package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/Karan0009/wordotron_api/app/lib"
	"github.com/Karan0009/wordotron_api/app/models"
	"github.com/Karan0009/wordotron_api/app/repository/db"
)

// WordSenseRepository is the persistence contract for a word's meanings.
type WordSenseRepository interface {
	Create(ctx context.Context, in models.CreateWordSenseInput) (*models.WordSense, error)
	GetByID(ctx context.Context, id uuid.UUID) (*models.WordSense, error)
	ListByWord(ctx context.Context, wordID uuid.UUID) ([]models.WordSense, error)
	Update(ctx context.Context, id uuid.UUID, in models.UpdateWordSenseInput) (*models.WordSense, error)
	Delete(ctx context.Context, id uuid.UUID) error
	DeleteByWord(ctx context.Context, wordID uuid.UUID) error
}

type wordSenseRepository struct {
	q db.Querier
}

var _ WordSenseRepository = (*wordSenseRepository)(nil)

func (r *wordSenseRepository) Create(ctx context.Context, in models.CreateWordSenseInput) (*models.WordSense, error) {
	metaJSON, err := marshalWordMeta(in.Meta)
	if err != nil {
		return nil, lib.Internal(fmt.Errorf("marshal sense meta: %w", err))
	}

	row, err := r.q.CreateWordSense(ctx, db.CreateWordSenseParams{
		WordID:       in.WordID,
		PartOfSpeech: in.PartOfSpeech,
		Definition:   in.Definition,
		Example:      in.Example,
		Meta:         metaJSON,
		SenseOrder:   in.SenseOrder,
	})
	if err != nil {
		return nil, lib.Internal(fmt.Errorf("create word sense: %w", err))
	}
	return toDomainWordSense(row)
}

func (r *wordSenseRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.WordSense, error) {
	row, err := r.q.GetWordSense(ctx, id)
	if err != nil {
		if lib.IsNoRows(err) {
			return nil, lib.NotFound("Word sense").Wrap(err)
		}
		return nil, lib.Internal(fmt.Errorf("get word sense: %w", err))
	}
	return toDomainWordSense(row)
}

func (r *wordSenseRepository) ListByWord(ctx context.Context, wordID uuid.UUID) ([]models.WordSense, error) {
	rows, err := r.q.ListWordSensesByWord(ctx, wordID)
	if err != nil {
		return nil, lib.Internal(fmt.Errorf("list word senses: %w", err))
	}

	senses := make([]models.WordSense, 0, len(rows))
	for i := range rows {
		sense, err := toDomainWordSense(rows[i])
		if err != nil {
			return nil, err
		}
		senses = append(senses, *sense)
	}
	return senses, nil
}

func (r *wordSenseRepository) Update(ctx context.Context, id uuid.UUID, in models.UpdateWordSenseInput) (*models.WordSense, error) {
	metaJSON, err := marshalWordMeta(in.Meta)
	if err != nil {
		return nil, lib.Internal(fmt.Errorf("marshal sense meta: %w", err))
	}

	row, err := r.q.UpdateWordSense(ctx, db.UpdateWordSenseParams{
		ID:                id,
		PartOfSpeech:      in.PartOfSpeech,
		ClearPartOfSpeech: in.ClearPartOfSpeech,
		Definition:        in.Definition,
		Example:           in.Example,
		ClearExample:      in.ClearExample,
		Meta:              metaJSON,
	})
	if err != nil {
		if lib.IsNoRows(err) {
			return nil, lib.NotFound("Word sense").Wrap(err)
		}
		return nil, lib.Internal(fmt.Errorf("update word sense: %w", err))
	}
	return toDomainWordSense(row)
}

func (r *wordSenseRepository) Delete(ctx context.Context, id uuid.UUID) error {
	affected, err := r.q.DeleteWordSense(ctx, id)
	if err != nil {
		return lib.Internal(fmt.Errorf("delete word sense: %w", err))
	}
	if affected == 0 {
		return lib.NotFound("Word sense")
	}
	return nil
}

func (r *wordSenseRepository) DeleteByWord(ctx context.Context, wordID uuid.UUID) error {
	if err := r.q.DeleteWordSensesByWord(ctx, wordID); err != nil {
		return lib.Internal(fmt.Errorf("delete word senses: %w", err))
	}
	return nil
}

func toDomainWordSense(row db.WordSense) (*models.WordSense, error) {
	var meta models.WordMeta
	if len(row.Meta) > 0 {
		if err := json.Unmarshal(row.Meta, &meta); err != nil {
			return nil, lib.Internal(fmt.Errorf("unmarshal sense meta: %w", err))
		}
	}

	return &models.WordSense{
		ID:           row.ID,
		WordID:       row.WordID,
		PartOfSpeech: row.PartOfSpeech,
		Definition:   row.Definition,
		Example:      row.Example,
		Meta:         meta,
		SenseOrder:   row.SenseOrder,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}, nil
}

// marshalWordMeta returns nil (not an empty slice) for a nil input, so sqlc's
// nullable []byte param is left NULL and the SQL-side COALESCE(...) keeps
// whatever meta already exists instead of overwriting it with '{}'.
func marshalWordMeta(meta *models.WordMeta) ([]byte, error) {
	if meta == nil {
		return nil, nil
	}
	return json.Marshal(meta)
}
