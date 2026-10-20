package usecase

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/middleware"
	"github.com/amirullazmi0/fms-backend/internal/repository"
	"github.com/amirullazmi0/fms-backend/internal/tracker"
	"github.com/amirullazmi0/fms-backend/pkg/apperror"
	"github.com/amirullazmi0/fms-backend/pkg/password"
)

const pairCodeTTL = 10 * time.Minute

var delayCodePattern = regexp.MustCompile(`^[A-Z0-9_]{2,20}$`)

// MdtUsecase: the MDT tablet in the cab (pairing, operator login, status changes) and the
// unit status / delay code data behind it.
type MdtUsecase interface {
	// Supervisor side
	PairCode(ctx context.Context, imei, userID string) (*dto.MdtPairCode, error)
	Tablets(ctx context.Context, imei string) ([]dto.MdtTablet, error)
	RevokeTablet(ctx context.Context, id string) error
	SetPin(ctx context.Context, driverID string, body dto.SetPinRequest, userID string) error
	DelayCodes(ctx context.Context, all bool) ([]dto.DelayCode, error)
	UpsertDelayCode(ctx context.Context, body dto.UpsertDelayCodeRequest, userID string) (*dto.DelayCode, error)
	StatusLogs(ctx context.Context, q dto.UnitStatusQuery) ([]dto.UnitStatusLog, error)
	CurrentStatuses(ctx context.Context) ([]dto.UnitStatusLog, error)
	SetStatus(ctx context.Context, body dto.SetUnitStatusRequest, userID string) (*dto.UnitStatusLog, error)

	// Tablet side
	Pair(ctx context.Context, body dto.MdtPairRequest) (*dto.MdtPairResponse, error)
	TabletByToken(ctx context.Context, tokenHash string) (string, string, error)
	Login(ctx context.Context, body dto.MdtLoginRequest) (*dto.MdtOperator, error)
	Bootstrap(ctx context.Context, imei, tabletID string) (*dto.MdtBootstrap, error)
	Position(imei string) *dto.MdtPosition
	SubmitStatuses(ctx context.Context, imei, tabletID string, body dto.MdtStatusBatch) (*dto.MdtStatusBatchResult, error)
}

type mdtUsecase struct {
	repo     repository.MdtRepository
	tripRepo repository.TripRepository
	tracker  *tracker.Tracker
	tz       string
}

func NewMdtUsecase(repo repository.MdtRepository, tripRepo repository.TripRepository, t *tracker.Tracker, tz string) MdtUsecase {
	return &mdtUsecase{repo: repo, tripRepo: tripRepo, tracker: t, tz: tz}
}

func (u *mdtUsecase) unit(imei string) (dto.MdtUnit, bool) {
	for _, d := range u.tracker.Devices() {
		if d.Imei == imei {
			return dto.MdtUnit{Imei: imei, Label: tracker.UnitLabel(d), Vehicle: d.Vehicle, UnitType: d.UnitType, PayloadTon: d.PayloadTon}, true
		}
	}
	return dto.MdtUnit{}, false
}

// statusChanged tells dashboards a unit's status changed.
func (u *mdtUsecase) statusChanged(imei string) {
	u.tracker.Broadcast(map[string]any{"type": "unit_status", "imei": imei})
}

// ---------- Supervisor side ----------

func (u *mdtUsecase) PairCode(ctx context.Context, imei, userID string) (*dto.MdtPairCode, error) {
	if _, ok := u.unit(imei); !ok {
		return nil, apperror.NotFound("Unit tidak ditemukan")
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return nil, err
	}
	code := fmt.Sprintf("%06d", n.Int64())
	expires := time.Now().Add(pairCodeTTL).UnixMilli()
	if err := u.repo.CreatePairCode(ctx, imei, middleware.HashToken(code), expires, userID); err != nil {
		return nil, err
	}
	return &dto.MdtPairCode{Code: code, ExpiresAt: expires}, nil
}

func (u *mdtUsecase) Tablets(ctx context.Context, imei string) ([]dto.MdtTablet, error) {
	return u.repo.Tablets(ctx, imei)
}

func (u *mdtUsecase) RevokeTablet(ctx context.Context, id string) error {
	ok, err := u.repo.RevokeTablet(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return apperror.NotFound("Tablet tidak ditemukan atau sudah dilepas")
	}
	return nil
}

