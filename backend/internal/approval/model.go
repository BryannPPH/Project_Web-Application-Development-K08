// Package approval: migration hanya boleh dijalankan bila leader setiap group
// yang punya akses (view_only/view_edit) ke database menyetujui revisi yang sama.
package approval

import "time"

// Action = jenis eksekusi; setiap aksi butuh approval sendiri.
type Action string

const (
	ActionApply    Action = "apply"
	ActionRollback Action = "rollback"
	ActionForward  Action = "forward"
)

// Valid: hanya 3 nilai di atas yang diterima (sama dengan CHECK di SQL).
func (a Action) Valid() bool {
	return a == ActionApply || a == ActionRollback || a == ActionForward
}

// Decision = keputusan seorang leader.
type Decision string

const (
	DecisionApproved Decision = "approved"
	DecisionRejected Decision = "rejected"
)

// Valid: hanya approved / rejected.
func (d Decision) Valid() bool {
	return d == DecisionApproved || d == DecisionRejected
}

// Status review: approved, pending, rejected, atau blocked (ada group tanpa leader).
type Status string

const (
	StatusApproved Status = "approved"
	StatusPending  Status = "pending"
	StatusRejected Status = "rejected"
	StatusBlocked  Status = "blocked"
)

// Person: user ringkas untuk ditampilkan (tanpa data sensitif).
type Person struct {
	UserID   string `json:"userId"`
	Username string `json:"username"`
}

// RequiredGroup = group yang punya grant ke database. Leader nil = belum punya leader.
type RequiredGroup struct {
	GroupID     string  `json:"groupId"`
	GroupName   string  `json:"groupName"`
	AccessLevel string  `json:"accessLevel"` // "view_only" | "view_edit"
	Leader      *Person `json:"leader"`
}

// DecisionRecord = keputusan terbaru seorang approver untuk satu revisi.
type DecisionRecord struct {
	ApproverID       string    `json:"approverId"`
	ApproverUsername string    `json:"approverUsername"`
	Decision         Decision  `json:"decision"`
	Note             *string   `json:"note"`
	DecidedAt        time.Time `json:"decidedAt"`
}

// GroupReview = RequiredGroup + keputusan leader-nya (satu baris di UI review).
type GroupReview struct {
	RequiredGroup
	Decision  *Decision  `json:"decision"`
	Note      *string    `json:"note"`
	DecidedAt *time.Time `json:"decidedAt"`
}

// Target = migration yang sedang direview: status & revisi saat ini.
type Target struct {
	MigrationID   string
	DatabaseID    string
	Status        string // status migration_entries: draft, succeeded, ...
	Revision      int64
	PartialAction string
	Reversible    bool
}

// Review = jawaban GET .../approvals.
type Review struct {
	MigrationID   string        `json:"migrationId"`
	DatabaseID    string        `json:"databaseId"`
	Action        Action        `json:"action"`
	Revision      int64         `json:"revision"`
	Status        Status        `json:"status"`
	Groups        []GroupReview `json:"groups"`
	ApprovedCount int           `json:"approvedCount"`
	RequiredCount int           `json:"requiredCount"`
	CanDecide     bool          `json:"canDecide"`
	MyDecision    *Decision     `json:"myDecision"`
}

// DecideInput = body POST .../approvals. Revision = revisi yang dilihat leader.
type DecideInput struct {
	Action   Action   `json:"action"`
	Revision int64    `json:"revision"`
	Decision Decision `json:"decision"`
	Note     *string  `json:"note"`
}

// NewDecision = data yang disimpan repository (1 baris baru, append-only).
type NewDecision struct {
	MigrationID string
	Action      Action
	Revision    int64
	ApproverID  string
	Decision    Decision
	Note        *string
	DecidedAt   time.Time
}
