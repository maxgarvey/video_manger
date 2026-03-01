package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/maxgarvey/video_manger/db"
	_ "modernc.org/sqlite"
)

// SQLiteStore implements Store backed by a SQLite database.
type SQLiteStore struct {
	q    *db.Queries
	conn *sql.DB
}

// NewSQLite opens (or creates) a SQLite database at path and applies the schema.
func NewSQLite(path string) (*SQLiteStore, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if err := applySchema(conn); err != nil {
		return nil, err
	}
	return &SQLiteStore{q: db.New(conn), conn: conn}, nil
}

func applySchema(conn *sql.DB) error {
	// Create all non-video tables (idempotent).
	if _, err := conn.Exec(`
		CREATE TABLE IF NOT EXISTS directories (
			id   INTEGER PRIMARY KEY AUTOINCREMENT,
			path TEXT    NOT NULL UNIQUE
		);
		CREATE TABLE IF NOT EXISTS tags (
			id   INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT    NOT NULL UNIQUE
		);
		CREATE TABLE IF NOT EXISTS video_tags (
			video_id INTEGER NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
			tag_id   INTEGER NOT NULL REFERENCES tags(id)   ON DELETE CASCADE,
			PRIMARY KEY(video_id, tag_id)
		);
		PRAGMA foreign_keys = ON;
	`); err != nil {
		return err
	}
	return migrateVideos(conn)
}

// migrateVideos ensures the videos table exists with the current schema:
//   - directory_id nullable (ON DELETE SET NULL so videos survive directory removal)
//   - directory_path stored directly (accessible even when directory_id is NULL)
//   - UNIQUE(filename, directory_path) instead of (filename, directory_id)
func migrateVideos(conn *sql.DB) error {
	// Check whether the table exists at all.
	var exists int
	if err := conn.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='videos'`,
	).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		_, err := conn.Exec(`
			CREATE TABLE videos (
				id             INTEGER PRIMARY KEY AUTOINCREMENT,
				filename       TEXT    NOT NULL,
				directory_id   INTEGER REFERENCES directories(id) ON DELETE SET NULL,
				directory_path TEXT    NOT NULL DEFAULT '',
				display_name   TEXT    NOT NULL DEFAULT '',
				UNIQUE(filename, directory_path)
			)`)
		return err
	}

	// Table exists — check whether it already has the directory_path column.
	var hasPath int
	if err := conn.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('videos') WHERE name='directory_path'`,
	).Scan(&hasPath); err != nil {
		return err
	}
	if hasPath > 0 {
		return nil // Already up to date.
	}

	// Recreate the table with the new schema, carrying over all existing data.
	// PRAGMA foreign_keys must be OFF while we drop and rename tables.
	steps := []string{
		`PRAGMA foreign_keys = OFF`,
		`CREATE TABLE videos_new (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			filename       TEXT    NOT NULL,
			directory_id   INTEGER REFERENCES directories(id) ON DELETE SET NULL,
			directory_path TEXT    NOT NULL DEFAULT '',
			display_name   TEXT    NOT NULL DEFAULT '',
			UNIQUE(filename, directory_path)
		)`,
		// Copy rows; populate directory_path from the directories join.
		`INSERT INTO videos_new (id, filename, directory_id, directory_path, display_name)
			SELECT v.id, v.filename, v.directory_id,
			       COALESCE(d.path, ''), v.display_name
			FROM videos v
			LEFT JOIN directories d ON d.id = v.directory_id`,
		`DROP TABLE videos`,
		`ALTER TABLE videos_new RENAME TO videos`,
		`PRAGMA foreign_keys = ON`,
	}
	for _, s := range steps {
		if _, err := conn.Exec(s); err != nil {
			return fmt.Errorf("migrate videos: %w (step: %.60s)", err, s)
		}
	}
	return nil
}

// --- Directories ---

func (s *SQLiteStore) AddDirectory(ctx context.Context, path string) (Directory, error) {
	row, err := s.q.AddDirectory(ctx, path)
	if err != nil {
		return Directory{}, err
	}
	return Directory{ID: row.ID, Path: row.Path}, nil
}

func (s *SQLiteStore) ListDirectories(ctx context.Context) ([]Directory, error) {
	rows, err := s.q.ListDirectories(ctx)
	if err != nil {
		return nil, err
	}
	dirs := make([]Directory, len(rows))
	for i, r := range rows {
		dirs[i] = Directory{ID: r.ID, Path: r.Path}
	}
	return dirs, nil
}

func (s *SQLiteStore) DeleteDirectory(ctx context.Context, id int64) error {
	return s.q.DeleteDirectory(ctx, id)
}

// --- Videos (raw SQL — directory_id is nullable, so no sqlc JOIN queries) ---

func (s *SQLiteStore) UpsertVideo(ctx context.Context, dirID int64, dirPath string, filename string) (Video, error) {
	row := s.conn.QueryRowContext(ctx, `
		INSERT INTO videos (filename, directory_id, directory_path)
		VALUES (?, ?, ?)
		ON CONFLICT (filename, directory_path)
			DO UPDATE SET directory_id = excluded.directory_id
		RETURNING id, filename, directory_id, directory_path, display_name
	`, filename, dirID, dirPath)
	return scanVideoRow(row)
}

