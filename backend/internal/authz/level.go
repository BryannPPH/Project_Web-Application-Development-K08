// Package authz adalah satu-satunya sumber keputusan authorization platform.
//
// Authentication (package auth/session) menjawab "siapa"; package ini menjawab
// "boleh melakukan apa". Akses basis data hanya berasal dari group:
//
//	effective access = level tertinggi dari seluruh grant group yang diikuti user
//	none < view_only < view_edit
//
// Role platform_admin tidak pernah memberi akses basis data secara implisit.
package authz

import (
	"errors"
	"strings"
)

// AccessLevel adalah tingkat akses terhadap satu managed database.
type AccessLevel string

const (
	AccessNone     AccessLevel = "none"
	AccessViewOnly AccessLevel = "view_only"
	AccessViewEdit AccessLevel = "view_edit"
)

// ErrInvalidAccessLevel menandai nilai access level yang tidak dikenal.
var ErrInvalidAccessLevel = errors.New("access level must be view_only or view_edit")

func (l AccessLevel) rank() int {
	switch l {
	case AccessViewOnly:
		return 1
	case AccessViewEdit:
		return 2
	default:
		return 0
	}
}

// Valid bernilai true untuk ketiga level yang dikenal, termasuk none.
func (l AccessLevel) Valid() bool {
	return l == AccessNone || l == AccessViewOnly || l == AccessViewEdit
}

// Grantable bernilai true untuk level yang boleh disimpan sebagai grant group.
func (l AccessLevel) Grantable() bool {
	return l == AccessViewOnly || l == AccessViewEdit
}

// AtLeast membandingkan ranking level.
func (l AccessLevel) AtLeast(minimum AccessLevel) bool {
	return l.rank() >= minimum.rank()
}

// CanView bernilai true untuk view_only dan view_edit.
func (l AccessLevel) CanView() bool {
	return l.AtLeast(AccessViewOnly)
}

// CanEdit hanya bernilai true untuk view_edit.
func (l AccessLevel) CanEdit() bool {
	return l.AtLeast(AccessViewEdit)
}

// Highest menghitung effective access dari beberapa grant.
func Highest(levels ...AccessLevel) AccessLevel {
	highest := AccessNone
	for _, level := range levels {
		if level.rank() > highest.rank() {
			highest = level
		}
	}
	return highest
}

// ParseGrantLevel memvalidasi input admin untuk grant group.
func ParseGrantLevel(raw string) (AccessLevel, error) {
	level := AccessLevel(strings.TrimSpace(raw))
	if !level.Grantable() {
		return "", ErrInvalidAccessLevel
	}
	return level, nil
}

// levelFromRank mengubah ranking hasil query SQL menjadi AccessLevel.
func levelFromRank(rank int) AccessLevel {
	switch {
	case rank >= 2:
		return AccessViewEdit
	case rank == 1:
		return AccessViewOnly
	default:
		return AccessNone
	}
}
