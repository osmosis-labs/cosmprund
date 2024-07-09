VERSION := $(shell git describe --tags)
COMMIT  := $(shell git log -1 --format='%H')
GO_VERSION := $(shell cat go.mod | grep -E 'go [0-9].[0-9]+' | cut -d ' ' -f 2)

all: install

build:
	@echo "Building cosmprund"
	@go build -mod readonly $(BUILD_FLAGS) -o build/cosmprund main.go

clean:
	rm -rf build

.PHONY: all lint test race msan tools clean build

###############################################################################
###                                Docker                                  ###
###############################################################################

docker-build:
	docker build -t cosmprund:local .

docker-run:
	docker run cosmprund:local

###############################################################################
###                                Release                                  ###
###############################################################################
GORELEASER_IMAGE := ghcr.io/goreleaser/goreleaser-cross:v$(GO_VERSION)

ifdef GITHUB_TOKEN
release:
	docker run \
		--rm \
		-e GITHUB_TOKEN=$(GITHUB_TOKEN) \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v `pwd`:/go/src/diffusion \
		-w /go/src/diffusion \
		$(GORELEASER_IMAGE) \
		release \
		--clean
else
release:
	@echo "Error: GITHUB_TOKEN is not defined. Please define it before running 'make release'."
endif

release-dry-run:
	docker run \
		--rm \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v `pwd`:/go/src/diffusion \
		-w /go/src/diffusion \
		$(GORELEASER_IMAGE) \
		release \
		--clean \
		--skip=publish

release-snapshot:
	docker run \
		--rm \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v `pwd`:/go/src/osmosisd \
		-w /go/src/osmosisd \
		$(GORELEASER_IMAGE) \
		release \
		--clean \
		--snapshot \
		--skip=validate \
		--skip=publish