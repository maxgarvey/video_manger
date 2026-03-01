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
	_, err := conn.Exec(`
		CREATE TABLE IF NOT EXISTS directories (
			id   INTEGER PRIMARY KEY AUTOINCREMENT,
			path TEXT    NOT NULL UNIQUE
		);
		CREATE TABLE IF NOT EXISTS videos (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			filename     TEXT    NOT NULL,
			directory_id INTEGER NOT NULL REFERENCES directories(id) ON DELETE CASCADE,
			display_name TEXT    NOT NULL DEFAULT '',
			UNIQUE(filename, directory_id)
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
	`)
	return err
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

// --- Videos ---

func (s *SQLiteStore) UpsertVideo(ctx context.Context, dirID int64, filename string) (Video, error) {
	row, err := s.q.UpsertVideo(ctx, db.UpsertVideoParams{
		Filename:    filename,
		DirectoryID: dirID,
	})
	if err != nil {
		return Video{}, err
	}
	return Video{
		ID:          row.ID,
		Filename:    row.Filename,
		DirectoryID: row.DirectoryID,
		DisplayName: row.DisplayName,
	}, nil
}

func (s *SQLiteStore) ListVideos(ctx context.Context) ([]Video, error) {
	rows, err := s.q.ListVideos(ctx)
	if err != nil {
		return nil, err
	}
	videos := make([]Video, len(rows))
	for i, r := range rows {
		videos[i] = Video{
			ID:            r.ID,
			Filename:      r.Filename,
			DirectoryID:   r.DirectoryID,
			DirectoryPath: r.DirectoryPath,
			DisplayName:   r.DisplayName,
		}
	}
	return videos, nil
}

func (s *SQLiteStore) ListVideosByTag(ctx context.Context, tagID int64) ([]Video, error) {
	rows, err := s.q.ListVideosByTag(ctx, tagID)
	if err != nil {
		return nil, err
	}
	videos := make([]Video, len(rows))
	for i, r := range rows {
		videos[i] = Video{
			ID:            r.ID,
			Filename:      r.Filename,
			DirectoryID:   r.DirectoryID,
			DirectoryPath: r.DirectoryPath,
			DisplayName:   r.DisplayName,
		}
	}
	return videos, nil
}

func (s *SQLiteStore) GetVideo(ctx context.Context, id int64) (Video, error) {
	r, err := s.q.GetVideoByID(ctx, id)
	if err != nil {
		return Video{}, err
	}
	return Video{
		ID:            r.ID,
		Filename:      r.Filename,
		DirectoryID:   r.DirectoryID,
		DirectoryPath: r.DirectoryPath,
		DisplayName:   r.DisplayName,
	}, nil
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
