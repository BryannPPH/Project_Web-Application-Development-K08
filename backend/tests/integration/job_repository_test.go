package integration_test

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/migration"
)

func TestPGJobRepositoryClaimsParallelDatabasesAndSerializesSameDatabase(t *testing.T) {
	platformURL := os.Getenv("TEST_PLATFORM_DATABASE_URL")
	if platformURL == "" {
		t.Skip("TEST_PLATFORM_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	platform, err := pgxpool.New(ctx, platformURL)
	if err != nil {
		t.Fatalf("connect platform database: %v", err)
	}
	defer platform.Close()
	if err := platform.Ping(ctx); err != nil {
		t.Fatalf("ping platform database: %v", err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	var actorID, firstDatabaseID, secondDatabaseID string
	if err := platform.QueryRow(ctx, `INSERT INTO users (username,password_hash)
		VALUES ($1,'test-only') RETURNING id::text`, "queue_claim_"+suffix).Scan(&actorID); err != nil {
		t.Fatalf("create queue actor: %v", err)
	}
	for index, id := range []*string{&firstDatabaseID, &secondDatabaseID} {
		if err := platform.QueryRow(ctx, `INSERT INTO managed_databases
			(name,host,database_name,username,credential_ciphertext,ssl_mode)
			VALUES ($1,'localhost','target_db','target_owner',$2,'disable') RETURNING id::text`,
			fmt.Sprintf("queue_claim_db_%d_%s", index, suffix), []byte("test-only")).Scan(id); err != nil {
			t.Fatalf("create queue database %d: %v", index+1, err)
		}
	}
	var firstMigrationID, laterMigrationID, otherDatabaseMigrationID string
	for _, fixture := range []struct {
		databaseID string
		version    int
		name       string
		result     *string
	}{
		{firstDatabaseID, 1, "first migration", &firstMigrationID},
		{firstDatabaseID, 2, "later migration", &laterMigrationID},
		{secondDatabaseID, 1, "other database migration", &otherDatabaseMigrationID},
	} {
		if err := platform.QueryRow(ctx, `INSERT INTO migration_entries
			(database_id,version,name,sql_text,irreversible_reason,created_by)
			VALUES ($1::uuid,$2,$3,'SELECT 1','queue claim fixture',$4::uuid) RETURNING id::text`,
			fixture.databaseID, fixture.version, fixture.name, actorID).Scan(fixture.result); err != nil {
			t.Fatalf("create %s: %v", fixture.name, err)
		}
	}

	cleanup := func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = platform.Exec(cleanupCtx, `DELETE FROM migration_jobs WHERE database_id IN ($1::uuid,$2::uuid)`, firstDatabaseID, secondDatabaseID)
		_, _ = platform.Exec(cleanupCtx, `DELETE FROM migration_entries WHERE database_id IN ($1::uuid,$2::uuid)`, firstDatabaseID, secondDatabaseID)
		_, _ = platform.Exec(cleanupCtx, `DELETE FROM managed_databases WHERE id IN ($1::uuid,$2::uuid)`, firstDatabaseID, secondDatabaseID)
		_, _ = platform.Exec(cleanupCtx, `DELETE FROM users WHERE id=$1::uuid`, actorID)
	}
	t.Cleanup(cleanup)

	now := time.Now().UTC().Truncate(time.Microsecond)
	firstJobID := enqueueClaimFixture(t, ctx, platform, firstDatabaseID, firstMigrationID, actorID, now.Add(-3*time.Minute))
	laterJobID := enqueueClaimFixture(t, ctx, platform, firstDatabaseID, laterMigrationID, actorID, now.Add(-2*time.Minute))
	otherDatabaseJobID := enqueueClaimFixture(t, ctx, platform, secondDatabaseID, otherDatabaseMigrationID, actorID, now.Add(-time.Minute))
	var tokenA, tokenB string
	if err := platform.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&tokenA); err != nil {
		t.Fatalf("create first worker lease token: %v", err)
	}
	if err := platform.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&tokenB); err != nil {
		t.Fatalf("create second worker lease token: %v", err)
	}

	repository := migration.NewPGJobRepository(platform)
	start := make(chan struct{})
	results := make(chan claimResult, 2)
	for _, worker := range []struct{ id, token string }{{"worker-a", tokenA}, {"worker-b", tokenB}} {
		go func(workerID, leaseToken string) {
			<-start
			job, claimed, err := repository.ClaimNext(ctx, workerID, leaseToken, time.Minute, now)
			results <- claimResult{job: job, claimed: claimed, err: err}
		}(worker.id, worker.token)
	}
	close(start)
	claimedJobs := make(map[string]migration.Job, 2)
	for range 2 {
		result := <-results
		if result.err != nil || !result.claimed {
			t.Fatalf("concurrent claim: claimed=%v error=%v", result.claimed, result.err)
		}
		claimedJobs[result.job.ID] = result.job
	}
	if len(claimedJobs) != 2 {
		t.Fatalf("two workers claimed the same job: %#v", claimedJobs)
	}
	if _, ok := claimedJobs[firstJobID]; !ok {
		t.Fatalf("oldest same-database job was not claimed: %#v", claimedJobs)
	}
	if _, ok := claimedJobs[otherDatabaseJobID]; !ok {
		t.Fatalf("independent database job did not run in parallel: %#v", claimedJobs)
	}
	if _, ok := claimedJobs[laterJobID]; ok {
		t.Fatalf("later same-database migration was claimed before its predecessor: %#v", claimedJobs)
	}
	if _, claimed, err := repository.ClaimNext(ctx, "worker-c", tokenA, time.Minute, now); err != nil || claimed {
		t.Fatalf("same-database successor was claimable while predecessor ran: claimed=%v error=%v", claimed, err)
	}

	firstJob := claimedJobs[firstJobID]
	otherJob := claimedJobs[otherDatabaseJobID]
	if err := repository.Finish(ctx, firstJob, migration.JobSucceeded, nil, nil, now); err != nil {
		t.Fatalf("finish first database job: %v", err)
	}
	if err := repository.Finish(ctx, otherJob, migration.JobSucceeded, nil, nil, now); err != nil {
		t.Fatalf("finish independent database job: %v", err)
	}
	lastJob, claimed, err := repository.ClaimNext(ctx, "worker-c", tokenA, time.Minute, now)
	if err != nil || !claimed || lastJob.ID != laterJobID {
		t.Fatalf("same-database successor after predecessor finished: job=%s claimed=%v error=%v",
			lastJob.ID, claimed, err)
	}
}

type claimResult struct {
	job     migration.Job
	claimed bool
	err     error
}

func enqueueClaimFixture(t *testing.T, ctx context.Context, platform *pgxpool.Pool, databaseID, migrationID, actorID string, createdAt time.Time) string {
	t.Helper()
	var jobID string
	err := platform.QueryRow(ctx, `INSERT INTO migration_jobs
		(database_id,migration_id,requested_revision,action,execution_mode,actor_id,actor_username,
		 status,next_attempt_at,created_at,updated_at)
		VALUES ($1::uuid,$2::uuid,1,'apply','single',$3::uuid,'queue-test','queued',$4,$4,$4)
		RETURNING id::text`, databaseID, migrationID, actorID, createdAt).Scan(&jobID)
	if err != nil {
		t.Fatalf("enqueue queue claim fixture: %v", err)
	}
	return jobID
}
