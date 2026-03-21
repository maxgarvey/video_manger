package db_test

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/maxgarvey/video_manger/db"
)

const schema = `
CREATE TABLE IF NOT EXISTS directories (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    path TEXT    NOT NULL UNIQUE
);
CREATE TABLE IF NOT EXISTS tags (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT    NOT NULL UNIQUE
);
CREATE TABLE IF NOT EXISTS videos (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    filename       TEXT    NOT NULL,
    directory_id   INTEGER REFERENCES directories(id) ON DELETE SET NULL,
    directory_path TEXT    NOT NULL DEFAULT '',
    display_name   TEXT    NOT NULL DEFAULT '',
    show_name      TEXT    NOT NULL DEFAULT '',
    thumbnail_path TEXT    NOT NULL DEFAULT '',
    UNIQUE(filename, directory_path)
);
CREATE TABLE IF NOT EXISTS video_tags (
    video_id INTEGER NOT NULL REFERENCES videos(id)   ON DELETE CASCADE,
    tag_id   INTEGER NOT NULL REFERENCES tags(id)     ON DELETE CASCADE,
    PRIMARY KEY(video_id, tag_id)
);
`

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := conn.Exec(schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestNew(t *testing.T) {
	conn := newTestDB(t)
	q := db.New(conn)
	if q == nil {
		t.Fatal("expected non-nil Queries")
	}
}

func TestWithTx(t *testing.T) {
	conn := newTestDB(t)
	q := db.New(conn)
	tx, err := conn.Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer tx.Rollback()
	q2 := q.WithTx(tx)
	if q2 == nil {
		t.Fatal("expected non-nil Queries from WithTx")
	}
}

func TestAddDirectory(t *testing.T) {
	q := db.New(newTestDB(t))
	ctx := context.Background()
	dir, err := q.AddDirectory(ctx, "/videos/movies")
	if err != nil {
		t.Fatalf("AddDirectory: %v", err)
	}
	if dir.Path != "/videos/movies" {
		t.Errorf("got path %q, want /videos/movies", dir.Path)
	}
	if dir.ID == 0 {
		t.Error("expected non-zero ID")
	}
}

func TestDeleteDirectory(t *testing.T) {
	q := db.New(newTestDB(t))
	ctx := context.Background()
	dir, _ := q.AddDirectory(ctx, "/to/delete")
	if err := q.DeleteDirectory(ctx, dir.ID); err != nil {
		t.Fatalf("DeleteDirectory: %v", err)
	}
	dirs, _ := q.ListDirectories(ctx)
	for _, d := range dirs {
		if d.ID == dir.ID {
			t.Error("directory still present after delete")
		}
	}
}

func TestListDirectories(t *testing.T) {
	q := db.New(newTestDB(t))
	ctx := context.Background()
	q.AddDirectory(ctx, "/a")
	q.AddDirectory(ctx, "/b")
	dirs, err := q.ListDirectories(ctx)
	if err != nil {
		t.Fatalf("ListDirectories: %v", err)
	}
	if len(dirs) != 2 {
		t.Errorf("expected 2 directories, got %d", len(dirs))
	}
}

func TestListDirectories_Empty(t *testing.T) {
	q := db.New(newTestDB(t))
	dirs, err := q.ListDirectories(context.Background())
	if err != nil {
		t.Fatalf("ListDirectories empty: %v", err)
	}
	if len(dirs) != 0 {
		t.Errorf("expected 0 directories, got %d", len(dirs))
	}
}

func TestUpsertTag(t *testing.T) {
	q := db.New(newTestDB(t))
	ctx := context.Background()
	tag, err := q.UpsertTag(ctx, "genre:action")
	if err != nil {
		t.Fatalf("UpsertTag: %v", err)
	}
	if tag.Name != "genre:action" {
		t.Errorf("got name %q, want genre:action", tag.Name)
	}
	// Upsert again — same name, same ID.
	tag2, err := q.UpsertTag(ctx, "genre:action")
	if err != nil {
		t.Fatalf("UpsertTag (conflict): %v", err)
	}
	if tag2.ID != tag.ID {
		t.Errorf("expected same ID on conflict, got %d vs %d", tag2.ID, tag.ID)
	}
}

func TestListTags(t *testing.T) {
	q := db.New(newTestDB(t))
	ctx := context.Background()
	q.UpsertTag(ctx, "b-tag")
	q.UpsertTag(ctx, "a-tag")
	tags, err := q.ListTags(ctx)
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(tags))
	}
	// Should be sorted by name.
	if tags[0].Name != "a-tag" {
		t.Errorf("expected a-tag first, got %q", tags[0].Name)
	}
}

