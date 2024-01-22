.DEFAULT_GOAL := help

VERSION ?= local

CYAN := $$(tput setaf 6)
RESET := $$(tput sgr0)

.PHONY: help
help: ## Print this help
	@echo "Usage: make [target]"
	@grep -E '^[a-z.A-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST)  | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

.PHONY: install-deps
install-deps: ## Install dependencies
	@echo "===> Installing dependencies"
	@go install github.com/swaggo/swag/cmd/swag@latest
	@go install go.uber.org/mock/mockgen@latest
	@echo "===> Done"

.PHONY: test
test: mocks ## Run tests
	@ARGO_TIMEOUT=1 go test -v ./... -count=1 -coverprofile coverage.out `go list ./... | egrep -v '(test|mock)'`

.PHONY: build
build: docs ## Build the binaries
	@echo "===> Building [$(CYAN)${VERSION}$(RESET)] version of [$(CYAN)argo-watcher$(RESET)] binary"
	@CGO_ENABLED=0 go build -ldflags="-s -w -X server/router.version=${VERSION}" -o argo-watcher ./cmd/argo-watcher
	@echo "===> Done"

.PHONY: kind-upload
kind-upload:
	@echo "===> Building [$(CYAN)dev$(RESET)] version of [$(CYAN)argo-watcher$(RESET)] binary"
	@CGO_ENABLED=0 GOARCH=arm64 GOOS=linux go build -ldflags="-s -w -X server/router.version=dev" -o argo-watcher ./cmd/argo-watcher
	@echo "===> Building web UI"
	@cd web && npm run build
	@echo "===> Building [$(CYAN)argo-watcher$(RESET)] docker image"
	@docker build -t ghcr.io/shini4i/argo-watcher:dev .
	@echo "===> Loading [$(CYAN)argo-watcher$(RESET)] docker image into [$(CYAN)kind$(RESET)] cluster"
	@kind load docker-image ghcr.io/shini4i/argo-watcher:dev -n disposable-cluster
	@echo "===> Restarting [$(CYAN)argo-watcher$(RESET)] deployment"
	@kubectl rollout restart deploy argo-watcher -n argo-watcher

.PHONY: build-goreleaser
build-goreleaser:
	@echo "===> Building [$(CYAN)${VERSION}$(RESET)] version of [$(CYAN)argo-watcher$(RESET)] binary"
	@goreleaser build --snapshot --clean --single-target
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
	@mockgen --source=cmd/argo-watcher/argocd/argo_api.go --destination=cmd/argo-watcher/mock/argo_api.go --package=mock
	@mockgen --source=cmd/argo-watcher/state/state.go --destination=cmd/argo-watcher/mock/state.go --package=mock
	@mockgen --source=cmd/argo-watcher/prometheus/metrics.go --destination=cmd/argo-watcher/mock/metrics.go --package=mock
	@mockgen --source=pkg/updater/interfaces.go --destination=pkg/updater/mock/interfaces.go --package=mock

.PHONY: bootstrap
bootstrap: ## Bootstrap docker compose setup
	@docker compose up -d

.PHONY: bootstrap-minimal
bootstrap-minimal: ## Bootstrap docker compose setup with mock and postgres only
	@docker compose up -d postgres mock

.PHONY: teardown
teardown: ## Teardown docker compose setup
	@docker compose down
