package viewer

import (
	"strconv"
	"strings"
)

const (
	DefaultLimit = 50
	MaxLimit     = 100
	// maxOffset mencegah OFFSET ekstrem yang memaksa target memindai jutaan baris.
	maxOffset = 10_000_000
)

// Page adalah parameter pagination yang sudah tervalidasi.
type Page struct {
	Limit  int
	Offset int64
}

// ParsePage memvalidasi query string limit/offset.
// Kosong -> default (limit 50, offset 0). Syarat: 1 <= limit <= 100, offset >= 0.
func ParsePage(rawLimit, rawOffset string) (Page, error) {
	page := Page{Limit: DefaultLimit}
	if value := strings.TrimSpace(rawLimit); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil || limit < 1 || limit > MaxLimit {
			return Page{}, ErrInvalidPagination
		}
		page.Limit = limit
	}
	if value := strings.TrimSpace(rawOffset); value != "" {
		offset, err := strconv.ParseInt(value, 10, 64)
		if err != nil || offset < 0 || offset > maxOffset {
			return Page{}, ErrInvalidPagination
		}
		page.Offset = offset
	}
	return page, nil
}
