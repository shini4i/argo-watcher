.PHONY: test up down

up:
	@docker compose up &

down:
	@docker compose down

test:
	@cd cmd && go test -v ./...
