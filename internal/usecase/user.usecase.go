package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/amirullazmi0/fms-backend/config"
	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/middleware"
	"github.com/amirullazmi0/fms-backend/internal/model"
	"github.com/amirullazmi0/fms-backend/internal/repository"
	"github.com/amirullazmi0/fms-backend/pkg/apperror"
	"github.com/amirullazmi0/fms-backend/pkg/database"
	"github.com/amirullazmi0/fms-backend/pkg/logger"
	"github.com/amirullazmi0/fms-backend/pkg/password"
	"go.uber.org/zap"
)

type UserUsecase interface {
	Login(ctx context.Context, req *dto.LoginRequest, ip string) (*dto.AuthResponse, error)
	RefreshToken(ctx context.Context, req *dto.RefreshTokenRequest) (*dto.AuthResponse, error)
	Logout(ctx context.Context, userID string) error
	GetProfile(ctx context.Context, userID string) (*dto.UserResponse, error)
	GetUsers(ctx context.Context, q *dto.QueryGlobal) ([]dto.UserResponse, int, error)
	CreateUser(ctx context.Context, req *dto.CreateUserRequest, actorID string) (string, error)
	UpdateUser(ctx context.Context, id string, req *dto.UpdateUserRequest, actorID string) error
	UpdateUserRole(ctx context.Context, id string, req *dto.UpdateUserRoleRequest, actorID string) error
	DeleteUser(ctx context.Context, id, actorID string) error
}

// Login rate limit (per IP, in memory): 5 failures lock the IP for 5 minutes.
const (
	maxLoginFails = 5
	loginLock     = 5 * time.Minute
)

type loginAttempt struct {
	count int
	until time.Time
}

type userUsecase struct {
	userRepo repository.UserRepository
	roleRepo repository.RoleRepository
	jwtCfg   *config.JWTConfig

	mu       sync.Mutex
	attempts map[string]*loginAttempt
}

func NewUserUsecase(userRepo repository.UserRepository, roleRepo repository.RoleRepository, jwtCfg *config.JWTConfig) UserUsecase {
	return &userUsecase{userRepo: userRepo, roleRepo: roleRepo, jwtCfg: jwtCfg, attempts: map[string]*loginAttempt{}}
}

func toUserResponse(u *model.User) dto.UserResponse {
	r := dto.UserResponse{
		ID: u.ID, Email: u.Email, Name: u.Name, RoleID: u.RoleID, Role: u.Role,
		IsAdministrator: u.IsAdministrator, AdminApp: u.IsAdministrator || u.RoleAdminApp, IsActive: u.IsActive,
		CreatedAt: database.Ms(u.CreatedAt), UpdatedAt: database.Ms(u.UpdatedAt),
	}
	if u.LastLoginAt != nil {
		v := database.Ms(*u.LastLoginAt)
		r.LastLoginAt = &v
	}
	return r
}

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

func (u *userUsecase) lockedFor(ip string) time.Duration {
	u.mu.Lock()
	defer u.mu.Unlock()
	a, ok := u.attempts[ip]
	if ok && a.count >= maxLoginFails && time.Now().Before(a.until) {
		return time.Until(a.until)
	}
	return 0
}

func (u *userUsecase) recordFail(ip string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	a, ok := u.attempts[ip]
	if !ok || time.Now().After(a.until) {
		a = &loginAttempt{}
		u.attempts[ip] = a
	}
	a.count++
	a.until = time.Now().Add(loginLock)
}

func (u *userUsecase) clearFails(ip string) {
	u.mu.Lock()
	delete(u.attempts, ip)
	u.mu.Unlock()
}

// permissionsOf: an administrator holds every permission; others get their role's.
func (u *userUsecase) permissionsOf(ctx context.Context, user *model.User) ([]string, error) {
	if !user.IsAdministrator {
		return u.userRepo.FindPermissionNamesByRoleID(ctx, user.RoleID)
	}
	all, err := u.roleRepo.FindAllPermissions(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(all))
	for i, p := range all {
		names[i] = p.Name
	}
	return names, nil
}

// actorIsAdministrator: only administrators may grant or revoke the administrator flag.
func (u *userUsecase) actorIsAdministrator(ctx context.Context, actorID string) (bool, error) {
	actor, err := u.userRepo.FindByID(ctx, actorID)
	if err != nil || actor == nil {
		return false, err
	}
	return actor.IsAdministrator, nil
}

