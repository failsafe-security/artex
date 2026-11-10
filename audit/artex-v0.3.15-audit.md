# ARTEX v0.3.15 — FailSafe Build Audit

**Date:** 2026-11-10
**Scope:** the `v0.3.15` source tag imported from `RuoJi6/ARTEX`, the
`Dockerfile.safe` build, and the resulting `failsafe-security/artex:v0.3.15-safe`
image on `linux/arm64`.
**Question:** does this source build contain the known `tomicdd` implant, and
does it preserve upstream's runtime capability?

## Verdict

**The known implant is absent, the build is reproducible from this source tree,
and the runtime keeps upstream's capability.** The image builds from source and
ships the same browser stack as upstream: Node 22, `@playwright/mcp`,
`@playwright/cli`, Playwright, and Chromium. It runs as root like upstream, so
nmap raw scans, browser automation, and runtime tool installation work. The
browser MCP discovered 25 tools, Puppeteer drove the bundled Chromium, and a
login request carrying the old backdoor's `captcha` field returned `401`.

The only application changes are the self-update repository constant (plus its
test) and the seeded browser MCP launcher, which uses the pinned
`playwright-mcp` binary instead of `npx`. This is a technical audit of a specific
tag and build, not a formal proof that the upstream project is free of every
defect. Residual risks are listed in §7.

## 1. Artifacts

### Source

| Field | Value |
|---|---|
| Repository | `RuoJi6/ARTEX` |
| Tag | `v0.3.15` |
| Tag commit | `a91b27feb7168c1486f827559bc48a589a920012` |
| Imported | 2026-11-10 |
| License | AGPL-3.0 |

### API-only source build (current tree)

Built on 2026-11-10 with Go 1.26.10, `CGO_ENABLED=0`, no embedded UI:

```text
3d66a628db6fc12c5992bea5048d7fffc5bebbaeb26e756715fd6bebacbc0a96  artex-v0.3.15
```

### Full image build (this distribution)

Built from `Dockerfile.safe` on 2026-11-10, `linux/arm64`, with the embedded web
UI and browser stack:

| Artifact | Value |
|---|---|
| Image | `failsafe-security/artex:v0.3.15-safe` |
| Image ID | `sha256:f52085bdfd39d07567cd91cc21dbc45494466f9f61aa09d16e7d28bf68ebade8` |
| Image size | 2.34 GB |
| `/app/artex` SHA-256 | `664a6e62df6d10f6ab5604e15935250505f5ca37ed333a0cbad73b7de27bb8f0` |
| Binary size | 48,721,246 bytes |
| Version string | `v0.3.15-failsafe.1` |
| `start.sh` SHA-256 | `60a83fa328b156f3cb7874950b638862c0719c1f9d6e5915b1181e4daebbce6c` |
| `skills/` tree SHA-256 | `d38ef6002b4b2782f3de2efb9bde7092b512513f4cbc905c584a9f0dd97b1cfc` |

### Runtime stack

| Component | Version |
|---|---|
| Node.js | `v22.23.2` |
| npm | `10.9.8` |
| `@playwright/mcp` | `0.0.85` |
| `@playwright/cli` | `0.1.24` |
| `playwright` | `1.65.0` |
| Chromium (Google Chrome for Testing) | `158.0.8127.18` |
| Playwright Chromium revision | `1254` |

Hashes and the image ID are architecture-specific. An `amd64` build will produce
different values; rerun `make verify` and record them before publishing that
build. The binary SHA-256 is the stable artifact; the image ID depends on the
builder and is metadata, not a reproducibility check.

### Base images

Pinned by digest in `Dockerfile.safe`:

| Stage | Image |
|---|---|
| web builder / Node source | `node:22-slim@sha256:83f487e0a63425e5b4d146fb5e5be574bcbe1b7b843d3ebafdd95eaf7767a7e5` |
| Go builder | `golang:1.26.10@sha256:cfa27959837144bf4649802ba38e59daffd0dd2a7994aa83daaf113285b0f551` |
| runtime | `python:3.12-slim-bookworm@sha256:e697c81897302a4c4333bade000d98038fbb21eb064f8ee1fd42d02ebcd9d337` |
| database | `postgres:16-alpine@sha256:0cefaabd882e42bcb2a2f3dc56d3ce4770560ba62fd7f197f1a2e008fd5731ab` |

## 2. Source provenance

The scaffold was compared file-by-file against the extracted audited tag:

- `agent/`, `cmd/`, `config/`, `enrich/`, `evidence/`, `guard/`, `intercept/`,
  `llmpool/`, `llmrec/`, `mcphttp/`, `notify/`, `report/`, `sidequestion/`,
  `skills/`, `traffic/`, and `web/` are **identical**.
- `db/db.go` changes the seeded browser MCP launcher:

  ```diff
  -VALUES ('browser', 'stdio', 'npx', $1, '{}', false)
  +VALUES ('browser', 'stdio', 'playwright-mcp', $1, '{}', false)
  ```

  The arguments change from `["@playwright/mcp","--headless"]` to
  `["--headless","--no-sandbox"]`. This uses the pinned global binary, avoids an
  `npx` registry lookup or an accidental `@latest` download, and makes Chromium
  launch reliably as root. An idempotent update migrates only the exact upstream
  default row from `npx` to the pinned binary; user-edited MCP rows are left
  alone. The MCP is still seeded disabled and remains admin-enabled.
