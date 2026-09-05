package jobs

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestAdmitConcurrentIdentityAndFullQueueReplay(t *testing.T) {
	repo, userID := queueTestPostgres(t)
	ctx := context.Background()
	const attempts = 16
	dispositions := make([]Admission, attempts)
	results := make([]*Job, attempts)
	errs := make([]error, attempts)
	var wg sync.WaitGroup
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hash := "abcdef0123456789abcdef0123456789abcdef01"
			if i%2 == 0 {
				hash = strings.ToUpper(hash)
			}
			results[i] = &Job{UserID: userID, InfoHash: hash, Source: SourceTorrent,
				BudgetGated: true, InputKey: "torrent-input/original", Season: 2, Episode: 3}
			dispositions[i], errs[i] = repo.Admit(ctx, results[i], 1, 1, 1_000_000_000_000)
		}()
	}
	wg.Wait()
	admitted := 0
	for i := range attempts {
		if errs[i] != nil || results[i].ID != results[0].ID {
			t.Fatalf("attempt %d: job=%+v err=%v", i, results[i], errs[i])
		}
		if dispositions[i] == AdmissionAdmitted {
			admitted++
		} else if dispositions[i] != AdmissionExisting {
			t.Fatalf("attempt %d: unexpected disposition %s", i, dispositions[i])
		}
	}
	if admitted != 1 {
		t.Fatalf("assigned %d times, want 1", admitted)
	}
	stored, err := repo.Get(ctx, results[0].ID)
	if err != nil || !stored.BudgetGated || stored.InputKey != "torrent-input/original" || stored.Season != 2 || stored.Episode != 3 {
		t.Fatalf("internal policy/input or episode metadata lost: job=%+v err=%v", stored, err)
	}
	waiting := &Job{UserID: userID, InfoHash: "other-hash", Source: SourceTorrent}
	if d, err := repo.Admit(ctx, waiting, 1, 1, 0); err != nil || d != AdmissionQueued {
		t.Fatalf("fill queue: %s %v", d, err)
	}
	replay := &Job{UserID: userID, InfoHash: stored.InfoHash, Source: SourceTorrent, BudgetGated: true}
	if d, err := repo.Admit(ctx, replay, 1, 1, 0); err != nil || d != AdmissionExisting || replay.ID != stored.ID {
		t.Fatalf("full queue rejected replay: %s %+v %v", d, replay, err)
	}
	newJob := &Job{UserID: userID, InfoHash: "new-hash", Source: SourceTorrent}
	if _, err := repo.Admit(ctx, newJob, 1, 1, 0); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("new job should hit queue cap: %v", err)
	}
}

func TestAdmitRacesCachedCreateOnce(t *testing.T) {
	repo, userID := queueTestPostgres(t)
	ctx := context.Background()
	const attempts = 12
	results := make([]*Job, attempts)
	created := make([]bool, attempts)
	errs := make([]error, attempts)
	var wg sync.WaitGroup
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			job := &Job{UserID: userID, InfoHash: "same-cached-hash", Source: SourceTorrent, Status: StatusComplete}
			results[i] = job
			if i%2 == 0 {
				created[i], errs[i] = repo.CreateOnce(ctx, job)
			} else {
				d, err := repo.Admit(ctx, job, 1, 1, 0)
				created[i], errs[i] = d == AdmissionAdmitted, err
			}
		}()
	}
	wg.Wait()
	count := 0
	for i := range attempts {
		if errs[i] != nil || results[i].ID != results[0].ID {
			t.Fatalf("attempt %d: result=%+v err=%v", i, results[i], errs[i])
		}
		if created[i] {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("created %d canonical rows, want 1", count)
	}
}

func TestReadmitReusesCanonicalAndDoesNotAssignTwice(t *testing.T) {
	repo, userID := queueTestPostgres(t)
	ctx := context.Background()
	old := &Job{UserID: userID, InfoHash: "retry-hash", Source: SourceTorrent, Status: StatusFailed}
	if err := repo.Create(ctx, old); err != nil {
		t.Fatal(err)
	}
	const attempts = 8
	dispositions := make([]Admission, attempts)
	errs := make([]error, attempts)
	var wg sync.WaitGroup
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dispositions[i], _, errs[i] = repo.Readmit(ctx, old.ID, 1, 1, 1_000_000_000_000)
		}()
	}
	wg.Wait()
	admitted := 0
	for i, d := range dispositions {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if d == AdmissionAdmitted {
			admitted++
		} else if d != AdmissionExisting {
			t.Fatalf("unexpected retry result: %s", d)
		}
	}
	if admitted != 1 {
		t.Fatalf("retry assigned %d times", admitted)
	}
	if err := repo.Update(ctx, old); err != nil { // Restore failed history.
		t.Fatal(err)
	}
	canonical := &Job{UserID: userID, InfoHash: old.InfoHash, Source: SourceTorrent, Status: StatusComplete}
	if err := repo.Create(ctx, canonical); err != nil {
		t.Fatal(err)
	}
	d, got, err := repo.Readmit(ctx, old.ID, 1, 1, 0)
	if err != nil || d != AdmissionExisting || got.ID != canonical.ID {
		t.Fatalf("retry conflicted with newer canonical row: %s %+v %v", d, got, err)
	}
}

func TestSameHashClaimsAcrossAccountsAndStaleScheduler(t *testing.T) {
	repo, firstUser := queueTestPostgres(t)
	ctx := context.Background()
	secondUser := firstUser + "-other"
	t.Cleanup(func() { repo.Pool().Exec(ctx, `DELETE FROM jobs WHERE user_id=$1`, secondUser) })
	first := &Job{UserID: firstUser, InfoHash: "cross-account-queue-hash", Source: SourceTorrent, Status: StatusQueued}
	second := &Job{UserID: secondUser, InfoHash: first.InfoHash, Source: SourceTorrent, Status: StatusQueued}
	for _, job := range []*Job{first, second} {
		if err := repo.Create(ctx, job); err != nil {
			t.Fatal(err)
		}
	}
	results := make([]*Job, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, job := range []*Job{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = repo.PromoteQueued(ctx, job.ID, 1, 1_000_000_000_000)
		}()
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil || (results[0] == nil) == (results[1] == nil) {
		t.Fatalf("expected one physical claim: results=%+v errors=%v", results, errs)
	}
	active, waiting := results[0], second
	if active == nil {
		active, waiting = results[1], first
	}
	thirdUser := firstUser + "-third"
	t.Cleanup(func() { repo.Pool().Exec(ctx, `DELETE FROM jobs WHERE user_id=$1`, thirdUser) })
	third := &Job{UserID: thirdUser, InfoHash: first.InfoHash, Source: SourceTorrent}
	if d, err := repo.Admit(ctx, third, 1, 1, 0); err != nil || d != AdmissionQueued {
		t.Fatalf("active hash must queue on new admission: %s %v", d, err)
	}
	if changed, err := repo.SetQueuedPriority(ctx, active.ID, 99); err != nil || changed {
		t.Fatalf("stale scheduler changed promoted job: %v %v", changed, err)
	}
	if changed, err := repo.FailQueued(ctx, active.ID, "stale snapshot"); err != nil || changed {
		t.Fatalf("stale scheduler failed promoted job: %v %v", changed, err)
	}
	if err := repo.Delete(ctx, active.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.PromoteQueued(ctx, waiting.ID, 1, 1_000_000_000_000); err != nil || got == nil {
		t.Fatalf("sibling did not recover after deletion: %+v %v", got, err)
	}
}
