package middleware

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/amirullazmi0/fms-backend/config"
	"github.com/amirullazmi0/fms-backend/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const (
	TokenAccess  = "access"
	TokenRefresh = "refresh"
)

type Claims struct {
	UserID      string   `json:"user_id"`
	Email       string   `json:"email"`
	Role        string   `json:"role"`
	Permissions []string `json:"permissions,omitempty"`      // access tokens only
	IsAdmin     bool     `json:"is_administrator,omitempty"` // superuser
	AdminApp    bool     `json:"admin_app,omitempty"`
	Type        string   `json:"typ"` // access | refresh
	jwt.RegisteredClaims
}

// Identity is who a token is issued for.
type Identity struct {
	UserID      string
	Email       string
	Role        string
	IsAdmin     bool
	AdminApp    bool
	Permissions []string
}

const (
	ctxUserID          = "user_id"
	ctxUserEmail       = "user_email"
	ctxUserRole        = "user_role"
	ctxUserPermissions = "user_permissions"
	ctxUserIsAdmin     = "user_is_administrator"
	ctxUserAdminApp    = "user_admin_app"
)

// tokenFromRequest reads the access token: "Authorization: Bearer <token>" first, then
// the cookies. The header wins because fms-supervisor and fms-administrator both run on
// localhost and share cookies, so a cookie from one app must not override the other's token.
func tokenFromRequest(c *gin.Context) (string, error) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		if t, err := c.Cookie("access_token"); err == nil && t != "" {
			return t, nil
		}
		if t, err := c.Cookie("accessToken"); err == nil && t != "" {
			return t, nil
		}
		return "", errors.New("Authorization required")
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" || parts[1] == "" {
		return "", errors.New("Invalid authorization header format")
	}
	return parts[1], nil
}

// ParseToken validates a token of the given type with its secret.
func ParseToken(tokenString, typ string, cfg *config.JWTConfig) (*Claims, error) {
	secret := cfg.Secret
	if typ == TokenRefresh {
		secret = cfg.RefreshSecret
	}
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return nil, errors.New("Invalid or expired token")
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || claims.Type != typ {
		return nil, errors.New("Invalid token claims")
	}
	return claims, nil
}

func setUser(c *gin.Context, claims *Claims) {
	c.Set(ctxUserID, claims.UserID)
	c.Set(ctxUserEmail, claims.Email)
	c.Set(ctxUserRole, claims.Role)
	c.Set(ctxUserPermissions, claims.Permissions)
	c.Set(ctxUserIsAdmin, claims.IsAdmin)
	c.Set(ctxUserAdminApp, claims.AdminApp || claims.IsAdmin)
}

// JWTAuth middleware validates the access token.
func JWTAuth(cfg *config.JWTConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString, err := tokenFromRequest(c)
		if err != nil {
			response.Error(c, http.StatusUnauthorized, err.Error(), nil)
			c.Abort()
			return
		}
		claims, err := ParseToken(tokenString, TokenAccess, cfg)
		if err != nil {
			response.Error(c, http.StatusUnauthorized, err.Error(), nil)
			c.Abort()
			return
		}
		setUser(c, claims)
		c.Next()
	}
}

// JWTAuthOptional sets the user when a valid access token is present, never aborts.
func JWTAuthOptional(cfg *config.JWTConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if tokenString, err := tokenFromRequest(c); err == nil {
			if claims, err := ParseToken(tokenString, TokenAccess, cfg); err == nil {
				setUser(c, claims)
			}
		}
		c.Next()
	}
}

func generate(id Identity, typ string, ttl time.Duration, secret string) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:      id.UserID,
		Email:       id.Email,
		Role:        id.Role,
		Permissions: id.Permissions,
		IsAdmin:     id.IsAdmin,
		AdminApp:    id.AdminApp,
		Type:        typ,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// AccessTTL / RefreshTTL are the token lifetimes from config.
func AccessTTL(cfg *config.JWTConfig) time.Duration {
	return time.Duration(cfg.ExpiredHour) * time.Hour
}

func RefreshTTL(cfg *config.JWTConfig) time.Duration {
	return time.Duration(cfg.RefreshExpiredHour) * time.Hour
}

// GenerateToken generates a new access token carrying the user's permissions.
func GenerateToken(id Identity, cfg *config.JWTConfig) (string, error) {
	return generate(id, TokenAccess, AccessTTL(cfg), cfg.Secret)
}

// GenerateRefreshToken generates a refresh token (own secret + type, longer expiry).
// Permissions are left out: they are reloaded from the role on every refresh.
func GenerateRefreshToken(id Identity, cfg *config.JWTConfig) (string, error) {
	id.Permissions = nil
	return generate(id, TokenRefresh, RefreshTTL(cfg), cfg.RefreshSecret)
}
