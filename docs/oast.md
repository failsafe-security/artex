# Out-of-band callbacks (OAST)

ARTEX can mint callback tokens so an agent can confirm vulnerability classes that
only produce a side effect elsewhere: blind SSRF, XXE, blind SQL injection and
out-of-band command execution.

This is **phase 1**: an HTTP callback listener. DNS and SMTP listeners, external
(interactsh-compatible) providers and automatic finding promotion are not
implemented yet. An interaction is evidence the target made the request; the
agent still correlates it with the injected payload and records the finding.

## Enabling

The feature is off by default.

```bash
export ARTEX_OAST=1
export ARTEX_OAST_PUBLIC_URL=https://oast.example.com   # tunnel / public endpoint
export ARTEX_OAST_ZONE=oast.example.com                  # optional host-style tokens
export ARTEX_OAST_BIND=127.0.0.1:8088                    # optional; loopback only
```

| Variable | Default | Meaning |
| --- | --- | --- |
| `ARTEX_OAST` | off | `1`, `true`, `yes` or `on` enables the listener |
| `ARTEX_OAST_BIND` | `127.0.0.1:0` | listener address; a non-loopback host is refused |
| `ARTEX_OAST_PUBLIC_URL` | empty | absolute `http(s)` base URL of the operator's tunnel |
| `ARTEX_OAST_ZONE` | empty | bare DNS zone used to also return `token.zone` |

Without `ARTEX_OAST_PUBLIC_URL`, minted URLs point at the loopback listener and
are useful only for local testing — a target cannot reach your loopback address.
Expose the listener with a tunnel or port-forward you control; ARTEX never binds
a public interface itself.

When `ARTEX_OAST_ZONE` is set, tokens also come with a `callback_host` such as
`abc234def567.oast.example.com`. This does not start a DNS server: point the
wildcard `*.oast.example.com` at the same tunnel (or add an HTTP DNS listener in
a later phase).

## Agent workflow

1. `oast_token` returns a token, `callback_url` (and `callback_host` if a zone is
   configured). A token lives for 60 minutes by default, up to 24 hours.
2. Inject the URL or host into the payload under test.
3. `oast_poll` lists callbacks received for the token. Pass `next_after_id` from
   the previous poll to fetch only new ones.
4. Correlate the interaction with the payload, then record the fact or finding
   with the normal graph tools. `oast_poll` is read-only and does not promote
   anything by itself.

## Storage and safety

- Tokens and interactions are appended to the `oast_tokens` and
  `oast_interactions` tables; rows are never updated by the runtime.
- Unknown, expired and rate-limited callbacks receive a normal `200 ok` but are
  not persisted, so the tables stay free of unauthenticated noise.
- Request bodies are capped at 64 KiB; the tool returns at most 8 KiB per body
  in its result. Nothing is relayed anywhere.
- The listener enforces 200 callbacks/second globally and 20/second per token.
  On a loopback listener a local flood can still consume the global budget.
- Token access is capability-based: whoever holds the 60-bit random token can
  poll it. Tokens are bound to the task that minted them, and a tool call from a
  different task is rejected.

## Follow-ups

- DNS and SMTP listeners, authoritative-only and non-relaying.
- An `external` provider mode for deployments that cannot accept inbound traffic.
- Automatic promotion of a matched interaction to a verified finding with
  `poe_level=3`.