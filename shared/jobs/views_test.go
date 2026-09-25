package jobs

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRecordViewRefreshesOnRepeatPlay(t *testing.T) {
	repo, userID := queueTestPostgres(t)
	ctx := context.Background()
	hash := strings.Repeat("a", 40)
	t.Cleanup(func() {
		repo.Pool().Exec(context.Background(), `DELETE FROM views WHERE info_hash=$1`, hash)
	})

	job := &Job{UserID: userID, InfoHash: hash, Name: "Ep", Source: SourceTorrent, Status: StatusComplete}
	if _, err := repo.CreateOnce(ctx, job); err != nil {
		t.Fatal(err)
	}

	read := func() (int, time.Time) {
		var ac int
		var la time.Time
		repo.Pool().QueryRow(ctx, `SELECT access_count, last_accessed_at FROM jobs WHERE id=$1`, job.ID).Scan(&ac, &la)
		return ac, la
	}

	if _, err := repo.RecordView(ctx, hash, "stream"); err != nil {
		t.Fatal(err)
	}
	ac1, la1 := read()
	if ac1 != 1 || la1.IsZero() {
		t.Fatalf("first view: access_count=%d last_accessed=%v (want 1, set)", ac1, la1)
	}

	time.Sleep(15 * time.Millisecond)
	if _, err := repo.RecordView(ctx, hash, "stream"); err != nil {
		t.Fatal(err)
	}
	ac2, la2 := read()
	if ac2 != 1 {
		t.Fatalf("repeat play must not bump access_count: got %d, want 1", ac2)
	}
	if !la2.After(la1) {
		t.Fatalf("repeat play must refresh last_accessed_at: %v not after %v", la2, la1)
	}
}
