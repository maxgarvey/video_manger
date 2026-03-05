package store

import (
	"context"
	"database/sql"
	"path/filepath"
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
	// show_name is not known during initial sync; insert empty string so
	// RETURNING ordering stays consistent with scan helpers.
	row := s.conn.QueryRowContext(ctx, `
		INSERT INTO videos (filename, directory_id, directory_path, original_filename, show_name)
		VALUES (?, ?, ?, ?, '')
		ON CONFLICT (filename, directory_path)
			DO UPDATE SET directory_id = excluded.directory_id
		RETURNING id, filename, directory_id, directory_path, show_name, display_name, rating, original_filename,
		          genre, season_number, episode_number, episode_title, actors, studio, channel,
		          NULL -- watched_at: new/upserted videos have no watch history yet
	`, filename, dirID, dirPath, filename)
	return scanVideoRow(row)
}

func (s *SQLiteStore) ListVideos(ctx context.Context) ([]Video, error) {
	rows, err := s.conn.QueryContext(ctx, `
		SELECT v.id, v.filename, v.directory_id, v.directory_path, v.display_name, v.show_name, v.rating, v.original_filename,
		       v.genre, v.season_number, v.episode_number, v.episode_title, v.actors, v.studio, v.channel,
		       wh.watched_at
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
		SELECT v.id, v.filename, v.directory_id, v.directory_path, v.display_name, v.show_name, v.rating, v.original_filename,
		       v.genre, v.season_number, v.episode_number, v.episode_title, v.actors, v.studio, v.channel,
		       wh.watched_at
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
		SELECT v.id, v.filename, v.directory_id, v.directory_path, v.display_name, v.show_name, v.rating, v.original_filename,
		       v.genre, v.season_number, v.episode_number, v.episode_title, v.actors, v.studio, v.channel,
		       wh.watched_at
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
		SELECT v.id, v.filename, v.directory_id, v.directory_path, v.display_name, v.show_name, v.rating, v.original_filename,
		       v.genre, v.season_number, v.episode_number, v.episode_title, v.actors, v.studio, v.channel,
		       wh.watched_at
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
		SELECT v.id, v.filename, v.directory_id, v.directory_path, v.display_name, v.show_name, v.rating, v.original_filename,
		       v.genre, v.season_number, v.episode_number, v.episode_title, v.actors, v.studio, v.channel,
		       wh.watched_at
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
		SELECT v.id, v.filename, v.directory_id, v.directory_path, v.display_name, v.show_name, v.rating, v.original_filename,
		       v.genre, v.season_number, v.episode_number, v.episode_title, v.actors, v.studio, v.channel,
		       wh.watched_at
		FROM videos v
		LEFT JOIN watch_history wh ON v.id = wh.video_id
		WHERE v.show_name = ?
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
			SELECT v.id, v.filename, v.directory_id, v.directory_path, v.display_name, v.show_name, v.rating, v.original_filename,
			       v.genre, v.season_number, v.episode_number, v.episode_title, v.actors, v.studio, v.channel,
			       wh.watched_at
			FROM videos v
			JOIN video_tags vt ON v.id = vt.video_id
			LEFT JOIN watch_history wh ON v.id = wh.video_id
			WHERE vt.tag_id = ? AND wh.video_id IS NULL
			ORDER BY COALESCE(NULLIF(v.display_name,''), v.filename)
			LIMIT 1
		`, tagID)
	} else {
		row = s.conn.QueryRowContext(ctx, `
			SELECT v.id, v.filename, v.directory_id, v.directory_path, v.display_name, v.show_name, v.rating, v.original_filename,
			       v.genre, v.season_number, v.episode_number, v.episode_title, v.actors, v.studio, v.channel,
			       wh.watched_at
			FROM videos v
			LEFT JOIN watch_history wh ON v.id = wh.video_id
			WHERE wh.video_id IS NULL
			ORDER BY COALESCE(NULLIF(v.display_name,''), v.filename)
			LIMIT 1
		`)
	}
	return scanVideoRow(row)
}

