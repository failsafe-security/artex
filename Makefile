IMAGE ?= failsafe-security/artex:v0.3.15-safe
COMPOSE_BIN ?= $(shell if docker compose version >/dev/null 2>&1; then echo "docker compose"; else echo "docker-compose"; fi)
COMPOSE ?= $(COMPOSE_BIN) --env-file .env.safe -f docker-compose.safe.yml

.PHONY: build up down logs ps verify shell test clean

build:
	docker build -f Dockerfile.safe -t $(IMAGE) .

up:
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f artex

ps:
	$(COMPOSE) ps

shell:
	docker run --rm -it --network none --entrypoint /bin/sh $(IMAGE)

verify:
	docker run --rm --network none --entrypoint /bin/sh $(IMAGE) -c '\
	  sha256sum /app/artex; \
	  for m in verifyCaptcha tomicdd congdonglinux; do \
	    if strings /app/artex | grep -q "$$m"; then echo "FOUND: $$m"; exit 1; \
	    else echo "absent: $$m"; fi; \
	  done; \
	  id; \
	  ls -l /app/artex /app/start.sh'
	docker image inspect $(IMAGE) --format 'image id: {{.Id}}'

test:
	go test ./...

clean:
	$(COMPOSE) down -v