.PHONY: fmt test build

fmt:
	gofmt -w -s .

test:
	go test ./... -race -count=1 -timeout 60s

build:
	go build -o video_manger .
