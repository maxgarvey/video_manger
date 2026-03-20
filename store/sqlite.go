package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteStore implements Store backed by a SQLite database.
type SQLiteStore struct {
	conn *sql.DB
}

// NewSQLite opens (or creates) a SQLite database at path and applies all
// pending migrations from the embedded migrations/ directory.
func NewSQLite(path string) (*SQLiteStore, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// Limit to one open connection to avoid "database is locked" errors
	// under concurrent requests with SQLite's default journal mode.
	conn.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := conn.Exec(pragma); err != nil {
			return nil, err
		}
	}
	if err := runMigrations(conn); err != nil {
		return nil, err
	}
	return &SQLiteStore{conn: conn}, nil
}

// Close releases the underlying database connection.
func (s *SQLiteStore) Close() error {
	return s.conn.Close()
}

// --- Directories ---

func (s *SQLiteStore) GetDirectory(ctx context.Context, id int64) (Directory, error) {
	row := s.conn.QueryRowContext(ctx, `SELECT id, path FROM directories WHERE id = ?`, id)
	var d Directory
	if err := row.Scan(&d.ID, &d.Path); err != nil {
		return Directory{}, err
	}
	return d, nil
}

func (s *SQLiteStore) AddDirectory(ctx context.Context, path string) (Directory, error) {
	var d Directory
	err := s.conn.QueryRowContext(ctx,
		`INSERT INTO directories (path) VALUES (?) RETURNING id, path`, path,
	).Scan(&d.ID, &d.Path)
	return d, err
}

func (s *SQLiteStore) ListDirectories(ctx context.Context) ([]Directory, error) {
	rows, err := s.conn.QueryContext(ctx, `SELECT id, path FROM directories ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var dirs []Directory
	for rows.Next() {
		var d Directory
		if err := rows.Scan(&d.ID, &d.Path); err != nil {
			return nil, err
		}
		dirs = append(dirs, d)
	}
	return dirs, rows.Err()
}

func (s *SQLiteStore) DeleteDirectory(ctx context.Context, id int64) error {
	_, err := s.conn.ExecContext(ctx, `DELETE FROM directories WHERE id = ?`, id)
	return err
}

func (s *SQLiteStore) DeleteDirectoryAndVideos(ctx context.Context, id int64) ([]string, error) {
	// Collect file paths before deletion so the caller can remove them from disk.
	rows, err := s.conn.QueryContext(ctx,
		`SELECT directory_path, filename FROM videos WHERE directory_id = ?`, id)
	if err != nil {
		return nil, err
	}
	var paths []string
	for rows.Next() {
		var dir, file string
		if err := rows.Scan(&dir, &file); err != nil {
			rows.Close()
			return nil, err
		}
		paths = append(paths, filepath.Join(dir, file))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM videos WHERE directory_id = ?`, id); err != nil {
		tx.Rollback() //nolint:errcheck
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM directories WHERE id = ?`, id); err != nil {
		tx.Rollback() //nolint:errcheck
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return paths, nil
}

// --- Videos (raw SQL — directory_id is nullable, so no sqlc JOIN queries) ---

func (s *SQLiteStore) UpsertVideo(ctx context.Context, dirID int64, dirPath string, filename string) (Video, error) {
	row := s.conn.QueryRowContext(ctx, `
		INSERT INTO videos (filename, directory_id, directory_path, original_filename)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (filename, directory_path)
			DO UPDATE SET directory_id = excluded.directory_id
		RETURNING id, filename, directory_id, directory_path, display_name,
		          (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		           FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		           WHERE vt.video_id=id AND t.name LIKE 'show:%' LIMIT 1) AS show_name,
		          rating, original_filename,
		          (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		           FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		           WHERE vt.video_id=id AND t.name LIKE 'genre:%' LIMIT 1) AS genre,
		          season_number,
		          episode_number,
		          episode_title,
		          (SELECT GROUP_CONCAT(SUBSTR(t.name, INSTR(t.name,':')+1), ', ')
		           FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		           WHERE vt.video_id=id AND t.name LIKE 'actor:%') AS actors,
		          (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		           FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		           WHERE vt.video_id=id AND t.name LIKE 'studio:%' LIMIT 1) AS studio,
		          (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		           FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		           WHERE vt.video_id=id AND t.name LIKE 'channel:%' LIMIT 1) AS channel,
		          (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		           FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		           WHERE vt.video_id=id AND t.name LIKE 'type:%' LIMIT 1) AS video_type,
		          (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		           FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		           WHERE vt.video_id=id AND t.name LIKE 'color:%' LIMIT 1) AS color_label,
		          thumbnail_path, duration_s, air_date,
		          NULL AS watched_at,
		          watched
	`, filename, dirID, dirPath, filename)
	return scanVideoRow(row)
}

func (s *SQLiteStore) ListVideos(ctx context.Context) ([]Video, error) {
	rows, err := s.conn.QueryContext(ctx, `
		SELECT v.id, v.filename, v.directory_id, v.directory_path, v.display_name,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'show:%' LIMIT 1) AS show_name,
		       v.rating, v.original_filename,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'genre:%' LIMIT 1) AS genre,
		       v.season_number,
		       v.episode_number,
		       v.episode_title,
		       (SELECT GROUP_CONCAT(SUBSTR(t.name, INSTR(t.name,':')+1), ', ')
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'actor:%') AS actors,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'studio:%' LIMIT 1) AS studio,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'channel:%' LIMIT 1) AS channel,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'type:%' LIMIT 1) AS video_type,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'color:%' LIMIT 1) AS color_label,
		       v.thumbnail_path, v.duration_s, v.air_date,
		       wh.watched_at, v.watched
		FROM videos v
		LEFT JOIN watch_history wh ON v.id = wh.video_id
		ORDER BY v.directory_path ASC, COALESCE(NULLIF(v.display_name, ''), v.filename) ASC
	`)
	if err != nil {
		return nil, err
	}
	return scanVideos(rows)
}

func (s *SQLiteStore) CountVideos(ctx context.Context) (int, error) {
	var n int
	err := s.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM videos`).Scan(&n)
	return n, err
}

