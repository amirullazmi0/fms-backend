package dto

// QueryGlobal is the common list query. Page/Limit are optional: when Limit is 0
// the whole list is returned (the map screens need every site/route/truck).
type QueryGlobal struct {
	Search string `form:"search"`
	SortBy string `form:"sort"`
	Order  string `form:"order"`
	Page   int    `form:"page"`
	Limit  int    `form:"limit"`
}

// Paginated reports whether the caller asked for a page.
func (q *QueryGlobal) Paginated() bool {
	return q.Limit > 0
}

// Normalize fills the page when a limit is given.
func (q *QueryGlobal) Normalize() {
	if q.Limit > 0 && q.Page <= 0 {
		q.Page = 1
	}
}

// DeleteResult is returned by deletes that may fall back to deactivating.
type DeleteResult struct {
	Result string `json:"result"` // deleted | deactivated | ended
}
