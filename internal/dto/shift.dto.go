package dto

type Driver struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Phone      *string `json:"phone"`
	EmployeeNo *string `json:"employeeNo"`
	Ibutton    *string `json:"ibutton"` // iButton ID (hex)
	Active     bool    `json:"active"`
	Note       *string `json:"note"`
	CreatedAt  int64   `json:"createdAt"`
	UpdatedAt  int64   `json:"updatedAt"`
}

type UpsertDriverRequest struct {
	Name       string  `json:"name" validate:"required,max=60"`
	Phone      *string `json:"phone" validate:"omitempty,max=30"`
	EmployeeNo *string `json:"employeeNo" validate:"omitempty,max=30"`
	Ibutton    *string `json:"ibutton" validate:"omitempty,max=16"`
	Active     *bool   `json:"active"`
	Note       *string `json:"note" validate:"omitempty,max=200"`
}

// DriverLast is the truck & route of a driver's latest assignment (form prefill).
type DriverLast struct {
	Imei    string `json:"imei"`
	RouteID string `json:"routeId"`
}

type ShiftTemplate struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	StartTime string `json:"startTime"` // HH:MM
	EndTime   string `json:"endTime"`   // HH:MM (earlier than start = past midnight)
	AutoClose bool   `json:"autoClose"`
	Sort      int    `json:"sort"`
}

type UpsertShiftTemplateRequest struct {
	Name      string `json:"name" validate:"required,max=40"`
	StartTime string `json:"startTime" validate:"required,hhmm"`
	EndTime   string `json:"endTime" validate:"required,hhmm,nefield=StartTime"`
	AutoClose *bool  `json:"autoClose"`
	Sort      int    `json:"sort"`
}

type Assignment struct {
	ID        string  `json:"id"`
	ShiftID   string  `json:"shiftId"`
	DriverID  string  `json:"driverId"`
	Imei      string  `json:"imei"`
	RouteID   string  `json:"routeId"`
	Note      *string `json:"note"`
	StartAt   *int64  `json:"startAt"` // null = from shift start
	EndAt     *int64  `json:"endAt"`   // null = valid until the shift ends
	CreatedAt int64   `json:"createdAt"`
}

type Shift struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	RouteID      *string      `json:"routeId"`
	Date         string       `json:"date"` // YYYY-MM-DD
	PlannedStart int64        `json:"plannedStart"`
	PlannedEnd   int64        `json:"plannedEnd"`
	AutoClose    bool         `json:"autoClose"`
	Status       string       `json:"status"` // planned | active | closed
	StartedAt    *int64       `json:"startedAt"`
	ClosedAt     *int64       `json:"closedAt"`
	CloseReason  *string      `json:"closeReason"`
	Note         *string      `json:"note"`
	Overdue      bool         `json:"overdue"`
	Assignments  []Assignment `json:"assignments"`
	CreatedAt    int64        `json:"createdAt"`
	UpdatedAt    int64        `json:"updatedAt"`

	OverdueNoted bool `json:"-"`
}

type ShiftDetail struct {
	Shift Shift        `json:"shift"`
	Trips []TripRecord `json:"trips"`
}

type CreateShiftRequest struct {
	Name         string  `json:"name" validate:"required,max=40"`
	RouteID      string  `json:"routeId" validate:"required,uuid"`
	Date         string  `json:"date" validate:"required,datetime=2006-01-02"`
	PlannedStart int64   `json:"plannedStart" validate:"required"`
	PlannedEnd   int64   `json:"plannedEnd" validate:"required,gtfield=PlannedStart"`
	AutoClose    *bool   `json:"autoClose"`
	Note         *string `json:"note" validate:"omitempty,max=200"`
}

// UpdateShiftRequest is a partial update: nil fields keep their value.
type UpdateShiftRequest struct {
	Name         *string `json:"name" validate:"omitempty,min=1,max=40"`
	RouteID      *string `json:"routeId" validate:"omitempty,uuid"`
	Date         *string `json:"date" validate:"omitempty,datetime=2006-01-02"`
	PlannedStart *int64  `json:"plannedStart"`
	PlannedEnd   *int64  `json:"plannedEnd"`
	AutoClose    *bool   `json:"autoClose"`
	Note         *string `json:"note" validate:"omitempty,max=200"`
}

// ShiftInput is a fully resolved shift row to write.
type ShiftInput struct {
	Name         string
	RouteID      *string
	Date         string
	PlannedStart int64
	PlannedEnd   int64
	AutoClose    bool
	Note         *string
}

type ShiftListQuery struct {
	From string `form:"from"`
	To   string `form:"to"`
}

type AssignmentRequest struct {
	DriverID string  `json:"driverId" validate:"required,uuid"`
	Imei     string  `json:"imei" validate:"required,len=15,numeric"`
	RouteID  *string `json:"routeId" validate:"omitempty,uuid"`
	Note     *string `json:"note" validate:"omitempty,max=200"`
}

type CopyPreviousResponse struct {
	From    string   `json:"from"`
	Added   int      `json:"added"`
	Skipped []string `json:"skipped"`
}

// DueShift is a shift row the scheduler acts on.
type DueShift struct {
	ID           string
	Name         string
	Date         string
	PlannedStart int64
	PlannedEnd   int64
}
