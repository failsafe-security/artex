# FailSafe ARTEX — upstream provenance

## Source

| Field | Value |
|---|---|
| Upstream project | `RuoJi6/ARTEX` |
| Upstream tag | `v0.3.15` |
| Tag commit | `a91b27feb7168c1486f827559bc48a589a920012` |
| Imported | 2026-11-10 |
| License | AGPL-3.0 (`LICENSE`) |
| Original author | Autumn-27 / ARTEX contributors |

`RuoJi6/ARTEX` is the best available source base after the original upstream
repository became unavailable. The `v0.3.15` tag was chosen because it predates
the `v0.3.16` prebuilt image that carried the `verifyCaptcha` implant, and its
Go sources match the tree that was audited on 2026-11-04/10.

The self-update constant currently assumes the public repository will be
`failsafe-security/artex`. If the final GitHub organization or repository name
differs, change it in `selfupdate/github.go` and `selfupdate/github_test.go`,
then rebuild and record the new binary hash. Do not make the update source
runtime-configurable: that would turn a writable setting into a code-execution
path.

## What is unchanged

All Go source files, `go.mod`, `go.sum`, `start.sh`, and `skills/` are
byte-identical to the audited `v0.3.15` tree, except for the deliberate
changes below. The web UI source is unchanged.

## What changed

1. **`selfupdate/github.go`** — `const Repo` changed from `RuoJi6/ARTEX` to
   `failsafe-security/artex`. This points the self-update release check at this
   fork instead of upstream. The Docker build embeds a `-failsafe.N` version
   suffix, which disables the one-click updater, so this is defense in depth.
2. **`selfupdate/github_test.go`** — test-only updates so the expected URL,
   error string, and fixture URLs match the new `Repo` constant. No production
   behavior changes.
3. **Distribution files removed** — the upstream `Dockerfile`,
   `docker-compose.yml`, `docker-compose.bench.yml`, `install.sh`, `update.sh`,
   `bench/`, `.env.example`, and `.github/workflows/release.yml` were removed
   so nobody accidentally pulls a prebuilt ARTEX image, publishes one from this
   fork, or runs an unaudited installer from this repository.
4. **`README.md`** — replaced with the FailSafe build/run instructions.
   Upstream's README is preserved as `README.upstream.md`.
5. **Added** — `Dockerfile.safe`, `docker-compose.safe.yml`, `.env.safe.example`,
   `SECURITY.md`, `UPSTREAM.md`, `Makefile`, `.dockerignore`, `.gitignore`, and
   this repository's audit reports under `audit/`.
6. **Runtime capability parity** — `Dockerfile.safe` installs the same browser
   stack as the upstream Dockerfile: Node 22, `@playwright/mcp`,
   `@playwright/cli`, Playwright, and Chromium. It runs as root like upstream so
   nmap raw scans and runtime tool installation work. The deployment differences
   are loopback-only console binding, an unpublished proxy, and
   `no-new-privileges`.
7. **`db/db.go`** — the seeded browser MCP launches the pinned `playwright-mcp`
   binary with `["--headless","--no-sandbox"]` instead of
   `npx @playwright/mcp`. This avoids a registry lookup, an accidental
   `@latest` download, and Chromium startup failures as root. The MCP is still
   seeded disabled and remains an admin-enabled feature.

No other application logic was changed.

## The incident this fork responds to

On 2026-11-03/04, public analysis found that prebuilt Docker images published
under `tomicdd/artex` (and copies under `congdonglinux/artex`) contained an
unauthenticated command-execution backdoor in `server/auth.go`:

- a hidden `server.verifyCaptcha` function called
  `exec.Command("sh", "-c", input)`;
- `authLogin` invoked it when the `captcha` JSON field began with a secret
  marker, before checking credentials;
- the command output was returned in the login response as `{"captcha": ...}`.

The GitHub source did not contain the implant; it was image-only. This fork
therefore **builds from source and never pulls an ARTEX image**. The exact base
images used by the build are pinned by digest in `Dockerfile.safe`.

See:

- [`audit/artex-v0.3.15-audit.md`](audit/artex-v0.3.15-audit.md) — this tag
- [`SECURITY.md`](SECURITY.md) — deployment guidance and residual risk

## Update policy

1. Do not merge upstream changes blindly. Fetch the tag, diff it against this
   tree, and review every change to `agent/`, `server/`, `cmd/`, `skills/`,
   `web/`, `start.sh`, `go.mod`, and `go.sum`.
2. Rebuild with `make build`, then run `make verify` to check the known
   implant indicators and record the new binary hash.
3. Update the audit report and the pinned base-image digests in
   `Dockerfile.safe` deliberately.
4. Never add a `docker-compose.yml` that references `tomicdd/artex`,
   `congdonglinux/artex`, or any other prebuilt ARTEX image.

## Upstream links

- Source: https://github.com/RuoJi6/ARTEX
- License: https://www.gnu.org/licenses/agpl-3.0.html