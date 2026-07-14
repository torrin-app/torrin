package auth

import (
	"context"
	"time"
)

func (s *Store) BanUser(ctx context.Context, userID, reason string) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET banned=TRUE, ban_reason=$1, updated_at=$2 WHERE id=$3`,
		reason, time.Now(), userID)
	return err
}

func (s *Store) UnbanUser(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET banned=FALSE, ban_reason='', updated_at=$1 WHERE id=$2`,
		time.Now(), userID)
	return err
}

func (s *Store) GetBlocklist(ctx context.Context) (hard, soft []string, err error) {
	rows, err := s.pool.Query(ctx, `SELECT term, tier FROM blocklist_terms`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var term, tier string
		if rows.Scan(&term, &tier) != nil {
			continue
		}
		if tier == "soft" {
			soft = append(soft, term)
		} else {
			hard = append(hard, term)
		}
	}
	return hard, soft, rows.Err()
}

func (s *Store) AddBlocklistTerm(ctx context.Context, term, tier string) error {
	if tier != "soft" {
		tier = "hard"
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO blocklist_terms (term, tier) VALUES ($1,$2) ON CONFLICT (term) DO UPDATE SET tier=$2`,
		term, tier)
	return err
}

func (s *Store) RemoveBlocklistTerm(ctx context.Context, term string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM blocklist_terms WHERE term=$1`, term)
	return err
}
