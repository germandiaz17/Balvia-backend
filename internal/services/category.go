package services

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/germandiaz17/Balvia-backend/internal/database"
	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/domain"
)

// CategoryService manages system + user-defined categories. System categories
// are read-only; only the user's own categories can be updated or deleted.
type CategoryService struct {
	store database.Store
}

func NewCategoryService(store database.Store) *CategoryService {
	return &CategoryService{store: store}
}

type CreateCategoryInput struct {
	Name         string
	CategoryType string
	ParentID     *uuid.UUID
	Icon         *string
	Color        *string
	DisplayOrder int32
}

type UpdateCategoryInput struct {
	Name         string
	ParentID     *uuid.UUID
	Icon         *string
	Color        *string
	DisplayOrder int32
}

func (s *CategoryService) Create(ctx context.Context, userID uuid.UUID, in CreateCategoryInput) (sqlc.Category, error) {
	return s.store.CreateCategory(ctx, sqlc.CreateCategoryParams{
		UserID:       uuid.NullUUID{UUID: userID, Valid: true},
		ParentID:     ptrToNullUUID(in.ParentID),
		Name:         in.Name,
		CategoryType: in.CategoryType,
		Icon:         in.Icon,
		Color:        in.Color,
		DisplayOrder: in.DisplayOrder,
	})
}

func (s *CategoryService) List(ctx context.Context, userID uuid.UUID) ([]sqlc.Category, error) {
	return s.store.ListCategoriesForUser(ctx, uuid.NullUUID{UUID: userID, Valid: true})
}

func (s *CategoryService) Get(ctx context.Context, userID, id uuid.UUID) (sqlc.Category, error) {
	cat, err := s.store.GetCategoryForUser(ctx, sqlc.GetCategoryForUserParams{
		ID:     id,
		UserID: uuid.NullUUID{UUID: userID, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Category{}, domain.ErrNotFound
	}
	return cat, err
}

func (s *CategoryService) Update(ctx context.Context, userID, id uuid.UUID, in UpdateCategoryInput) (sqlc.Category, error) {
	cat, err := s.store.UpdateCategory(ctx, sqlc.UpdateCategoryParams{
		Name:         in.Name,
		ParentID:     ptrToNullUUID(in.ParentID),
		Icon:         in.Icon,
		Color:        in.Color,
		DisplayOrder: in.DisplayOrder,
		ID:           id,
		UserID:       uuid.NullUUID{UUID: userID, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Category{}, domain.ErrNotFound // missing or a system category
	}
	return cat, err
}

func (s *CategoryService) Delete(ctx context.Context, userID, id uuid.UUID) error {
	_, err := s.store.SoftDeleteCategory(ctx, sqlc.SoftDeleteCategoryParams{
		ID:     id,
		UserID: uuid.NullUUID{UUID: userID, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}

func ptrToNullUUID(id *uuid.UUID) uuid.NullUUID {
	if id == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *id, Valid: true}
}
