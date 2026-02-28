// Command populate fetches episode metadata from TVMaze, renames video files to
// include the episode title, and writes full metadata to each file via ffmpeg.
//
// Usage:
//
//	go run ./cmd/populate -dir /path/to/bobs_burgers
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/maxgarvey/video_manger/metadata"
)

type episode struct {
	Season  int    `json:"season"`
	Number  int    `json:"number"`
	Name    string `json:"name"`
	Airdate string `json:"airdate"`
	Summary string `json:"summary"`
}

var (
	htmlTagRe = regexp.MustCompile(`<[^>]+>`)
	epKeyRe   = regexp.MustCompile(`(?i)^(S\d+E\d+)`)
)

func main() {
	dir := flag.String("dir", "/Users/maxgarvey/video_stuff/bobs_burgers", "root directory containing Season N subdirectories")
	flag.Parse()

	if _, err := exec.LookPath("ffmpeg"); err != nil {
		log.Fatal("ffmpeg not found in PATH — required for metadata writing")
	}

	log.Println("Fetching episode data from TVMaze...")
	eps, err := fetchEpisodes(107) // Bob's Burgers show ID
	if err != nil {
		log.Fatalf("fetch episodes: %v", err)
	}
	log.Printf("Loaded %d episodes", len(eps))

	var renamed, tagged, skipped, failed int

	for season := 1; season <= 14; season++ {
		seasonDir := filepath.Join(*dir, fmt.Sprintf("Season %d", season))
		entries, err := os.ReadDir(seasonDir)
		if err != nil {
			log.Printf("skip %s: %v", seasonDir, err)
			continue
		}

		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || strings.ToLower(filepath.Ext(name)) != ".mp4" {
				continue
			}

			m := epKeyRe.FindStringSubmatch(name)
			if m == nil {
				log.Printf("  skip (no S##E## prefix): %s", name)
				skipped++
				continue
			}
			key := strings.ToUpper(m[1]) // e.g. "S01E01"
			ep, ok := eps[key]
			if !ok {
				log.Printf("  skip (no TVMaze data): %s", name)
				skipped++
				continue
			}

			oldPath := filepath.Join(seasonDir, name)
			newName := fmt.Sprintf("%s - %s.mp4", key, sanitize(ep.Name))
			newPath := filepath.Join(seasonDir, newName)

			if oldPath != newPath {
				if err := os.Rename(oldPath, newPath); err != nil {
					log.Printf("  FAIL rename %s: %v", name, err)
					failed++
					continue
				}
				renamed++
			}

			show := "Bob's Burgers"
			genre := "Animation"
			network := "Fox"
			desc := stripHTML(ep.Summary)
			seasonStr := fmt.Sprintf("%d", ep.Season)
			epNumStr := fmt.Sprintf("%d", ep.Number)
			keywords := []string{
				"Bob's Burgers",
				"Animation",
				"Comedy",
				fmt.Sprintf("Season %d", ep.Season),
				"Fox",
			}

			if err := metadata.Write(newPath, metadata.Updates{
				Title:       &ep.Name,
				Description: &desc,
				Genre:       &genre,
				Date:        &ep.Airdate,
				Show:        &show,
				EpisodeID:   &key,
				SeasonNum:   &seasonStr,
				EpisodeNum:  &epNumStr,
				Network:     &network,
				Keywords:    keywords,
			}); err != nil {
				log.Printf("  FAIL metadata %s: %v", newName, err)
				failed++
				continue
			}

			log.Printf("  ✓ %s — %s (%s)", key, ep.Name, ep.Airdate)
			tagged++
		}
	}

	fmt.Printf("\nDone.\n  renamed: %d\n  tagged:  %d\n  skipped: %d\n  failed:  %d\n",
		renamed, tagged, skipped, failed)
}

func fetchEpisodes(showID int) (map[string]episode, error) {
	url := fmt.Sprintf("https://api.tvmaze.com/shows/%d/episodes", showID)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var list []episode
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}

	m := make(map[string]episode, len(list))
	for _, ep := range list {
		key := fmt.Sprintf("S%02dE%02d", ep.Season, ep.Number)
		m[key] = ep
	}
	return m, nil
}

// stripHTML removes HTML tags and decodes common entities.
func stripHTML(s string) string {
	s = htmlTagRe.ReplaceAllString(s, "")
	s = strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&#39;", "'",
		"&nbsp;", " ",
	).Replace(s)
	return strings.TrimSpace(s)
}

// sanitize makes a string safe to use as part of a filename.
func sanitize(s string) string {
	return strings.TrimSpace(strings.NewReplacer(
		"/", "-",
		"\\", "-",
		":", "-",
		"*", "",
		"?", "",
		`"`, "",
		"<", "",
		">", "",
		"|", "-",
	).Replace(s))
}
