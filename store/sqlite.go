package store

import (
	"context"
	"database/sql"

	"github.com/maxgarvey/video_manger/db"
	_ "modernc.org/sqlite"
)

// SQLiteStore implements Store backed by a SQLite database.
type SQLiteStore struct {
	q    *db.Queries
	conn *sql.DB
}

// NewSQLite opens (or creates) a SQLite database at path and applies all
// pending migrations from the embedded migrations/ directory.
func NewSQLite(path string) (*SQLiteStore, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return nil, err
	}
	if err := runMigrations(conn); err != nil {
		return nil, err
	}
	return &SQLiteStore{q: db.New(conn), conn: conn}, nil
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
		RETURNING id, filename, directory_id, directory_path, display_name, rating
	`, filename, dirID, dirPath)
	return scanVideoRow(row)
}

func (s *SQLiteStore) ListVideos(ctx context.Context) ([]Video, error) {
	rows, err := s.conn.QueryContext(ctx, `
		SELECT id, filename, directory_id, directory_path, display_name, rating
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
		SELECT v.id, v.filename, v.directory_id, v.directory_path, v.display_name, v.rating
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
		SELECT id, filename, directory_id, directory_path, display_name, rating
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
		SELECT id, filename, directory_id, directory_path, display_name, rating
		FROM videos WHERE id = ?
	`, id)
	return scanVideoRow(row)
}

func (s *SQLiteStore) SetVideoRating(ctx context.Context, id int64, rating int) error {
	_, err := s.conn.ExecContext(ctx, `UPDATE videos SET rating = ? WHERE id = ?`, rating, id)
	return err
}

func (s *SQLiteStore) ListVideosByRating(ctx context.Context) ([]Video, error) {
	rows, err := s.conn.QueryContext(ctx, `
		SELECT id, filename, directory_id, directory_path, display_name, rating
		FROM videos
		ORDER BY rating DESC, COALESCE(NULLIF(display_name, ''), filename)
	`)
	if err != nil {
		return nil, err
	}
	return scanVideos(rows)
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
		SELECT id, filename, directory_id, directory_path, display_name, rating
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
	if err := row.Scan(&v.ID, &v.Filename, &dirID, &v.DirectoryPath, &v.DisplayName, &v.Rating); err != nil {
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
		if err := rows.Scan(&v.ID, &v.Filename, &dirID, &v.DirectoryPath, &v.DisplayName, &v.Rating); err != nil {
			return nil, err
		}
		if dirID.Valid {
			v.DirectoryID = dirID.Int64
		}
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

func (s *SQLiteStore) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.conn.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT (key) DO UPDATE SET value = excluded.value`,
		key, value)
	return err
}

// --- Watch history ---

func (s *SQLiteStore) RecordWatch(ctx context.Context, videoID int64, position float64) error {
	_, err := s.conn.ExecContext(ctx, `
		INSERT INTO watch_history (video_id, position, watched_at)
		VALUES (?, ?, datetime('now'))
		ON CONFLICT (video_id) DO UPDATE SET
			position   = excluded.position,
			watched_at = excluded.watched_at
	`, videoID, position)
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

func (s *SQLiteStore) ListWatchedIDs(ctx context.Context) (map[int64]bool, error) {
	rows, err := s.conn.QueryContext(ctx, `SELECT video_id FROM watch_history`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, rows.Err()
}
