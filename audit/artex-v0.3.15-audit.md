# ARTEX v0.3.15 — FailSafe Build Audit

**Date:** 2026-11-10
**Scope:** the `v0.3.15` source tag imported from `RuoJi6/ARTEX`, the
`Dockerfile.safe` build, and the resulting `failsafe-security/artex:v0.3.15-safe`
image on `linux/arm64`.
**Question:** does this source build contain the known `tomicdd` implant, and is
it a safe base for a public FailSafe distribution?

## Verdict

**The known implant is absent and the build is reproducible from this source
tree.** The tag's application source is byte-identical to the tree audited on
2026-11-04/10, except for one deliberate self-update constant. The image builds
from source, runs as UID 10001 with no capabilities, binds the console to
localhost in the supplied compose file, and returns `401` to a login request
that carries the old backdoor's `captcha` field.

This is a technical audit of a specific tag and build, not a formal proof that
the upstream project is free of every defect. Residual risks are listed in §7.

## 1. Artifacts

### Source

| Field | Value |
|---|---|
| Repository | `RuoJi6/ARTEX` |
| Tag | `v0.3.15` |
| Tag commit | `a91b27feb7168c1486f827559bc48a589a920012` |
| Imported | 2026-11-10 |
| License | AGPL-3.0 |

### API-only source build (used for source comparison)

Built on 2026-11-10 with Go 1.26.10, `CGO_ENABLED=0`, no embedded UI:

```text
772df1e35767bc236ee8c2e68ba4d43bf0bc3d0e5a28b830730a5a1fbd89c1b3  artex-v0.3.15
```

### Safe image build (this distribution)

Built from `Dockerfile.safe` on 2026-11-10, `linux/arm64`, with the embedded
web UI:

| Artifact | Value |
|---|---|
| Image | `failsafe-security/artex:v0.3.15-safe` |
| Image ID | `sha256:15268d9255a8e4ef3fb1897efa5eeb4f7a856d484dfbfb71decd0235d1e219dc` |
| `/app/artex` SHA-256 | `f668ea7422d0032ba74a3d24a8202ac8ec76f48839d3def942fe331ca0ad4d08` |
| Binary size | 48,721,022 bytes |
| Version string | `v0.3.15-failsafe.1` |
| `start.sh` SHA-256 | `60a83fa328b156f3cb7874950b638862c0719c1f9d6e5915b1181e4daebbce6c` |
| `skills/` tree SHA-256 | `d38ef6002b4b2782f3de2efb9bde7092b512513f4cbc905c584a9f0dd97b1cfc` |

Hashes are architecture-specific. An `amd64` build will produce different
binary and image hashes; rerun `make verify` and record them before publishing
that build. The binary SHA-256 is the stable artifact; the image ID depends on
the builder and is metadata, not a reproducibility check.

### Base images

Pinned by digest in `Dockerfile.safe`:

| Stage | Image |
|---|---|
| web builder | `node:22-slim@sha256:83f487e0a63425e5b4d146fb5e5be574bcbe1b7b843d3ebafdd95eaf7767a7e5` |
| Go builder | `golang:1.26.10@sha256:cfa27959837144bf4649802ba38e59daffd0dd2a7994aa83daaf113285b0f551` |
| runtime | `python:3.12-slim-bookworm@sha256:e697c81897302a4c4333bade000d98038fbb21eb064f8ee1fd42d02ebcd9d337` |
| database | `postgres:16-alpine@sha256:0cefaabd882e42bcb2a2f3dc56d3ce4770560ba62fd7f197f1a2e008fd5731ab` |

## 2. Source provenance

The scaffold was compared file-by-file against the extracted audited tag:

- `agent/`, `cmd/`, `config/`, `db/`, `enrich/`, `evidence/`, `guard/`,
  `intercept/`, `llmpool/`, `llmrec/`, `mcphttp/`, `notify/`, `report/`,
  `sidequestion/`, `skills/`, `traffic/`, and `web/` are **identical**.
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
  `install.sh`, `update.sh`, and `bench/` were removed. They are distribution
  files, not application logic; the upstream image is the compromised artifact.
- `README.md` was replaced and `README.upstream.md` preserves the original.

No Go application logic, web source, skill, or startup script was changed.

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
   `curl`, `dig`, `whois`, `telnet`, `netcat`, `jq`, `binutils`, etc.), creates
   UID 10001 `artex`, copies the binary, `start.sh`, and `skills/`, and runs as
   that user.

