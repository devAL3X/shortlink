.PHONY: up down reset test demo

up:
	docker compose up -d --build

down:
	docker compose down

reset:
	docker compose down -v
	docker compose up -d --build

test:
	go vet ./...
	go test ./...

demo:
	./demo/run.sh
