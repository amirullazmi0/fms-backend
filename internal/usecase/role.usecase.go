package usecase

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/model"
	"github.com/amirullazmi0/fms-backend/internal/repository"
	"github.com/amirullazmi0/fms-backend/pkg/apperror"
	"github.com/amirullazmi0/fms-backend/pkg/database"
)

var (
	errRoleNotFound      = apperror.NotFound("Role tidak ditemukan")
	errRoleNameTaken     = apperror.Conflict("Nama role sudah dipakai")
	errRoleIsDefault     = apperror.Conflict("Role ini adalah role default untuk user baru, pilih role default lain terlebih dahulu")
	errLastRoleWithGuard = apperror.Conflict("Minimal satu role harus tetap punya permission " + model.PermissionUserUpdateRole)
	errLastAdminRole     = apperror.Conflict("Minimal satu role harus tetap administrator (bisa masuk fms-administrator)")
)

type RoleUsecase interface {
	Create(ctx context.Context, actor *model.Actor, req *dto.CreateRoleRequest) (*dto.RoleResponse, error)
	GetByID(ctx context.Context, id string) (*dto.RoleResponse, error)
	GetAll(ctx context.Context, q *dto.QueryGlobal) ([]dto.RoleListResponse, int, error)
	Update(ctx context.Context, actor *model.Actor, id string, req *dto.UpdateRoleRequest) (*dto.RoleResponse, error)
	Delete(ctx context.Context, actor *model.Actor, id string) error
	GetAllPermissions(ctx context.Context) ([]dto.PermissionResponse, error)
}

type roleUsecase struct {
	repo repository.RoleRepository
}

func NewRoleUsecase(repo repository.RoleRepository) RoleUsecase {
	return &roleUsecase{repo: repo}
}

func toRoleResponse(role *model.Role, permissions []string) dto.RoleResponse {
	if permissions == nil {
		permissions = []string{}
	}
	return dto.RoleResponse{
		ID:              role.ID,
		Name:            role.Name,
		IsDefault:       role.IsDefault,
		IsAdministrator: role.IsAdministrator,
		Permissions:     permissions,
		CreatedAt:       database.Ms(role.CreatedAt),
		UpdatedAt:       database.Ms(role.UpdatedAt),
	}
}