func (u *mdtUsecase) SetPin(ctx context.Context, driverID string, body dto.SetPinRequest, userID string) error {
	hash, err := password.Hash(body.Pin)
	if err != nil {
		return err
	}
	ok, err := u.repo.SetPin(ctx, driverID, hash, userID)
	if err != nil {
		return err
	}
	if !ok {
		return apperror.NotFound("Operator tidak ditemukan")
	}
	return nil
}

func (u *mdtUsecase) DelayCodes(ctx context.Context, all bool) ([]dto.DelayCode, error) {
	return u.repo.DelayCodes(ctx, !all)
}

func (u *mdtUsecase) UpsertDelayCode(ctx context.Context, body dto.UpsertDelayCodeRequest, userID string) (*dto.DelayCode, error) {
	body.Code = strings.ToUpper(strings.TrimSpace(body.Code))
	body.Name = strings.TrimSpace(body.Name)
	if !delayCodePattern.MatchString(body.Code) {
		return nil, apperror.BadRequest("Kode hanya boleh berisi huruf besar, angka, dan _ (2–20 karakter)")
	}
	if err := u.repo.UpsertDelayCode(ctx, body, userID); err != nil {
		return nil, err
	}
	return u.repo.DelayCode(ctx, body.Code)
}

func (u *mdtUsecase) StatusLogs(ctx context.Context, q dto.UnitStatusQuery) ([]dto.UnitStatusLog, error) {
	to := q.To
	if to <= 0 {
		to = time.Now().UnixMilli()
	}
	from := q.From
	if from <= 0 {
		from = to - 24*hourMs
	}
	return u.repo.StatusLogs(ctx, q.Imei, from, to)
}

func (u *mdtUsecase) CurrentStatuses(ctx context.Context) ([]dto.UnitStatusLog, error) {
	m, err := u.repo.CurrentStatuses(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]dto.UnitStatusLog, 0, len(m))
	for _, l := range m {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Truck < out[j].Truck })
	return out, nil
}

func (u *mdtUsecase) SetStatus(ctx context.Context, body dto.SetUnitStatusRequest, userID string) (*dto.UnitStatusLog, error) {
	if _, ok := u.unit(body.Imei); !ok {
		return nil, apperror.NotFound("Unit tidak ditemukan")
	}
	code, err := u.repo.DelayCode(ctx, body.DelayCode)
	if err != nil {
		return nil, err
	}
	if code == nil {
		return nil, apperror.BadRequest("Delay code tidak dikenal")
	}
	now := time.Now().UnixMilli()
	start := body.StartAt
	if start <= 0 || start > now {
		start = now
	}
	by := userID
	ids, err := u.repo.InsertStatuses(ctx, []dto.UnitStatusInput{{
		Imei: body.Imei, DelayCode: code.Code, Status: code.Status, StartAt: start, Source: "supervisor", Note: body.Note, CreatedBy: &by,
	}})
	if err != nil {
		return nil, err
	}
	u.statusChanged(body.Imei)
	logs, err := u.repo.StatusLogs(ctx, body.Imei, start, start)
	if err != nil {
		return nil, err
	}
	for _, l := range logs {
		if len(ids) > 0 && l.ID == ids[0] {
			return &l, nil
		}
	}
	return nil, nil
}

// ---------- Tablet side ----------

func (u *mdtUsecase) Pair(ctx context.Context, body dto.MdtPairRequest) (*dto.MdtPairResponse, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	token := middleware.MdtTokenPrefix + hex.EncodeToString(raw)
	id, imei, err := u.repo.Pair(ctx, middleware.HashToken(body.Code), middleware.HashToken(token), body.Name)
	if err != nil {
		return nil, err
	}
	if id == "" {
		return nil, apperror.BadRequest("Kode pairing salah atau sudah kedaluwarsa. Minta kode baru ke supervisor.")
	}
	unit, _ := u.unit(imei)
	u.tracker.Log("conn", imei, "Tablet MDT dipasangkan ke unit "+unit.Label, "info")
	return &dto.MdtPairResponse{Token: token, Imei: imei, Unit: unit}, nil
}

func (u *mdtUsecase) TabletByToken(ctx context.Context, tokenHash string) (string, string, error) {
	return u.repo.TabletByToken(ctx, tokenHash)
}

