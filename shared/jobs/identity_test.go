package jobs

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func identityTestPostgres(t *testing.T) (*Postgres, string) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	repo, err := NewPostgres(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	userPrefix := "identity-test-" + uuid.NewString()
	t.Cleanup(func() {
		ctx := context.Background()
		repo.Pool().Exec(ctx, `DELETE FROM byos_objects WHERE user_id LIKE $1`, userPrefix+"%")
		repo.Pool().Exec(ctx, `DELETE FROM jobs WHERE user_id LIKE $1`, userPrefix+"%")
		repo.Close()
	})
	return repo, userPrefix
}

func TestCreateOnceConcurrentlyReusesLiveUserHash(t *testing.T) {
	repo, userID := identityTestPostgres(t)
	ctx := context.Background()
	const attempts = 8

	jobsByAttempt := make([]*Job, attempts)
	createdByAttempt := make([]bool, attempts)
	errs := make([]error, attempts)
	var wg sync.WaitGroup
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hash := "abcdef0123456789abcdef0123456789abcdef01"
			if i%2 == 0 {
				hash = "ABCDEF0123456789ABCDEF0123456789ABCDEF01"
			}
			job := &Job{UserID: userID, InfoHash: hash, Source: SourceTorrent, Status: StatusComplete}
			createdByAttempt[i], errs[i] = repo.CreateOnce(ctx, job)
			jobsByAttempt[i] = job
		}()
	}
	wg.Wait()

	createdCount := 0
	canonicalID := ""
	for i := range attempts {
		if errs[i] != nil {
			t.Fatalf("attempt %d: %v", i, errs[i])
		}
		if createdByAttempt[i] {
			createdCount++
		}
		if canonicalID == "" {
			canonicalID = jobsByAttempt[i].ID
		}
		if jobsByAttempt[i].ID != canonicalID {
			t.Fatalf("attempt %d returned id %q, want canonical %q", i, jobsByAttempt[i].ID, canonicalID)
		}
	}
	if createdCount != 1 {
		t.Fatalf("created %d rows, want 1", createdCount)
	}
}

func TestCreateOnceKeepsSameHashSeparateAcrossUsersAndBYOS(t *testing.T) {
	repo, userPrefix := identityTestPostgres(t)
	ctx := context.Background()
	firstUserID, secondUserID := userPrefix+"-first", userPrefix+"-second"
	hash := "1234567890abcdef1234567890abcdef12345678"

	first := &Job{UserID: firstUserID, InfoHash: hash, Source: SourceTorrent, Status: StatusComplete}
	second := &Job{UserID: secondUserID, InfoHash: hash, Source: SourceTorrent, Status: StatusComplete}
	if created, err := repo.CreateOnce(ctx, first); err != nil || !created {
		t.Fatalf("first create: created=%v err=%v", created, err)
	}
	if created, err := repo.CreateOnce(ctx, second); err != nil || !created {
		t.Fatalf("second create: created=%v err=%v", created, err)
	}
	if first.ID == second.ID {
		t.Fatalf("cross-user jobs unexpectedly share id %q", first.ID)
	}

	files := []File{{Index: 0, Name: "Show.S12E03.mkv", Size: 1234}}
	if err := repo.MarkBYOSObject(ctx, first.ID, firstUserID, hash, "first-bucket", "First", files); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkBYOSObject(ctx, second.ID, secondUserID, hash, "second-bucket", "Second", files); err != nil {
		t.Fatal(err)
	}
	firstObject, firstOK := repo.GetBYOSObjectByUserHash(ctx, firstUserID, hash)
	secondObject, secondOK := repo.GetBYOSObjectByUserHash(ctx, secondUserID, hash)
	if !firstOK || firstObject.Bucket != "first-bucket" {
		t.Fatalf("first user's BYOS object = %+v, ok=%v", firstObject, firstOK)
	}
	if !secondOK || secondObject.Bucket != "second-bucket" {
		t.Fatalf("second user's BYOS object = %+v, ok=%v", secondObject, secondOK)
	}
}

func TestCreateOnceAllowsNewLiveRowAfterEviction(t *testing.T) {
	repo, userID := identityTestPostgres(t)
	ctx := context.Background()
	hash := "9876543210abcdef9876543210abcdef98765432"

	evicted := &Job{UserID: userID, InfoHash: hash, Source: SourceUsenet, Status: StatusEvicted}
	if created, err := repo.CreateOnce(ctx, evicted); err != nil || !created {
		t.Fatalf("evicted history create: created=%v err=%v", created, err)
	}
	restored := &Job{UserID: userID, InfoHash: hash, Source: SourceUsenet, Status: StatusPending}
	if created, err := repo.CreateOnce(ctx, restored); err != nil || !created {
		t.Fatalf("restore create: created=%v err=%v", created, err)
	}
	if restored.ID == evicted.ID {
		t.Fatalf("restore reused evicted job id %q", restored.ID)
	}

	replayed := &Job{UserID: userID, InfoHash: hash, Source: SourceUsenet, Status: StatusPending}
	if created, err := repo.CreateOnce(ctx, replayed); err != nil || created {
		t.Fatalf("live replay: created=%v err=%v", created, err)
	}
	if replayed.ID != restored.ID {
		t.Fatalf("live replay id=%q, want restored id %q", replayed.ID, restored.ID)
	}
}
