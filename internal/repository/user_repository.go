package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/Karan0009/wordotron_api/internal/database"
	"github.com/Karan0009/wordotron_api/internal/models"
	"github.com/Karan0009/wordotron_api/internal/repository/db"
	"github.com/Karan0009/wordotron_api/pkg/apperror"
)

// UserRepository is the persistence contract for accounts.
type UserRepository interface {
	Create(ctx context.Context, in models.CreateUserInput) (*models.User, error)
	GetByID(ctx context.Context, id uuid.UUID) (*models.User, error)
	GetByEmail(ctx context.Context, email string) (*models.User, error)
	Update(ctx context.Context, id uuid.UUID, in models.UpdateUserInput) (*models.User, error)
	UpdatePassword(ctx context.Context, id uuid.UUID, passwordHash string) error
	TouchLastLogin(ctx context.Context, id uuid.UUID) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, filter models.ListUsersFilter) ([]models.User, int64, error)
	EmailExists(ctx context.Context, email string) (bool, error)
}

type userRepository struct {
	q db.Querier
}

var _ UserRepository = (*userRepository)(nil)

func (r *userRepository) Create(ctx context.Context, in models.CreateUserInput) (*models.User, error) {
	row, err := r.q.CreateUser(ctx, db.CreateUserParams{
		Email:        in.Email,
		PasswordHash: in.PasswordHash,
		FullName:     in.FullName,
		Role:         in.Role.String(),
	})
	if err != nil {
		if database.IsPgError(err, database.PgUniqueViolation) {
			return nil, apperror.Conflict("An account with this email already exists").Wrap(err)
		}
		return nil, apperror.Internal(fmt.Errorf("create user: %w", err))
	}
	return toDomainUser(row), nil
}

func (r *userRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
	row, err := r.q.GetUserByID(ctx, id)
	if err != nil {
		if database.IsNoRows(err) {
			return nil, apperror.NotFound("User").Wrap(err)
		}
		return nil, apperror.Internal(fmt.Errorf("get user by id: %w", err))
	}
	return toDomainUser(row), nil
}

func (r *userRepository) GetByEmail(ctx context.Context, email string) (*models.User, error) {
	row, err := r.q.GetUserByEmail(ctx, email)
	if err != nil {
		if database.IsNoRows(err) {
			return nil, apperror.NotFound("User").Wrap(err)
		}
		return nil, apperror.Internal(fmt.Errorf("get user by email: %w", err))
	}
	return toDomainUser(row), nil
}

func (r *userRepository) Update(ctx context.Context, id uuid.UUID, in models.UpdateUserInput) (*models.User, error) {
	var role *string
	if in.Role != nil {
		value := in.Role.String()
		role = &value
	}

	row, err := r.q.UpdateUser(ctx, db.UpdateUserParams{
		ID:        id,
		FullName:  in.FullName,
		AvatarUrl: in.AvatarURL,
		Role:      role,
		IsActive:  in.IsActive,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return nil, apperror.NotFound("User").Wrap(err)
		}
		if database.IsPgError(err, database.PgCheckViolation) {
			return nil, apperror.BadRequest("Invalid role").Wrap(err)
		}
		return nil, apperror.Internal(fmt.Errorf("update user: %w", err))
	}
	return toDomainUser(row), nil
}

func (r *userRepository) UpdatePassword(ctx context.Context, id uuid.UUID, passwordHash string) error {
	if err := r.q.UpdateUserPassword(ctx, db.UpdateUserPasswordParams{
		ID:           id,
		PasswordHash: passwordHash,
	}); err != nil {
		return apperror.Internal(fmt.Errorf("update password: %w", err))
	}
	return nil
}

func (r *userRepository) TouchLastLogin(ctx context.Context, id uuid.UUID) error {
	if err := r.q.UpdateLastLogin(ctx, id); err != nil {
		return apperror.Internal(fmt.Errorf("update last login: %w", err))
	}
	return nil
}

func (r *userRepository) Delete(ctx context.Context, id uuid.UUID) error {
	affected, err := r.q.DeleteUser(ctx, id)
	if err != nil {
		return apperror.Internal(fmt.Errorf("delete user: %w", err))
	}
	if affected == 0 {
		return apperror.NotFound("User")
	}
	return nil
}

func (r *userRepository) List(ctx context.Context, filter models.ListUsersFilter) ([]models.User, int64, error) {
	page := filter.Page

	var search *string
	if page.Search != "" {
		// Escape LIKE metacharacters so a literal % does not turn into a
		// full-table wildcard scan.
		escaped := escapeLike(page.Search)
		search = &escaped
	}

	var role *string
	if filter.Role != nil {
		value := filter.Role.String()
		role = &value
	}

	rows, err := r.q.ListUsers(ctx, db.ListUsersParams{
		Search:     search,
		Role:       role,
		IsActive:   filter.IsActive,
		SortBy:     page.Sort,
		SortOrder:  page.Order.String(),
		PageLimit:  int32(page.Limit),
		PageOffset: int32(page.Offset()),
	})
	if err != nil {
		return nil, 0, apperror.Internal(fmt.Errorf("list users: %w", err))
	}

	total, err := r.q.CountUsers(ctx, db.CountUsersParams{
		Search:   search,
		Role:     role,
		IsActive: filter.IsActive,
	})
	if err != nil {
		return nil, 0, apperror.Internal(fmt.Errorf("count users: %w", err))
	}

	users := make([]models.User, 0, len(rows))
	for i := range rows {
		users = append(users, *toDomainUser(rows[i]))
	}
	return users, total, nil
}

func (r *userRepository) EmailExists(ctx context.Context, email string) (bool, error) {
	exists, err := r.q.EmailExists(ctx, email)
	if err != nil {
		return false, apperror.Internal(fmt.Errorf("check email: %w", err))
	}
	return exists, nil
}

func toDomainUser(row db.User) *models.User {
	return &models.User{
		ID:              row.ID,
		Email:           row.Email,
		FullName:        row.FullName,
		Role:            models.Role(row.Role),
		AvatarURL:       row.AvatarUrl,
		IsActive:        row.IsActive,
		EmailVerifiedAt: row.EmailVerifiedAt,
		LastLoginAt:     row.LastLoginAt,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
		PasswordHash:    row.PasswordHash,
	}
}

// escapeLike neutralises the ILIKE wildcards a caller might type into ?search=.
func escapeLike(input string) string {
	out := strings.ReplaceAll(input, `\`, `\\`)
	out = strings.ReplaceAll(out, "%", `\%`)
	out = strings.ReplaceAll(out, "_", `\_`)
	return out
}