func (s *SQLiteStore) ListVideosByTag(ctx context.Context, tagID int64) ([]Video, error) {
	rows, err := s.conn.QueryContext(ctx, `
		SELECT v.id, v.filename, v.directory_id, v.directory_path, v.display_name,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt2 ON t.id=vt2.tag_id
		        WHERE vt2.video_id=v.id AND t.name LIKE 'show:%' LIMIT 1) AS show_name,
		       v.rating, v.original_filename,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt2 ON t.id=vt2.tag_id
		        WHERE vt2.video_id=v.id AND t.name LIKE 'genre:%' LIMIT 1) AS genre,
		       v.season_number,
		       v.episode_number,
		       v.episode_title,
		       (SELECT GROUP_CONCAT(SUBSTR(t.name, INSTR(t.name,':')+1), ', ')
		        FROM tags t JOIN video_tags vt2 ON t.id=vt2.tag_id
		        WHERE vt2.video_id=v.id AND t.name LIKE 'actor:%') AS actors,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt2 ON t.id=vt2.tag_id
		        WHERE vt2.video_id=v.id AND t.name LIKE 'studio:%' LIMIT 1) AS studio,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt2 ON t.id=vt2.tag_id
		        WHERE vt2.video_id=v.id AND t.name LIKE 'channel:%' LIMIT 1) AS channel,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt2 ON t.id=vt2.tag_id
		        WHERE vt2.video_id=v.id AND t.name LIKE 'type:%' LIMIT 1) AS video_type,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt2 ON t.id=vt2.tag_id
		        WHERE vt2.video_id=v.id AND t.name LIKE 'color:%' LIMIT 1) AS color_label,
		       v.thumbnail_path, v.duration_s, v.air_date,
		       wh.watched_at, v.watched
		FROM videos v
		JOIN video_tags vt ON v.id = vt.video_id
		LEFT JOIN watch_history wh ON v.id = wh.video_id
		WHERE vt.tag_id = ?
		ORDER BY v.directory_path ASC, COALESCE(NULLIF(v.display_name, ''), v.filename) ASC
	`, tagID)
	if err != nil {
		return nil, err
	}
	return scanVideos(rows)
}

func (s *SQLiteStore) ListVideosByDirectory(ctx context.Context, dirID int64) ([]Video, error) {
	rows, err := s.conn.QueryContext(ctx, `
		SELECT v.id, v.filename, v.directory_id, v.directory_path, v.display_name,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'show:%' LIMIT 1) AS show_name,
		       v.rating, v.original_filename,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'genre:%' LIMIT 1) AS genre,
		       v.season_number,
		       v.episode_number,
		       v.episode_title,
		       (SELECT GROUP_CONCAT(SUBSTR(t.name, INSTR(t.name,':')+1), ', ')
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'actor:%') AS actors,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'studio:%' LIMIT 1) AS studio,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'channel:%' LIMIT 1) AS channel,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'type:%' LIMIT 1) AS video_type,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'color:%' LIMIT 1) AS color_label,
		       v.thumbnail_path, v.duration_s, v.air_date,
		       wh.watched_at, v.watched
		FROM videos v
		LEFT JOIN watch_history wh ON v.id = wh.video_id
		WHERE v.directory_id = ?
		ORDER BY v.filename ASC
	`, dirID)
	if err != nil {
		return nil, err
	}
	return scanVideos(rows)
}

func (s *SQLiteStore) GetVideo(ctx context.Context, id int64) (Video, error) {
	row := s.conn.QueryRowContext(ctx, `
		SELECT v.id, v.filename, v.directory_id, v.directory_path, v.display_name,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'show:%' LIMIT 1) AS show_name,
		       v.rating, v.original_filename,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'genre:%' LIMIT 1) AS genre,
		       v.season_number,
		       v.episode_number,
		       v.episode_title,
		       (SELECT GROUP_CONCAT(SUBSTR(t.name, INSTR(t.name,':')+1), ', ')
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'actor:%') AS actors,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'studio:%' LIMIT 1) AS studio,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'channel:%' LIMIT 1) AS channel,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'type:%' LIMIT 1) AS video_type,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'color:%' LIMIT 1) AS color_label,
		       v.thumbnail_path, v.duration_s, v.air_date,
		       wh.watched_at, v.watched
		FROM videos v
		LEFT JOIN watch_history wh ON v.id = wh.video_id
		WHERE v.id = ?
	`, id)
	return scanVideoRow(row)
}

func (s *SQLiteStore) SetVideoRating(ctx context.Context, id int64, rating int) error {
	_, err := s.conn.ExecContext(ctx, `UPDATE videos SET rating = ? WHERE id = ?`, rating, id)
	return err
}

