# Security policy

## Reporting a vulnerability

Do not open a public issue for a security problem. Report it privately:

- **Email:** security@failsafe.security (replace with the monitored address)
- **GitHub:** use the repository's private vulnerability reporting form

Include the affected version, a description, and a minimal reproduction. We
will acknowledge within 3 business days and coordinate disclosure.

For vulnerabilities in upstream ARTEX itself, also notify the upstream
maintainers. For the November 2026 image backdoor, see the indicators in
[`UPSTREAM.md`](UPSTREAM.md) and [`audit/artex-v0.3.15-audit.md`](audit/artex-v0.3.15-audit.md).

## Supported versions

Only the latest release built from this repository is supported. Upstream tags
that have not been reviewed and rebuilt here are not supported.

## Intended use

ARTEX is an autonomous security testing agent. It can execute shell commands,
run tools, and send arbitrary network traffic. It is intended only for systems
you own or have explicit written authorization to test. You are responsible for
scope, rules of engagement, and applicable law.

## Deployment guidance

- **Bind to localhost.** The default compose file publishes only
  `127.0.0.1:8787`. If you expose the console, put it behind an authenticated
  reverse proxy and TLS. Do not expose the recording proxy.
- **Use a fresh database and keys.** Do not reuse a database or `.env` from a
  host that ran a prebuilt ARTEX image.
- **Treat the console as admin-equivalent.** Custom Python tools and MCP stdio
  servers execute arbitrary code by design. A stolen admin session is code
  execution.
- **Expect root inside the container.** The image runs as root, matching
  upstream, so nmap raw scans and runtime tool installation work. Keep
  `no-new-privileges`, do not add `privileged: true`, and never mount the Docker
  socket. Treat the container as a high-value host if it is compromised.
- **Review skills.** `skills/` is copied into the image. A skill can instruct
  the agent to run commands. Review any skill you add or mount.
- **Keep secrets out of images.** The image contains no API keys, database
  passwords, or JWT keys. Pass them at runtime through `.env.safe`. The JWT
  signing key is generated on first boot and stored in the `artex_key` volume.
- **Limit egress.** The agent needs network access to the targets you
  authorize. Egress filtering is recommended for production use.

## Supply chain

- Base images are pinned by digest in `Dockerfile.safe`.
- The application is built from source in this repository; no prebuilt ARTEX
  image is pulled.
- Go module checksums are verified with `go mod verify`; there are no `replace`
  directives in `go.mod`.
- Node 22 is copied from the pinned `node:22-slim` stage. `@playwright/mcp`,
  `@playwright/cli`, and `playwright` are installed at pinned versions, and
  Chromium is installed with `playwright install --with-deps chromium`.
- Release artifacts should be published with SHA-256 checksums and, once
  available, a signed image and SBOM. Signing is not yet automated.

## Known residual risks

1. **Source provenance.** The `v0.3.15` source comes from a mirror of the
   original project because the original upstream is unavailable. The build is
   reproducible from this tree, but that proves the binary matches the tree,
   not that the tree matches the original author's last release.
2. **Dependency trust.** Third-party Go modules, apt packages, npm packages,
   and Chromium are verified by their ecosystems but not audited line by line.
   The browser stack is the largest third-party surface in the image.
3. **Root runtime and browser surface.** The container runs as root to preserve
   upstream capability, and Chromium parses untrusted web content. Do not run it
   on a host with sensitive data, and keep the console loopback-only.
4. **Online self-update is disabled.** It is compiled out in this fork by the
   `selfupdate.Enabled = false` constant, so the binary never checks or
   downloads a remote release: `/api/update/check` reports it disabled without
   any outbound request, and `/api/update/apply` and `/api/update/rollback`
   return 403. No setting or admin session can re-enable it; that requires
   editing the source, re-pointing the release feed, and re-auditing the
   download/verify path. (Previously the updater was only soft-disabled by the
   embedded version suffix, while `/api/update/check` still reached GitHub.)
5. **Browser sandboxing.** Chromium runs as root with `--no-sandbox` for
   reliable startup, so the browser's own sandbox is not a security boundary
   here. Rely on the container and host isolation instead.