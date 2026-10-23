package viewer

import (
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net/netip"
	"time"
	"unicode/utf8"
)

// maxCellRunes membatasi ukuran satu sel agar payload response tetap kecil.
const maxCellRunes = 2000

// normalizeValue mengubah nilai hasil pgx menjadi tipe JSON yang aman dan konsisten:
//   - nil, bool, string, dan integer kecil dipertahankan;
//   - integer di luar 2^53 dan float NaN/Inf menjadi string;
//   - waktu menjadi RFC 3339; bytea menjadi "\x..." heksadesimal;
//   - tipe lain (numeric, uuid, interval, inet, enum, ...) menjadi string.
//
// Nilai kedua bernilai true bila nilai dipotong.
func normalizeValue(value any) (any, bool) {
	switch typed := value.(type) {
	case nil:
		return nil, false
	case bool:
		return typed, false
	case string:
		return truncate(typed)
	case int8, int16, int32, uint8, uint16, uint32:
		return typed, false
	case int64:
		return safeInt(typed), false
	case int:
		return safeInt(int64(typed)), false
	case uint64:
		if typed > 1<<53 {
			return fmt.Sprint(typed), false
		}
		return typed, false
	case float32:
		return safeFloat(float64(typed)), false
	case float64:
		return safeFloat(typed), false
	case time.Time:
		return typed.Format(time.RFC3339Nano), false
	case []byte:
		encoded := `\x` + hex.EncodeToString(typed)
		return truncate(encoded)
	case [16]byte:
		return formatUUID(typed), false
	case netip.Prefix:
		return typed.String(), false
	case netip.Addr:
		return typed.String(), false
	case *big.Int:
		return typed.String(), false
	case map[string]any, []any:
		// json/jsonb atau array: pastikan bisa di-encode; bila tidak, jadikan string.
		encoded, err := json.Marshal(normalizeNested(typed))
		if err != nil {
			return truncate(fmt.Sprint(typed))
		}
		if utf8.RuneCount(encoded) > maxCellRunes {
			return truncate(string(encoded))
		}
		return json.RawMessage(encoded), false
	case driver.Valuer:
		raw, err := typed.Value()
		if err != nil {
			return truncate(fmt.Sprint(typed))
		}
		if text, ok := raw.(string); ok {
			return truncate(text)
		}
		return normalizeValue(raw)
	case fmt.Stringer:
		return truncate(typed.String())
	default:
		return truncate(fmt.Sprint(typed))
	}
}

func normalizeNested(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[key] = normalizeNested(item)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = normalizeNested(item)
		}
		return result
	default:
		normalized, _ := normalizeValue(typed)
		return normalized
	}
}

func safeInt(value int64) any {
	const maxSafe = 1<<53 - 1
	if value > maxSafe || value < -maxSafe {
		return fmt.Sprint(value)
	}
	return value
}

func safeFloat(value float64) any {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Sprint(value)
	}
	return value
}

func truncate(value string) (any, bool) {
	if utf8.RuneCountInString(value) <= maxCellRunes {
		return value, false
	}
	runes := []rune(value)
	return string(runes[:maxCellRunes]) + "…", true
}

func formatUUID(value [16]byte) string {
	encoded := hex.EncodeToString(value[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}
