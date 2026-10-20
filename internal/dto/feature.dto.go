package dto

import "encoding/json"

// FeatureResponse is one row of the features table.
type FeatureResponse struct {
	Key         string          `json:"key"`
	Name        string          `json:"name"`
	Description *string         `json:"description"`
	Enabled     bool            `json:"enabled"`
	Config      json.RawMessage `json:"config" swaggertype:"object"`
	UpdatedAt   int64           `json:"updated_at"`
}

// UpdateFeatureRequest: switch a feature and/or replace its config (a JSON object).
type UpdateFeatureRequest struct {
	Enabled *bool           `json:"enabled"`
	Config  json.RawMessage `json:"config" swaggertype:"object"`
}

// SimulationConfig is the config of the "simulation" feature.
type SimulationConfig struct {
	Devices  int  `json:"devices"`
	Interval int  `json:"interval"`
	Loop     bool `json:"loop"`
	// BlankSpot: no signal away from the loading site; records are kept on the device and
	// sent in bulk when the truck is back in coverage (Teltonika store and forward).
	BlankSpot bool `json:"blankSpot"`
	// Queue: a truck waits before a loading/dumping spot another truck is using.
	Queue bool `json:"queue"`
	// Running is kept by the backend (not the form): the simulator was on when the server
	// stopped, so it resumes on the next start.
	Running bool `json:"running"`
}

// StartSimulationRequest overrides the feature config for one run (optional).
type StartSimulationRequest struct {
	Devices  *int `json:"devices" validate:"omitempty,min=1,max=50"`
	Interval *int `json:"interval" validate:"omitempty,min=1,max=60"`
}

// SimulationStatus is the state of the backend's simulator.
type SimulationStatus struct {
	Enabled   bool    `json:"enabled"` // feature switched on
	Running   bool    `json:"running"`
	Devices   int     `json:"devices"`
	Interval  int     `json:"interval"`
	StartedAt *int64  `json:"started_at"`
	StartedBy *string `json:"started_by"`
}
