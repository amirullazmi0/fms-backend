package dto

// RoadPathRow is a stored road path (list: without coords).
type RoadPathRow struct {
	ID         string       `json:"id"`
	FromLat    float64      `json:"from_lat"`
	FromLng    float64      `json:"from_lng"`
	ToLat      float64      `json:"to_lat"`
	ToLng      float64      `json:"to_lng"`
	FromSite   *string      `json:"from_site"` // site within 50 m of the start, if any
	ToSite     *string      `json:"to_site"`
	PointCount int          `json:"point_count"`
	DistanceM  float64      `json:"distance_m"`
	DurationS  float64      `json:"duration_s"`
	CreatedAt  int64        `json:"created_at"`
	LastUsedAt int64        `json:"last_used_at"`
	Coords     [][2]float64 `json:"coords,omitempty"` // detail only
}

// RoadAreaRow is stored OSM road data of one area (without the elements).
type RoadAreaRow struct {
	ID           string  `json:"id"`
	Kind         string  `json:"kind"` // major | all
	South        float64 `json:"south"`
	West         float64 `json:"west"`
	North        float64 `json:"north"`
	East         float64 `json:"east"`
	ElementCount int     `json:"element_count"`
	SizeBytes    int     `json:"size_bytes"`
	FetchedAt    int64   `json:"fetched_at"`
}

type DeletedCount struct {
	Deleted int `json:"deleted"`
}
