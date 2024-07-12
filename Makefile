SHELL=/usr/bin/env bash -o pipefail
VERSION?=$(shell cat VERSION | tr -d " \t\n\r")

PROMETHEUS_COMMON_PKG=github.com/prometheus/common

BUILD_DATE=$(shell date +"%Y%m%d-%T")
# source: https://docs.github.com/en/free-pro-team@latest/actions/reference/environment-variables#default-environment-variables
ifndef GITHUB_ACTIONS
	BUILD_USER?=$(USER)
	BUILD_BRANCH?=$(shell git branch --show-current)
	BUILD_REVISION?=$(shell git rev-parse --short HEAD)
else
	BUILD_USER=Action-Run-ID-$(GITHUB_RUN_ID)
	BUILD_BRANCH=$(GITHUB_REF:refs/heads/%=%)
	BUILD_REVISION=$(GITHUB_SHA)
endif

# The ldflags for the go build process to set the version related data.
GO_BUILD_LDFLAGS=\
	-s \
	-X $(PROMETHEUS_COMMON_PKG)/version.Revision=$(BUILD_REVISION)  \
	-X $(PROMETHEUS_COMMON_PKG)/version.BuildUser=$(BUILD_USER) \
	-X $(PROMETHEUS_COMMON_PKG)/version.BuildDate=$(BUILD_DATE) \
	-X $(PROMETHEUS_COMMON_PKG)/version.Branch=$(BUILD_BRANCH) \
	-X $(PROMETHEUS_COMMON_PKG)/version.Version=$(VERSION)

GO_BUILD_RECIPE=\
	GOOS=$(GOOS) \
	GOARCH=$(GOARCH) \
	CGO_ENABLED=0 \
	go build -ldflags="$(GO_BUILD_LDFLAGS)"

make: tidy

.PHONY: tidy
tidy:
	go mod tidy -v

.PHONY: build
build: clean node-metadata-agent

node-metadata-agent:
	$(GO_BUILD_RECIPE) -o bin/$@

run:
	go run main.go

clean:
	rm -rf bin

docker:
	docker buildx bake --load local
