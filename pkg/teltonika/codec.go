// Package teltonika encodes/decodes the Teltonika protocol: Codec 8, Codec 8
// Extended (AVL data) and Codec 12 (GPRS commands). Port of fmctest/server/codec.ts.
package teltonika

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"strconv"

	"github.com/amirullazmi0/fms-backend/internal/dto"
)

const (
	Codec8  = 0x08
	Codec8E = 0x8e
	Codec12 = 0x0c

	// maxFrame guards against garbage length headers.
	maxFrame = 1 << 20
)

var ErrShortFrame = errors.New("teltonika: frame too short")

// CRC16 is CRC-16/IBM over Codec ID .. Number of Data 2.
func CRC16(buf []byte) uint32 {
	var crc uint32
	for _, b := range buf {
		crc ^= uint32(b)
		for i := 0; i < 8; i++ {
			if crc&1 == 1 {
				crc = (crc >> 1) ^ 0xa001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

// TakeFrame cuts one full packet (preamble + length + data + CRC) from a TCP buffer.
// ok is false while more bytes are needed.
func TakeFrame(buf []byte) (frame, rest []byte, ok bool, err error) {
	if len(buf) < 8 {
		return nil, buf, false, nil
	}
	dataLen := binary.BigEndian.Uint32(buf[4:8])
	if dataLen > maxFrame {
		return nil, nil, false, errors.New("teltonika: data length too large")
	}
	total := 8 + int(dataLen) + 4
	if len(buf) < total {
		return nil, buf, false, nil
	}
	return buf[:total], buf[total:], true, nil
}

func CRCOk(frame []byte) bool {
	if len(frame) < 12 {
		return false
	}
	dataLen := int(binary.BigEndian.Uint32(frame[4:8]))
	if len(frame) < 8+dataLen+4 {
		return false
	}
	return CRC16(frame[8:8+dataLen]) == binary.BigEndian.Uint32(frame[8+dataLen:])
}

// CodecID of a frame.
func CodecID(frame []byte) byte {
	return frame[8]
}

type ParsedAvl struct {
	Codec   byte
	Count   int
	Records []dto.AvlRecord
}

type reader struct {
	b []byte
	o int
}

func (r *reader) need(n int) error {
	if r.o+n > len(r.b) {
		return ErrShortFrame
	}
	return nil
}

func (r *reader) u8() (uint8, error) {
	if err := r.need(1); err != nil {
		return 0, err
	}
	v := r.b[r.o]
	r.o++
	return v, nil
}

func (r *reader) u16() (uint16, error) {
	if err := r.need(2); err != nil {
		return 0, err
	}
	v := binary.BigEndian.Uint16(r.b[r.o:])
	r.o += 2
	return v, nil
}

func (r *reader) u32() (uint32, error) {
	if err := r.need(4); err != nil {
		return 0, err
	}
	v := binary.BigEndian.Uint32(r.b[r.o:])
	r.o += 4
	return v, nil
}

func (r *reader) u64() (uint64, error) {
	if err := r.need(8); err != nil {
		return 0, err
	}
	v := binary.BigEndian.Uint64(r.b[r.o:])
	r.o += 8
	return v, nil
}

func toSigned(v uint64, size int) int64 {
	bits := uint(size * 8)
	if v >= 1<<(bits-1) {
		return int64(v) - int64(1)<<bits
	}
	return int64(v)
}

const maxSafeInteger = 1<<53 - 1

// readValue decodes one fixed-size IO value. Numbers become float64 (what JSON
// round-trips to), 8-byte values beyond 2^53 become decimal strings.
func readValue(r *reader, size int, id int) (any, error) {
	var raw uint64
	switch size {
	case 1:
		v, err := r.u8()
		if err != nil {
			return nil, err
		}
		raw = uint64(v)
	case 2:
		v, err := r.u16()
		if err != nil {
			return nil, err
		}
		raw = uint64(v)
	case 4:
		v, err := r.u32()
		if err != nil {
			return nil, err
		}
		raw = uint64(v)
	default:
		v, err := r.u64()
		if err != nil {
			return nil, err
		}
		if v > maxSafeInteger {
			return strconv.FormatUint(v, 10), nil
		}
		return float64(v), nil
	}
	if isSigned(id) {
		return float64(toSigned(raw, size)), nil
	}
	return float64(raw), nil
}

// ParseAvl decodes a Codec 8 / 8E packet into records.
func ParseAvl(frame []byte) (*ParsedAvl, error) {
	if len(frame) < 10 {
		return nil, ErrShortFrame
	}
	codec := frame[8]
	ext := codec == Codec8E
	count := int(frame[9])
	r := &reader{b: frame, o: 10}
	rd := func() (int, error) {
		if ext {
			v, err := r.u16()
			return int(v), err
		}
		v, err := r.u8()
		return int(v), err
	}

	records := make([]dto.AvlRecord, 0, count)
	for i := 0; i < count; i++ {
		if err := r.need(24); err != nil {
			return nil, err
		}
		b := frame[r.o:]
		rec := dto.AvlRecord{
			AvlHead: dto.AvlHead{
				Time:     int64(binary.BigEndian.Uint64(b[0:8])),
				Priority: int(b[8]),
				Lng:      float64(int32(binary.BigEndian.Uint32(b[9:13]))) / 1e7,
				Lat:      float64(int32(binary.BigEndian.Uint32(b[13:17]))) / 1e7,
				Alt:      int(int16(binary.BigEndian.Uint16(b[17:19]))),
				Angle:    int(binary.BigEndian.Uint16(b[19:21])),
				Sats:     int(b[21]),
				Speed:    int(binary.BigEndian.Uint16(b[22:24])),
			},
			Io: dto.IoMap{},
		}
		r.o += 24
		ev, err := rd()
		if err != nil {
			return nil, err
		}
		rec.EventIo = ev
		if _, err := rd(); err != nil { // total IO count
			return nil, err
		}

		for _, size := range []int{1, 2, 4, 8} {
			n, err := rd()
			if err != nil {
				return nil, err
			}
			for k := 0; k < n; k++ {
				id, err := rd()
				if err != nil {
					return nil, err
				}
				v, err := readValue(r, size, id)
				if err != nil {
					return nil, err
				}
				rec.Io[strconv.Itoa(id)] = v
			}
		}

		if ext {
			nx, err := r.u16()
			if err != nil {
				return nil, err
			}
			for k := 0; k < int(nx); k++ {
				id, err := r.u16()
				if err != nil {
					return nil, err
				}
				l, err := r.u16()
				if err != nil {
					return nil, err
				}
				if err := r.need(int(l)); err != nil {
					return nil, err
				}
				rec.Io[strconv.Itoa(int(id))] = hex.EncodeToString(frame[r.o : r.o+int(l)])
				r.o += int(l)
			}
		}
		rec.Valid = !(rec.Lat == 0 && rec.Lng == 0)
		records = append(records, rec)
	}
	return &ParsedAvl{Codec: codec, Count: count, Records: records}, nil
}

func wrap(data []byte) []byte {
	out := make([]byte, 8+len(data)+4)
	binary.BigEndian.PutUint32(out[4:8], uint32(len(data)))
	copy(out[8:], data)
	binary.BigEndian.PutUint32(out[8+len(data):], CRC16(data))
	return out
}

// Codec 12 types: 0x05 = command (server -> device), 0x06 = response (device -> server).
const (
	Codec12Command  = 0x05
	Codec12Response = 0x06
)

// BuildCodec12 wraps an ASCII command/response in a Codec 12 packet.
func BuildCodec12(text string, typ byte) []byte {
	body := []byte(text)
	data := make([]byte, 8+len(body))
	data[0] = Codec12
	data[1] = 1
	data[2] = typ
	binary.BigEndian.PutUint32(data[3:7], uint32(len(body)))
	copy(data[7:], body)
	data[7+len(body)] = 1
	return wrap(data)
}

// ParseCodec12 extracts the type and text of a Codec 12 packet.
func ParseCodec12(frame []byte) (byte, string, error) {
	if len(frame) < 15 {
		return 0, "", ErrShortFrame
	}
	typ := frame[10]
	size := int(binary.BigEndian.Uint32(frame[11:15]))
	if 15+size > len(frame) {
		return 0, "", ErrShortFrame
	}
	return typ, string(frame[15 : 15+size]), nil
}

// OutIo is one IO value to encode (simulator).
type OutIo struct {
	ID   int
	V    int64
	Size int
}

// OutRecord is one record to encode as Codec 8E (simulator).
type OutRecord struct {
	Time     int64
	Priority int
	Lat      float64
	Lng      float64
	Alt      int
	Angle    int
	Sats     int
	Speed    int
	EventIo  int
	Io       []OutIo
}

// BuildAvl8E encodes records as a Codec 8E packet.
func BuildAvl8E(records []OutRecord) []byte {
	u16 := func(n int) []byte {
		b := make([]byte, 2)
		binary.BigEndian.PutUint16(b, uint16(n))
		return b
	}
	parts := []byte{Codec8E, byte(len(records))}
	for _, r := range records {
		head := make([]byte, 24)
		binary.BigEndian.PutUint64(head[0:8], uint64(r.Time))
		head[8] = byte(r.Priority)
		binary.BigEndian.PutUint32(head[9:13], uint32(int32(math.Round(r.Lng*1e7))))
		binary.BigEndian.PutUint32(head[13:17], uint32(int32(math.Round(r.Lat*1e7))))
		binary.BigEndian.PutUint16(head[17:19], uint16(int16(r.Alt)))
		binary.BigEndian.PutUint16(head[19:21], uint16(r.Angle))
		head[21] = byte(r.Sats)
		binary.BigEndian.PutUint16(head[22:24], uint16(r.Speed))
		parts = append(parts, head...)
		parts = append(parts, u16(r.EventIo)...)
		parts = append(parts, u16(len(r.Io))...)
		for _, size := range []int{1, 2, 4, 8} {
			var group []OutIo
			for _, x := range r.Io {
				if x.Size == size {
					group = append(group, x)
				}
			}
			parts = append(parts, u16(len(group))...)
			for _, x := range group {
				parts = append(parts, u16(x.ID)...)
				v := x.V
				if size < 8 && v < 0 {
					v += int64(1) << uint(size*8)
				}
				b := make([]byte, size)
				switch size {
				case 1:
					b[0] = byte(v)
				case 2:
					binary.BigEndian.PutUint16(b, uint16(v))
				case 4:
					binary.BigEndian.PutUint32(b, uint32(v))
				default:
					binary.BigEndian.PutUint64(b, uint64(v))
				}
				parts = append(parts, b...)
			}
		}
		parts = append(parts, u16(0)...) // empty NX
	}
	parts = append(parts, byte(len(records)))
	return wrap(parts)
}
