package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/pkg/database"
)

// FeatureRepository: feature switches and their config.
type FeatureRepository interface {
	List(ctx context.Context) ([]dto.FeatureResponse, error)
	Get(ctx context.Context, key string) (*dto.FeatureResponse, error)
	Update(ctx context.Context, key string, enabled *bool, config []byte, updatedBy string) (bool, error)
}

type featureRepository struct {
	db *sql.DB
}

func NewFeatureRepository(db *sql.DB) FeatureRepository {
	return &featureRepository{db: db}
}

var featureColumns = []string{"f.key", "f.name", "f.description", "f.enabled", "f.config", "f.updated_at"}

func scanFeature(row interface{ Scan(...any) error }) (dto.FeatureResponse, error) {
	var f dto.FeatureResponse
	var desc sql.NullString
	var config []byte
	var updated time.Time
	if err := row.Scan(&f.Key, &f.Name, &desc, &f.Enabled, &config, &updated); err != nil {
		return f, err
	}
	f.Description, f.Config, f.UpdatedAt = database.NullString(desc), config, database.Ms(updated)
	return f, nil
}

func (r *featureRepository) List(ctx context.Context) ([]dto.FeatureResponse, error) {
	rows, err := database.NewQueryBuilder("features f").Select(featureColumns...).
		WhereNull("f.deleted_at").
		OrderBy("f.name").
		ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []dto.FeatureResponse{}
	for rows.Next() {
		f, err := scanFeature(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (r *featureRepository) Get(ctx context.Context, key string) (*dto.FeatureResponse, error) {
	query, args := database.NewQueryBuilder("features f").Select(featureColumns...).
		Where("f.key = $1", key).
		WhereNull("f.deleted_at").
		Build()
	f, err := scanFeature(database.RawQueryRowContext(ctx, r.db, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (r *featureRepository) Update(ctx context.Context, key string, enabled *bool, config []byte, updatedBy string) (bool, error) {
	ub := database.NewUpdateBuilder("features")
	if enabled != nil {
		ub.Set("enabled", *enabled)
	}
	if config != nil {
		ub.Set("config", string(config))
	}
	if updatedBy != "" {
		ub.SetUpdatedBy(updatedBy)
	} else {
		ub.SetRaw("updated_at", "CURRENT_TIMESTAMP") // by the server itself
	}
	res, err := ub.
		Where("key = $1", key).
		Where("deleted_at IS NULL").
		ExecuteContext(ctx, r.db)
	return affected(res, err)
}