func (s *SQLiteStore) ListVideosByRating(ctx context.Context) ([]Video, error) {
	rows, err := s.conn.QueryContext(ctx, `
		SELECT v.id, v.filename, v.directory_id, v.directory_path, v.display_name,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'show:%' LIMIT 1) AS show_name,
		       v.rating, v.original_filename,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'genre:%' LIMIT 1) AS genre,
		       v.season_number,
		       v.episode_number,
		       v.episode_title,
		       (SELECT GROUP_CONCAT(SUBSTR(t.name, INSTR(t.name,':')+1), ', ')
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'actor:%') AS actors,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'studio:%' LIMIT 1) AS studio,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'channel:%' LIMIT 1) AS channel,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'type:%' LIMIT 1) AS video_type,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'color:%' LIMIT 1) AS color_label,
		       v.thumbnail_path, v.duration_s, v.air_date,
		       wh.watched_at, v.watched
		FROM videos v
		LEFT JOIN watch_history wh ON v.id = wh.video_id
		ORDER BY v.rating DESC, COALESCE(NULLIF(v.display_name, ''), v.filename) ASC
	`)
	if err != nil {
		return nil, err
	}
	return scanVideos(rows)
}

func (s *SQLiteStore) ListVideosByShow(ctx context.Context, showName string) ([]Video, error) {
	rows, err := s.conn.QueryContext(ctx, `
		SELECT v.id, v.filename, v.directory_id, v.directory_path, v.display_name,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt2 ON t.id=vt2.tag_id
		        WHERE vt2.video_id=v.id AND t.name LIKE 'show:%' LIMIT 1) AS show_name,
		       v.rating, v.original_filename,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt2 ON t.id=vt2.tag_id
		        WHERE vt2.video_id=v.id AND t.name LIKE 'genre:%' LIMIT 1) AS genre,
		       v.season_number,
		       v.episode_number,
		       v.episode_title,
		       (SELECT GROUP_CONCAT(SUBSTR(t.name, INSTR(t.name,':')+1), ', ')
		        FROM tags t JOIN video_tags vt2 ON t.id=vt2.tag_id
		        WHERE vt2.video_id=v.id AND t.name LIKE 'actor:%') AS actors,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt2 ON t.id=vt2.tag_id
		        WHERE vt2.video_id=v.id AND t.name LIKE 'studio:%' LIMIT 1) AS studio,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt2 ON t.id=vt2.tag_id
		        WHERE vt2.video_id=v.id AND t.name LIKE 'channel:%' LIMIT 1) AS channel,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt2 ON t.id=vt2.tag_id
		        WHERE vt2.video_id=v.id AND t.name LIKE 'type:%' LIMIT 1) AS video_type,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt2 ON t.id=vt2.tag_id
		        WHERE vt2.video_id=v.id AND t.name LIKE 'color:%' LIMIT 1) AS color_label,
		       v.thumbnail_path, v.duration_s, v.air_date,
		       wh.watched_at, v.watched
		FROM videos v
		JOIN video_tags vt ON vt.video_id = v.id
		JOIN tags t ON t.id = vt.tag_id
		LEFT JOIN watch_history wh ON v.id = wh.video_id
		WHERE t.name = 'show:' || ?
		ORDER BY v.season_number ASC, v.episode_number ASC, COALESCE(NULLIF(v.display_name, ''), v.filename) ASC
	`, showName)
	if err != nil {
		return nil, err
	}
	return scanVideos(rows)
}