func (s *SQLiteStore) ListVideos(ctx context.Context) ([]Video, error) {
	rows, err := s.conn.QueryContext(ctx, `
		SELECT id, filename, directory_id, directory_path, display_name
		FROM videos
		ORDER BY COALESCE(NULLIF(display_name, ''), filename)
	`)
	if err != nil {
		return nil, err
	}
	return scanVideos(rows)
}

func (s *SQLiteStore) ListVideosByTag(ctx context.Context, tagID int64) ([]Video, error) {
	rows, err := s.conn.QueryContext(ctx, `
		SELECT v.id, v.filename, v.directory_id, v.directory_path, v.display_name
		FROM videos v
		JOIN video_tags vt ON v.id = vt.video_id
		WHERE vt.tag_id = ?
		ORDER BY COALESCE(NULLIF(v.display_name, ''), v.filename)
	`, tagID)
	if err != nil {
		return nil, err
	}
	return scanVideos(rows)
}

func (s *SQLiteStore) ListVideosByDirectory(ctx context.Context, dirID int64) ([]Video, error) {
	rows, err := s.conn.QueryContext(ctx, `
		SELECT id, filename, directory_id, directory_path, display_name
		FROM videos
		WHERE directory_id = ?
		ORDER BY filename
	`, dirID)
	if err != nil {
		return nil, err
	}
	return scanVideos(rows)
}

func (s *SQLiteStore) GetVideo(ctx context.Context, id int64) (Video, error) {
	row := s.conn.QueryRowContext(ctx, `
		SELECT id, filename, directory_id, directory_path, display_name
		FROM videos WHERE id = ?
	`, id)
	return scanVideoRow(row)
}

func (s *SQLiteStore) UpdateVideoName(ctx context.Context, id int64, name string) error {
	return s.q.UpdateVideoName(ctx, db.UpdateVideoNameParams{
		ID:          id,
		DisplayName: name,
	})
}

func (s *SQLiteStore) DeleteVideo(ctx context.Context, id int64) error {
	_, err := s.conn.ExecContext(ctx, "DELETE FROM videos WHERE id = ?", id)
	return err
}

func (s *SQLiteStore) SearchVideos(ctx context.Context, query string) ([]Video, error) {
	rows, err := s.conn.QueryContext(ctx, `
		SELECT id, filename, directory_id, directory_path, display_name
		FROM videos
		WHERE LOWER(COALESCE(NULLIF(display_name, ''), filename)) LIKE LOWER(?)
		ORDER BY COALESCE(NULLIF(display_name, ''), filename)
	`, "%"+query+"%")
	if err != nil {
		return nil, err
	}
	return scanVideos(rows)
}

// --- Tags ---

func (s *SQLiteStore) UpsertTag(ctx context.Context, name string) (Tag, error) {
	row, err := s.q.UpsertTag(ctx, name)
	if err != nil {
		return Tag{}, err
	}
	return Tag{ID: row.ID, Name: row.Name}, nil
}

func (s *SQLiteStore) ListTags(ctx context.Context) ([]Tag, error) {
	rows, err := s.q.ListTags(ctx)
	if err != nil {
		return nil, err
	}
	tags := make([]Tag, len(rows))
	for i, r := range rows {
		tags[i] = Tag{ID: r.ID, Name: r.Name}
	}
	return tags, nil
}

func (s *SQLiteStore) TagVideo(ctx context.Context, videoID, tagID int64) error {
	return s.q.TagVideo(ctx, db.TagVideoParams{VideoID: videoID, TagID: tagID})
}

func (s *SQLiteStore) UntagVideo(ctx context.Context, videoID, tagID int64) error {
	return s.q.UntagVideo(ctx, db.UntagVideoParams{VideoID: videoID, TagID: tagID})
}

func (s *SQLiteStore) ListTagsByVideo(ctx context.Context, videoID int64) ([]Tag, error) {
	rows, err := s.q.ListTagsByVideo(ctx, videoID)
	if err != nil {
		return nil, err
	}
	tags := make([]Tag, len(rows))
	for i, r := range rows {
		tags[i] = Tag{ID: r.ID, Name: r.Name}
	}
	return tags, nil
}

// --- scan helpers ---

func scanVideoRow(row *sql.Row) (Video, error) {
	var v Video
	var dirID sql.NullInt64
	if err := row.Scan(&v.ID, &v.Filename, &dirID, &v.DirectoryPath, &v.DisplayName); err != nil {
		return Video{}, err
	}
	if dirID.Valid {
		v.DirectoryID = dirID.Int64
	}
	return v, nil
}

func scanVideos(rows *sql.Rows) ([]Video, error) {
	defer rows.Close()
	var videos []Video
	for rows.Next() {
		var v Video
		var dirID sql.NullInt64
		if err := rows.Scan(&v.ID, &v.Filename, &dirID, &v.DirectoryPath, &v.DisplayName); err != nil {
			return nil, err
		}
		if dirID.Valid {
			v.DirectoryID = dirID.Int64
		}
		videos = append(videos, v)
	}
	return videos, rows.Err()
}