func (s *SQLiteStore) GetRandomVideo(ctx context.Context) (Video, error) {
	// Use OFFSET instead of ORDER BY RANDOM() to avoid a full-table sort.
	// MAX(1, …) prevents modulo-by-zero when the table is empty; the query
	// still returns no rows because there are none to offset into.
	row := s.conn.QueryRowContext(ctx, `
		SELECT v.id, v.filename, v.directory_id, v.directory_path, v.display_name, v.show_name, v.rating, v.original_filename,
		       v.genre, v.season_number, v.episode_number, v.episode_title, v.actors, v.studio, v.channel,
		       wh.watched_at
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
	_, err := s.conn.ExecContext(ctx, `UPDATE videos SET show_name = ? WHERE id = ?`, showName, id)
	return err
}

func (s *SQLiteStore) UpdateVideoThumbnail(ctx context.Context, videoID int64, thumbnailPath string) error {
	_, err := s.conn.ExecContext(ctx, `UPDATE videos SET thumbnail_path = ? WHERE id = ?`, thumbnailPath, videoID)
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
	_, err := s.conn.ExecContext(ctx, `
		UPDATE videos SET
			genre=?, season_number=?, episode_number=?,
			episode_title=?, actors=?, studio=?, channel=?
		WHERE id=?`,
		f.Genre, f.SeasonNumber, f.EpisodeNumber,
		f.EpisodeTitle, f.Actors, f.Studio, f.Channel, id)
	return err
}

func (s *SQLiteStore) ListVideosByMinRating(ctx context.Context, minRating int) ([]Video, error) {
	rows, err := s.conn.QueryContext(ctx, `
		SELECT v.id, v.filename, v.directory_id, v.directory_path, v.display_name, v.show_name, v.rating, v.original_filename,
		       v.genre, v.season_number, v.episode_number, v.episode_title, v.actors, v.studio, v.channel,
		       wh.watched_at
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
		rows, err := s.conn.QueryContext(ctx, `
			SELECT v.id, v.filename, v.directory_id, v.directory_path, v.display_name, v.show_name, v.rating, v.original_filename,
			       v.genre, v.season_number, v.episode_number, v.episode_title, v.actors, v.studio, v.channel,
			       wh.watched_at
			FROM videos v
			JOIN videos_fts ON videos_fts.rowid = v.id
			LEFT JOIN watch_history wh ON v.id = wh.video_id
			WHERE videos_fts MATCH ?
			ORDER BY COALESCE(NULLIF(v.display_name, ''), v.filename) ASC
		`, ftsQuery)
		if err == nil {
			return scanVideos(rows)
		}
		// FTS table unavailable — fall through to LIKE.
	}
	// LIKE fallback: escape special chars so they are treated literally.
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
	rows, err := s.conn.QueryContext(ctx, `
		SELECT v.id, v.filename, v.directory_id, v.directory_path, v.display_name, v.show_name, v.rating, v.original_filename,
		       v.genre, v.season_number, v.episode_number, v.episode_title, v.actors, v.studio, v.channel,
		       wh.watched_at
		FROM videos v
		LEFT JOIN watch_history wh ON v.id = wh.video_id
		WHERE LOWER(COALESCE(NULLIF(v.display_name, ''), v.filename)) LIKE LOWER(?) ESCAPE '\'
		ORDER BY COALESCE(NULLIF(v.display_name, ''), v.filename) ASC
	`, "%"+escaped+"%")
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

// --- scan helpers ---

func scanVideoRow(row *sql.Row) (Video, error) {
	var v Video
	var dirID sql.NullInt64
	var watchedAt sql.NullString
	if err := row.Scan(
		&v.ID, &v.Filename, &dirID, &v.DirectoryPath, &v.DisplayName, &v.ShowName, &v.Rating, &v.OriginalFilename,
		&v.Genre, &v.SeasonNumber, &v.EpisodeNumber, &v.EpisodeTitle, &v.Actors, &v.Studio, &v.Channel,
		&watchedAt,
	); err != nil {
		return Video{}, err
	}
	if dirID.Valid {
		v.DirectoryID = dirID.Int64
	}
	if watchedAt.Valid {
		v.WatchedAt = watchedAt.String
	}
	return v, nil
}

func scanVideos(rows *sql.Rows) ([]Video, error) {
	defer rows.Close()
	var videos []Video
	for rows.Next() {
		var v Video
		var dirID sql.NullInt64
		var watchedAt sql.NullString
		if err := rows.Scan(
			&v.ID, &v.Filename, &dirID, &v.DirectoryPath, &v.DisplayName, &v.ShowName, &v.Rating, &v.OriginalFilename,
			&v.Genre, &v.SeasonNumber, &v.EpisodeNumber, &v.EpisodeTitle, &v.Actors, &v.Studio, &v.Channel,
			&watchedAt,
		); err != nil {
			return nil, err
		}
		if dirID.Valid {
			v.DirectoryID = dirID.Int64
		}
		if watchedAt.Valid {
			v.WatchedAt = watchedAt.String
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
	_, err := s.conn.ExecContext(ctx, `
		INSERT INTO watch_history (video_id, position, watched_at)
		VALUES (?, ?, datetime('now'))
		ON CONFLICT (video_id) DO UPDATE SET
			position   = excluded.position,
			watched_at = excluded.watched_at
	`, videoID, position)
	return err
}

func (s *SQLiteStore) ClearWatch(ctx context.Context, videoID int64) error {
	_, err := s.conn.ExecContext(ctx, `DELETE FROM watch_history WHERE video_id = ?`, videoID)
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
