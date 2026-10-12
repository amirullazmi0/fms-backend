package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/model"
	"github.com/amirullazmi0/fms-backend/pkg/database"
	"github.com/lib/pq"
)

// RoleRepository covers roles, the permission catalogue and their grants.
type RoleRepository interface {
	Create(ctx context.Context, name string, isAdministrator bool, createdBy string) (string, error)
	FindByID(ctx context.Context, id string) (*model.Role, error)
	// FindByName also sees soft-deleted rows: the unique name spans them, so one has to be revived.
	FindByName(ctx context.Context, name string) (*model.Role, error)
	FindDefault(ctx context.Context) (*model.Role, error)
	FindAll(ctx context.Context, q *dto.QueryGlobal) ([]model.Role, int, error)
	// Update renames the role ("" = keep) and sets is_administrator (nil = keep).
	Update(ctx context.Context, id, name string, isAdministrator *bool, updatedBy string) error
	// CountOtherAdministratorRoles: live administrator roles other than excludeRoleID.
	CountOtherAdministratorRoles(ctx context.Context, excludeRoleID string) (int, error)
	Restore(ctx context.Context, id, name, updatedBy string) error
	Delete(ctx context.Context, id, deletedBy string) error
	SetDefault(ctx context.Context, id, updatedBy string) error
	ReplacePermissions(ctx context.Context, roleID string, permissionIDs []string, actorID string) error
	FindPermissionsFor(ctx context.Context, roleIDs []string) (map[string][]string, error)
	FindAllPermissions(ctx context.Context) ([]model.Permission, error)
	FindPermissionIDsByName(ctx context.Context, names []string) (map[string]string, error)
	CountUsersFor(ctx context.Context, roleIDs []string) (map[string]int, error)
	CountOtherRolesWithPermission(ctx context.Context, permissionName, excludeRoleID string) (int, error)
}

type roleRepository struct {
	db *sql.DB
}

func NewRoleRepository(db *sql.DB) RoleRepository {
	return &roleRepository{db: db}
}

var roleColumns = []string{"r.id", "r.name", "r.is_default", "r.is_administrator", "r.created_at", "r.updated_at", "r.deleted_at"}

func scanRole(row interface{ Scan(...any) error }) (*model.Role, error) {
	var role model.Role
	var deletedAt sql.NullTime
	if err := row.Scan(&role.ID, &role.Name, &role.IsDefault, &role.IsAdministrator, &role.CreatedAt, &role.UpdatedAt, &deletedAt); err != nil {
		return nil, err
	}
	if deletedAt.Valid {
		role.DeletedAt = &deletedAt.Time
	}
	return &role, nil
}

