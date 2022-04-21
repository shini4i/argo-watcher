.PHONY: test migrate up down run

up:
	@docker compose up &

down:
	@docker compose down

migrate:
	@dbmate up

run:
	@poetry run argo-watcher

test:
	@STATE_TYPE=in-memory ARGO_URL=https://argocd.example.com ARGO_USER=test ARGO_PASSWORD=test pytest