// issueTokens creates and stores a new access/refresh token pair (rotation). The access
// token carries the role's current permissions, reloaded on every login/refresh.
func (u *userUsecase) issueTokens(ctx context.Context, user *model.User) (*dto.AuthResponse, error) {
	permissions, err := u.permissionsOf(ctx, user)
	if err != nil {
		return nil, err
	}
	identity := middleware.Identity{
		UserID: user.ID, Email: user.Email, Role: user.Role,
		IsAdmin: user.IsAdministrator, AdminApp: user.IsAdministrator || user.RoleAdminApp, Permissions: permissions,
	}
	access, err := middleware.GenerateToken(identity, u.jwtCfg)
	if err != nil {
		return nil, err
	}
	refresh, err := middleware.GenerateRefreshToken(identity, u.jwtCfg)
	if err != nil {
		return nil, err
	}
	if err := u.userRepo.SaveRefreshToken(ctx, user.ID, hashToken(refresh), time.Now().Add(middleware.RefreshTTL(u.jwtCfg))); err != nil {
		return nil, err
	}
	userResponse := toUserResponse(user)
	userResponse.Permissions = permissions
	return &dto.AuthResponse{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int64(middleware.AccessTTL(u.jwtCfg).Seconds()),
		User:         userResponse,
	}, nil
}

func (u *userUsecase) Login(ctx context.Context, req *dto.LoginRequest, ip string) (*dto.AuthResponse, error) {
	if wait := u.lockedFor(ip); wait > 0 {
		return nil, apperror.New(http.StatusTooManyRequests, fmt.Sprintf("Terlalu banyak percobaan. Coba lagi %d menit lagi.", int(math.Ceil(wait.Minutes()))))
	}
	user, err := u.userRepo.FindByEmail(ctx, req.Email)
	if err != nil {
		return nil, err
	}
	if user == nil {
		password.VerifyDummy(req.Password) // same response time for unknown emails
		u.recordFail(ip)
		return nil, apperror.Unauthorized("Email atau password salah")
	}
	ok, legacy := password.Verify(req.Password, user.PasswordHash)
	if !ok {
		u.recordFail(ip)
		return nil, apperror.Unauthorized("Email atau password salah")
	}
	if !user.IsActive {
		return nil, apperror.Forbidden("Akun ini dinonaktifkan")
	}
	u.clearFails(ip)

	// Demo accounts still carry scrypt hashes: move them to bcrypt now that we know the password.
	if legacy {
		if h, err := password.Hash(req.Password); err == nil {
			if err := u.userRepo.UpdatePasswordHash(ctx, user.ID, h); err != nil {
				logger.Warn("Gagal upgrade hash password", zap.String("user_id", user.ID), zap.Error(err))
			}
		}
	}
	if err := u.userRepo.MarkLogin(ctx, user.ID); err != nil {
		logger.Warn("Gagal nyatet waktu login", zap.Error(err))
	}
	now := time.Now()
	user.LastLoginAt = &now
	return u.issueTokens(ctx, user)
}

func (u *userUsecase) RefreshToken(ctx context.Context, req *dto.RefreshTokenRequest) (*dto.AuthResponse, error) {
	if _, err := middleware.ParseToken(req.RefreshToken, middleware.TokenRefresh, u.jwtCfg); err != nil {
		return nil, apperror.Unauthorized("Refresh token nggak valid atau udah kedaluwarsa")
	}
	// The DB only knows the latest token: a rotated (older) one is rejected here.
	user, err := u.userRepo.FindByRefreshToken(ctx, hashToken(req.RefreshToken))
	if err != nil {
		return nil, err
	}
	if user == nil || !user.IsActive {
		return nil, apperror.Unauthorized("Refresh token nggak valid atau udah kedaluwarsa")
	}
	return u.issueTokens(ctx, user)
}

func (u *userUsecase) Logout(ctx context.Context, userID string) error {
	return u.userRepo.ClearRefreshToken(ctx, userID)
}

func (u *userUsecase) GetProfile(ctx context.Context, userID string) (*dto.UserResponse, error) {
	user, err := u.userRepo.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, apperror.NotFound("User nggak ada")
	}
	r := toUserResponse(user)
	if r.Permissions, err = u.permissionsOf(ctx, user); err != nil {
		return nil, err
	}
	return &r, nil
}

