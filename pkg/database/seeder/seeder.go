package seeder

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/amirullazmi0/fms-backend/config"
	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/model"
	"github.com/amirullazmi0/fms-backend/internal/repository"
	"github.com/amirullazmi0/fms-backend/pkg/logger"
	"github.com/amirullazmi0/fms-backend/pkg/password"
	"go.uber.org/zap"
)

// Seed creates the default shift templates (when there are none) and one user per
// built-in role, all with SEED_PASSWORD. Existing users are left untouched.
func Seed(db *sql.DB, cfg *config.SeedConfig) error {
	ctx := context.Background()
	if err := repository.NewDriverRepository(db).SeedTemplates(ctx); err != nil {
		return fmt.Errorf("seed shift templates: %w", err)
	}
	if cfg.Password == "" {
		logger.Info("SEED_PASSWORD kosong, user tidak di-seed")
		return nil
	}
	adminEmail, adminName := cfg.AdminEmail, cfg.AdminName
	if adminEmail == "" {
		adminEmail = "admin@fms.id"
	}
	if adminName == "" {
		adminName = "Administrator"
	}
	users := []struct {
		email, name, role string
		admin             bool
	}{
		{adminEmail, adminName, model.RoleAdmin, true},
		{"owner@fms.id", "Owner", model.RoleOwner, false},
		{"supervisor@fms.id", "Supervisor", model.RoleSupervisor, false},
		{"viewer@fms.id", "Viewer", model.RoleViewer, false},
	}
	for _, u := range users {
		created, err := UpsertUser(ctx, db, u.email, cfg.Password, u.name, u.role, false)
		if err != nil {
			return fmt.Errorf("seed %s: %w", u.email, err)
		}
		if created && u.admin {
			if err := SetAdministrator(ctx, db, u.email, true); err != nil {
				return err
			}
		}
	}
	return nil
}

// UpsertUser creates a user, or (overwrite) resets an existing one's password/name/role.
// role is a role name (e.g. ADMIN, SUPERVISOR, VIEWER, or any role made in fms-administrator).
// Returns true when the user was created.
func UpsertUser(ctx context.Context, db *sql.DB, email, plain, name, role string, overwrite bool) (bool, error) {
	if len(plain) < 8 {
		return false, fmt.Errorf("password minimal 8 karakter")
	}
	r, err := repository.NewRoleRepository(db).FindByName(ctx, strings.TrimSpace(role))
	if err != nil {
		return false, err
	}
	if r == nil || r.IsDeleted() {
		return false, fmt.Errorf("role %q tidak ditemukan", role)
	}
	repo := repository.NewUserRepository(db)
	existing, err := repo.FindByEmail(ctx, email)
	if err != nil {
		return false, err
	}
	hash, err := password.Hash(plain)
	if err != nil {
		return false, err
	}
	var namePtr *string
	if n := strings.TrimSpace(name); n != "" {
		namePtr = &n
	}
	if existing != nil {
		if !overwrite {
			logger.Info("User already exists, skipping...", zap.String("email", email))
			return false, nil
		}
		active := true
		if _, err := repo.UpdateUser(ctx, existing.ID, dto.UpdateUserRequest{Name: namePtr, IsActive: &active}, &hash, existing.ID); err != nil {
			return false, err
		}
		_, err := repo.UpdateRole(ctx, existing.ID, r.ID, existing.ID)
		return false, err
	}
	if _, err := repo.CreateUser(ctx, email, hash, namePtr, r.ID, false, ""); err != nil {
		return false, err
	}
	logger.Info("User seeded", zap.String("email", email), zap.String("role", role))
	return true, nil
}

// SetAdministrator sets users.is_administrator (superuser) for an email.
func SetAdministrator(ctx context.Context, db *sql.DB, email string, on bool) error {
	repo := repository.NewUserRepository(db)
	u, err := repo.FindByEmail(ctx, email)
	if err != nil || u == nil {
		return fmt.Errorf("user %s tidak ditemukan: %v", email, err)
	}
	_, err = repo.UpdateUser(ctx, u.ID, dto.UpdateUserRequest{IsAdministrator: &on}, nil, u.ID)
	return err
}
