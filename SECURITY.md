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
- **Run unprivileged.** The image runs as UID 10001 with `cap_drop: ALL` and
  `no-new-privileges`. Do not add `privileged: true` or mount the Docker socket.
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
- Release artifacts should be published with SHA-256 checksums and, once
  available, a signed image and SBOM. Signing is not yet automated.

## Known residual risks

1. **Source provenance.** The `v0.3.15` source comes from a mirror of the
   original project because the original upstream is unavailable. The build is
   reproducible from this tree, but that proves the binary matches the tree,
   not that the tree matches the original author's last release.
2. **Dependency trust.** Third-party Go modules and apt packages are verified
   by their ecosystems but not audited line by line.
3. **Browser automation omitted.** Some ARTEX features that need Playwright or
   Chromium are unavailable. This is a deliberate reduction in attack surface.
4. **No signature verification on self-update.** One-click update is disabled
   by the embedded version suffix. Do not re-enable it against an untrusted
   feed.
5. **Raw-socket scans.** Without `CAP_NET_RAW`, `nmap` can do connect scans
   only. Add the capability only when authorized.