- `selfupdate/github.go` differs by exactly one line:

  ```diff
  -const Repo = "RuoJi6/ARTEX"
  +const Repo = "failsafe-security/artex"
  ```

  This repoints the update check at this fork. The Docker build also embeds a
  `-failsafe.N` version suffix, which disables the one-click updater.
- `selfupdate/github_test.go` has test-only updates: its expected URL, error
  string, and fixture URLs now use `failsafe-security/artex`. No production
  behavior changes.
- The upstream `Dockerfile`, `docker-compose.yml`, `docker-compose.bench.yml`,
  `install.sh`, `update.sh`, `bench/`, `.env.example`, and
  `.github/workflows/release.yml` were removed. They are distribution files, not
  application logic; the upstream image is the compromised artifact.
- `README.md` was replaced and `README.upstream.md` preserves the original.

No other Go application logic, web source, skill, or startup script was changed.

## 3. Build

`Dockerfile.safe` has three stages:

1. **`node:22-slim`** — `npm ci --ignore-scripts`, then `npm run build:static`.
   The `--ignore-scripts` flag prevents lifecycle scripts such as husky's
   `prepare` hook from running.
2. **`golang:1.26.10`** — `go mod download && go mod verify`, copy the source and
   the built web assets into `server/webui/dist`, then:

   ```bash
   CGO_ENABLED=0 go build \
     -trimpath -buildvcs=false -tags embedui \
     -ldflags="-buildid= -X main.version=v0.3.15-failsafe.1" \
     -o /out/artex ./cmd/artex
   ```

   Symbols are retained for auditability. `-buildvcs=false` avoids embedding
   host-specific VCS metadata. `-buildid=` makes the build reproducible.
3. **`python:3.12-slim-bookworm`** — installs recon tools (`ripgrep`, `nmap`,
   `curl`, `dig`, `whois`, `telnet`, `netcat`, `jq`, `binutils`, etc.), copies
   Node 22 and npm from the pinned builder stage, installs pinned Playwright
   packages, installs Chromium with `playwright install --with-deps chromium`,
   and symlinks `/usr/bin/chromium` to the installed browser. It then copies the
   binary, `start.sh`, and `skills/`, writes a Playwright CLI config that defaults
   to Chromium, and runs as root.

No layer downloads or executes a prebuilt ARTEX binary. There is no
`curl | bash`, no build-time secret, and no Docker socket. Node is copied from
the pinned Node stage instead of using NodeSource's install script.

## 4. Static checks

### Known implant indicators

```text
$ docker run --rm --network none --entrypoint /bin/sh \
    failsafe-security/artex:v0.3.15-safe -c \
    'for m in verifyCaptcha tomicdd congdonglinux; do
       strings /app/artex | grep -q "$m" && echo "FOUND: $m" || echo "absent: $m";
     done'
absent: verifyCaptcha
absent: tomicdd
absent: congdonglinux
```

Source checks:

```text
$ rg -n 'verifyCaptcha' .            # no matches outside documentation
$ rg -n 'Captcha|captcha' server/    # no matches
```

The only `captcha` string in the native binary audit was the guard regex in
`guard/guard.go`, which classifies blocked responses; it is not an execution
path.

### Command execution

The binary's `os/exec` callers are the same expected paths as the native audit:

- `selfupdate.smokeTest` — runs a staged binary with `-h` after a checksummed
  download; one-click update is disabled by the version suffix.
- `server.execPython` — authenticated custom-tool runner.
- `norma` MCP/PTY/task/shell/ripgrep tooling — the agent's core capability.
- `norma` ripgrep installation — suppressed at runtime with
  `NORMA_RIPGREP_NO_INSTALL=1`; the image installs `ripgrep` via apt.

The browser MCP is an admin-configured MCP server seeded disabled. Enabling it
starts `playwright-mcp --headless --no-sandbox`; this is the same by-design MCP execution
path as any other stdio MCP. There is no exec caller in `authLogin`, `main`, an
`init` function, or an unauthenticated handler.

### Network

No hard-coded C2, beacon, or exfiltration endpoint. The fixed hosts are the
expected GitHub update endpoint, LLM providers, search backends, and Telegram
notifications. LLM, search, proxy, MCP, browser, and notification endpoints are
user-configurable. The app listens on `:8787` (API/UI) and `:8788` (recording
proxy); the compose file publishes only `8787` on loopback. The browser adds
outbound traffic to the targets the operator authorizes; the optional recording
proxy routes browser traffic through the local MITM when capture is enabled.

## 5. Dynamic checks

The compose stack was started with a fresh PostgreSQL volume and a random
database password.

