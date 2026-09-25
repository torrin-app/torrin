package jobs

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type IMDBCorrection struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	InfoHash     string    `json:"info_hash"`
	ProposedIMDB string    `json:"proposed_imdb"`
	Note         string    `json:"note"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	JobName      string    `json:"job_name"`
	CurrentIMDB  string    `json:"current_imdb"`
}

func (p *Postgres) CreateIMDBCorrection(ctx context.Context, userID, infoHash, proposedIMDB, note string) error {
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx, `INSERT INTO imdb_corrections (id, user_id, info_hash, proposed_imdb, note)
		VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, id.String(), userID, infoHash, proposedIMDB, note)
	return err
}

func (p *Postgres) ListIMDBCorrections(ctx context.Context, status string, limit int) ([]IMDBCorrection, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := p.pool.Query(ctx, `SELECT c.id, c.user_id, c.info_hash, c.proposed_imdb, c.note, c.status, c.created_at,
		COALESCE(max(j.name), ''), COALESCE(max(j.imdb_id), '')
		FROM imdb_corrections c LEFT JOIN jobs j ON j.info_hash = c.info_hash
		WHERE c.status = $1
		GROUP BY c.id, c.user_id, c.info_hash, c.proposed_imdb, c.note, c.status, c.created_at
		ORDER BY c.created_at ASC LIMIT $2`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IMDBCorrection{}
	for rows.Next() {
		var c IMDBCorrection
		if err := rows.Scan(&c.ID, &c.UserID, &c.InfoHash, &c.ProposedIMDB, &c.Note, &c.Status, &c.CreatedAt, &c.JobName, &c.CurrentIMDB); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (p *Postgres) ResolveIMDBCorrection(ctx context.Context, id string, approve bool) (string, string, error) {
	status := "rejected"
	if approve {
		status = "approved"
	}
	var infoHash, proposed string
	if err := p.pool.QueryRow(ctx, `UPDATE imdb_corrections SET status=$2, resolved_at=now()
		WHERE id=$1 AND status='pending' RETURNING info_hash, proposed_imdb`, id, status).Scan(&infoHash, &proposed); err != nil {
		return "", "", err
	}
	if approve {
		if err := p.OverwriteIMDB(ctx, infoHash, proposed); err != nil {
			return "", "", err
		}
	}
	return infoHash, proposed, nil
}

func (p *Postgres) OverwriteIMDB(ctx context.Context, infoHash, imdbID string) error {
	if infoHash == "" || imdbID == "" {
		return nil
	}
	_, err := p.pool.Exec(ctx, `UPDATE jobs SET imdb_id=$2 WHERE info_hash=$1`, infoHash, imdbID)
	return err
}
