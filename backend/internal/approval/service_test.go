package approval

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz/authztest"
)

const (
	dbID        = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	migrationID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
)

type fakeRepository struct {
	targets   map[string]Target          // migrationID -> target
	groups    map[string][]RequiredGroup // databaseID  -> group wajib
	usernames map[string]string          // userID      -> username
	decisions []NewDecision              // append-only, sama seperti tabel asli
}

func (f *fakeRepository) Target(_ context.Context, databaseID, id string) (Target, error) {
	target, ok := f.targets[id]
	if !ok || target.DatabaseID != databaseID {
		return Target{}, ErrNotFound
	}
	return target, nil
}

func (f *fakeRepository) RequiredGroups(_ context.Context, databaseID string) ([]RequiredGroup, error) {
	return append([]RequiredGroup(nil), f.groups[databaseID]...), nil
}

// LatestDecisions meniru DISTINCT ON: baris yang ditambahkan belakangan menang.
func (f *fakeRepository) LatestDecisions(_ context.Context, id string, action Action, revision int64) (map[string]DecisionRecord, error) {
	result := make(map[string]DecisionRecord)
	for _, d := range f.decisions {
		if d.MigrationID == id && d.Action == action && d.Revision == revision {
			result[d.ApproverID] = DecisionRecord{ApproverID: d.ApproverID, ApproverUsername: f.usernames[d.ApproverID],
				Decision: d.Decision, Note: d.Note, DecidedAt: d.DecidedAt}
		}
	}
	return result, nil
}

// InsertDecision meniru "INSERT ... WHERE revision = $3": revisi basi tidak tersimpan.
func (f *fakeRepository) InsertDecision(_ context.Context, d NewDecision) error {
	if f.targets[d.MigrationID].Revision != d.Revision {
		return ErrStaleRevision
	}
	f.decisions = append(f.decisions, d)
	return nil
}

// editMigration meniru trigger SQL: SQL diedit -> revisi naik 1.
func (f *fakeRepository) editMigration(id string) {
	target := f.targets[id]
	target.Revision++
	f.targets[id] = target
}

type world struct {
	service  *Service
	repo     *fakeRepository
	store    *authztest.Store
	budi     auth.Principal // leader Tim Backend (view_edit)
	rina     auth.Principal // anggota Tim Backend, bukan leader
	sari     auth.Principal // leader Analis Data (view_only)
	tono     auth.Principal // leader Tim Marketing (tanpa grant), anggota Tim Backend
	outsider auth.Principal // tidak punya akses apa pun ke Produksi
}

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func newWorld(t *testing.T) *world {
	t.Helper()
	store := authztest.NewStore()
	store.AddDatabase(dbID, true)
	w := &world{
		store:    store,
		budi:     store.AddUser("budi", auth.PlatformRoleUser),
		rina:     store.AddUser("rina", auth.PlatformRoleUser),
		sari:     store.AddUser("sari", auth.PlatformRoleUser),
		tono:     store.AddUser("tono", auth.PlatformRoleUser),
		outsider: store.AddUser("orang_luar", auth.PlatformRoleUser),
	}
	// Grant di authz (menentukan siapa boleh MELIHAT database).
	store.Grant("tim-backend", dbID, authz.AccessViewEdit, w.budi.UserID, w.rina.UserID, w.tono.UserID)
	store.Grant("analis-data", dbID, authz.AccessViewOnly, w.sari.UserID)
	store.AddMember("tim-marketing", w.tono.UserID) // tanpa grant ke Produksi

	// Data approval (yang dibaca repository.RequiredGroups dari SQL).
	w.repo = &fakeRepository{
		targets: map[string]Target{
			migrationID: {MigrationID: migrationID, DatabaseID: dbID, Status: "draft", Revision: 1},
		},
		groups: map[string][]RequiredGroup{
			dbID: {
				{GroupID: "g-analis", GroupName: "Analis Data", AccessLevel: "view_only", Leader: &Person{w.sari.UserID, "sari"}},
				{GroupID: "g-backend", GroupName: "Tim Backend", AccessLevel: "view_edit", Leader: &Person{w.budi.UserID, "budi"}},
			},
		},
		usernames: map[string]string{w.budi.UserID: "budi", w.sari.UserID: "sari", w.tono.UserID: "tono"},
	}
	service, err := NewService(w.repo, store.Policy(), fixedClock{now: time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	w.service = service
	return w
}

func approve(revision int64) DecideInput {
	return DecideInput{Action: ActionApply, Revision: revision, Decision: DecisionApproved}
}

func (w *world) decide(t *testing.T, who auth.Principal, input DecideInput) (Review, error) {
	t.Helper()
	return w.service.Decide(context.Background(), who, dbID, migrationID, input)
}

func (w *world) mustDecide(t *testing.T, who auth.Principal, input DecideInput) Review {
	t.Helper()
	review, err := w.decide(t, who, input)
	if err != nil {
		t.Fatalf("Decide(%s) error = %v", who.Username, err)
	}
	return review
}

func (w *world) gate(revision int64) error {
	return w.service.RequireApproved(context.Background(), dbID, migrationID, string(ActionApply), revision)
}

func TestRequireApproved_AllLeadersApproved(t *testing.T) {
	w := newWorld(t)
	w.mustDecide(t, w.budi, approve(1))
	review := w.mustDecide(t, w.sari, approve(1))

	if review.Status != StatusApproved || review.ApprovedCount != 2 || review.RequiredCount != 2 {
		t.Fatalf("review = %+v, want approved 2/2", review)
	}
	if err := w.gate(1); err != nil {
		t.Fatalf("RequireApproved() = %v, want nil", err)
	}
}

// Hanya budi (view_edit) yang setuju. Leader group view_only (sari) belum.
// Ini membuktikan group view_only JUGA wajib menyetujui, bukan hanya view_edit.
func TestRequireApproved_PartialApproval(t *testing.T) {
	w := newWorld(t)
	review := w.mustDecide(t, w.budi, approve(1))
	if review.Status != StatusPending || review.ApprovedCount != 1 {
		t.Fatalf("review = %+v, want pending 1/2", review)
	}

	err := w.gate(1)
	if !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("RequireApproved() = %v, want ErrApprovalRequired", err)
	}
	var incomplete *IncompleteError
	if !errors.As(err, &incomplete) || incomplete.Status != StatusPending ||
		len(incomplete.Waiting) != 1 || incomplete.Waiting[0] != "Analis Data" {
		t.Fatalf("incomplete = %+v, want pending waiting for [Analis Data]", incomplete)
	}
	public, _ := Describe(err)
	if public.Status != 409 || public.Code != "APPROVAL_REQUIRED" {
		t.Fatalf("Describe() = %+v, want 409 APPROVAL_REQUIRED", public)
	}
}

func TestRequireApproved_NoDecisionsYet(t *testing.T) {
	w := newWorld(t)
	if err := w.gate(1); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("RequireApproved() = %v, want ErrApprovalRequired", err)
	}
}

