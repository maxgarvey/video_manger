# video_manger

[![CI](https://github.com/maxgarvey/video_manger/actions/workflows/ci.yml/badge.svg)](https://github.com/maxgarvey/video_manger/actions/workflows/ci.yml)

A localhost web application for serving and playing videos in the browser.

## Overview

`video_manger` is a Go web server that runs locally and streams your video files through a browser-based player. Point it at a directory, open your browser, and watch — no external media players needed.

Built with [chi](https://github.com/go-chi/chi) for routing and [htmx](https://htmx.org) for dynamic UI updates without a JS framework.

## Usage

```bash
go run main.go -dir /path/to/videos -port 8080
```

Then open `http://localhost:8080` in your browser.

| Flag | Default | Description |
|------|---------|-------------|
| `-dir` | `.` | Directory to scan for video files |
| `-port` | `8080` | Port to listen on |

## Supported Formats

`.mp4`, `.webm`, `.ogg`, `.mov`, `.mkv`, `.avi`

## Development

```bash
# Run tests
go test ./... -v -race

# Build binary
go build -o video_manger .
```

## Stack

- **[chi](https://github.com/go-chi/chi)** — lightweight, idiomatic Go router
- **[htmx](https://htmx.org)** — dynamic UI via HTML attributes, no JS framework
- **`net/http`** — video streaming with range request support for seeking
- **`embed`** — HTML templates compiled into the binary