// GetNextUnwatched returns the first video (by filename) that has no watch_history
// entry. If tagID > 0, only videos with that tag are considered.
func (s *SQLiteStore) GetNextUnwatched(ctx context.Context, tagID int64) (Video, error) {
	var row *sql.Row
	if tagID > 0 {
		row = s.conn.QueryRowContext(ctx, `
			SELECT v.id, v.filename, v.directory_id, v.directory_path, v.display_name,
			       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
			        FROM tags t JOIN video_tags vt2 ON t.id=vt2.tag_id
			        WHERE vt2.video_id=v.id AND t.name LIKE 'show:%' LIMIT 1) AS show_name,
			       v.rating, v.original_filename,
			       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
			        FROM tags t JOIN video_tags vt2 ON t.id=vt2.tag_id
			        WHERE vt2.video_id=v.id AND t.name LIKE 'genre:%' LIMIT 1) AS genre,
			       v.season_number,
			       v.episode_number,
			       v.episode_title,
			       (SELECT GROUP_CONCAT(SUBSTR(t.name, INSTR(t.name,':')+1), ', ')
			        FROM tags t JOIN video_tags vt2 ON t.id=vt2.tag_id
			        WHERE vt2.video_id=v.id AND t.name LIKE 'actor:%') AS actors,
			       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
			        FROM tags t JOIN video_tags vt2 ON t.id=vt2.tag_id
			        WHERE vt2.video_id=v.id AND t.name LIKE 'studio:%' LIMIT 1) AS studio,
			       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
			        FROM tags t JOIN video_tags vt2 ON t.id=vt2.tag_id
			        WHERE vt2.video_id=v.id AND t.name LIKE 'channel:%' LIMIT 1) AS channel,
			       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
			        FROM tags t JOIN video_tags vt2 ON t.id=vt2.tag_id
			        WHERE vt2.video_id=v.id AND t.name LIKE 'type:%' LIMIT 1) AS video_type,
			       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
			        FROM tags t JOIN video_tags vt2 ON t.id=vt2.tag_id
			        WHERE vt2.video_id=v.id AND t.name LIKE 'color:%' LIMIT 1) AS color_label,
			       v.thumbnail_path, v.duration_s, v.air_date,
			       wh.watched_at, v.watched
			FROM videos v
			JOIN video_tags vt ON v.id = vt.video_id
			LEFT JOIN watch_history wh ON v.id = wh.video_id
			WHERE vt.tag_id = ? AND v.watched = 0
			ORDER BY COALESCE(NULLIF(v.display_name,''), v.filename)
			LIMIT 1
		`, tagID)
	} else {
		row = s.conn.QueryRowContext(ctx, `
			SELECT v.id, v.filename, v.directory_id, v.directory_path, v.display_name,
			       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
			        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
			        WHERE vt.video_id=v.id AND t.name LIKE 'show:%' LIMIT 1) AS show_name,
			       v.rating, v.original_filename,
			       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
			        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
			        WHERE vt.video_id=v.id AND t.name LIKE 'genre:%' LIMIT 1) AS genre,
			       v.season_number,
			       v.episode_number,
			       v.episode_title,
			       (SELECT GROUP_CONCAT(SUBSTR(t.name, INSTR(t.name,':')+1), ', ')
			        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
			        WHERE vt.video_id=v.id AND t.name LIKE 'actor:%') AS actors,
			       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
			        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
			        WHERE vt.video_id=v.id AND t.name LIKE 'studio:%' LIMIT 1) AS studio,
			       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
			        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
			        WHERE vt.video_id=v.id AND t.name LIKE 'channel:%' LIMIT 1) AS channel,
			       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
			        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
			        WHERE vt.video_id=v.id AND t.name LIKE 'type:%' LIMIT 1) AS video_type,
			       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
			        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
			        WHERE vt.video_id=v.id AND t.name LIKE 'color:%' LIMIT 1) AS color_label,
			       v.thumbnail_path, v.duration_s, v.air_date,
			       wh.watched_at, v.watched
			FROM videos v
			LEFT JOIN watch_history wh ON v.id = wh.video_id
			WHERE v.watched = 0
			ORDER BY COALESCE(NULLIF(v.display_name,''), v.filename)
			LIMIT 1
		`)
	}
	return scanVideoRow(row)
}

// GetNextUnwatchedFromSearch returns the first unwatched video (alphabetically)
// that also appears in the SearchVideos results for query.  Using SearchVideos
// directly guarantees the candidate set is identical to what the user sees in
// the library panel, avoiding any FTS5 vs LIKE divergence.
func (s *SQLiteStore) GetNextUnwatchedFromSearch(ctx context.Context, query string, tagID int64) (Video, error) {
	if query == "" {
		return s.GetNextUnwatched(ctx, tagID)
	}
	videos, err := s.SearchVideos(ctx, query)
	if err != nil {
		return Video{}, err
	}
	for _, v := range videos {
		if !v.Watched {
			return v, nil
		}
	}
	return Video{}, sql.ErrNoRows
}

func (s *SQLiteStore) GetRandomVideo(ctx context.Context) (Video, error) {
	// Use OFFSET instead of ORDER BY RANDOM() to avoid a full-table sort.
	// MAX(1, …) prevents modulo-by-zero when the table is empty; the query
	// still returns no rows because there are none to offset into.
	row := s.conn.QueryRowContext(ctx, `
		SELECT v.id, v.filename, v.directory_id, v.directory_path, v.display_name,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'show:%' LIMIT 1) AS show_name,
		       v.rating, v.original_filename,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'genre:%' LIMIT 1) AS genre,
		       v.season_number,
		       v.episode_number,
		       v.episode_title,
		       (SELECT GROUP_CONCAT(SUBSTR(t.name, INSTR(t.name,':')+1), ', ')
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'actor:%') AS actors,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'studio:%' LIMIT 1) AS studio,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'channel:%' LIMIT 1) AS channel,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'type:%' LIMIT 1) AS video_type,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'color:%' LIMIT 1) AS color_label,
		       v.thumbnail_path, v.duration_s, v.air_date,
		       wh.watched_at, v.watched
		FROM videos v
		LEFT JOIN watch_history wh ON v.id = wh.video_id
		LIMIT 1 OFFSET ABS(RANDOM()) % MAX(1, (SELECT COUNT(*) FROM videos))
	`)
	return scanVideoRow(row)
}

func (s *SQLiteStore) UpdateVideoName(ctx context.Context, id int64, name string) error {
	_, err := s.conn.ExecContext(ctx, `UPDATE videos SET display_name = ? WHERE id = ?`, name, id)
	return err
}

func (s *SQLiteStore) UpdateVideoShowName(ctx context.Context, id int64, showName string) error {
	return s.SetExclusiveSystemTag(ctx, id, "show", showName)
}

func (s *SQLiteStore) UpdateVideoType(ctx context.Context, id int64, videoType string) error {
	return s.SetExclusiveSystemTag(ctx, id, "type", videoType)
}