func (r *roleRepository) one(ctx context.Context, qb *database.QueryBuilder) (*model.Role, error) {
	query, args := qb.Limit(1).Build()
	role, err := scanRole(database.RawQueryRowContext(ctx, r.db, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return role, err
}

func (r *roleRepository) Create(ctx context.Context, name string, isAdministrator bool, createdBy string) (string, error) {
	ib := database.NewInsertBuilder("roles").Set("name", name).Set("is_administrator", isAdministrator)
	if createdBy != "" {
		ib.SetCreatedBy(createdBy)
	}
	var id string
	err := ib.QueryRowContext(ctx, r.db).Scan(&id)
	return id, err
}

func (r *roleRepository) FindByID(ctx context.Context, id string) (*model.Role, error) {
	return r.one(ctx, database.NewQueryBuilder("roles r").Select(roleColumns...).Where("r.id = $1", id).WhereNull("r.deleted_at"))
}

func (r *roleRepository) FindByName(ctx context.Context, name string) (*model.Role, error) {
	return r.one(ctx, database.NewQueryBuilder("roles r").Select(roleColumns...).Where("lower(r.name) = lower($1)", name))
}

func (r *roleRepository) FindDefault(ctx context.Context) (*model.Role, error) {
	return r.one(ctx, database.NewQueryBuilder("roles r").Select(roleColumns...).Where("r.is_default = true").WhereNull("r.deleted_at"))
}

func (r *roleRepository) FindAll(ctx context.Context, q *dto.QueryGlobal) ([]model.Role, int, error) {
	qb := database.NewQueryBuilder("roles r").Select(roleColumns...).WhereNull("r.deleted_at")

	n := 0
	next := func() int { n++; return n }

	if q.Search != "" {
		qb.Where(fmt.Sprintf("r.name ILIKE $%d", next()), "%"+q.Search+"%")
	}
	total, err := count(ctx, r.db, qb)
	if err != nil {
		return nil, 0, err
	}
	page(qb, q, map[string]string{"name": "r.name", "created_at": "r.created_at", "updated_at": "r.updated_at"}, "r.name", "ASC")

	rows, err := qb.ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	list := []model.Role{}
	for rows.Next() {
		role, err := scanRole(rows)
		if err != nil {
			return nil, 0, err
		}
		list = append(list, *role)
	}
	return list, total, rows.Err()
}

func withActor(ub *database.UpdateBuilder, actorID string) *database.UpdateBuilder {
	if actorID != "" {
		return ub.SetUpdatedBy(actorID)
	}
	return ub.SetRaw("updated_at", "CURRENT_TIMESTAMP")
}

func (r *roleRepository) Update(ctx context.Context, id, name string, isAdministrator *bool, updatedBy string) error {
	ub := database.NewUpdateBuilder("roles")
	if name != "" {
		ub.Set("name", name)
	}
	if isAdministrator != nil {
		ub.Set("is_administrator", *isAdministrator)
	}
	_, err := withActor(ub.Where("id = $1", id).Where("deleted_at IS NULL"), updatedBy).ExecuteContext(ctx, r.db)
	return err
}

func (r *roleRepository) Restore(ctx context.Context, id, name, updatedBy string) error {
	ub := database.NewUpdateBuilder("roles").
		Set("name", name).
		SetRaw("deleted_at", "NULL").
		SetRaw("deleted_by", "NULL").
		SetRaw("is_default", "false").
		Where("id = $1", id)
	_, err := withActor(ub, updatedBy).ExecuteContext(ctx, r.db)
	return err
}

// Delete takes the permission grants with it: they belong to the role.
func (r *roleRepository) Delete(ctx context.Context, id, deletedBy string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := database.NewDeleteBuilder("role_permissions").SetDeletedBy(deletedBy).
		Where("role_id = $1", id).Where("deleted_at IS NULL").ExecuteContext(ctx, tx); err != nil {
		return err
	}
	if _, err := database.NewDeleteBuilder("roles").SetDeletedBy(deletedBy).
		Where("id = $1", id).Where("deleted_at IS NULL").ExecuteContext(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// SetDefault clears the flag elsewhere first: a partial unique index lets only one row carry it.
func (r *roleRepository) SetDefault(ctx context.Context, id, updatedBy string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := withActor(database.NewUpdateBuilder("roles").Set("is_default", false).
		Where("is_default = true").Where("id <> $1", id), updatedBy).ExecuteContext(ctx, tx); err != nil {
		return err
	}
	if _, err := withActor(database.NewUpdateBuilder("roles").Set("is_default", true).
		Where("id = $1", id).Where("deleted_at IS NULL"), updatedBy).ExecuteContext(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// ReplacePermissions makes the grants match the given set exactly. A grant that was
// soft-deleted and is given back is revived rather than duplicated.
func (r *roleRepository) ReplacePermissions(ctx context.Context, roleID string, permissionIDs []string, actorID string) error {
	wanted := make(map[string]bool, len(permissionIDs))
	for _, id := range permissionIDs {
		wanted[id] = true
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := database.NewQueryBuilder("role_permissions").
		Select("id", "permission_id", "deleted_at IS NULL").
		Where("role_id = $1", roleID).
		ExecuteContext(ctx, tx)
	if err != nil {
		return err
	}
	type grant struct {
		id, permissionID string
		live             bool
	}
	var existing []grant
	for rows.Next() {
		var g grant
		if err := rows.Scan(&g.id, &g.permissionID, &g.live); err != nil {
			_ = rows.Close()
			return err
		}
		existing = append(existing, g)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	seen := map[string]bool{}
	for _, g := range existing {
		switch {
		case wanted[g.permissionID] && !g.live:
			if _, err := withActor(database.NewUpdateBuilder("role_permissions").
				SetRaw("deleted_at", "NULL").SetRaw("deleted_by", "NULL").
				Where("id = $1", g.id), actorID).ExecuteContext(ctx, tx); err != nil {
				return err
			}
		case !wanted[g.permissionID] && g.live:
			if _, err := database.NewDeleteBuilder("role_permissions").SetDeletedBy(actorID).
				Where("id = $1", g.id).ExecuteContext(ctx, tx); err != nil {
				return err
			}
		}
		if wanted[g.permissionID] {
			seen[g.permissionID] = true
		}
	}
	for _, id := range permissionIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		ib := database.NewInsertBuilder("role_permissions").
			Set("role_id", roleID).
			Set("permission_id", id).
			WithoutReturning()
		if actorID != "" {
			ib.SetCreatedBy(actorID)
		}
		if _, err := ib.ExecContext(ctx, tx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *roleRepository) FindPermissionsFor(ctx context.Context, roleIDs []string) (map[string][]string, error) {
	result := make(map[string][]string, len(roleIDs))
	if len(roleIDs) == 0 {
		return result, nil
	}
	for _, id := range roleIDs {
		result[id] = []string{}
	}
	rows, err := database.NewQueryBuilder("role_permissions rp").
		Select("rp.role_id", "p.name").
		InnerJoin("permissions p", "p.id = rp.permission_id").
		Where("rp.role_id = ANY($1::uuid[])", pq.Array(roleIDs)).
		WhereNull("rp.deleted_at").
		WhereNull("p.deleted_at").
		OrderBy("p.name").
		ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var roleID, name string
		if err := rows.Scan(&roleID, &name); err != nil {
			return nil, err
		}
		result[roleID] = append(result[roleID], name)
	}
	return result, rows.Err()
}

func (r *roleRepository) FindAllPermissions(ctx context.Context) ([]model.Permission, error) {
	rows, err := database.NewQueryBuilder("permissions").
		Select("id", "name", "description").
		WhereNull("deleted_at").
		OrderBy("name").
		ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	list := []model.Permission{}
	for rows.Next() {
		var p model.Permission
		var desc sql.NullString
		if err := rows.Scan(&p.ID, &p.Name, &desc); err != nil {
			return nil, err
		}
		p.Description = database.NullString(desc)
		list = append(list, p)
	}
	return list, rows.Err()
}

func (r *roleRepository) FindPermissionIDsByName(ctx context.Context, names []string) (map[string]string, error) {
	result := make(map[string]string, len(names))
	if len(names) == 0 {
		return result, nil
	}
	rows, err := database.NewQueryBuilder("permissions").
		Select("name", "id").
		Where("name = ANY($1::text[])", pq.Array(names)).
		WhereNull("deleted_at").
		ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name, id string
		if err := rows.Scan(&name, &id); err != nil {
			return nil, err
		}
		result[name] = id
	}
	return result, rows.Err()
}

func (r *roleRepository) CountUsersFor(ctx context.Context, roleIDs []string) (map[string]int, error) {
	result := make(map[string]int, len(roleIDs))
	if len(roleIDs) == 0 {
		return result, nil
	}
	rows, err := database.NewQueryBuilder("users").
		Select("role_id", "COUNT(*)::int").
		Where("role_id = ANY($1::uuid[])", pq.Array(roleIDs)).
		WhereNull("deleted_at").
		GroupBy("role_id").
		ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		result[id] = n
	}
	return result, rows.Err()
}

func (r *roleRepository) CountOtherRolesWithPermission(ctx context.Context, permissionName, excludeRoleID string) (int, error) {
	return count(ctx, r.db, database.NewQueryBuilder("role_permissions rp").
		InnerJoin("permissions p", "p.id = rp.permission_id").
		InnerJoin("roles r", "r.id = rp.role_id").
		Where("p.name = $1", permissionName).
		Where("rp.role_id <> $2", excludeRoleID).
		WhereNull("rp.deleted_at").
		WhereNull("p.deleted_at").
		WhereNull("r.deleted_at"))
}

func (r *roleRepository) CountOtherAdministratorRoles(ctx context.Context, excludeRoleID string) (int, error) {
	return count(ctx, r.db, database.NewQueryBuilder("roles").
		Where("is_administrator = true").
		Where("id <> $1", excludeRoleID).
		WhereNull("deleted_at"))
}
