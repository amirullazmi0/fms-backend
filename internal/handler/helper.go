package handler

import (
	"net/http"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/pkg/apperror"
	"github.com/amirullazmi0/fms-backend/pkg/database"
	"github.com/amirullazmi0/fms-backend/pkg/logger"
	"github.com/amirullazmi0/fms-backend/pkg/response"
	"github.com/amirullazmi0/fms-backend/pkg/validator"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// respondError answers with the AppError status/message, or 500 for anything else.
func respondError(c *gin.Context, err error, fallback string) {
	if ae, ok := apperror.As(err); ok {
		response.Error(c, ae.Status, ae.Message, nil)
		return
	}
	logger.Error(fallback, zap.Error(err), zap.String("path", c.Request.URL.Path))
	response.Error(c, http.StatusInternalServerError, fallback, err.Error())
}

// bindJSON binds and validates a JSON body; false means a 400 was already sent.
func bindJSON(c *gin.Context, body any) bool {
	if err := c.ShouldBindJSON(body); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request body", err.Error())
		return false
	}
	if err := validator.Validate(body); err != nil {
		response.ValidationError(c, validator.FormatValidationErrors(err))
		return false
	}
	return true
}

// bindQuery binds and validates query parameters; false means a 400 was already sent.
func bindQuery(c *gin.Context, q any) bool {
	if err := c.ShouldBindQuery(q); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid query parameters", err.Error())
		return false
	}
	if err := validator.Validate(q); err != nil {
		response.ValidationError(c, validator.FormatValidationErrors(err))
		return false
	}
	return true
}

// listQuery binds the common search/sort/page query.
func listQuery(c *gin.Context) (*dto.QueryGlobal, bool) {
	var q dto.QueryGlobal
	if err := c.ShouldBindQuery(&q); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid query parameters", err.Error())
		return nil, false
	}
	q.Normalize()
	return &q, true
}

// listMeta is the pagination meta of a list (one page = everything when not paginated).
func listMeta(q *dto.QueryGlobal, total int) *response.Meta {
	if !q.Paginated() {
		return &response.Meta{TotalData: total, TotalPage: 1, CurrentPage: 1, Limit: total}
	}
	return &response.Meta{TotalData: total, TotalPage: database.CalculateTotalPage(total, q.Limit), CurrentPage: q.Page, Limit: q.Limit}
}

// validUUID rejects malformed :id params before they reach Postgres.
func validUUID(c *gin.Context, name string) (string, bool) {
	id := c.Param(name)
	if err := validator.Validate(struct {
		ID string `validate:"uuid"`
	}{id}); err != nil {
		response.Error(c, http.StatusNotFound, "Data nggak ada", nil)
		return "", false
	}
	return id, true
}

func validImei(c *gin.Context) (string, bool) {
	imei := c.Param("imei")
	if err := validator.Validate(struct {
		Imei string `validate:"len=15,numeric"`
	}{imei}); err != nil {
		response.Error(c, http.StatusNotFound, "Alat nggak ada", nil)
		return "", false
	}
	return imei, true
}
