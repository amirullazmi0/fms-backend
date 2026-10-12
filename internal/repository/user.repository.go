package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/model"
	"github.com/amirullazmi0/fms-backend/pkg/database"
)

type UserRepository interface {
	FindByEmail(ctx context.Context, email string) (*model.User, error)
	FindByID(ctx context.Context, id string) (*model.User, error)
	FindByRefreshToken(ctx context.Context, tokenHash string) (*model.User, error)
	FindPermissionNamesByRoleID(ctx context.Context, roleID string) ([]string, error)
	SaveRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error
	ClearRefreshToken(ctx context.Context, userID string) error
	MarkLogin(ctx context.Context, userID string) error
	UpdatePasswordHash(ctx context.Context, userID, hash string) error
	GetUsers(ctx context.Context, q *dto.QueryGlobal) ([]model.User, int, error)
	CreateUser(ctx context.Context, email, hash string, name *string, roleID string, isAdministrator bool, createdBy string) (string, error)
	UpdateUser(ctx context.Context, id string, body dto.UpdateUserRequest, hash *string, updatedBy string) (bool, error)
	UpdateRole(ctx context.Context, id, roleID, updatedBy string) (bool, error)
	DeleteUser(ctx context.Context, id, deletedBy string) (bool, error)
	CountUsers(ctx context.Context) (int, error)
}

type userRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) UserRepository {
	return &userRepository{db: db}
}

var userColumns = []string{
	"u.id", "u.email", "u.name", "u.password_hash", "u.role_id", "r.name", "u.is_administrator", "r.is_administrator", "u.is_active",
	"u.refresh_token", "u.token_expiry", "u.last_login_at", "u.created_at", "u.updated_at",
}

// users joined with their role name.
func users() *database.QueryBuilder {
	return database.NewQueryBuilder("users u").
		Select(userColumns...).
		InnerJoin("roles r", "r.id = u.role_id").
		WhereNull("u.deleted_at")
}

func scanUser(row interface{ Scan(...any) error }) (*model.User, error) {
	var u model.User
	var name, refresh sql.NullString
	var expiry, lastLogin sql.NullTime
	if err := row.Scan(&u.ID, &u.Email, &name, &u.PasswordHash, &u.RoleID, &u.Role, &u.IsAdministrator, &u.RoleAdminApp, &u.IsActive, &refresh, &expiry, &lastLogin, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return nil, err
	}
	u.Name = database.NullString(name)
	u.RefreshToken = database.NullString(refresh)
	if expiry.Valid {
		u.TokenExpiry = &expiry.Time
	}
	if lastLogin.Valid {
		u.LastLoginAt = &lastLogin.Time
	}
	return &u, nil
}

// findOne runs a users() builder that already carries its WHERE conditions.
func (r *userRepository) findOne(ctx context.Context, qb *database.QueryBuilder) (*model.User, error) {
	query, args := qb.Limit(1).Build()
	u, err := scanUser(database.RawQueryRowContext(ctx, r.db, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return u, err
}

func (r *userRepository) FindByEmail(ctx context.Context, email string) (*model.User, error) {
	return r.findOne(ctx, users().Where("u.email = $1", strings.ToLower(strings.TrimSpace(email))))
}

func (r *userRepository) FindByID(ctx context.Context, id string) (*model.User, error) {
	return r.findOne(ctx, users().Where("u.id = $1", id))
}

func (r *userRepository) FindByRefreshToken(ctx context.Context, tokenHash string) (*model.User, error) {
	return r.findOne(ctx, users().
		Where("u.refresh_token = $1", tokenHash).
		Where("u.token_expiry > $2", time.Now()))
}

func (r *userRepository) FindPermissionNamesByRoleID(ctx context.Context, roleID string) ([]string, error) {
	rows, err := database.NewQueryBuilder("role_permissions rp").
		Select("p.name").
		InnerJoin("permissions p", "p.id = rp.permission_id").
		Where("rp.role_id = $1", roleID).
		WhereNull("rp.deleted_at").
		WhereNull("p.deleted_at").
		OrderBy("p.name").
		ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, err
	}
	return scanStrings(rows)
}

func (r *userRepository) SaveRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error {
	_, err := database.NewUpdateBuilder("users").
		Set("refresh_token", tokenHash).
		Set("token_expiry", expiresAt).
		Where("id = $1", userID).
		ExecuteContext(ctx, r.db)
	return err
}

func (r *userRepository) ClearRefreshToken(ctx context.Context, userID string) error {
	_, err := database.NewUpdateBuilder("users").
		SetRaw("refresh_token", "NULL").
		SetRaw("token_expiry", "NULL").
		Where("id = $1", userID).
		ExecuteContext(ctx, r.db)
	return err
}

func (r *userRepository) MarkLogin(ctx context.Context, userID string) error {
	_, err := database.NewUpdateBuilder("users").
		SetRaw("last_login_at", "CURRENT_TIMESTAMP").
		Where("id = $1", userID).
		ExecuteContext(ctx, r.db)
	return err
}

func (r *userRepository) UpdatePasswordHash(ctx context.Context, userID, hash string) error {
	_, err := database.NewUpdateBuilder("users").
		Set("password_hash", hash).
		SetRaw("updated_at", "CURRENT_TIMESTAMP").
		Where("id = $1", userID).
		ExecuteContext(ctx, r.db)
	return err
}

func (r *userRepository) GetUsers(ctx context.Context, q *dto.QueryGlobal) ([]model.User, int, error) {
	qb := users()

	n := 0
	next := func() int { n++; return n }

	if q.Search != "" {
		p := next()
		qb.Where(fmt.Sprintf("(u.email ILIKE $%d OR u.name ILIKE $%d OR r.name ILIKE $%d)", p, p, p), "%"+q.Search+"%")
	}
	total, err := count(ctx, r.db, qb)
	if err != nil {
		return nil, 0, err
	}
	page(qb, q, map[string]string{
		"email": "u.email", "name": "u.name", "role": "r.name", "created_at": "u.created_at", "last_login_at": "u.last_login_at",
	}, "u.created_at", "DESC")

	rows, err := qb.ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	list := []model.User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, 0, err
		}
		list = append(list, *u)
	}
	return list, total, rows.Err()
}

