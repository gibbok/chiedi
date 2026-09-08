package store

import (
	"context"
	"database/sql"
	"path/filepath"
)

// CountsWithFailedPaths reads counts and source locations from one snapshot.
// Paths include every stored failed document, ordered by root and relative path.
func (s *Store) CountsWithFailedPaths(ctx context.Context) (Counts, []string, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Counts{}, nil, err
	}
	defer tx.Rollback()
	return countsWithFailedPaths(ctx, tx)
}

func countsWithFailedPaths(ctx context.Context, reader sqlReader) (Counts, []string, error) {
	counts, err := countsWith(ctx, reader)
	if err != nil {
		return Counts{}, nil, err
	}
	rows, err := reader.QueryContext(ctx, `SELECT r.path,d.relative_path
		FROM documents d JOIN roots r ON r.id=d.root_id
		WHERE d.status='failed' ORDER BY r.path,d.relative_path`)
	if err != nil {
		return Counts{}, nil, err
	}
	defer rows.Close()
	paths := make([]string, 0, counts.Failed)
	for rows.Next() {
		var root, relative string
		if err := rows.Scan(&root, &relative); err != nil {
			return Counts{}, nil, err
		}
		paths = append(paths, filepath.Join(root, filepath.FromSlash(relative)))
	}
	if err := rows.Err(); err != nil {
		return Counts{}, nil, err
	}
	return counts, paths, nil
}