// Satu penolakan menang, walaupun leader lain menyetujui.
func TestRequireApproved_RejectionBlocks(t *testing.T) {
	w := newWorld(t)
	w.mustDecide(t, w.budi, approve(1))
	note := "kolom email harus nullable dulu"
	review := w.mustDecide(t, w.sari, DecideInput{Action: ActionApply, Revision: 1, Decision: DecisionRejected, Note: &note})
	if review.Status != StatusRejected {
		t.Fatalf("status = %s, want rejected", review.Status)
	}
	var incomplete *IncompleteError
	if err := w.gate(1); !errors.As(err, &incomplete) || incomplete.Status != StatusRejected {
		t.Fatalf("RequireApproved() = %v, want rejected", err)
	}
}

// Group wajib tanpa leader -> blocked (fail closed), tidak pernah lolos.
func TestRequireApproved_GroupWithoutLeaderBlocks(t *testing.T) {
	w := newWorld(t)
	w.repo.groups[dbID][0].Leader = nil // Analis Data kehilangan leader
	w.mustDecide(t, w.budi, approve(1))

	var incomplete *IncompleteError
	if err := w.gate(1); !errors.As(err, &incomplete) || incomplete.Status != StatusBlocked {
		t.Fatalf("RequireApproved() = %v, want blocked", err)
	}
	if public, _ := Describe(&IncompleteError{Status: StatusBlocked}); !strings.Contains(public.Message, "no leader") {
		t.Fatalf("blocked message should tell the user a leader is missing, got %q", public.Message)
	}
}

// Admin memberi akses ke group BARU setelah semua setuju -> kembali pending.
func TestRequireApproved_NewGroupAccessReopensReview(t *testing.T) {
	w := newWorld(t)
	w.mustDecide(t, w.budi, approve(1))
	w.mustDecide(t, w.sari, approve(1))
	w.repo.groups[dbID] = append(w.repo.groups[dbID], RequiredGroup{
		GroupID: "g-marketing", GroupName: "Tim Marketing", AccessLevel: "view_only", Leader: &Person{w.tono.UserID, "tono"},
	})
	if err := w.gate(1); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("RequireApproved() = %v, want approval required for the new group", err)
	}
}

