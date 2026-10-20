package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/amirullazmi0/fms-backend/pkg/response"
	"github.com/gin-gonic/gin"
)

// MdtTokenPrefix marks a tablet token so it is never mistaken for a user JWT.
const MdtTokenPrefix = "mdt_"

// Context keys set by MdtAuth.
const (
	CtxMdtTablet = "mdt_tablet_id"
	CtxMdtImei   = "mdt_imei"
)

// HashToken is how tablet tokens and pairing codes are stored (sha256 hex).
func HashToken(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// MdtAuth lets a paired MDT tablet in: "Authorization: Bearer mdt_<token>". lookup returns
// the tablet id and its unit ("" = unknown or revoked).
func MdtAuth(lookup func(ctx context.Context, tokenHash string) (id, imei string, err error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		parts := strings.SplitN(c.GetHeader("Authorization"), " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" || !strings.HasPrefix(parts[1], MdtTokenPrefix) {
			response.Error(c, http.StatusUnauthorized, "Tablet belum dipasangkan ke unit", nil)
			c.Abort()
			return
		}
		id, imei, err := lookup(c.Request.Context(), HashToken(parts[1]))
		if err != nil {
			response.Error(c, http.StatusInternalServerError, "Gagal cek tablet", err.Error())
			c.Abort()
			return
		}
		if id == "" {
			response.Error(c, http.StatusUnauthorized, "Tablet dicabut atau unit dihapus, pairing ulang", nil)
			c.Abort()
			return
		}
		c.Set(CtxMdtTablet, id)
		c.Set(CtxMdtImei, imei)
		c.Next()
	}
}
