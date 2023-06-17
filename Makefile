.DEFAULT_GOAL := help

VERSION ?= local

CYAN := $$(tput setaf 6)
RESET := $$(tput sgr0)

.PHONY: help
help: ## Print this help
	@echo "Usage: make [target]"
	@grep -E '^[a-z.A-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST)  | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

.PHONY: test
test: mocks ## Run tests
	@ARGO_TIMEOUT=1 go test -v ./... -count=1 -coverprofile coverage.out `go list ./... | egrep -v '(test|mock)'`

.PHONY: ensure-dirs
ensure-dirs:
	@mkdir -p bin

.PHONY: build
build: ensure-dirs docs ## Build the binaries
	@echo "===> Building [$(CYAN)${VERSION}$(RESET)] version of [$(CYAN)argo-watcher$(RESET)] binary"
	@CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=${VERSION}" -o bin/argo-watcher ./cmd/argo-watcher
	@echo "===> Done"

.PHONY: build-ui
build-ui: ## Build the UI
	@echo "===> Building UI"
	@cd web && npm ci --silent && npm install react-scripts --silent && npm run build
	@echo "===> Done"

.PHONY: docs
docs: ## Generate swagger docs
	@echo "===> Generating swagger docs"
	@cd cmd/argo-watcher && swag init --parseDependency --parseInternal

.PHONY: mocks
mocks:
	@echo "===> Generating mocks"
# generate API mock
	@mockgen --source=cmd/argo-watcher/argo_api.go --destination=cmd/argo-watcher/mock/argo_api.go --package=mock
# generate State mock
	@mockgen --source=cmd/argo-watcher/state/state.go --destination=cmd/argo-watcher/mock/state.go --package=mock
# generate Metrics mock
	@mockgen --source=cmd/argo-watcher/metrics.go --destination=cmd/argo-watcher/mock/metrics.go --package=mock

.PHONY: bootstrap
bootstrap: ## Bootstrap docker compose setup
	@docker compose up -d

.PHONY: bootstrap-minimal
bootstrap-minimal: ## Bootstrap docker compose setup with mock and postgres only
	@docker compose up -d postgres mock

.PHONY: teardown
teardown: ## Teardown docker compose setup
	@docker compose down
