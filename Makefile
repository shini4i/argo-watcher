.PHONY: test migrate up down

up:
	@docker compose up &

down:
	@docker compose down

migrate:
	@dbmate up

test:
	@HISTORY_TTL=1 pytest