func TestEditAfterApprovalRequiresReapproval(t *testing.T) {
	w := newWorld(t)
	w.mustDecide(t, w.budi, approve(1))
	w.mustDecide(t, w.sari, approve(1))
	if err := w.gate(1); err != nil {
		t.Fatalf("before edit: RequireApproved(1) = %v", err)
	}

	w.repo.editMigration(migrationID) // SQL diubah -> revisi 2

	if err := w.gate(2); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("after edit: RequireApproved(2) = %v, want approval required", err)
	}
	review, err := w.service.Review(context.Background(), w.budi, dbID, migrationID, ActionApply)
	if err != nil {
		t.Fatal(err)
	}
	if review.Revision != 2 || review.Status != StatusPending || review.ApprovedCount != 0 || review.MyDecision != nil {
		t.Fatalf("review after edit = %+v, want revision 2 pending 0/2", review)
	}
	// Approval lama tetap tersimpan (audit), hanya tidak dihitung.
	if len(w.repo.decisions) != 2 {
		t.Fatalf("old decisions must be kept for audit, got %d", len(w.repo.decisions))
	}
}

// Leader membuka halaman di revisi 1, lalu pembuat migration mengedit (revisi 2).
// Tombol "Setujui" yang ditekan masih mengirim revisi 1 -> ditolak.
func TestDecide_StaleRevisionRejected(t *testing.T) {
	w := newWorld(t)
	w.repo.editMigration(migrationID)
	if _, err := w.decide(t, w.budi, approve(1)); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("Decide(stale) = %v, want ErrStaleRevision", err)
	}
	if len(w.repo.decisions) != 0 {
		t.Fatal("a stale decision must not be stored")
	}
}

// tono memimpin Tim Marketing (tidak punya grant ke Produksi). Ia BISA melihat
// Produksi karena anggota Tim Backend, tapi BUKAN leader group wajib -> 403.
func TestDecide_LeaderOfOtherGroupForbidden(t *testing.T) {
	w := newWorld(t)
	_, err := w.decide(t, w.tono, approve(1))
	if !errors.Is(err, ErrNotRequiredLeader) {
		t.Fatalf("Decide(tono) = %v, want ErrNotRequiredLeader", err)
	}
	if public, _ := Describe(err); public.Status != 403 || public.Code != "APPROVAL_NOT_LEADER" {
		t.Fatalf("Describe() = %+v, want 403 APPROVAL_NOT_LEADER", public)
	}
}

// rina punya view_edit (anggota Tim Backend) tapi bukan leader -> 403.
// Akses edit TIDAK sama dengan hak approval.
func TestDecide_MemberWhoIsNotLeaderForbidden(t *testing.T) {
	w := newWorld(t)
	if _, err := w.decide(t, w.rina, approve(1)); !errors.Is(err, ErrNotRequiredLeader) {
		t.Fatalf("Decide(rina) = %v, want ErrNotRequiredLeader", err)
	}
}

// Orang tanpa akses ke database sama sekali -> 404 dari authz (database
// disembunyikan, sama seperti viewer & migration Milestone 1).
func TestDecide_NoDatabaseAccessHidden(t *testing.T) {
	w := newWorld(t)
	_, err := w.decide(t, w.outsider, approve(1))
	if !errors.Is(err, authz.ErrDatabaseNotFound) {
		t.Fatalf("Decide(outsider) = %v, want authz.ErrDatabaseNotFound", err)
	}
	if _, err := w.service.Review(context.Background(), w.outsider, dbID, migrationID, ActionApply); !errors.Is(err, authz.ErrDatabaseNotFound) {
		t.Fatalf("Review(outsider) = %v, want authz.ErrDatabaseNotFound", err)
	}
}

// Leader view_only boleh MEMUTUSKAN walaupun tidak boleh MENGEDIT/menjalankan.
func TestDecide_ViewOnlyLeaderCanDecide(t *testing.T) {
	w := newWorld(t)
	review := w.mustDecide(t, w.sari, approve(1))
	if review.MyDecision == nil || *review.MyDecision != DecisionApproved {
		t.Fatalf("myDecision = %v, want approved", review.MyDecision)
	}
}

func TestDecide_RejectRequiresNote(t *testing.T) {
	w := newWorld(t)
	blank := "   "
	for _, note := range []*string{nil, &blank} {
		input := DecideInput{Action: ActionApply, Revision: 1, Decision: DecisionRejected, Note: note}
		if _, err := w.decide(t, w.budi, input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("reject without note = %v, want ErrInvalidInput", err)
		}
	}
}

func TestDecide_InvalidInput(t *testing.T) {
	w := newWorld(t)
	long := strings.Repeat("x", maxNoteRunes+1)
	cases := map[string]DecideInput{
		"unknown action":   {Action: "delete", Revision: 1, Decision: DecisionApproved},
		"unknown decision": {Action: ActionApply, Revision: 1, Decision: "maybe"},
		"zero revision":    {Action: ActionApply, Revision: 0, Decision: DecisionApproved},
		"note too long":    {Action: ActionApply, Revision: 1, Decision: DecisionApproved, Note: &long},
	}
	for name, input := range cases {
		if _, err := w.decide(t, w.budi, input); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: err = %v, want ErrInvalidInput", name, err)
		}
	}
}

