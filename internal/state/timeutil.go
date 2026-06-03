package state

import (
	"fmt"
	"time"
)

// minEpochMillis is the millisecond timestamp for 2020-01-01T00:00:00Z.
//
// Values smaller than this, when interpreted as milliseconds, are legacy
// second-precision rows. They are scaled up to milliseconds in memory so that
// old state databases keep correct ordering after newer writes use millis.
const minEpochMillis int64 = 1_577_836_800_000

// datetimeToEpochMillis returns the Unix epoch time of t in milliseconds.
func datetimeToEpochMillis(t time.Time) int64 {
	return t.UnixMilli()
}

// datetimeToEpochSeconds returns the Unix epoch time of t in seconds.
func datetimeToEpochSeconds(t time.Time) int64 {
	return t.Unix()
}

// epochMillisToDatetime converts a stored timestamp to UTC time.
//
// Legacy second-precision values (below minEpochMillis) are scaled to
// milliseconds first, matching the Rust `epoch_millis_to_datetime`.
func epochMillisToDatetime(value int64) (time.Time, error) {
	millis := value
	if value < minEpochMillis {
		millis = saturatingMul1000(value)
	}
	// time.UnixMilli accepts any int64; mirror the Rust None case only for the
	// theoretical overflow boundary, which Go does not hit for int64 millis.
	return time.UnixMilli(millis).UTC(), nil
}

// epochSecondsToDatetime converts a Unix-seconds timestamp to UTC time.
func epochSecondsToDatetime(value int64) (time.Time, error) {
	if value < -62135596800 || value > 253402300799 {
		return time.Time{}, fmt.Errorf("invalid unix timestamp seconds: %d", value)
	}
	return time.Unix(value, 0).UTC(), nil
}

// canonicalizeDatetime round-trips t through millisecond precision so values
// stored and read back compare equal, matching the Rust builder behavior.
func canonicalizeDatetime(t time.Time) time.Time {
	out, err := epochMillisToDatetime(datetimeToEpochMillis(t))
	if err != nil {
		return t
	}
	return out
}

// saturatingMul1000 multiplies v by 1000, clamping at int64 bounds, mirroring
// Rust's `saturating_mul`.
func saturatingMul1000(v int64) int64 {
	const maxI64 = int64(^uint64(0) >> 1)
	const minI64 = -maxI64 - 1
	if v > maxI64/1000 {
		return maxI64
	}
	if v < minI64/1000 {
		return minI64
	}
	return v * 1000
}