func (s *SQLiteStore) ListVideosByType(ctx context.Context, videoType string) ([]Video, error) {
	rows, err := s.conn.QueryContext(ctx, `
		SELECT v.id, v.filename, v.directory_id, v.directory_path, v.display_name,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt2 ON t.id=vt2.tag_id
		        WHERE vt2.video_id=v.id AND t.name LIKE 'show:%' LIMIT 1) AS show_name,
		       v.rating, v.original_filename,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt2 ON t.id=vt2.tag_id
		        WHERE vt2.video_id=v.id AND t.name LIKE 'genre:%' LIMIT 1) AS genre,
		       v.season_number,
		       v.episode_number,
		       v.episode_title,
		       (SELECT GROUP_CONCAT(SUBSTR(t.name, INSTR(t.name,':')+1), ', ')
		        FROM tags t JOIN video_tags vt2 ON t.id=vt2.tag_id
		        WHERE vt2.video_id=v.id AND t.name LIKE 'actor:%') AS actors,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt2 ON t.id=vt2.tag_id
		        WHERE vt2.video_id=v.id AND t.name LIKE 'studio:%' LIMIT 1) AS studio,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt2 ON t.id=vt2.tag_id
		        WHERE vt2.video_id=v.id AND t.name LIKE 'channel:%' LIMIT 1) AS channel,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt2 ON t.id=vt2.tag_id
		        WHERE vt2.video_id=v.id AND t.name LIKE 'type:%' LIMIT 1) AS video_type,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt2 ON t.id=vt2.tag_id
		        WHERE vt2.video_id=v.id AND t.name LIKE 'color:%' LIMIT 1) AS color_label,
		       v.thumbnail_path, v.duration_s, v.air_date,
		       wh.watched_at, v.watched
		FROM videos v
		JOIN video_tags vt ON vt.video_id = v.id
		JOIN tags t ON t.id = vt.tag_id
		LEFT JOIN watch_history wh ON v.id = wh.video_id
		WHERE t.name = 'type:' || ?
		ORDER BY v.directory_path ASC, COALESCE(NULLIF(v.display_name, ''), v.filename) ASC
	`, videoType)
	if err != nil {
		return nil, err
	}
	return scanVideos(rows)
}

func (s *SQLiteStore) UpdateVideoThumbnail(ctx context.Context, videoID int64, thumbnailPath string) error {
	_, err := s.conn.ExecContext(ctx, `UPDATE videos SET thumbnail_path = ? WHERE id = ?`, thumbnailPath, videoID)
	return err
}

func (s *SQLiteStore) UpdateVideoDuration(ctx context.Context, videoID int64, duration float64) error {
	_, err := s.conn.ExecContext(ctx, `UPDATE videos SET duration_s = ? WHERE id = ?`, duration, videoID)
	return err
}

func (s *SQLiteStore) DeleteVideo(ctx context.Context, id int64) error {
	_, err := s.conn.ExecContext(ctx, "DELETE FROM videos WHERE id = ?", id)
	return err
}

func (s *SQLiteStore) UpdateVideoPath(ctx context.Context, id, dirID int64, dirPath, filename string) error {
	_, err := s.conn.ExecContext(ctx,
		`UPDATE videos SET directory_id=?, directory_path=?, filename=? WHERE id=?`,
		dirID, dirPath, filename, id)
	return err
}

func (s *SQLiteStore) UpdateVideoFields(ctx context.Context, id int64, f VideoFields) error {
	// Structured numeric fields stay as column updates.
	if _, err := s.conn.ExecContext(ctx,
		`UPDATE videos SET season_number=?, episode_number=?, episode_title=?, air_date=? WHERE id=?`,
		f.SeasonNumber, f.EpisodeNumber, f.EpisodeTitle, f.AirDate, id); err != nil {
		return err
	}
	// season: tag mirrors the season_number column for sidebar filtering.
	seasonVal := ""
	if f.SeasonNumber > 0 {
		seasonVal = strconv.Itoa(f.SeasonNumber)
	}
	if err := s.SetExclusiveSystemTag(ctx, id, "season", seasonVal); err != nil {
		return err
	}
	if err := s.SetExclusiveSystemTag(ctx, id, "genre", f.Genre); err != nil {
		return err
	}
	if err := s.SetExclusiveSystemTag(ctx, id, "studio", f.Studio); err != nil {
		return err
	}
	if err := s.SetExclusiveSystemTag(ctx, id, "channel", f.Channel); err != nil {
		return err
	}
	// Actors: split on comma, one tag per actor.
	actors := splitActors(f.Actors)
	return s.SetMultiSystemTag(ctx, id, "actor", actors)
}

func splitActors(actors string) []string {
	var result []string
	for a := range strings.SplitSeq(actors, ",") {
		if s := strings.TrimSpace(a); s != "" {
			result = append(result, s)
		}
	}
	return result
}

func (s *SQLiteStore) ListVideosByMinRating(ctx context.Context, minRating int) ([]Video, error) {
	rows, err := s.conn.QueryContext(ctx, `
		SELECT v.id, v.filename, v.directory_id, v.directory_path, v.display_name,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'show:%' LIMIT 1) AS show_name,
		       v.rating, v.original_filename,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'genre:%' LIMIT 1) AS genre,
		       v.season_number,
		       v.episode_number,
		       v.episode_title,
		       (SELECT GROUP_CONCAT(SUBSTR(t.name, INSTR(t.name,':')+1), ', ')
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'actor:%') AS actors,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'studio:%' LIMIT 1) AS studio,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'channel:%' LIMIT 1) AS channel,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'type:%' LIMIT 1) AS video_type,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'color:%' LIMIT 1) AS color_label,
		       v.thumbnail_path, v.duration_s, v.air_date,
		       wh.watched_at, v.watched
		FROM videos v
		LEFT JOIN watch_history wh ON v.id = wh.video_id
		WHERE v.rating >= ?
		ORDER BY v.rating DESC, COALESCE(NULLIF(v.display_name, ''), v.filename) ASC
	`, minRating)
	if err != nil {
		return nil, err
	}
	return scanVideos(rows)
}