func (u *mdtUsecase) Login(ctx context.Context, body dto.MdtLoginRequest) (*dto.MdtOperator, error) {
	op, hash, err := u.repo.OperatorByEmployeeNo(ctx, strings.TrimSpace(body.EmployeeNo))
	if err != nil {
		return nil, err
	}
	if op == nil || hash == "" {
		password.VerifyDummy(body.Pin)
		return nil, apperror.Unauthorized("NIK atau PIN salah")
	}
	if ok, _ := password.Verify(body.Pin, hash); !ok {
		return nil, apperror.Unauthorized("NIK atau PIN salah")
	}
	return op, nil
}

func (u *mdtUsecase) Bootstrap(ctx context.Context, imei, tabletID string) (*dto.MdtBootstrap, error) {
	unit, ok := u.unit(imei)
	if !ok {
		return nil, apperror.NotFound("Unit tidak ditemukan")
	}
	codes, err := u.repo.DelayCodes(ctx, true)
	if err != nil {
		return nil, err
	}
	ops, err := u.repo.Operators(ctx)
	if err != nil {
		return nil, err
	}
	duty, err := u.repo.Duty(ctx, imei)
	if err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	out := &dto.MdtBootstrap{ServerTime: now, Unit: unit, Duty: duty, DelayCodes: codes, Operators: ops}
	out.Position = u.Position(imei)
	if duty != nil && duty.StartedAt != nil {
		rep, err := u.tripRepo.Report(ctx, "truck", dto.TripFilter{From: *duty.StartedAt, To: now, Imei: imei, ShiftID: duty.ShiftID}, u.tz)
		if err != nil {
			return nil, err
		}
		out.Production = &dto.MdtProduction{Rit: rep.Total.Done, Ton: round1(rep.Total.Ton),
			AvgCycleMs: rep.Total.AvgDurationMs + rep.Total.BackAvgMs, At: now}
	}
	cur, err := u.repo.CurrentStatuses(ctx)
	if err != nil {
		return nil, err
	}
	if l, ok := cur[imei]; ok {
		out.Current = &l
	}
	_ = u.repo.TouchTablet(ctx, tabletID)
	return out, nil
}

// Position: latest position of the unit's tracker (Teltonika), nil = none yet. The tablet follows
// this, not its own GPS.
func (u *mdtUsecase) Position(imei string) *dto.MdtPosition {
	pos, speed, ok := u.tracker.Snapshot(imei)
	if !ok {
		return nil
	}
	return &dto.MdtPosition{Lat: pos.Lat, Lng: pos.Lng, Angle: pos.Angle, Speed: speed, Time: pos.Time}
}

func (u *mdtUsecase) SubmitStatuses(ctx context.Context, imei, tabletID string, body dto.MdtStatusBatch) (*dto.MdtStatusBatchResult, error) {
	codes, err := u.repo.DelayCodes(ctx, false)
	if err != nil {
		return nil, err
	}
	byCode := map[string]dto.DelayCode{}
	for _, c := range codes {
		byCode[c.Code] = c
	}
	now := time.Now().UnixMilli()
	// The tablet clock may be off: shift every time by (server − tablet) measured on this send.
	offset := now - body.SentAt
	items := append([]dto.MdtStatusItem(nil), body.Items...)
	sort.SliceStable(items, func(i, j int) bool { return items[i].ClientAt < items[j].ClientAt })

	res := &dto.MdtStatusBatchResult{Accepted: []string{}, Rejected: []string{}, OffsetMs: offset}
	inputs := []dto.UnitStatusInput{}
	for _, it := range items {
		c, ok := byCode[it.DelayCode]
		if !ok {
			res.Rejected = append(res.Rejected, it.ID)
			continue
		}
		clientAt := it.ClientAt
		start := min(clientAt+offset, now)
		inputs = append(inputs, dto.UnitStatusInput{
			ID: it.ID, Imei: imei, DriverID: it.DriverID, DelayCode: c.Code, Status: c.Status,
			StartAt: start, ClientAt: &clientAt, Source: "mdt", Note: it.Note,
		})
	}
	if len(inputs) > 0 {
		ids, err := u.repo.InsertStatuses(ctx, inputs)
		if err != nil {
			return nil, err
		}
		res.Accepted = ids
		u.statusChanged(imei)
	}
	_ = u.repo.TouchTablet(ctx, tabletID)
	return res, nil
}
