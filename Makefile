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
	docker run --rm --network none --shm-size=1g --entrypoint /bin/sh $(IMAGE) -c '\
	  sha256sum /app/artex; \
	  for m in verifyCaptcha tomicdd congdonglinux; do \
	    if strings /app/artex | grep -q "$$m"; then echo "FOUND: $$m"; exit 1; \
	    else echo "absent: $$m"; fi; \
	  done; \
	  id; \
	  node --version; \
	  playwright-mcp --version; \
	  playwright-cli --version; \
	  node -e "const {chromium}=require(\"/usr/local/lib/node_modules/@playwright/mcp/node_modules/playwright\"); (async()=>{const b=await chromium.launch({headless:true,args:[\"--no-sandbox\"]});const p=await b.newPage();await p.setContent(\"<title>ok</title>\");if(await p.title()!==\"ok\")process.exit(1);await b.close();console.log(\"browser: ok\")})().catch(e=>{console.error(e);process.exit(1)})"; \
	  nmap -sS -p 8787 127.0.0.1 | tail -3; \
	  ls -l /app/artex /app/start.sh /usr/bin/chromium'
	docker image inspect $(IMAGE) --format 'image id: {{.Id}}'

test:
	go test ./...

clean:
	$(COMPOSE) down -v