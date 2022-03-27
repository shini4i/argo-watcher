.PHONY: test migrate up down

up:
	@docker compose up &

down:
	@docker compose down

migrate:
	@dbmate up

test:
	@ARGO_URL=https://argocd.example.com ARGO_USER=test ARGO_PASSWORD=test pytest
