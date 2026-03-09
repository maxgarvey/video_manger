package store

import (
	"context"
	"database/sql"
	"strings"
)

// migrate013Actors splits the comma-separated actors column into individual
// actor: system tags before the SQL migration drops the column.
func migrate013Actors(ctx context.Context, conn *sql.DB) error {
	rows, err := conn.QueryContext(ctx, `SELECT id, actors FROM videos WHERE actors != ''`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type row struct {
		id     int64
		actors string
	}
	var data []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.actors); err != nil {
			return err
		}
		data = append(data, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range data {
		for _, a := range strings.Split(r.actors, ",") {
			name := "actor:" + strings.TrimSpace(a)
			if name == "actor:" {
				continue
			}
			if _, err := conn.ExecContext(ctx,
				`INSERT OR IGNORE INTO tags (name) VALUES (?)`, name); err != nil {
				return err
			}
			if _, err := conn.ExecContext(ctx,
				`INSERT OR IGNORE INTO video_tags (video_id, tag_id)
                 SELECT ?, id FROM tags WHERE name = ?`, r.id, name); err != nil {
				return err
			}
		}
	}
	return nil
}
