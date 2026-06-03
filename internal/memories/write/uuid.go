package write

import (
	"strconv"
	"strings"
	"time"
)

// uuid is the 128-bit representation of a parsed UUID, stored big-endian in
// bytes, mirroring the subset of the Rust uuid crate behavior used by the
// file-stem generator.
type uuid struct {
	bytes [16]byte
}

// parseUUID parses a canonical hyphenated UUID string (8-4-4-4-12 hex). The ok
// flag mirrors Uuid::parse_str succeeding.
func parseUUID(s string) (uuid, bool) {
	parts := strings.Split(s, "-")
	if len(parts) != 5 {
		return uuid{}, false
	}
	expectedLens := [5]int{8, 4, 4, 4, 12}
	for i, p := range parts {
		if len(p) != expectedLens[i] {
			return uuid{}, false
		}
	}

	hex := strings.Join(parts, "")
	if len(hex) != 32 {
		return uuid{}, false
	}
	var u uuid
	for i := 0; i < 16; i++ {
		v, err := strconv.ParseUint(hex[i*2:i*2+2], 16, 8)
		if err != nil {
			return uuid{}, false
		}
		u.bytes[i] = byte(v)
	}
	return u, true
}

// version returns the UUID version nibble (the high nibble of byte 6).
func (u uuid) version() int {
	return int(u.bytes[6] >> 4)
}

// asU128Low32 returns the low 32 bits of the 128-bit value, mirroring
// `(uuid.as_u128() & 0xFFFF_FFFF) as u32`.
func (u uuid) asU128Low32() uint32 {
	return uint32(u.bytes[12])<<24 |
		uint32(u.bytes[13])<<16 |
		uint32(u.bytes[14])<<8 |
		uint32(u.bytes[15])
}

// uuidTimestamp extracts the embedded timestamp for time-based UUID versions
// (v1, v6, v7), mirroring Uuid::get_timestamp().to_unix() followed by
// DateTime::<Utc>::from_timestamp. The ok flag is false when the UUID carries no
// timestamp (e.g. v4) or the value is out of range.
func uuidTimestamp(u uuid) (time.Time, bool) {
	switch u.version() {
	case 7:
		// v7: first 48 bits are Unix epoch milliseconds.
		millis := int64(u.bytes[0])<<40 |
			int64(u.bytes[1])<<32 |
			int64(u.bytes[2])<<24 |
			int64(u.bytes[3])<<16 |
			int64(u.bytes[4])<<8 |
			int64(u.bytes[5])
		seconds := millis / 1000
		nanos := (millis % 1000) * 1_000_000
		return time.Unix(seconds, nanos).UTC(), true
	case 1, 6:
		// v1/v6: 60-bit count of 100ns intervals since 1582-10-15 (the Gregorian
		// epoch). to_unix() converts to (seconds, subsec-nanos) since the Unix
		// epoch. The 100ns ticks between the two epochs is a fixed constant.
		ticks := gregorianTicks(u)
		const gregorianToUnix100ns = 0x01B2_1DD2_1381_4000
		unixTicks := int64(ticks) - gregorianToUnix100ns
		seconds := unixTicks / 10_000_000
		subsec100ns := unixTicks % 10_000_000
		if subsec100ns < 0 {
			seconds--
			subsec100ns += 10_000_000
		}
		nanos := subsec100ns * 100
		return time.Unix(seconds, nanos).UTC(), true
	default:
		return time.Time{}, false
	}
}

// gregorianTicks reconstructs the 60-bit 100ns timestamp for v1 (time_low,
// time_mid, time_hi) and v6 (reordered) UUIDs.
func gregorianTicks(u uuid) uint64 {
	b := u.bytes
	switch u.version() {
	case 6:
		// v6 stores the timestamp in big-endian most-significant-first order
		// across bytes 0..8 with the version nibble embedded in byte 6.
		timeHighAndMid := uint64(b[0])<<40 | uint64(b[1])<<32 | uint64(b[2])<<24 |
			uint64(b[3])<<16 | uint64(b[4])<<8 | uint64(b[5])
		timeLow := uint64(b[6]&0x0F)<<8 | uint64(b[7])
		return timeHighAndMid<<12 | timeLow
	default: // version 1
		timeLow := uint64(b[0])<<24 | uint64(b[1])<<16 | uint64(b[2])<<8 | uint64(b[3])
		timeMid := uint64(b[4])<<8 | uint64(b[5])
		timeHi := uint64(b[6]&0x0F)<<8 | uint64(b[7])
		return timeHi<<48 | timeMid<<32 | timeLow
	}
}