func (s *SQLiteStore) SearchVideos(ctx context.Context, query string) ([]Video, error) {
	// The FTS5 trigram tokenizer requires ≥ 3 characters to form any trigrams.
	// For short queries fall back to LIKE, which is fast enough at that scale.
	if len([]rune(query)) >= 3 {
		// Wrap in FTS5 phrase quotes so spaces and punctuation are treated
		// literally (equivalent to LIKE '%query%' with the trigram tokenizer).
		ftsQuery := `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
		escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
		rows, err := s.conn.QueryContext(ctx, `
			SELECT v.id, v.filename, v.directory_id, v.directory_path, v.display_name,
			       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
			        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
			        WHERE vt.video_id=v.id AND t.name LIKE 'show:%' LIMIT 1) AS show_name,
			       v.rating, v.original_filename,
			       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
			        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
			        WHERE vt.video_id=v.id AND t.name LIKE 'genre:%' LIMIT 1) AS genre,
			       v.season_number,
			       v.episode_number,
			       v.episode_title,
			       (SELECT GROUP_CONCAT(SUBSTR(t.name, INSTR(t.name,':')+1), ', ')
			        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
			        WHERE vt.video_id=v.id AND t.name LIKE 'actor:%') AS actors,
			       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
			        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
			        WHERE vt.video_id=v.id AND t.name LIKE 'studio:%' LIMIT 1) AS studio,
			       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
			        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
			        WHERE vt.video_id=v.id AND t.name LIKE 'channel:%' LIMIT 1) AS channel,
			       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
			        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
			        WHERE vt.video_id=v.id AND t.name LIKE 'type:%' LIMIT 1) AS video_type,
			       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
			        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
			        WHERE vt.video_id=v.id AND t.name LIKE 'color:%' LIMIT 1) AS color_label,
			       v.thumbnail_path, v.duration_s, v.air_date,
			       wh.watched_at, v.watched
			FROM videos v
			JOIN videos_fts ON videos_fts.rowid = v.id
			LEFT JOIN watch_history wh ON v.id = wh.video_id
			WHERE videos_fts MATCH ?
			   OR EXISTS (
			      SELECT 1 FROM video_tags vt2
			      JOIN tags t2 ON t2.id = vt2.tag_id
			      WHERE vt2.video_id = v.id AND LOWER(t2.name) LIKE LOWER(?) ESCAPE '\'
			   )
			ORDER BY COALESCE(NULLIF(v.display_name, ''), v.filename) ASC
		`, ftsQuery, "%"+escaped+"%")
		if err == nil {
			return scanVideos(rows)
		}
		// FTS table unavailable — fall through to LIKE.
	}
	// LIKE fallback: escape special chars so they are treated literally.
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
	rows, err := s.conn.QueryContext(ctx, `
		SELECT v.id, v.filename, v.directory_id, v.directory_path, v.display_name,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'show:%' LIMIT 1) AS show_name,
		       v.rating, v.original_filename,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'genre:%' LIMIT 1) AS genre,
		       v.season_number,
		       v.episode_number,
		       v.episode_title,
		       (SELECT GROUP_CONCAT(SUBSTR(t.name, INSTR(t.name,':')+1), ', ')
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'actor:%') AS actors,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'studio:%' LIMIT 1) AS studio,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'channel:%' LIMIT 1) AS channel,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'type:%' LIMIT 1) AS video_type,
		       (SELECT SUBSTR(t.name, INSTR(t.name,':')+1)
		        FROM tags t JOIN video_tags vt ON t.id=vt.tag_id
		        WHERE vt.video_id=v.id AND t.name LIKE 'color:%' LIMIT 1) AS color_label,
		       v.thumbnail_path, v.duration_s, v.air_date,
		       wh.watched_at, v.watched
		FROM videos v
		LEFT JOIN watch_history wh ON v.id = wh.video_id
		WHERE LOWER(COALESCE(NULLIF(v.display_name, ''), v.filename)) LIKE LOWER(?) ESCAPE '\'
		   OR EXISTS (
		      SELECT 1 FROM video_tags vt2
		      JOIN tags t2 ON t2.id = vt2.tag_id
		      WHERE vt2.video_id = v.id AND LOWER(t2.name) LIKE LOWER(?) ESCAPE '\'
		   )
		ORDER BY COALESCE(NULLIF(v.display_name, ''), v.filename) ASC
	`, "%"+escaped+"%", "%"+escaped+"%")
	if err != nil {
		return nil, err
	}
	return scanVideos(rows)
}

// --- Tags ---

func (s *SQLiteStore) UpsertTag(ctx context.Context, name string) (Tag, error) {
	var t Tag
	err := s.conn.QueryRowContext(ctx,
		`INSERT INTO tags (name) VALUES (?) ON CONFLICT (name) DO UPDATE SET name = excluded.name RETURNING id, name`,
		name,
	).Scan(&t.ID, &t.Name)
	return t, err
}

func (s *SQLiteStore) ListTags(ctx context.Context) ([]Tag, error) {
	rows, err := s.conn.QueryContext(ctx, `SELECT id, name FROM tags ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

