package store

import (
	"context"
	"path/filepath"
)

// Directory represents a registered video directory.
type Directory struct {
	ID   int64
	Path string
}

// Video represents a video file with optional metadata.
type Video struct {
	ID            int64
	Filename      string
	DirectoryID   int64
	DirectoryPath string
	DisplayName   string
}

// Title returns the display name if set, otherwise the filename.
func (v Video) Title() string {
	if v.DisplayName != "" {
		return v.DisplayName
	}
	return v.Filename
}

// FilePath returns the absolute path to the video file on disk.
func (v Video) FilePath() string {
	return filepath.Join(v.DirectoryPath, v.Filename)
}

// Tag represents a label that can be applied to videos.
type Tag struct {
	ID   int64
	Name string
}

// Store is the backend-agnostic interface for all persistence operations.
// Swap implementations (e.g. SQLite → Postgres) by providing a different Store.
type Store interface {
	// Directory management
	AddDirectory(ctx context.Context, path string) (Directory, error)
	ListDirectories(ctx context.Context) ([]Directory, error)
	DeleteDirectory(ctx context.Context, id int64) error

	// Video management
	UpsertVideo(ctx context.Context, dirID int64, dirPath string, filename string) (Video, error)
	ListVideos(ctx context.Context) ([]Video, error)
	ListVideosByTag(ctx context.Context, tagID int64) ([]Video, error)
	ListVideosByDirectory(ctx context.Context, dirID int64) ([]Video, error)
	GetVideo(ctx context.Context, id int64) (Video, error)
	UpdateVideoName(ctx context.Context, id int64, name string) error
	DeleteVideo(ctx context.Context, id int64) error
	SearchVideos(ctx context.Context, query string) ([]Video, error)

	// Tag management
	UpsertTag(ctx context.Context, name string) (Tag, error)
	ListTags(ctx context.Context) ([]Tag, error)
	TagVideo(ctx context.Context, videoID, tagID int64) error
	UntagVideo(ctx context.Context, videoID, tagID int64) error
	ListTagsByVideo(ctx context.Context, videoID int64) ([]Tag, error)
}
