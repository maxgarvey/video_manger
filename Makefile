.PHONY: fmt test build roku

fmt:
	gofmt -w -s .

test:
	go test ./... -race -count=1 -timeout 60s

build:
	go build -o video_manger .

# Package the Roku BrightScript channel for sideloading.
# Roku requires the zip to be rooted at the channel contents (manifest at the
# top level, not inside a subdirectory), so we cd into roku/ before zipping.
roku:
	cd roku && zip -r ../video_manger_roku.zip manifest source components images
	@echo "Roku channel: video_manger_roku.zip"
