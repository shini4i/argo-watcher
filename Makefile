.PHONY: test
test:
	@RLOG_LOG_LEVEL=NONE ARGO_TIMEOUT=1 go test -v ./... -count=1

.PHONY: ensure-dirs
ensure-dirs:
	@mkdir -p bin

.PHONY: build
build:
	@CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/argo-watcher ./cmd/argo-watcher
	@CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/client ./cmd/client

.PHONY: docs
docs:
	@cd cmd/argo-watcher && swag init --parseDependency --parseInternal