func TestListTags_Empty(t *testing.T) {
	q := db.New(newTestDB(t))
	tags, err := q.ListTags(context.Background())
	if err != nil {
		t.Fatalf("ListTags empty: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("expected 0 tags, got %d", len(tags))
	}
}

// insertVideo is a helper that inserts a minimal video row and returns its ID.
func insertVideo(t *testing.T, conn *sql.DB, filename string) int64 {
	t.Helper()
	res, err := conn.Exec(
		`INSERT INTO videos (filename, directory_path) VALUES (?, '')`, filename)
	if err != nil {
		t.Fatalf("insertVideo: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestTagVideo_And_ListTagsByVideo(t *testing.T) {
	conn := newTestDB(t)
	q := db.New(conn)
	ctx := context.Background()

	vid := insertVideo(t, conn, "movie.mp4")
	tag, _ := q.UpsertTag(ctx, "comedy")

	if err := q.TagVideo(ctx, db.TagVideoParams{VideoID: vid, TagID: tag.ID}); err != nil {
		t.Fatalf("TagVideo: %v", err)
	}

	tags, err := q.ListTagsByVideo(ctx, vid)
	if err != nil {
		t.Fatalf("ListTagsByVideo: %v", err)
	}
	if len(tags) != 1 || tags[0].ID != tag.ID {
		t.Errorf("expected 1 tag %d, got %+v", tag.ID, tags)
	}
}

func TestTagVideo_IdempotentInsertOrIgnore(t *testing.T) {
	conn := newTestDB(t)
	q := db.New(conn)
	ctx := context.Background()
	vid := insertVideo(t, conn, "ep1.mp4")
	tag, _ := q.UpsertTag(ctx, "sci-fi")
	q.TagVideo(ctx, db.TagVideoParams{VideoID: vid, TagID: tag.ID})
	// Second insert should be ignored, not error.
	if err := q.TagVideo(ctx, db.TagVideoParams{VideoID: vid, TagID: tag.ID}); err != nil {
		t.Errorf("TagVideo (duplicate): %v", err)
	}
}

func TestUntagVideo(t *testing.T) {
	conn := newTestDB(t)
	q := db.New(conn)
	ctx := context.Background()
	vid := insertVideo(t, conn, "series.mp4")
	tag, _ := q.UpsertTag(ctx, "drama")
	q.TagVideo(ctx, db.TagVideoParams{VideoID: vid, TagID: tag.ID})

	if err := q.UntagVideo(ctx, db.UntagVideoParams{VideoID: vid, TagID: tag.ID}); err != nil {
		t.Fatalf("UntagVideo: %v", err)
	}
	tags, _ := q.ListTagsByVideo(ctx, vid)
	if len(tags) != 0 {
		t.Errorf("expected 0 tags after untag, got %d", len(tags))
	}
}

func TestListTagsByVideo_Empty(t *testing.T) {
	conn := newTestDB(t)
	q := db.New(conn)
	ctx := context.Background()
	vid := insertVideo(t, conn, "notag.mp4")
	tags, err := q.ListTagsByVideo(ctx, vid)
	if err != nil {
		t.Fatalf("ListTagsByVideo: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("expected 0 tags, got %d", len(tags))
	}
}

func TestUpdateVideoName(t *testing.T) {
	conn := newTestDB(t)
	q := db.New(conn)
	ctx := context.Background()
	vid := insertVideo(t, conn, "raw.mp4")

	err := q.UpdateVideoName(ctx, db.UpdateVideoNameParams{DisplayName: "My Movie", ID: vid})
	if err != nil {
		t.Fatalf("UpdateVideoName: %v", err)
	}
	var name string
	conn.QueryRow(`SELECT display_name FROM videos WHERE id = ?`, vid).Scan(&name)
	if name != "My Movie" {
		t.Errorf("got display_name %q, want My Movie", name)
	}
}

func TestUpdateVideoShowName(t *testing.T) {
	conn := newTestDB(t)
	q := db.New(conn)
	ctx := context.Background()
	vid := insertVideo(t, conn, "ep.mp4")

	err := q.UpdateVideoShowName(ctx, db.UpdateVideoShowNameParams{ShowName: "Breaking Bad", ID: vid})
	if err != nil {
		t.Fatalf("UpdateVideoShowName: %v", err)
	}
	var name string
	conn.QueryRow(`SELECT show_name FROM videos WHERE id = ?`, vid).Scan(&name)
	if name != "Breaking Bad" {
		t.Errorf("got show_name %q, want Breaking Bad", name)
	}
}

func TestUpdateVideoThumbnail(t *testing.T) {
	conn := newTestDB(t)
	q := db.New(conn)
	ctx := context.Background()
	vid := insertVideo(t, conn, "vid.mp4")

	err := q.UpdateVideoThumbnail(ctx, db.UpdateVideoThumbnailParams{ThumbnailPath: "/thumbs/1.jpg", ID: vid})
	if err != nil {
		t.Fatalf("UpdateVideoThumbnail: %v", err)
	}
	var path string
	conn.QueryRow(`SELECT thumbnail_path FROM videos WHERE id = ?`, vid).Scan(&path)
	if path != "/thumbs/1.jpg" {
		t.Errorf("got thumbnail_path %q, want /thumbs/1.jpg", path)
	}
}