// Migration yang sudah jalan (succeeded) tidak bisa di-approve untuk "apply" lagi.
func TestDecide_AppliedMigrationNotReviewable(t *testing.T) {
	w := newWorld(t)
	target := w.repo.targets[migrationID]
	target.Status = "succeeded"
	w.repo.targets[migrationID] = target
	if _, err := w.decide(t, w.budi, approve(1)); !errors.Is(err, ErrNotReviewable) {
		t.Fatalf("Decide(apply on succeeded) = %v, want ErrNotReviewable", err)
	}
	// ...tapi rollback-nya BISA direview.
	rollback := DecideInput{Action: ActionRollback, Revision: 1, Decision: DecisionApproved}
	if _, err := w.decide(t, w.budi, rollback); err != nil {
		t.Fatalf("Decide(rollback on succeeded) = %v, want nil", err)
	}
}

// Approval "apply" TIDAK berlaku untuk "rollback": setiap aksi butuh approval sendiri.
func TestApprovalIsPerAction(t *testing.T) {
	w := newWorld(t)
	w.mustDecide(t, w.budi, approve(1))
	w.mustDecide(t, w.sari, approve(1))
	err := w.service.RequireApproved(context.Background(), dbID, migrationID, string(ActionRollback), 1)
	if !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("rollback gate = %v, want ErrApprovalRequired", err)
	}
}

// Berubah pikiran: menolak lalu menyetujui -> keputusan TERBARU yang dihitung.
func TestDecide_LatestDecisionWins(t *testing.T) {
	w := newWorld(t)
	note := "tunggu index dibuat"
	w.mustDecide(t, w.sari, DecideInput{Action: ActionApply, Revision: 1, Decision: DecisionRejected, Note: &note})
	w.mustDecide(t, w.sari, approve(1))
	review := w.mustDecide(t, w.budi, approve(1))
	if review.Status != StatusApproved {
		t.Fatalf("status = %s, want approved (latest decision wins)", review.Status)
	}
	if len(w.repo.decisions) != 3 {
		t.Fatalf("all decisions are kept (append-only), got %d", len(w.repo.decisions))
	}
}

// CanDecide hanya true untuk leader group wajib, dan hanya saat bisa direview.
func TestReview_CanDecide(t *testing.T) {
	w := newWorld(t)
	for _, tc := range []struct {
		who  auth.Principal
		want bool
	}{{w.budi, true}, {w.sari, true}, {w.rina, false}, {w.tono, false}} {
		review, err := w.service.Review(context.Background(), tc.who, dbID, migrationID, ActionApply)
		if err != nil {
			t.Fatalf("Review(%s) = %v", tc.who.Username, err)
		}
		if review.CanDecide != tc.want {
			t.Errorf("Review(%s).CanDecide = %v, want %v", tc.who.Username, review.CanDecide, tc.want)
		}
	}
}

func TestEvaluateTruthTable(t *testing.T) {
	leader := func(id string) *Person { return &Person{UserID: id, Username: id} }
	approved := DecisionRecord{Decision: DecisionApproved}
	rejected := DecisionRecord{Decision: DecisionRejected}
	cases := []struct {
		name      string
		groups    []RequiredGroup
		decisions map[string]DecisionRecord
		want      Status
	}{
		{"no groups", nil, nil, StatusBlocked},
		{"missing leader", []RequiredGroup{{Leader: nil}}, nil, StatusBlocked},
		{"none decided", []RequiredGroup{{Leader: leader("a")}}, nil, StatusPending},
		{"all approved", []RequiredGroup{{Leader: leader("a")}, {Leader: leader("b")}},
			map[string]DecisionRecord{"a": approved, "b": approved}, StatusApproved},
		{"one rejected", []RequiredGroup{{Leader: leader("a")}, {Leader: leader("b")}},
			map[string]DecisionRecord{"a": approved, "b": rejected}, StatusRejected},
		{"same leader for two groups", []RequiredGroup{{Leader: leader("a")}, {Leader: leader("a")}},
			map[string]DecisionRecord{"a": approved}, StatusApproved},
		{"missing leader beats rejection", []RequiredGroup{{Leader: nil}, {Leader: leader("b")}},
			map[string]DecisionRecord{"b": rejected}, StatusBlocked},
	}
	for _, tc := range cases {
		if got, _, _ := evaluate(tc.groups, tc.decisions); got != tc.want {
			t.Errorf("%s: evaluate() = %s, want %s", tc.name, got, tc.want)
		}
	}
}