func (s *SQLiteStore) TagVideo(ctx context.Context, videoID, tagID int64) error {
	_, err := s.conn.ExecContext(ctx, `INSERT OR IGNORE INTO video_tags (video_id, tag_id) VALUES (?, ?)`, videoID, tagID)
	return err
}

func (s *SQLiteStore) UntagVideo(ctx context.Context, videoID, tagID int64) error {
	_, err := s.conn.ExecContext(ctx, `DELETE FROM video_tags WHERE video_id = ? AND tag_id = ?`, videoID, tagID)
	return err
}

func (s *SQLiteStore) PruneOrphanTags(ctx context.Context) error {
	_, err := s.conn.ExecContext(ctx,
		`DELETE FROM tags WHERE id NOT IN (SELECT DISTINCT tag_id FROM video_tags)`)
	return err
}

func (s *SQLiteStore) ListTagsByVideo(ctx context.Context, videoID int64) ([]Tag, error) {
	rows, err := s.conn.QueryContext(ctx, `
		SELECT t.id, t.name FROM tags t
		JOIN video_tags vt ON t.id = vt.tag_id
		WHERE vt.video_id = ?
		ORDER BY t.name
	`, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// SetExclusiveSystemTag removes all tags with prefix "namespace:" from the video
// and upserts "namespace:value". Empty value just removes existing tags.
func (s *SQLiteStore) SetExclusiveSystemTag(ctx context.Context, videoID int64, namespace, value string) error {
	// Remove existing tags for this namespace from this video.
	if _, err := s.conn.ExecContext(ctx, `
		DELETE FROM video_tags WHERE video_id = ? AND tag_id IN (
			SELECT id FROM tags WHERE name LIKE ?
		)`, videoID, namespace+":%"); err != nil {
		return err
	}
	if value == "" {
		return nil
	}
	name := namespace + ":" + value
	if _, err := s.conn.ExecContext(ctx,
		`INSERT OR IGNORE INTO tags (name) VALUES (?)`, name); err != nil {
		return err
	}
	_, err := s.conn.ExecContext(ctx,
		`INSERT OR IGNORE INTO video_tags (video_id, tag_id)
		 SELECT ?, id FROM tags WHERE name = ?`, videoID, name)
	return err
}

// SetMultiSystemTag removes all tags with prefix "namespace:" then adds one tag
// per value in values (empty strings skipped).
func (s *SQLiteStore) SetMultiSystemTag(ctx context.Context, videoID int64, namespace string, values []string) error {
	// Remove all existing tags for this namespace from this video.
	if _, err := s.conn.ExecContext(ctx, `
		DELETE FROM video_tags WHERE video_id = ? AND tag_id IN (
			SELECT id FROM tags WHERE name LIKE ?
		)`, videoID, namespace+":%"); err != nil {
		return err
	}
	for _, value := range values {
		if value == "" {
			continue
		}
		name := namespace + ":" + value
		if _, err := s.conn.ExecContext(ctx,
			`INSERT OR IGNORE INTO tags (name) VALUES (?)`, name); err != nil {
			return err
		}
		if _, err := s.conn.ExecContext(ctx,
			`INSERT OR IGNORE INTO video_tags (video_id, tag_id)
			 SELECT ?, id FROM tags WHERE name = ?`, videoID, name); err != nil {
			return err
		}
	}
	return nil
}

// --- scan helpers ---

func scanVideoRow(row *sql.Row) (Video, error) {
	var v Video
	var dirID sql.NullInt64
	var showName, genre, actors, studio, channel, videoType, colorLabel, thumbnailPath, watchedAt, airDate sql.NullString
	var watched int
	if err := row.Scan(
		&v.ID, &v.Filename, &dirID, &v.DirectoryPath, &v.DisplayName, &showName, &v.Rating, &v.OriginalFilename,
		&genre, &v.SeasonNumber, &v.EpisodeNumber, &v.EpisodeTitle, &actors, &studio, &channel, &videoType,
		&colorLabel,
		&thumbnailPath, &v.DurationS, &airDate,
		&watchedAt, &watched,
	); err != nil {
		return Video{}, err
	}
	if dirID.Valid {
		v.DirectoryID = dirID.Int64
	}
	v.ShowName = showName.String
	v.Genre = genre.String
	v.Actors = actors.String
	v.Studio = studio.String
	v.Channel = channel.String
	v.VideoType = videoType.String
	v.ColorLabel = colorLabel.String
	v.ThumbnailPath = thumbnailPath.String
	v.AirDate = airDate.String
	if watchedAt.Valid {
		v.WatchedAt = watchedAt.String
	}
	v.Watched = watched != 0
	return v, nil
}

func scanVideos(rows *sql.Rows) ([]Video, error) {
	defer rows.Close()
	var videos []Video
	for rows.Next() {
		var v Video
		var dirID sql.NullInt64
		var showName, genre, actors, studio, channel, videoType, colorLabel, thumbnailPath, watchedAt, airDate sql.NullString
		var watched int
		if err := rows.Scan(
			&v.ID, &v.Filename, &dirID, &v.DirectoryPath, &v.DisplayName, &showName, &v.Rating, &v.OriginalFilename,
			&genre, &v.SeasonNumber, &v.EpisodeNumber, &v.EpisodeTitle, &actors, &studio, &channel, &videoType,
			&colorLabel,
			&thumbnailPath, &v.DurationS, &airDate,
			&watchedAt, &watched,
		); err != nil {
			return nil, err
		}
		if dirID.Valid {
			v.DirectoryID = dirID.Int64
		}
		v.ShowName = showName.String
		v.Genre = genre.String
		v.Actors = actors.String
		v.Studio = studio.String
		v.Channel = channel.String
		v.VideoType = videoType.String
		v.ColorLabel = colorLabel.String
		v.ThumbnailPath = thumbnailPath.String
		v.AirDate = airDate.String
		if watchedAt.Valid {
			v.WatchedAt = watchedAt.String
		}
		v.Watched = watched != 0
		videos = append(videos, v)
	}
	return videos, rows.Err()
}

// --- Settings ---

func (s *SQLiteStore) GetSetting(ctx context.Context, key string) (string, error) {
	var value string
	err := s.conn.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (s *SQLiteStore) SaveSettings(ctx context.Context, pairs map[string]string) error {
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for k, v := range pairs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO settings (key, value) VALUES (?, ?)
			 ON CONFLICT (key) DO UPDATE SET value = excluded.value`,
			k, v); err != nil {
			tx.Rollback() //nolint:errcheck
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) ListSettingsWithPrefix(ctx context.Context, prefix string) (map[string]string, error) {
	rows, err := s.conn.QueryContext(ctx, `SELECT key, value FROM settings WHERE key LIKE ?`, prefix+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		result[k] = v
	}
	return result, rows.Err()
}

// --- Sessions ---

func (s *SQLiteStore) SaveSession(ctx context.Context, token string, expiry time.Time) error {
	_, err := s.conn.ExecContext(ctx,
		`INSERT INTO sessions (token, expires_at) VALUES (?, ?)
		 ON CONFLICT (token) DO UPDATE SET expires_at = excluded.expires_at`,
		token, expiry.Unix())
	return err
}

func (s *SQLiteStore) DeleteSession(ctx context.Context, token string) error {
	_, err := s.conn.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, token)
	return err
}

func (s *SQLiteStore) LoadSessions(ctx context.Context) (map[string]time.Time, error) {
	rows, err := s.conn.QueryContext(ctx,
		`SELECT token, expires_at FROM sessions WHERE expires_at > ?`, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]time.Time)
	for rows.Next() {
		var token string
		var ts int64
		if err := rows.Scan(&token, &ts); err != nil {
			return nil, err
		}
		m[token] = time.Unix(ts, 0)
	}
	return m, rows.Err()
}

func (s *SQLiteStore) PruneExpiredSessions(ctx context.Context) error {
	_, err := s.conn.ExecContext(ctx,
		`DELETE FROM sessions WHERE expires_at <= ?`, time.Now().Unix())
	return err
}

// --- Watch history ---

func (s *SQLiteStore) RecordWatch(ctx context.Context, videoID int64, position float64) error {
	// Upsert position/timestamp in watch_history.
	_, err := s.conn.ExecContext(ctx, `
		INSERT INTO watch_history (video_id, position, watched_at)
		VALUES (?, ?, datetime('now'))
		ON CONFLICT (video_id) DO UPDATE SET
			position   = excluded.position,
			watched_at = excluded.watched_at
	`, videoID, position)
	if err != nil {
		return err
	}
	// Only flip watched 0→1 (avoids duplicate watch_events on repeated progress saves).
	res, err := s.conn.ExecContext(ctx,
		`UPDATE videos SET watched = 1 WHERE id = ? AND watched = 0`, videoID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		_, err = s.conn.ExecContext(ctx,
			`INSERT INTO watch_events (video_id) VALUES (?)`, videoID)
	}
	return err
}

func (s *SQLiteStore) ClearWatch(ctx context.Context, videoID int64) error {
	if _, err := s.conn.ExecContext(ctx,
		`UPDATE videos SET watched = 0 WHERE id = ?`, videoID); err != nil {
		return err
	}
	_, err := s.conn.ExecContext(ctx,
		`DELETE FROM watch_history WHERE video_id = ?`, videoID)
	return err
}

func (s *SQLiteStore) GetWatch(ctx context.Context, videoID int64) (WatchRecord, error) {
	row := s.conn.QueryRowContext(ctx, `
		SELECT video_id, position, watched_at
		FROM watch_history WHERE video_id = ?
	`, videoID)
	var w WatchRecord
	if err := row.Scan(&w.VideoID, &w.Position, &w.WatchedAt); err != nil {
		return WatchRecord{}, err
	}
	return w, nil
}

func (s *SQLiteStore) ListWatchHistory(ctx context.Context) (map[int64]WatchRecord, error) {
	rows, err := s.conn.QueryContext(ctx, `SELECT video_id, position, watched_at FROM watch_history`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[int64]WatchRecord)
	for rows.Next() {
		var w WatchRecord
		if err := rows.Scan(&w.VideoID, &w.Position, &w.WatchedAt); err != nil {
			return nil, err
		}
		m[w.VideoID] = w
	}
	return m, rows.Err()
}
