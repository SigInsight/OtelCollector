COMMIT_SHA ?= $(shell git rev-parse HEAD)
REPONAME ?= siginsight
IMAGE_NAME ?= siginsight-otel-collector
CONFIG_FILE ?= ./config/collector.local.yaml
DOCKER_TAG ?= latest

GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
GOPATH ?= $(shell go env GOPATH)
# The local Prometheus receiver implements static, file, and HTTP discovery.
# Exclude all upstream optional service discovery plugins from the build.
PROMETHEUS_BUILD_TAGS ?= remove_all_sd
GOTEST=go test -v $(RACE)
GOFMT=gofmt
FMT_LOG=.fmt.log
IMPORT_LOG=.import.log

CLICKHOUSE_HOST ?= 127.0.0.1
CLICKHOUSE_PORT ?= 9000

LD_FLAGS ?=


.PHONY: install-tools
install-tools:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2

.DEFAULT_GOAL := test-and-lint

.PHONY: test-and-lint
test-and-lint: test fmt lint

.PHONY: test
test:
	go test -tags="$(PROMETHEUS_BUILD_TAGS)" -count=1 -v -race -cover ./...

.PHONY: build
build:
	go build -tags="$(PROMETHEUS_BUILD_TAGS)" -o .build/${GOOS}-${GOARCH}/siginsight-otel-collector ./cmd/siginsightotelcollector

.PHONY: amd64
amd64:
	make GOARCH=amd64 build

.PHONY: arm64
arm64:
	make GOARCH=arm64 build

.PHONY: build-all
build-all: amd64 arm64

.PHONY: run
run:
	go run -tags="$(PROMETHEUS_BUILD_TAGS)" cmd/siginsightotelcollector/main.go --config ${CONFIG_FILE}

.PHONY: fmt
fmt:
	@echo Running go fmt on query service ...
	@$(GOFMT) -e -s -l -w .

.PHONY: build-and-push-siginsight-collector
build-and-push-siginsight-collector:
	@echo "------------------"
	@echo  "--> Build and push SigInsight collector docker image"
	@echo "------------------"
	docker buildx build --platform linux/amd64,linux/arm64 --progress plain \
		--no-cache --push -f cmd/siginsightotelcollector/Dockerfile \
		--tag $(REPONAME)/$(IMAGE_NAME):$(DOCKER_TAG) .

.PHONY: build-siginsight-collector
build-siginsight-collector:
	@echo "------------------"
	@echo  "--> Build SigInsight collector docker image"
	@echo "------------------"
	docker build --build-arg TARGETPLATFORM="linux/amd64" \
		--no-cache -f cmd/siginsightotelcollector/Dockerfile --progress plain \
		--tag $(REPONAME)/$(IMAGE_NAME):$(DOCKER_TAG) .

.PHONY: lint
lint:
	@echo "Running linters..."
	@$(GOPATH)/bin/golangci-lint -v --config .golangci.yml run && echo "Done."

.PHONY: install-ci
install-ci: install-tools

.PHONY: test-ci
test-ci: lint
