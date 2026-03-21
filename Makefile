.PHONY: fmt test build roku roku-deploy precommit install-hooks

fmt:
	gofmt -w -s .

test:
	go test ./... -race -count=1 -timeout 180s

precommit: fmt
	go test ./... -race -count=1 -timeout 180s

install-hooks:
	cp scripts/pre-commit .git/hooks/pre-commit
	chmod +x .git/hooks/pre-commit

build:
	go build -o video_manger .

# Package the Roku BrightScript channel for sideloading.
# Roku requires the zip to be rooted at the channel contents (manifest at the
# top level, not inside a subdirectory), so we cd into roku/ before zipping.
roku:
	cd roku && zip -r ../video_manger_roku.zip manifest source components images
	@echo "Roku channel: video_manger_roku.zip"

# Sideload the channel zip to a Roku device over the local network.
# Requires the device to be in developer mode (Settings > System > Advanced >
# Developer mode) and the rokudev password you chose during setup.
#
# Usage:
#   make roku-deploy ROKU_IP=192.168.1.x ROKU_PASS=yourpassword
#   # or export them so you don't have to repeat:
#   export ROKU_IP=192.168.1.x ROKU_PASS=yourpassword
#   make roku-deploy
ROKU_IP   ?= $(error ROKU_IP is not set — usage: make roku-deploy ROKU_IP=x.x.x.x ROKU_PASS=yourpassword)
ROKU_PASS ?= $(error ROKU_PASS is not set — usage: make roku-deploy ROKU_IP=x.x.x.x ROKU_PASS=yourpassword)

roku-deploy: roku
	curl --digest -u rokudev:$(ROKU_PASS) \
	     -F "mysubmit=Install" \
	     -F "archive=@video_manger_roku.zip" \
	     http://$(ROKU_IP)/plugin_install
	@echo ""
	@echo "Deployed to http://$(ROKU_IP)"
