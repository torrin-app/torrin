package jobs

import (
	"context"
	"fmt"
	"testing"
)

func TestIMDBCorrectionFlow(t *testing.T) {
	repo, userID := queueTestPostgres(t)
	ctx := context.Background()
	t.Cleanup(func() {
		repo.Pool().Exec(context.Background(), `DELETE FROM imdb_corrections WHERE user_id=$1`, userID)
	})

	hash := fmt.Sprintf("%040x", 12345)
	job := &Job{UserID: userID, InfoHash: hash, Name: "Wrong Title", Source: SourceTorrent, Status: StatusComplete, IMDBID: "tt0000001"}
	if _, err := repo.CreateOnce(ctx, job); err != nil {
		t.Fatal(err)
	}

	if err := repo.CreateIMDBCorrection(ctx, userID, hash, "tt0388629", "should be one piece"); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateIMDBCorrection(ctx, userID, hash, "tt0388629", ""); err != nil {
		t.Fatal(err)
	}

	pending, err := repo.ListIMDBCorrections(ctx, "pending", 50)
	if err != nil {
		t.Fatal(err)
	}
	var found *IMDBCorrection
	for i := range pending {
		if pending[i].InfoHash == hash {
			found = &pending[i]
		}
	}
	if found == nil {
		t.Fatal("correction not listed")
	}
	if found.JobName != "Wrong Title" || found.CurrentIMDB != "tt0000001" || found.ProposedIMDB != "tt0388629" {
		t.Fatalf("bad correction row: %+v", found)
	}

	if _, _, err := repo.ResolveIMDBCorrection(ctx, found.ID, true); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.IMDBID != "tt0388629" {
		t.Fatalf("imdb not overwritten: %s", got.IMDBID)
	}

	if _, _, err := repo.ResolveIMDBCorrection(ctx, found.ID, true); err == nil {
		t.Fatal("expected error resolving an already-resolved correction")
	}
}
