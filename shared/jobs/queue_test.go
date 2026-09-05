package jobs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func queueTestPostgres(t *testing.T) (*Postgres, string) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	repo, err := NewPostgres(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	userID := "queue-test-" + uuid.NewString()
	t.Cleanup(func() {
		repo.Pool().Exec(context.Background(), `DELETE FROM jobs WHERE user_id=$1`, userID)
		repo.Close()
	})
	return repo, userID
}

func TestAdmitConcurrentHonorsSlotsAndQueueCap(t *testing.T) {
	repo, userID := queueTestPostgres(t)
	ctx := context.Background()
	const submissions, maxConcurrent, maxQueued = 20, 3, 5

	var wg sync.WaitGroup
	var mu sync.Mutex
	admitted, queued, full := 0, 0, 0
	for i := 0; i < submissions; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			job := &Job{UserID: userID, InfoHash: fmt.Sprintf("%040x", i+1), Source: SourceTorrent}
			disposition, err := repo.Admit(ctx, job, maxConcurrent, maxQueued, 1_000_000_000_000)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil && disposition == AdmissionAdmitted:
				admitted++
			case err == nil && disposition == AdmissionQueued:
				queued++
			case errors.Is(err, ErrQueueFull):
				full++
			default:
				t.Errorf("submission %d: disposition=%q err=%v", i, disposition, err)
			}
		}(i)
	}
	wg.Wait()

	if admitted != maxConcurrent || queued != maxQueued || full != submissions-maxConcurrent-maxQueued {
		t.Fatalf("admitted=%d queued=%d full=%d", admitted, queued, full)
	}
	if active, _ := repo.DownloadingCount(ctx, userID); active != maxConcurrent {
		t.Fatalf("active=%d, want %d", active, maxConcurrent)
	}
	if waiting, _ := repo.QueuedCount(ctx, userID); waiting != maxQueued {
		t.Fatalf("queued=%d, want %d", waiting, maxQueued)
	}
}

func TestPromoteQueuedFIFOAndSeedIsolation(t *testing.T) {
	repo, userID := queueTestPostgres(t)
	ctx := context.Background()

	seed := &Job{UserID: userID, InfoHash: fmt.Sprintf("%040x", 100), Source: SourceTorrent, Status: StatusDownloading, Seed: true}
	if err := repo.Create(ctx, seed); err != nil {
		t.Fatal(err)
	}
	first := &Job{UserID: userID, InfoHash: fmt.Sprintf("%040x", 101), Source: SourceTorrent}
	second := &Job{UserID: userID, InfoHash: fmt.Sprintf("%040x", 102), Source: SourceTorrent}
	third := &Job{UserID: userID, InfoHash: fmt.Sprintf("%040x", 103), Source: SourceTorrent}
	if d, err := repo.Admit(ctx, first, 1, 10, 1_000_000_000_000); err != nil || d != AdmissionAdmitted {
		t.Fatalf("first disposition=%q err=%v", d, err)
	}
	if d, err := repo.Admit(ctx, second, 1, 10, 1_000_000_000_000); err != nil || d != AdmissionQueued {
		t.Fatalf("second disposition=%q err=%v", d, err)
	}
	if d, err := repo.Admit(ctx, third, 1, 10, 1_000_000_000_000); err != nil || d != AdmissionQueued {
		t.Fatalf("third disposition=%q err=%v", d, err)
	}
	if active, _ := repo.DownloadingCount(ctx, userID); active != 1 {
		t.Fatalf("seed leaked into active slots: %d", active)
	}

	first.Status = StatusComplete
	if err := repo.Update(ctx, first); err != nil {
		t.Fatal(err)
	}
	if promoted, err := repo.PromoteQueued(ctx, third.ID, 1, 1_000_000_000_000); err != nil || promoted != nil {
		t.Fatalf("third bypassed FIFO: promoted=%v err=%v", promoted, err)
	}
	promoted, err := repo.PromoteQueued(ctx, second.ID, 1, 1_000_000_000_000)
	if err != nil || promoted == nil || promoted.ID != second.ID {
		t.Fatalf("second not promoted: promoted=%v err=%v", promoted, err)
	}
}
