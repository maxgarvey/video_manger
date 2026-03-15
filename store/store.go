package store

import (
	"context"
	"path/filepath"
	"time"
)

// Directory represents a registered video directory.
type Directory struct {
	ID   int64
	Path string
}

// Video represents a video file with optional metadata.
type Video struct {
	ID               int64
	Filename         string
	DirectoryID      int64
	DirectoryPath    string
	DisplayName      string
	ShowName         string
	VideoType        string // classification: TV, Movie, Concert, Vlog, Blog, YouTube
	Rating           int    // 0=neutral, 1=liked, 2=double-liked
	OriginalFilename string // filename at first import; never changed on rename/move
	// Standardised descriptive fields (see VideoFields).
	Genre         string
	SeasonNumber  int
	EpisodeNumber int
	EpisodeTitle  string
	Actors        string
	Studio        string
	Channel       string
	AirDate       string  // original air/release date, e.g. "2023-04-15" (optional)
	ThumbnailPath string  // relative or absolute path to thumbnail image
	DurationS     float64 // total duration in seconds; 0 means unknown
	ColorLabel    string  // color label: red, orange, yellow, green, blue, purple, or empty
	// WatchedAt holds the last watch timestamp (SQLite datetime string, empty if never watched).
	// Populated by list queries via LEFT JOIN watch_history — do not set manually.
	WatchedAt string
}

// VideoFields holds the editable standardised descriptive fields for a video.
type VideoFields struct {
	Genre         string
	SeasonNumber  int
	EpisodeNumber int
	EpisodeTitle  string
	Actors        string
	Studio        string
	Channel       string
	AirDate       string
}

// VideoLabelColors maps color label names to hex values used by the UI.
var VideoLabelColors = map[string]string{
	"red":    "#dc2626",
	"orange": "#ea580c",
	"yellow": "#ca8a04",
	"green":  "#16a34a",
	"blue":   "#2563eb",
	"purple": "#9333ea",
}

// IsValidColorLabel reports whether the provided color label string is known.
func IsValidColorLabel(c string) bool {
	if c == "" {
		return true // empty means unset
	}
	_, ok := VideoLabelColors[c]
	return ok
}

// VideoTypes maps the canonical type string to a display color (used by UI).
// To add a new type you only need to update this map; handlers and templates
// derive their valid set from its keys.
var VideoTypes = map[string]string{
	"TV":      "#2563eb",
	"Movie":   "#16a34a",
	"Concert": "#9333ea",
	"Vlog":    "#dc2626",
	"Blog":    "#ea580c",
	"YouTube": "#ee2938",
}

// ValidVideoTypes returns a slice of known type names in iteration order.
// Note: map iteration order is random, but consumers (templates) may sort if
// needed.
func ValidVideoTypes() []string {
	list := make([]string, 0, len(VideoTypes))
	for t := range VideoTypes {
		list = append(list, t)
	}
	return list
}

// IsValidVideoType reports whether the provided type string is known.
func IsValidVideoType(t string) bool {
	if t == "" {
		return true // empty means unset
	}
	_, ok := VideoTypes[t]
	return ok
}

