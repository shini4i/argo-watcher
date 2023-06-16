.DEFAULT_GOAL := help

.PHONY: help
help: ## Print this help
	@echo "Usage: make [target]"
	@grep -E '^[a-z.A-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST)  | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

.PHONY: test
test: ## Run tests
	@ARGO_TIMEOUT=1 go test -v ./... -count=1 -coverprofile coverage.out `go list ./... | egrep -v '(test|mocks)'`

.PHONY: ensure-dirs
ensure-dirs:
	@mkdir -p bin

.PHONY: build
build: ensure-dirs ## Build the binaries
	@CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/argo-watcher ./cmd/argo-watcher
	@CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/client ./cmd/client

.PHONY: docs
docs: ## Generate swagger docs
	@cd cmd/argo-watcher && swag init --parseDependency --parseInternal

.PHONY: mocks
mocks:
# generate API mock
	@mockgen --source=cmd/argo-watcher/argo_api.go --destination=cmd/argo-watcher/mock/argo_api.go --package=mock
# generate State mock
	@mockgen --source=cmd/argo-watcher/state/state.go --destination=cmd/argo-watcher/mock/state.go --package=mock
# generate Metrics mock
	@mockgen --source=cmd/argo-watcher/metrics.go --destination=cmd/argo-watcher/mock/metrics.go --package=mock