func (r *userRepository) CreateUser(ctx context.Context, email, hash string, name *string, roleID string, isAdministrator bool, createdBy string) (string, error) {
	ib := database.NewInsertBuilder("users").
		Set("email", strings.ToLower(strings.TrimSpace(email))).
		Set("password_hash", hash).
		Set("name", name).
		Set("role_id", roleID).
		Set("is_administrator", isAdministrator).
		Set("is_active", true)
	if createdBy != "" {
		ib.SetCreatedBy(createdBy)
	}
	var id string
	err := ib.QueryRowContext(ctx, r.db).Scan(&id)
	return id, err
}

func (r *userRepository) UpdateUser(ctx context.Context, id string, body dto.UpdateUserRequest, hash *string, updatedBy string) (bool, error) {
	ub := database.NewUpdateBuilder("users")
	if body.Name != nil {
		ub.Set("name", nullIfEmpty(*body.Name))
	}
	if body.IsActive != nil {
		ub.Set("is_active", *body.IsActive)
	}
	if body.IsAdministrator != nil {
		ub.Set("is_administrator", *body.IsAdministrator)
		ub.SetRaw("refresh_token", "NULL") // next session carries the new rights
	}
	if hash != nil {
		ub.Set("password_hash", *hash)
	}
	// A new password or a deactivated account ends the current session.
	if hash != nil || (body.IsActive != nil && !*body.IsActive) {
		ub.SetRaw("refresh_token", "NULL")
	}
	if updatedBy != "" {
		ub.SetUpdatedBy(updatedBy)
	} else {
		ub.SetRaw("updated_at", "CURRENT_TIMESTAMP")
	}
	res, err := ub.Where("id = $1", id).
		Where("deleted_at IS NULL").
		ExecuteContext(ctx, r.db)
	return affected(res, err)
}

// UpdateRole moves the user to another role; the refresh token is dropped so the
// next session carries the new permissions.
func (r *userRepository) UpdateRole(ctx context.Context, id, roleID, updatedBy string) (bool, error) {
	ub := database.NewUpdateBuilder("users").
		Set("role_id", roleID).
		SetRaw("refresh_token", "NULL")
	if updatedBy != "" {
		ub.SetUpdatedBy(updatedBy)
	} else {
		ub.SetRaw("updated_at", "CURRENT_TIMESTAMP")
	}
	res, err := ub.Where("id = $1", id).
		Where("deleted_at IS NULL").
		ExecuteContext(ctx, r.db)
	return affected(res, err)
}

func (r *userRepository) DeleteUser(ctx context.Context, id, deletedBy string) (bool, error) {
	res, err := database.NewDeleteBuilder("users").
		SetDeletedBy(deletedBy).
		Where("id = $1", id).
		Where("deleted_at IS NULL").
		ExecuteContext(ctx, r.db)
	return affected(res, err)
}

func (r *userRepository) CountUsers(ctx context.Context) (int, error) {
	return count(ctx, r.db, database.NewQueryBuilder("users").WhereNull("deleted_at"))
}

// nullIfEmpty trims s and maps "" to NULL.
func nullIfEmpty(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

// noteOf trims an optional text and maps "" to NULL.
func noteOf(s *string) *string {
	if s == nil {
		return nil
	}
	return nullIfEmpty(*s)
}

func affected(res sql.Result, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