No layer downloads or executes a prebuilt ARTEX binary. There is no
`curl | bash`, no build-time secret, and no Docker socket.

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

There is no exec caller in `authLogin`, `main`, an `init` function, or an
unauthenticated handler.

### Network

No hard-coded C2, beacon, or exfiltration endpoint. The fixed hosts are the
expected GitHub update endpoint, LLM providers, search backends, and Telegram
notifications. LLM, search, proxy, MCP, and notification endpoints are
user-configurable. The app listens on `:8787` (API/UI) and `:8788` (recording
proxy); the compose file publishes only `8787` on loopback.

## 5. Dynamic checks

The compose stack was started with a fresh PostgreSQL volume and a random
database password.

| Check | Result |
|---|---|
| `GET /api/health` | `200 {"ok":true,"service":"artex","version":"v0.3.15-failsafe.1"}` |
| `GET /api/auth/status` | `{"initialized":false}` on a fresh database |
| Runtime user | `uid=10001(artex) gid=10001(artex)` |
| Effective capabilities | `CapEff: 0000000000000000` |
| Published ports | `127.0.0.1:8787->8787/tcp`; `8788` internal only |
| Container health | `healthy` |
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

## 6. Hardening differences

| Control | Default upstream | This build |
|---|---|---|
| Image source | prebuilt `tomicdd/artex` | built from this source |
| Runtime user | root | UID 10001 |
| Console | `0.0.0.0:8787` | `127.0.0.1:8787` |
| Recording proxy | published | internal only |
| Capabilities | default set | `cap_drop: ALL` |
| Privilege escalation | default | `no-new-privileges:true` |
| Browser automation | Playwright/Chromium | omitted |
| JWT key | `/app/jwt.key` (lost on recreate) | symlinked to persisted `artex_key` volume |
| Self-update | upstream | this fork; one-click disabled |
| Base images | floating tags | digest-pinned |

## 7. Residual risks

1. **Source provenance.** `RuoJi6/ARTEX` is a mirror because the original
   upstream repository is unavailable. The build is reproducible from this
   tree, but that proves the binary matches the tree, not that the tree is
   byte-identical to the original author's last release. Preserve the tag
   commit and re-diff on every update.
2. **Dependency trust.** Go modules are checked against `go.sum` with
   `go mod verify`; apt packages come from Debian. Neither was audited line by
   line. The author's `norma` module was reviewed for execution and network
   paths in the native audit.
3. **Feature loss.** Omitting Playwright/Chromium removes browser-driven
   features. This is deliberate. If browser automation is required, add it in a
   separate, reviewed builder stage and re-audit.
4. **Scan limits.** With `cap_drop: ALL`, `nmap` cannot use raw sockets, so only
   TCP connect scans work. Add `NET_RAW` only with explicit authorization.
5. **By-design code execution.** Custom Python tools and MCP stdio servers run
   arbitrary code. They are admin-configured and authenticated; a stolen admin
   session is equivalent to code execution.
6. **Data sensitivity.** Scan evidence can contain credentials and customer
   data. Protect the `artex_data` and `pgdata` volumes, and do not back them up
   to untrusted storage.
7. **No signing yet.** The image is not signed and has no SBOM or SLSA
   provenance. Publish checksums with releases and add `cosign`/SBOM generation
   before distributing images broadly.
8. **Egress.** The agent needs outbound access to authorized targets. Use
   network policy or an egress proxy in production.

## 8. Reproduce

```bash
cp .env.safe.example .env.safe
chmod 600 .env.safe
# set a unique POSTGRES_PASSWORD

docker compose --env-file .env.safe -f docker-compose.safe.yml up -d --build
make verify
```

`make verify` prints the binary hash, checks the three known implant markers,
shows the runtime user, and prints the image ID. Compare the results with the
artifacts in §1 before publishing.

## 9. Recommendation

Use this tag and this Dockerfile as the base for the FailSafe distribution,
subject to the residual risks above. Do not publish or recommend any prebuilt
ARTEX image. Before a public release:

- add image signing and an SBOM;
- run the same audit on the `amd64` build;
- add a provenance note to the companion benchmark post and rerun any ARTEX
  benchmark that used the backdoored image;
- keep the `verify` target in CI so a future upstream merge cannot silently
  reintroduce the implant.

The known ARTEX backdoor is **not** in this build.