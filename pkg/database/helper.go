package database

import (
	"strings"
)

// Pagination helper to return limit and offset
func GetPagination(page, limit int) (int, int) {
	if limit <= 0 {
		limit = 10
	}
	offset := (page - 1) * limit
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// Sorting helper to return safe sort column and order
func GetSorting(sortBy, order string, allowedFields map[string]string, defaultSort string) (string, string) {
	field := defaultSort
	if sortBy != "" {
		if val, ok := allowedFields[sortBy]; ok {
			field = val
		}
	}

	sortOrder := "DESC"
	if strings.ToUpper(order) == "ASC" {
		sortOrder = "ASC"
	}

	return field, sortOrder
}

// CalculateTotalPage helper to calculate total pages
func CalculateTotalPage(totalData, limit int) int {
	if limit <= 0 {
		return 0
	}
	totalPage := totalData / limit
	if totalData%limit > 0 {
		totalPage++
	}
	return totalPage
}