func (u *userUsecase) GetUsers(ctx context.Context, q *dto.QueryGlobal) ([]dto.UserResponse, int, error) {
	users, total, err := u.userRepo.GetUsers(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	out := make([]dto.UserResponse, len(users))
	for i := range users {
		out[i] = toUserResponse(&users[i])
	}
	return out, total, nil
}

func (u *userUsecase) CreateUser(ctx context.Context, req *dto.CreateUserRequest, actorID string) (string, error) {
	existing, err := u.userRepo.FindByEmail(ctx, req.Email)
	if err != nil {
		return "", err
	}
	if existing != nil {
		return "", apperror.Conflict("Email udah dipakai")
	}
	roleID := req.RoleID
	if roleID == "" {
		def, err := u.roleRepo.FindDefault(ctx)
		if err != nil {
			return "", err
		}
		if def == nil {
			return "", apperror.BadRequest("Belum ada role default, pilih role-nya")
		}
		roleID = def.ID
	} else if role, err := u.roleRepo.FindByID(ctx, roleID); err != nil {
		return "", err
	} else if role == nil {
		return "", apperror.BadRequest("Role nggak ada")
	}
	h, err := password.Hash(req.Password)
	if err != nil {
		return "", err
	}
	var name *string
	if req.Name != nil {
		if s := strings.TrimSpace(*req.Name); s != "" {
			name = &s
		}
	}
	isAdmin := req.IsAdministrator != nil && *req.IsAdministrator
	if isAdmin {
		ok, err := u.actorIsAdministrator(ctx, actorID)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", apperror.Forbidden("Cuma administrator yang boleh bikin administrator")
		}
	}
	return u.userRepo.CreateUser(ctx, req.Email, h, name, roleID, isAdmin, actorID)
}

func (u *userUsecase) UpdateUser(ctx context.Context, id string, req *dto.UpdateUserRequest, actorID string) error {
	if id == actorID && req.IsActive != nil && !*req.IsActive {
		return apperror.BadRequest("Nggak bisa nonaktifin akun sendiri")
	}
	if req.IsAdministrator != nil {
		if id == actorID && !*req.IsAdministrator {
			return apperror.BadRequest("Nggak bisa nyabut status administrator akun sendiri")
		}
		ok, err := u.actorIsAdministrator(ctx, actorID)
		if err != nil {
			return err
		}
		if !ok {
			return apperror.Forbidden("Cuma administrator yang boleh ngubah status administrator")
		}
	}
	var hash *string
	if req.Password != nil {
		h, err := password.Hash(*req.Password)
		if err != nil {
			return err
		}
		hash = &h
	}
	ok, err := u.userRepo.UpdateUser(ctx, id, *req, hash, actorID)
	if err != nil {
		return err
	}
	if !ok {
		return apperror.NotFound("User nggak ada")
	}
	return nil
}

// UpdateUserRole moves a user to another role. An administrator cannot move themself
// to a non-administrator role (they would lock themself out of fms-administrator).
func (u *userUsecase) UpdateUserRole(ctx context.Context, id string, req *dto.UpdateUserRoleRequest, actorID string) error {
	role, err := u.roleRepo.FindByID(ctx, req.RoleID)
	if err != nil {
		return err
	}
	if role == nil {
		return apperror.BadRequest("Role nggak ada")
	}
	actorAdmin, err := u.actorIsAdministrator(ctx, actorID)
	if err != nil {
		return err
	}
	if id == actorID && !actorAdmin && !role.IsAdministrator {
		return apperror.BadRequest("Nggak bisa mindahin akun sendiri ke role non-administrator")
	}
	ok, err := u.userRepo.UpdateRole(ctx, id, req.RoleID, actorID)
	if err != nil {
		return err
	}
	if !ok {
		return apperror.NotFound("User nggak ada")
	}
	return nil
}

func (u *userUsecase) DeleteUser(ctx context.Context, id, actorID string) error {
	if id == actorID {
		return apperror.BadRequest("Nggak bisa hapus akun sendiri")
	}
	ok, err := u.userRepo.DeleteUser(ctx, id, actorID)
	if err != nil {
		return err
	}
	if !ok {
		return apperror.NotFound("User nggak ada")
	}
	return nil
}