// HasFields reports whether any standardised field is populated.
func (v Video) HasFields() bool {
	return v.Genre != "" || v.SeasonNumber > 0 || v.EpisodeNumber > 0 ||
		v.EpisodeTitle != "" || v.Actors != "" || v.Studio != "" || v.Channel != "" || v.AirDate != ""
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

// WatchRecord holds the last playback position and timestamp for a video.
type WatchRecord struct {
	VideoID   int64
	Position  float64 // seconds
	WatchedAt string  // RFC3339 / SQLite datetime string
}

// Store is the backend-agnostic interface for all persistence operations.
// Swap implementations (e.g. SQLite → Postgres) by providing a different Store.
type Store interface {
	// Directory management
	AddDirectory(ctx context.Context, path string) (Directory, error)
	GetDirectory(ctx context.Context, id int64) (Directory, error)
	ListDirectories(ctx context.Context) ([]Directory, error)
	DeleteDirectory(ctx context.Context, id int64) error
	// DeleteDirectoryAndVideos atomically removes a directory and all its
	// video records in a single transaction. It returns the file paths of
	// the deleted videos so the caller can remove them from disk.
	DeleteDirectoryAndVideos(ctx context.Context, id int64) ([]string, error)

	// Video management
	UpsertVideo(ctx context.Context, dirID int64, dirPath string, filename string) (Video, error)
	ListVideos(ctx context.Context) ([]Video, error)
	CountVideos(ctx context.Context) (int, error)
	ListVideosByTag(ctx context.Context, tagID int64) ([]Video, error)
	ListVideosByDirectory(ctx context.Context, dirID int64) ([]Video, error)
	GetVideo(ctx context.Context, id int64) (Video, error)
	UpdateVideoName(ctx context.Context, id int64, name string) error
	SetVideoRating(ctx context.Context, id int64, rating int) error
	UpdateVideoShowName(ctx context.Context, id int64, showName string) error
	// UpdateVideoType sets the classification string; empty clears it.
	UpdateVideoType(ctx context.Context, id int64, videoType string) error
	DeleteVideo(ctx context.Context, id int64) error
	UpdateVideoPath(ctx context.Context, id, dirID int64, dirPath, filename string) error
	UpdateVideoFields(ctx context.Context, id int64, f VideoFields) error
	ListVideosByMinRating(ctx context.Context, minRating int) ([]Video, error)
	SearchVideos(ctx context.Context, query string) ([]Video, error)
	ListVideosByType(ctx context.Context, videoType string) ([]Video, error)
	ListVideosByRating(ctx context.Context) ([]Video, error)
	ListVideosByShow(ctx context.Context, showName string) ([]Video, error)
	GetRandomVideo(ctx context.Context) (Video, error)
	GetNextUnwatched(ctx context.Context, tagID int64) (Video, error)

	// Session persistence (used when password auth is enabled)
	SaveSession(ctx context.Context, token string, expiry time.Time) error
	DeleteSession(ctx context.Context, token string) error
	LoadSessions(ctx context.Context) (map[string]time.Time, error)
	PruneExpiredSessions(ctx context.Context) error

	// Settings
	GetSetting(ctx context.Context, key string) (string, error)
	// SaveSettings atomically writes multiple key-value pairs in a single transaction.
	SaveSettings(ctx context.Context, pairs map[string]string) error

	// Watch history
	RecordWatch(ctx context.Context, videoID int64, position float64) error
	ClearWatch(ctx context.Context, videoID int64) error
	GetWatch(ctx context.Context, videoID int64) (WatchRecord, error)
	ListWatchHistory(ctx context.Context) (map[int64]WatchRecord, error)

	// Tag management
	UpsertTag(ctx context.Context, name string) (Tag, error)
	ListTags(ctx context.Context) ([]Tag, error)
	TagVideo(ctx context.Context, videoID, tagID int64) error
	UntagVideo(ctx context.Context, videoID, tagID int64) error
	ListTagsByVideo(ctx context.Context, videoID int64) ([]Tag, error)
	// PruneOrphanTags removes tags that are no longer associated with any video.
	PruneOrphanTags(ctx context.Context) error

	// SetExclusiveSystemTag removes all tags with prefix "namespace:" from the video
	// and upserts "namespace:value". Empty value just removes existing tags.
	SetExclusiveSystemTag(ctx context.Context, videoID int64, namespace, value string) error

	// SetMultiSystemTag removes all tags with prefix "namespace:" then adds one tag
	// per value in values (empty strings skipped).
	SetMultiSystemTag(ctx context.Context, videoID int64, namespace string, values []string) error

	// Thumbnail management
	UpdateVideoThumbnail(ctx context.Context, videoID int64, thumbnailPath string) error

	// Duration
	UpdateVideoDuration(ctx context.Context, videoID int64, duration float64) error
}