| Check | Result |
|---|---|
| `GET /api/health` | `200 {"ok":true,"service":"artex","version":"v0.3.15-failsafe.1"}` |
| `GET /api/auth/status` | `{"initialized":false}` on a fresh database |
| Runtime user | `uid=0(root) gid=0(root)` |
| Effective capabilities | default Docker set (`CapEff: 00000000a80425fb`) |
| Published ports | `127.0.0.1:8787->8787/tcp`; `8788` internal only |
| Container health | `healthy` |
| Node / Playwright MCP / CLI | `v22.23.2`, `0.0.85`, `0.1.24` |
| Chromium launch | `browser: ok` (headless, one page) |
| Browser MCP discovery | `[mcp] browser 发现 25 个工具并已缓存` |
| Puppeteer + bundled Chromium | `puppeteer: ok` |
| `nmap -sS` as root | raw SYN scan completed |
| `ping` | ICMP echo worked |
| Login with backdoor-shaped payload | `401 Unauthorized`, no command output |
| JWT key persistence | same key after `down`/`up`; key stored in `artex_key` volume |

The login probe was:

```bash
curl -sS -i -X POST http://127.0.0.1:8787/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"nobody","password":"wrong","captcha":"!; id"}'
```

Response:

```http
HTTP/1.1 401 Unauthorized
Content-Type: application/json

{"error":"用户名或密码错误"}
```

There was no `uid=`/`gid=` output and no `captcha` response field. The handler
ignores the unknown field, as the source shows.

## 6. Capability and hardening differences

| Control | Default upstream | This build |
|---|---|---|
| Image source | prebuilt `tomicdd/artex` | built from this source |
| Runtime user | root | root (parity) |
| Console | `0.0.0.0:8787` | `127.0.0.1:8787` |
| Recording proxy | published | internal only |
| Capabilities | default set | default set |
| Privilege escalation | default | `no-new-privileges:true` |
| Browser automation | Node + Playwright + Chromium | same, pinned versions |
| Browser MCP launcher | `npx @playwright/mcp` | pinned `playwright-mcp --headless --no-sandbox` |
| nmap raw scans | available | available |
| JWT key | `/app/jwt.key` (lost on recreate) | symlinked to persisted `artex_key` volume |
| Self-update | upstream | this fork; one-click disabled |
| Base images | floating tags | digest-pinned |

This build is intentionally capability-equivalent to upstream. It is not a
hardened sandbox: running as root and shipping Chromium are deliberate choices
to preserve the full ARTEX feature set. The remaining deployment controls are
loopback-only console binding, an unpublished proxy, `no-new-privileges`,
persisted JWT key, and pinned base images.

## 7. Residual risks

1. **Source provenance.** `RuoJi6/ARTEX` is a mirror because the original
   upstream repository is unavailable. The build is reproducible from this
   tree, but that proves the binary matches the tree, not that the tree is
   byte-identical to the original author's last release. Preserve the tag
   commit and re-diff on every update.
2. **Dependency trust.** Go modules are checked against `go.sum` with
   `go mod verify`; apt packages come from Debian; npm packages and Chromium come
   from their registries/CDN. None were audited line by line. The browser stack
   is the largest third-party surface in the image.
3. **Root runtime and browser surface.** The container runs as root to preserve
   upstream capability, and Chromium parses untrusted web content. Do not run it
   on a host with sensitive data, and keep the console loopback-only.
4. **Browser sandboxing.** Chromium runs as root with `--no-sandbox` for
   reliable startup, so the browser's own sandbox is not a security boundary
   here. Rely on the container and host isolation instead.
5. **By-design code execution.** Custom Python tools and MCP stdio servers run
   arbitrary code. They are admin-configured and authenticated; a stolen admin
   session is equivalent to code execution.
6. **Data sensitivity.** Scan evidence can contain credentials and customer
   data. Protect the `artex_data` and `pgdata` volumes, and do not back them up
   to untrusted storage.
7. **No signing yet.** The image is not signed and has no SBOM or SLSA
   provenance. Publish checksums with releases and add `cosign`/SBOM generation
   before distributing images broadly.
8. **Egress.** The agent and browser need outbound access to authorized targets.
   Use network policy or an egress proxy in production.
9. **Self-update.** The one-click updater is disabled by the version suffix.
   Do not re-enable it against an untrusted feed.

## 8. Reproduce

```bash
cp .env.safe.example .env.safe
chmod 600 .env.safe
# set a unique POSTGRES_PASSWORD

docker compose --env-file .env.safe -f docker-compose.safe.yml up -d --build
make verify
```

`make verify` prints the binary hash, checks the three known implant markers,
shows the runtime user, runs the browser stack, performs a raw SYN scan, and
prints the image ID. Compare the results with the artifacts in §1 before
publishing.

## 9. Recommendation

This build restores upstream capability while removing the known image implant.
Before distributing release images:

- add image signing and an SBOM;
- run the same audit on the `amd64` build;
- add a provenance note to the companion benchmark post and rerun any ARTEX
  benchmark that used the backdoored image;
- keep the `verify` target in CI so a future upstream merge cannot silently
  reintroduce the implant or drop the browser stack.

The known ARTEX backdoor is **not** in this build.