func normalizeNames(names []string) []string {
	seen := make(map[string]bool, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func (u *roleUsecase) resolvePermissionIDs(ctx context.Context, names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	known, err := u.repo.FindPermissionIDsByName(ctx, names)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(names))
	var missing []string
	for _, name := range names {
		id, ok := known[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		ids = append(ids, id)
	}
	if len(missing) > 0 {
		return nil, apperror.BadRequest("Permission tidak dikenal: " + strings.Join(missing, ", "))
	}
	return ids, nil
}

// assertGuardSurvives refuses a change that leaves no live role able to reassign
// roles, which is the only way back from a bad grant.
func (u *roleUsecase) assertGuardSurvives(ctx context.Context, roleID string, next []string) error {
	if slices.Contains(next, model.PermissionUserUpdateRole) {
		return nil
	}
	current, err := u.repo.FindPermissionsFor(ctx, []string{roleID})
	if err != nil {
		return err
	}
	if !slices.Contains(current[roleID], model.PermissionUserUpdateRole) {
		return nil
	}
	others, err := u.repo.CountOtherRolesWithPermission(ctx, model.PermissionUserUpdateRole, roleID)
	if err != nil {
		return err
	}
	if others == 0 {
		return errLastRoleWithGuard
	}
	return nil
}

// assertAdminSurvives refuses dropping the last administrator role (nobody could
// open fms-administrator any more).
func (u *roleUsecase) assertAdminSurvives(ctx context.Context, role *model.Role) error {
	if !role.IsAdministrator {
		return nil
	}
	others, err := u.repo.CountOtherAdministratorRoles(ctx, role.ID)
	if err != nil {
		return err
	}
	if others == 0 {
		return errLastAdminRole
	}
	return nil
}

func (u *roleUsecase) Create(ctx context.Context, actor *model.Actor, req *dto.CreateRoleRequest) (*dto.RoleResponse, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.Permissions = normalizeNames(req.Permissions)
	permissionIDs, err := u.resolvePermissionIDs(ctx, req.Permissions)
	if err != nil {
		return nil, err
	}
	isAdmin := req.IsAdministrator != nil && *req.IsAdministrator

	existing, err := u.repo.FindByName(ctx, req.Name)
	if err != nil {
		return nil, err
	}
	var id string
	switch {
	case existing != nil && !existing.IsDeleted():
		return nil, errRoleNameTaken
	case existing != nil:
		// The unique name spans soft-deleted roles: bring the old one back.
		if err := u.repo.Restore(ctx, existing.ID, req.Name, actor.ID); err != nil {
			return nil, err
		}
		if err := u.repo.Update(ctx, existing.ID, "", &isAdmin, actor.ID); err != nil {
			return nil, err
		}
		id = existing.ID
	default:
		if id, err = u.repo.Create(ctx, req.Name, isAdmin, actor.ID); err != nil {
			return nil, err
		}
	}

	if err := u.repo.ReplacePermissions(ctx, id, permissionIDs, actor.ID); err != nil {
		return nil, err
	}
	if req.IsDefault != nil && *req.IsDefault {
		if err := u.repo.SetDefault(ctx, id, actor.ID); err != nil {
			return nil, err
		}
	}
	return u.GetByID(ctx, id)
}

func (u *roleUsecase) GetByID(ctx context.Context, id string) (*dto.RoleResponse, error) {
	role, err := u.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, errRoleNotFound
	}
	permissions, err := u.repo.FindPermissionsFor(ctx, []string{id})
	if err != nil {
		return nil, err
	}
	r := toRoleResponse(role, permissions[id])
	return &r, nil
}

func (u *roleUsecase) GetAll(ctx context.Context, q *dto.QueryGlobal) ([]dto.RoleListResponse, int, error) {
	list, total, err := u.repo.FindAll(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	ids := make([]string, len(list))
	for i := range list {
		ids[i] = list[i].ID
	}
	permissions, err := u.repo.FindPermissionsFor(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	users, err := u.repo.CountUsersFor(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	out := make([]dto.RoleListResponse, len(list))
	for i := range list {
		n := users[list[i].ID]
		out[i] = dto.RoleListResponse{RoleResponse: toRoleResponse(&list[i], permissions[list[i].ID]), UserCount: n, InUse: n > 0}
	}
	return out, total, nil
}

func (u *roleUsecase) Update(ctx context.Context, actor *model.Actor, id string, req *dto.UpdateRoleRequest) (*dto.RoleResponse, error) {
	role, err := u.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, errRoleNotFound
	}

	if req.Name = strings.TrimSpace(req.Name); req.Name != "" {
		existing, err := u.repo.FindByName(ctx, req.Name)
		if err != nil {
			return nil, err
		}
		if existing != nil && existing.ID != id {
			return nil, errRoleNameTaken
		}
	}
	if req.IsAdministrator != nil && !*req.IsAdministrator {
		if err := u.assertAdminSurvives(ctx, role); err != nil {
			return nil, err
		}
	}
	if req.Permissions != nil {
		names := normalizeNames(*req.Permissions)
		permissionIDs, err := u.resolvePermissionIDs(ctx, names)
		if err != nil {
			return nil, err
		}
		if err := u.assertGuardSurvives(ctx, id, names); err != nil {
			return nil, err
		}
		if err := u.repo.ReplacePermissions(ctx, id, permissionIDs, actor.ID); err != nil {
			return nil, err
		}
	}
	if req.IsDefault != nil {
		switch {
		case *req.IsDefault:
			if err := u.repo.SetDefault(ctx, id, actor.ID); err != nil {
				return nil, err
			}
		case role.IsDefault:
			return nil, errRoleIsDefault
		}
	}
	if err := u.repo.Update(ctx, id, req.Name, req.IsAdministrator, actor.ID); err != nil {
		return nil, err
	}
	return u.GetByID(ctx, id)
}

func (u *roleUsecase) Delete(ctx context.Context, actor *model.Actor, id string) error {
	role, err := u.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if role == nil {
		return errRoleNotFound
	}
	if role.IsDefault {
		return errRoleIsDefault
	}
	users, err := u.repo.CountUsersFor(ctx, []string{id})
	if err != nil {
		return err
	}
	if n := users[id]; n > 0 {
		return apperror.Conflict(fmt.Sprintf("Role masih dipakai %d user, pindahkan user tersebut terlebih dahulu", n))
	}
	if err := u.assertGuardSurvives(ctx, id, nil); err != nil {
		return err
	}
	if err := u.assertAdminSurvives(ctx, role); err != nil {
		return err
	}
	return u.repo.Delete(ctx, id, actor.ID)
}

func (u *roleUsecase) GetAllPermissions(ctx context.Context) ([]dto.PermissionResponse, error) {
	list, err := u.repo.FindAllPermissions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]dto.PermissionResponse, len(list))
	for i, p := range list {
		out[i] = dto.PermissionResponse{ID: p.ID, Name: p.Name, Description: p.Description}
	}
	return out, nil
}
