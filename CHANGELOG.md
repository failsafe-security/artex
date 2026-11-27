# Changelog

All notable changes to this project are documented in this file. The format is based on [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/).

## [Unreleased]

### Added

- **Phase 1 of the out-of-band callback fabric (`oast/`)**: when `ARTEX_OAST=1`, ARTEX starts a loopback HTTP listener, mints per-task callback tokens and exposes `oast_token` / `oast_poll` to workers. Callbacks are appended to `oast_tokens` / `oast_interactions`, unknown or expired tokens are not persisted, bodies are capped at 64 KiB and the listener enforces global/per-token rate limits. DNS and SMTP listeners, external providers and automatic finding promotion remain follow-ups. See `docs/oast.md`.

### Deployment and release maintenance (application version stays at 0.3.15)

- Migrated to the standalone `RuoJi6/ARTEX` repository, preserving the original commit history and branches.
- The in-app online update source is now `RuoJi6/ARTEX`; GitHub Actions builds packages for five platforms plus SHA256 checksum files.
- Docker Hub images are built on native AMD64 and ARM64 runners, with program startup, database connectivity and the embedded frontend verified.
- Once both architectures pass verification, the `v0.3.15` and `latest` images and the matching packages are published.
- Compose and the deployment docs use this repository's images, and now describe how to migrate an existing deployment while keeping its data.

## [0.3.15] - 2026-10-07

### Intercept

#### Added

- **New built-in intercept rule "Delete-style endpoint paths"**: the built-in HTTP destructive rule previously matched only the DELETE **method**, yet most applications' delete endpoints can be triggered with GET/POST, so a call like `curl 'http://t/api/user/delete?id=1'` would really delete target data. A new `deny` rule now covers `/delete /del /remove /unlink /erase /destroy` (the verb must be followed by a separator, so `/delivery`, `/details` and `/delta` are not falsely intercepted). Existing instances get it on upgrade too; it can be disabled or deleted under "System → Command Intercept".
- **Token usage of the model fallback approval can be viewed separately**: approval calls previously had no metering attribution of their own, so their cost could only be guessed. They are now metered separately, and a usage card sits below the "Model fallback approval" toggle under "System → Command Intercept" - cumulative stats for calls, input, output, cache reads and cache writes, plus a mini bar chart of daily usage over the last 30 days.

### Finding notifications

#### Added

- **Findings can be pushed to IM**: a new "System → Notifications" page pushes scan findings to six kinds of channel: **DingTalk, Feishu, WeCom, generic Webhook, Telegram and email**. Any number of instances can be configured per type, each with its own enable switch, rate limit and filter rules.
- **Two push timing modes, real-time and digest**: real-time mode sends each finding as soon as it is hit; digest mode merges them into one message per global period (30 minutes by default), opening with the number of new findings and their severity distribution. For "high severity in real time, the rest as a digest", create two channels and configure them separately.
- **Filter rules cover four dimensions**: minimum severity, restriction to a task / asset scope, finding-type keyword include and exclude (exclude wins), and whether to receive triage status changes (off by default). Messages carry a "View details" button whose target is set by the global "Backlink URL" setting; leave it empty to omit the button.
- **Delivery history and manual resend**: lists each delivery's status, attempt count, failure reason and channel, filterable by channel and status; failed items can be resent with one click (resending resets the retry count).
- **Channel credentials are echoed masked**: Webhook URLs, signing secrets, Bot Tokens, SMTP passwords and the like are echoed only as masked values with a trailing-characters hint; submitting the value back unchanged means "do not modify", while clearing the input deletes the field.

#### Fixed

> This section is the outcome of a security audit carried out after the first version of the feature was finished; every item was reproduced first, then fixed, then covered by a regression test.

- **Fixed the mask being bypassable by "changing the target URL while keeping the credentials" (critical)**: config merging kept the stored value for every "unmentioned key", so changing only the URL and saying nothing about the credentials made the server send the real stored credentials to an arbitrary address, completely silently. Now, once the target URL changes, every credential field must be stated explicitly - either a new value, or explicitly empty to indicate it is no longer needed.
- **Fixed markdown-based channels applying no escaping to untrusted content**: finding titles and summaries come from model output and asset URLs come from scan results, so a crafted title would render as a clickable external link in DingTalk / WeCom / Feishu, and image syntax would be fetched by the client at render time, leaking the reader's IP. Content is now uniformly flattened to a single line with markdown metacharacters escaped, and the escaping happens at each channel's render exit rather than in the shared title function.
- **Fixed the SSRF surface of delivery addresses**: loopback, internal and cloud metadata addresses were all deliverable, while the first 200 bytes of a failed response body went into `last_error` and were echoed in the delivery history, forming a semi-blind read primitive. The defense now sits at the **dial stage**, covering DNS rebinding, and cross-host redirects are refused; loopback and link-local addresses require explicitly setting `ARTEX_NOTIFY_ALLOW_LOCAL=1`, while RFC1918 private ranges are still allowed (self-hosted internal SMTP relays are common).
- **Fixed push credentials leaking through error messages**: DingTalk's `access_token`, WeCom's `key` and Telegram's bot token all live in the URL, and on delivery failure the full URL flowed into four places: `last_error`, the delivery history, server logs and frontend toasts. It is now uniformly redacted, keeping only `scheme://host` and the underlying cause.
- **Fixed silent loss caused by marking the whole batch delivered after a digest message was truncated**: the truncated findings were neither in the message nor in the failure list, yet the delivery history showed success. Packing is now done by **whole entries**: only those that fit into this message are marked, the rest go back to the queue to continue in the next message without consuming retries, and the message header states how many more will follow in the next one.
- **Fixed a typo in the minimum severity threshold silently disabling the filter**: with a typo such as `hgih` in `min_severity`, the unknown level's ordinal was 0 and the check degenerated to always-true, so users who thought they received only high severity actually had every finding poured into the group. The value is now validated on the write path, listing the allowed values.
- **Fixed `rate_per_min = 0` (no rate limit) being unreachable**: the database write layer quietly changed an explicit 0 to the default, so operators believed the limit was lifted while still being throttled. The default is now filled in at the API layer when the field is absent.
- **Fixed digest channels bypassing the token bucket entirely**: `rate_per_min` previously had no effect at all in digest mode. The number of claimed items is now bound by both this round's quota and an in-memory upper bound.
- **Fixed failure handling being decided by the batch's maximum attempt count, making new deliveries pay for old ones**: an old delivery already retried twice would drag brand-new deliveries in the same batch into failed, so new findings were lost permanently without using a single retry. The decision is now made per item.
- **Fixed deliveries with unparseable snapshots in a digest batch being silently marked successful**: such entries were skipped at the render stage yet counted as successful along with the whole batch. They are now explicitly failed with a reason.
- **Fixed a single channel's deliveries per round possibly exceeding the lease duration**: in multi-instance deployments, rows with expired leases would be re-claimed, sent again and have their attempt count incremented twice. The per-round cap is now derived from "lease / per-send timeout", and an assertion pins down the relationship between these constants.
- **Fixed Telegram truncation possibly cutting an HTML entity in half**: truncation only avoided half tags, not entity fragments such as `&amp`, which could make the parser reject the **entire** message. Both are now avoided.
- **Fixed the email channel treating transient SMTP failures as permanent**: 4xx replies such as greylisting's `450` are temporary rejections that should be retried later, but were all treated as fatal, so servers with greylisting enabled made every push fail after the first attempt. Replies are now distinguished by the first digit of the reply code: 4xx is retryable, 5xx is a permanent failure.
- **Fixed nested-structure submissions actually writing the mask literal to the database**: object fields like `webhook.headers` can only be masked as a whole or submitted as a whole; a mask sentinel placed inside the object was stored as a real value, silently breaking authentication. Such submissions are now explicitly rejected.

#### Design notes

- **The transaction that writes a finding does only one blind INSERT**: `notification_events` is written in the same transaction as the finding, so a commit guarantees the two are atomically consistent. This INSERT deliberately does not read the channel table or run user filter rules, and is wrapped in a `SAVEPOINT` - otherwise a single misconfigured filter condition could abort the transaction and keep a high-severity finding from being stored.
- **Deliveries are claimed by lease rather than a long transaction**: after claiming with `FOR UPDATE SKIP LOCKED`, the row is set to `sending` and `next_attempt_at` is pushed into the future as the lease, so no database lock is held during delivery; rows left behind by a process crash are re-claimed once the lease expires, self-healing without retrying forever.
- **Rate limiting does not consume the retry budget**: this round's quota is computed from the token bucket first, then that many items are claimed. The reverse order would charge an attempt to deliveries blocked by rate limiting, and the budget of three would be burned by pure waiting, ending in failure.

### Test infrastructure

#### Fixed

- **Fixed assets being left behind permanently due to cleanup ordering in the company ICP attribution tests**: `defer d.Close()` runs first when the function returns and `t.Cleanup` runs after it, so every cleanup statement landed on an already-closed connection and the errors were discarded; test assets and companies stayed in the database forever, and made other tests that assert on asset counts fail mysteriously. Closing the connection now also goes through `t.Cleanup` and is registered first, and cleanup failures are surfaced.

### Traffic

#### Added

- **Traffic list supports content search, path / status code / length filters and column sorting**: the filter row can filter by **response content** (via the full-text index), **path**, **status code** (an exact value or a bucket such as `2xx`) and **response length range**; the table header sorts by time, status code or response length, and the sort preference is stored locally. Two indexes were added and take effect automatically when existing instances upgrade; the Agent tool's query contract is unchanged.
- **Traffic list gains "Clear all"**: deletes all traffic records in one go (ignoring the current filters) and cleans up historical host directories the index no longer tracks, then runs a full compaction and reports how much space was actually freed. Traffic evidence already bound to findings lives in a separate evidence store and is unaffected.

#### Fixed

- **Fixed disk space not being released after deleting traffic**: the index database was created without `auto_vacuum`, so the file never shrank; the full-text index is also `contentless_delete`, where `DELETE` only writes tombstones and does not reclaim the original postings - deleting traffic actually made the index bigger (measured: capturing 6MB of bodies → 16MB index, still 16MB after deleting everything). Newly created index databases now enable `auto_vacuum=incremental`, and after every delete a background job runs full-text index merges and incremental reclamation in chunks, gradually returning space to the system (the same scenario drops back to 104KB); reclamation releases the write lock between chunks and does not block traffic recording.

> Upgrade note: `auto_vacuum` can only be set when the database is created, so **the index database of existing instances stays in the old mode** and incremental reclamation is a no-op on it (a hint line is printed at startup). On such databases tombstone growth has already stopped after the upgrade, and the space already occupied can be reclaimed once with "Clear all" - that step converts the database to incremental reclamation mode, after which routine deletes take effect on their own.

### LLM

#### Fixed

- **Fixed custom session headers not taking effect on the "one-shot LLM call" path**: goal decomposition (round 0) and cold-node compaction are both one-shot calls without a transcript store, so there is no session id on the context and the gateway cannot read the header - for endpoints such as opencode zen that return 400 outright when the header is missing, this showed up as "round 0 fails while later planner rounds are perfectly fine". Both paths now explicitly attach a session id that is stable per exploration, which also lets token usage be attributed correctly to the exploration (previously it was not recorded at all).

### Tools

#### Fixed

- **Fixed output truncation not applying to MCP and custom tools, so large results flooded the context wholesale**: MCP tool results previously bypassed truncation entirely, and HTTP-type custom tools, because an empty tool context was passed in, always used a hardcoded 6000 characters instead of the session's configured limit. Both paths now go through `actool.Capture`: output is truncated at the session's `MaxOutputChars` (30000 by default), and when `ToolOutputDir` is configured the full output is spilled to disk with only the leading snippet and a file pointer left in the message.
- **Upgraded norma to v0.4.3**: the harness layer now runs `CaptureOnce` uniformly, so no tool (including ones added later) can overflow the context again because truncation was not wired up.

### Deployment

#### Fixed

- **Fixed the activity stream failing to connect behind a reverse proxy**: the SSE URL was unconditionally built as `${hostname}:8787`, so in deployments reverse-proxied to 443 the page loaded from `https://<domain>/` while SSE was pointed at port 8787, which is not exposed publicly, showing up as the activity stream reconnecting over and over. It is now split by `NODE_ENV`: production defaults to same-origin, and only `next dev` keeps `:8787` to work around Next.js buffering SSE; the `NEXT_PUBLIC_SSE_BASE` override remains. The README gains a reverse-proxy deployment section.

### Findings

#### Fixed

- **Fixed the "By asset" view having no scrollbar when the asset list overflows**: the outer card only had `max-height` and no definite height, so the scroll viewport's percentage height could not be resolved, and with many assets the list either burst out of the card or was clipped and unscrollable. The height cap is now applied directly to the asset tree's native scroll container and adapts to the window height.

### Database

#### Fixed

- **Set an upper limit on the connection pool**: `database/sql` by default does not limit how many connections the application itself may open, and creates a new one unconditionally when the pool has no idle connection, climbing until PostgreSQL's `max_connections` (100 by default) refuses it. It is now capped at 32, and excess queries wait for an idle connection, so the same load shows up as slowness rather than errors; if `max_connections` has been lowered, lower this cap accordingly.
- **Improved connection reuse**: `MaxIdleConns` defaults to 2, so connections beyond a concurrency of 2 were closed right after use and the next request had to redo the TCP and PostgreSQL authentication handshakes. The idle limit is now raised to match the total limit, with a 30-minute connection lifetime and 5-minute idle eviction added so the pool naturally replaces long-lived bad connections.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.14] - 2026-09-24

### Task list

#### Added

- **Task list gains a "Findings" column**: shows counts bucketed by severity as Critical / High / Medium / Low, with non-zero buckets colored by severity, so the size and distribution of each task's findings is clear at a glance.

#### Changed

- **"Description / Goal" columns narrowed**: overlong content is truncated with an ellipsis and the full text is shown on hover, so long text takes up less horizontal space in the list.

### Exploration graph and planning situation overview

#### Changed

- **Slimmed down the planning situation overview (graph_overview) and capped every list**: recent facts, finished intents, pending intents, cold-zone summaries and confirmed finding details all switch to "latest window + count fallback", with anything not shown queryable on demand; this significantly reduces per-round LLM context size and avoids context bloat in long tasks (the cold zone of the linked-task overview is capped as well).

#### Fixed

- **Removed reverse duplicate edges between digest and finding in the exploration graph after cold nodes are collapsed**.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.13] - 2026-09-19

### Asset intercept

#### Added

- **New global asset intercept rules (blocklist)**: supports exact and fuzzy matching of domains / IPs / URLs as well as CIDR ranges, with create, read, update, delete and enable/disable for rules; the management page is at "System → Asset Intercept"; government (`.gov` / `.gov.cn`) and education (`.edu` / `.edu.cn`) sites are fuzzy-intercepted by default out of the box.
- **Asset intercept wired into the execution chain**: before issuing an intent (`add_intent`) or inserting assets (`insert_assets`), the Agent first runs an intercept check on the target assets - intercepted intents are not issued and intercepted assets are not inserted, and the asset info and intercept reason are returned to the Agent.
- **New task-level intercept / allow (allowlist) rules**: independent of the global rules and effective only for the task, evaluated "intercept first, then allow" - an intercept hit is forbidden; if nothing is intercepted but the task has allow rules configured and none of them match, the result is "not allowed to test"; with no allow rules configured, the allowlist is not enabled. Rules can be entered when creating a task, or added, edited, deleted and enabled/disabled in the task detail "Overview".
- **Task templates support preset categories and task-level intercept/allow rules**: a template can store the task category and a set of task-level rules, which are carried into the new-task form when the template is applied.

### Operation review

#### Fixed

- **Tightened the model judge output protocol to reduce false passes caused by truncation**: the verdict comment limit drops from 500 to 120 Chinese characters and "output JSON only, with no preamble or code block" is enforced, so a verdict is no longer truncated by `MaxTokens`, left unparseable and then let through under the model-failure policy (fail-open).

### Task archive

#### Fixed

- **Archiving skips symlinks instead of failing the whole package**: the archive format supports only regular files and directories end to end, and any symlink in the working directory used to fail the entire task archive; the symlink is now skipped and logged while the remaining files are archived normally (links are not followed and the directory tree is not escaped).

### Account and compliance

#### Added

- **New "Terms of Use and Disclaimer" dialog before login**: the agreement box must be checked before logging in.

### License and dependencies

#### Changed

- **The project adopts the AGPL-3.0 open-source license**, and the README's license and disclaimer sections were improved.
- **Upgraded norma to v0.4.1**.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.12] - 2026-09-17

### Exploration chain broadcast board

#### Added

- **Task detail page gains an "Exploration chain broadcast board"** (#144): presents the task's exploration nodes (start/goal/intent/fact/finding/hint/compaction) as a timeline feed, with filtering by type, keyword search, ascending/descending order, pagination and auto-refresh; on "the first page, newest first" it is in the live position and refreshes every round, while away from it only an unread count accumulates without disturbing the current reading, and "N new broadcasts · Back to latest" returns with one click. Entries are grouped by day, so even when paging through a long task you can tell "which day this happened".
- **Each broadcast board row shows the node id** (#145), making it easier to cross-reference the exploration chain graph and locate a specific node.
- **Expanded node details on the broadcast board show upstream/downstream and anchored assets** (#147): expanding a broadcast shows the node's upstream (where it came from) / downstream (what it produced) relations, and hovering over a related entry pops up that node's card (type/status/source/time/summary/payload snippet); the assets anchored to the node are listed as well (type tag + recognizable text). The data is delivered with the broadcast page, so expanding sends no extra request.
- **Broadcast board search can filter by node id** (#150): besides "content / source", the search box now matches node ids; enter plain digits or the "#41" form shown in the UI to pinpoint the node.

### Intent management

#### Added

- **Intent deletion supports two modes, "soft delete / hard delete"** (#149): pending, running and paused intents can all be deleted, a reason is required, and the mode is chosen in the confirmation dialog.
  - **Soft delete (default)**: the intent is set to "deleted" and the reason is recorded in a dedicated field, keeping the intent node and all of its outputs and lineage.
  - **Hard delete**: physically removes the intent and the exclusive descendant nodes "supported only by it" (cascading along the output / intent chain down to the leaves), so no orphaned data is left; the deleted intent's token metering is archived and kept under its original dates; shared nodes (still referenced by other intents), goals and task root facts are always kept, and the hard-delete dialog shows the estimated number of nodes affected by the cascade.

  Both modes notify the Planner that "this intent was deleted by the user + reason", and it replans accordingly.

### Operation review

#### Added

- **Approval records can be filtered by status and verdict source** (#139): filter approval records by approval status and verdict source to locate a record quickly.

#### Fixed

- **Pending approval requests load independently of history pagination** (#133): pending requests are no longer affected by pagination of the history list and stay fully visible while browsing history.

### Agent

#### Changed

- **Worker role description reworded as a general-purpose cybersecurity platform** (#138).

#### Fixed

- **Fixed cold-node compaction never triggering**: the per-task planner is now wired to the Compactor; cold-node compaction (cold-digest) never ran because it was not hooked up, and is now restored.

### Chat

#### Added

- **Chat supports @ references to multiple record types with scroll pagination** (#135): several types of record can be @-referenced in chat, and reference candidates load with scroll pagination.

#### Fixed

- **Long message bubbles are constrained to the session panel** (#137): overlong message bubbles no longer overflow the session area.
- **Fixed a "missing upload file" error when uploading a file before a new conversation is created**: the Composer's file picker now snapshots the `FileList` into an array before clearing the input and only then invokes the callback; previously the draft state had to create the session asynchronously first, and by the time execution resumed the `FileList`, live-bound to the input, had already been emptied, so the upload lacked the file field and the backend returned 400.

### Traffic

#### Fixed

- **Traffic recording proxy listens only on 127.0.0.1 by default** (#129, #130): avoids exposing an open proxy on other network interfaces under the default configuration.

### Web

#### Fixed

- **Fixed the task list page losing the global header in the static export build**.
- **Added the linked-traffic mock for the demo finding detail page, fixing a blank screen**.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@RuoJi6](https://github.com/RuoJi6)
- [@dingpotian](https://github.com/dingpotian)

## [0.3.11] - 2026-09-15

### Operation review

#### Added

- **Approval records support source navigation** (#125): clicking an approval record's "Source" jumps back to the tool execution that triggered that approval - it opens the original full session, auto-paginates to the target position, expands the command and result in place and highlights them centered in the scroll area, with the surrounding messages kept readable; auto-correction stops when the user scrolls manually. Across normal chats, task Workers, the planner and segmented main agent sessions, the target is located precisely via the persisted `tool_use_id` and task mapping (a scoped call-ID index was added); when it is missing, duplicated or ambiguously linked, an explicit notice is shown instead of jumping to a different execution. A clear notice is given when the session was deleted, the task was archived or the record is missing.
- **Approval records support pagination** (#115).

#### Changed

- **Slimmed down the model review input** (#124, #125): the input to model review (the LLM judge used when no rule matches) is reduced to a versioned JSON of "this one complete tool call + explicitly selected brief background + the local working directory". Background is taken only from real user messages (the current user message of the chat / task main agent); Workers no longer attach an intent summary and also clear background inherited from the parent Agent; the planner and auto-triggered sessions do not fabricate a user message. Task description, goal, operating constraints, global exploration situation, call history and the Worker's full intent are no longer attached - these are still used as before for Agent execution and independent session auditing, they just do not enter operation review. Every verdict must output `decision`/`comment` as JSON, and the comment must include "the actual operation, the consequence if it succeeds, and the rule that was hit"; instructions inside arguments or background cannot change the review policy. The input snapshot actually sent to the model and its fingerprint are saved; old snapshots are kept and labeled with their version, and old inputs are not reconstructed from current data.

#### Fixed

- **Model verdicts wrapped in a code block are no longer silently passed** (#126): if the verdict JSON returned by the model was wrapped in a ``` code block, parsing failed and it was silently passed via the model-failure policy; the code fence is now stripped before parsing, and the configured failure policy applies only if parsing still fails.

### Agent

#### Added

- **New experimental noa context compaction** (norma upgraded to v0.4.0): an experimental feature that can be enabled in system settings, off by default. When on, noa (model-driven context compaction) takes over context compaction for the four Agent kinds - main agent, planner, Worker and chat - replacing the built-in compaction; the original text being compacted is persisted to an archive, centralized under `<workDir>/noa/<session-id>/` (not scattered across task directories), in directories that are globally unique per session ID. If the integration fails it automatically falls back to built-in compaction without interrupting real tasks; the switch is read once per run, so toggling it only affects runs started afterwards.

#### Fixed

- **Exploration graph tools refuse instead of crashing on nil when there is no task context**: calling exploration graph tools outside a task context returns a clear error instead of crashing on a nil store dereference.

### MCP

#### Added

- **Support for legacy SSE MCP servers** (#117): compatible with MCP servers that only offer the old SSE transport.

### Traffic

#### Fixed

- **Traffic search matches a record's host in a port-aware way** (#114): `traffic_search` host matching now includes the port, so records for the same host on different ports no longer bleed into each other.
- **A failed `traffic_search` description migration no longer aborts later reporter migrations**: a single failed migration is isolated and does not affect the migrations that follow.

### Web

#### Changed

- **Divider added between task retest finding options** (#122): retest finding options are separated from each other for a clearer look.

#### Fixed

- **Fixed the demo task detail page crashing entirely**: in mock mode the task detail "Sessions" tab fetches `GET /api/tasks/<id>/side-questions` (side question history), but the mock handler had no such route, and the read fallback returned `[]` under its "a path ending in s is a collection" rule, leaving `data.items` as `undefined`, so the side-question hook's `merge()` threw `TypeError: t is not iterable` when iterating it; the exception occurred inside the `setItems` updater and was deferred by React and rethrown during the render phase, where the caller's `catch` could not catch it, so the error boundary took over the whole page and showed "This page couldn't load". The mock handler now explicitly returns an empty side-question history, and `sideAPI.history` also normalizes the return value (a non-array `items` always becomes `[]`) as defense in depth.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@RuoJi6](https://github.com/RuoJi6)

## [0.3.10] - 2026-09-13

### Network

#### Added

- **Web search gains an official DeepSeek source**: it directly reuses the currently active LLM config. It differs in nature from the other three sources - DeepSeek has no directly callable search API; search exists only inside its Anthropic-compatible API (the `web_search_20250305` server-side tool) and is executed on DeepSeek's servers. It therefore **supports only the official DeepSeek endpoint + the anthropic protocol** (OpenAI-protocol endpoints reject server-side tools outright), each search **costs one extra model call**, the request **does not go through the search egress proxy** and **is not recorded in traffic**, and results contain **only titles and links** (no snippets; WebFetch fetches the body when it is needed). The settings page explains these limits but **does not validate or block**; whether they are met is for the user to confirm, and the "Test search" button runs a real search to verify.

### Agent

#### Added

- **Workers can look back across works**: new `search_all_worker_traces` (no need to know the intent_id first; searches globally by keyword for matching steps in the execution of all works in this task) and `get_worker_trace` (after picking a work, lists its step stream, searches it by keyword in place and fetches full content by step_id), for reusing observations another work saw but never wrote into a fact and avoiding duplicate effort.
- **`node_detail` made available to workers**: together with the look-back tools above, a worker holding an intent_id / node id can query that node's full details directly.
- **Planning rounds triggered by `add_hint` are announced explicitly**: previously a new hint was only folded into the situation overview for the Planner to discover on its own; now one `add_hint` records an "a human added N strategic hints: ..." trigger for the Planner (a batch counts as one, without flooding per item), and the Planner is told explicitly that "this round was triggered by new hints" and sees the hint content directly.

#### Changed

- **Global situation prompt reworded in a "loosened" tone**: encourages divergent exploration and prompt reporting of cross-intent leads rather than converging too early.
- **Trimmed the worker boundary wording**: being blocked at first does not mean the intent is exhausted; the focus is "finish the bypass techniques within this intent before drawing a conclusion".
- `insert_assets` drops the per-asset `related` parameter: its only effect was to decide whether to add the asset to this task's scope, and the value was not persisted (it was void once the same asset was registered again, and the UI could not show what had been judged unrelated), so in practice it just made the model perform one more judgment that could not be retained.
- **Test scope (`task_scope`) is no longer affected by the asset coverage switch**: the auto-scoping of `insert_assets` (`source='auto'`) runs whether coverage is on or off, and `add_task_scope` is always provided to the planner / in-task main agent / goal decomposition Agent. Scope is the task's authorization boundary and the filter baseline for asset queries; the coverage switch only decides whether to use it as the denominator for metrics and should not decide whether scope itself accumulates. Previously, turning coverage off disabled both the `auto` and `agent` write paths at once, leaving `task_scope` with only rows added manually in the UI. `list_untested_assets` is still hidden when coverage is off (it is purely a coverage view).

#### Fixed

- **Fixed assets leaking between tasks** (#59): `list_assets` used to hardcode the task id to 0 and query the whole shared asset store unfiltered, and the model had no way to write a scope filter, so it treated other tasks' assets (especially IPs) as this task's targets and testing was led astray. It now returns only assets that fall within the test scope (`task_scope`) of [this task and its directly linked tasks], matched by **ownership** rather than literal value: a root domain in scope returns all subdomains/services/endpoints under it, and a network range in scope returns the hosts and services inside it; fetching an out-of-scope asset directly by id fails as well. Non-task contexts (Auto/pentest) have no scope to rely on and still fall back to the whole store. Also fixed an ownership blind spot where hosts accessed directly by IP (e.g. `http://1.2.3.4/api`) could not be matched by a network-range scope. The UI's "Test assets" view (filtered by task producer) is unaffected.
- **Fixed worker cross-work look-back tools being removed by mistake**: after `search_all_worker_traces` / `get_worker_trace` (and `node_detail`) were added to the worker default toolset, an old migration that "narrows the worker tool surface" unbound them again at startup, so workers never actually got these tools. They have been taken off that unbind list, and databases that already ran the old migration get them re-bound to the worker once.
- **Fixed `/btw` side questions occasionally failing to persist**: NUL (`\u0000`) escapes, which JSONB does not support, are stripped before a side_question checkpoint is stored, avoiding write errors for content containing that character.

### Triggers

#### Fixed

- **Merged trigger sessions deduplicate task description/goal per task**: when several tasks are merged into one trigger, the same task's description/goal is no longer concatenated into the message repeatedly, preventing long goals from piling up and blowing up the context.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.9] - 2026-09-11

### Agent

#### Added

- The in-task main agent supports **multiple sessions**: sessions can be created, switched and have their context reset independently, and every session is interactive.
- Persistent `/btw` **side questions**: ask follow-ups without interrupting the main thread; the Q&A is stored and survives restarts.
- `spawn_task` gains `source_task_ids`, letting a new task **inherit read-only** the assets and findings of its source tasks.

#### Changed

- Disabled the worker's cross-engagement memory and narrowed its default tool surface: reading context and cross-work review are planning duties, while a worker only executes a single intent and writes back.
- Trimmed the worker default prompt; `get_worker_output` drops the `terminated` and `worker_name` fields; trimmed the descriptions of `insert_assets` / `list_assets`.

#### Fixed

- Fixed works being cut short by an **idle turn**: the model sometimes spends an entire turn outputting only thinking, with neither text nor a tool call, and the harness then sees a natural finish (`end_turn` with no `tool_use`) and wraps up with `completed` + an empty summary - an unfinished intent is cut off midway, showing up as "the task ended normally but with no text summary at all". None of the five LLM retry layers can reach it: it is not an error; the entry to the same-provider safe-window retry is "stream failure"; the circuit breaker counts `err == nil` as success outright; intent rerun only recognizes `model_error`; and the SDK's empty-response retry cannot reach it either, because that layer judges emptiness by "whether any event was yielded", and thinking deltas are themselves events. The work's Stop hook now recognizes such a turn and injects a continue instruction, letting the model carry on to the next step with the thinking it has already produced - "resend as is" was deliberately not chosen, because this kind of idling is usually determined by the prompt and the shape of the context, a stable behavior rather than a random glitch, and resending would only make the model think it all over again.
- The count reuses the **empty-response retry count** under "Retry and backoff" on the LLM page (both answer "the model finished but produced nothing substantive", differing only in criterion and remedy), defaulting to 2; set `-1` to turn it off and return to the former "an idle turn ends the work" behavior. It is a cap on the **total** for one intent rather than a consecutive count: the harness itself already limits consecutive idle turns to a single nudge, and the quota refreshes only after a real tool turn has happened, so what this number stops is loops like "tool → idle → nudge → tool → idle" draining the intent's budget. When triggered, the log records `[work <worker> · #<意图>] 空转回合…注入续跑指令 (n/N)`. See `docs/LLM重试设计.md` §1.1 for the design.
- Fixed the `/btw` context budget and input layout in long conversations; in non-secure contexts (non-HTTPS access) side-question request ID generation falls back to a degraded method.

### Traffic

#### Added

- Findings support **linking multiple traffic evidence items**, which can be sorted, annotated and distinguished by role (request/response/corroboration).
- The report Agent **automatically links** relevant traffic evidence before writing a report.
- New Agent **traffic binding switch**, and evidence handoff between Agents is now complete.

### Tasks

#### Added

- **Session-level finding retest**: a retest runs in its own Agent session, with the run status shown in the list and detail views.

### Intercept

#### Added

- Approval records gain **details and execution audit**: view the tool request context, the initial model/rule verdict, the execution output and the argument fingerprint.

### Assets

#### Added

- Task test assets support **DSL search**.

### LLM

#### Fixed

- Fixed the connection test not sending the custom session header, which made opencode zen return 400.

### Network

#### Added

- MCP's HTTP transport supports **skipping TLS certificate verification**, making it easier to connect to services with self-signed certificates.

### UI

#### Added

- The session list is grouped by Agent, with expand/collapse and independent pinning; conversations can be filtered by Agent.
- The attack chain graph renders compaction (digest) nodes and collapses their members.

#### Fixed

- Fixed a blank screen caused by login credentials getting out of sync.
- The intercept notice wording now clearly says "platform control", so it is not misread as a defense on the target side.

### Deployment and updates

#### Added

- **One-click update from the page**: the system config page gains a "Version and updates" card, and the top bar lights up a notice when a new version is available. The flow is: download the release package → verify `SHA256SUMS` → smoke test → stage → exit and let the supervisor script relaunch to complete the swap, after which the page refreshes automatically.
- New supervisor launch scripts `start.sh` / `start.bat` as the official entry point (included in both the release packages and the Docker image; `install.sh` is unchanged); they decide by exit code whether to relaunch, and forward SIGTERM to artex. The verification and swap logic all lives in Go, keeping the scripts dumb.
- Automatic fallback on failure: if verification or the smoke test fails, the update is discarded and the current version keeps running; if the new version fails to start 3 times in a row, it rolls back to the previous version. The settings page also offers manual rollback (note that the database schema is not rolled back).
- Updates accept only GitHub domains with HTTPS enforced, and the release source is not configurable; one-click update is disabled in development builds. GitHub query results are cached for 30 minutes so the top-bar notice does not exhaust the API quota.

#### Known limitations

- Under Docker only the program is swapped, not the image: the toolchain is not upgraded along with it, and recreating the container reverts to the version bundled in the image; use `docker compose pull artex` when needed.
- `skills/` in the release package is not synced, so built-in skills added in a new version do not take effect automatically.
- Updating means restarting, which interrupts running tasks.

### Dependencies

#### Changed

- Upgraded norma to v0.3.7.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@RuoJi6](https://github.com/RuoJi6)

## [0.3.8] - 2026-09-09

### LLM

#### Added

- The LLM page gains a "Retry and backoff" tab: both the **count** and the **interval** of all five retry layers are configurable. A failed model call passes, from the inside out, through five layers: connection retry (SDK; connection reset / timeout / 429 / 5xx before the stream starts), empty-response retry (SDK; finished normally but with no content at all, openai format only), same-provider safe-window retry (replay after a stream break before any output has been delivered to the caller), failover circuit breaker (cool down and skip once consecutive failures reach the threshold) and intent rerun (the whole intent is rerun after a worker ends with model_error); an outer layer takes over only when the inner one is exhausted. Each layer has two knobs with uniform semantics: empty = use the original default count and exponential backoff; a count uses that count; an interval replaces exponential backoff with a fixed interval; -1 = turn this retry layer off. The first three layers follow the endpoint and can override the global default field by field in each model config (a config that pins only the interval still inherits the global count); the circuit breaker and intent rerun have process-level semantics and exist only once, globally. Changes take effect hot on save, with no restart needed. Leaving everything empty is the current behavior, and old databases are byte-for-byte unchanged after upgrade (new columns default to 0, and a missing settings key means all defaults). The connection test deliberately omits these parameters - it has a hard 30s timeout, and stacking user-configured retries on top would only make a working endpoint test as a timeout failure. See `docs/LLM重试设计.md` for the design.
- Each LLM config supports a custom **session header** (`session_header_key`): when non-empty, every LLM request carries that HTTP header with the current run's session id as its value (`conv-<id>` for chat sessions, `exp<x>-worker-i<intent>` for workers, and so on), for gateways that do prompt caching / sticky routing by a session-id header. It is implemented by reading the session id from the request context and injecting it, with no change to norma, and the same shared provider can send different header values per session. Old databases get a migration, and the frontend LLM config dialog gains an input field.

#### Fixed

- Fixed saving an LLM config failing outright with `23502` when the session header field is empty: the column is `NOT NULL DEFAULT ''`, and `NULLIF($n,'')` was wrongly applied, writing an unfilled session header as NULL and tripping the not-null constraint; the parameter is now passed directly with empty-string semantics.

### Agent

#### Added

- Wall-clock timeout now **wraps up in place**: relying on norma v0.3.6, when `MaxDuration` is reached the running tool is interrupted and the run continues on the live ctx for the wrap-up rounds (writing back what has been identified + a summary, with final state timeout), instead of relying on the worker's/planner's own external hard ctx of `maxDur+90s` to kill a stuck run straight into `aborted_tools`. chat goes through the same harness and gets the same behavior automatically; mainagent has no `MaxDuration` and is unaffected.
- Stall fallback: when the planner is woken by a heartbeat / no-change wakeup and the whole graph has no open or running intent, its opening changes to a stall alert, stating clearly that no worker is running and no direction is queued, and that this round must produce one or more new, non-duplicate intents (producing 0 intents is not allowed).

#### Changed

- Worker prompt restructured: the intent / launch instruction / raw JSON of anchored assets move into the system prompt, rebuilt every round and never squeezed out by compaction, and resuming no longer depends on the first transcript message surviving; the launch user message is slimmed down to just the global situation overview (degradable, tolerates stale). The cost is that system mixes in per-intent data and loses cross-intent cache reuse - a deliberate trade-off for "never lose the intent".
- Trimmed the planner / worker default prompt bodies and fixed several real-world issues: the planner distinguishes `recent_done` by status (for blocked/exhausted, check the trace before deciding - neither treat it as a dead end nor rerun blindly), negative conclusions become "observation / in doubt, not final" with evidence checked before accepting them, intents must be produced when the goal is unmet and there is no open/running intent (hard floor), and depth takes priority over coverage; for negative conclusions the worker writes only "observation + tentative reading" and the verdict belongs to the planner, and cross-intent leads go into the fact's summary for the Planner rather than being chased by the worker itself. reseed appends the new default as a new version and switches to it, while user customizations / old versions stay in history and can be rolled back.
- Narrowed the work agent default toolset and the asset write-back prompt: a worker only executes a single intent and writes back, while reading context / cross-work review are planning duties, so `list_facts` / `node_detail` / `list_companies` and cross-work search (`search_all_worker_traces` / `list_worker_traces` / `get_worker_trace`) are removed from its default tools, leaving only `list_findings` (duplicate check before reporting a finding) + `add_finding` / `record_fact` + `insert_assets` / `list_assets`; the prompt drops stale field descriptions that conflicted with the `insert_assets` schema (`type=tech`/`on_url`/`props`). Old databases get a one-off migration that strips the corresponding bindings; the same-named bindings of planner/main are untouched.

### Exploration graph

#### Added

- **Exploration graph cold-node compaction (cold-digest)**: folds old, long-inactive intents / facts into digest nodes shown in `graph_overview`, while the original nodes are kept forever and can be fully restored by id (storage is lossless, only presentation is compacted, and folding is reversible). Hot/cold is decided by reverse reachability + any live branch means hot, with R=6 rounds of debounce and grouping by connected component; background compaction (minor folds uncovered cold blocks, major re-compacts from source to merge fragments) has an activity recheck and cooldown mutual exclusion, never runs on the hot path and never overwrites revived nodes. The overview side provides `cold_digests` and the asset-indexed `cold_index`, with `expand_digest` / `expand_index` doing the restoring. Overviews of linked / inherited tasks likewise reuse their own folded view, and `expand_digest` supports cross-task read-only restore. `expand_digest` / `expand_index` are given only to planner and main agent, not to worker. All come with migrations for old databases.
- `graph_overview` outputs the full `finding_list`: findings are a task's highest-value output and a single task usually has few of them, so they are now carried in full in the overview (unlike facts, which get only a recent window), letting planner/worker see every confirmed finding each round without calling `list_findings`. Each entry is trimmed to `{id, summary, evidence?, from_intent?, assets?}`, where affected assets are given as readable content (url / domain / ip:port) rather than bare ids.

#### Changed

- `graph_overview` no longer flattens `hosts`: the coverage block drops the host list (in large-scope tasks each round carried up to 500 host strings, of limited value for planning decisions), keeping only `host_count`; specific hosts are looked up via `list_assets` as needed. A new top-level `done_intents_total` (total number of finished intents) sits alongside `recent_done_intents`, which is truncated to ≤15, so the planner knows when deduplicating whether anything was cut.

### Tools

#### Changed

- `add_company_scope` default binding moves from worker to planner: defining a company's asset scope is a duty of planning / main control / Auto, and worker only executes exploration. New databases seed the default binding as mainagent/planner/auto, and old databases get a one-off migration.
- planner binds `list_assets` by default: it now has both `list_assets` (DSL search over the whole store) and `list_untested_assets` (untested within scope); old databases are backfilled once without overriding user unbinds.

### Tasks

#### Added

- Task list gains a "Running Workers" column: counts the intent nodes with `state='running'` under the task, using exactly the same definition as "Running Workers" in the task detail overview, and shows 0 when no Worker is running.

### Network

#### Added

- New **global egress proxy** setting: all target traffic can leave through a single global proxy (http/https/socks5, with `user:pass` support). With traffic capture on, it acts as the upstream of the MITM recording proxy (traffic is recorded as usual and then leaves through the proxy; both the intercept and passthrough paths go upstream without leaking the source IP); with capture off, it is injected directly into the agent's bash environment and WebFetch (`proxyEnv` adds `ALL_PROXY` for socks5 support). The setting is stored in the settings KV table (no migration needed), and the frontend system config page gains a "Global proxy" card, independent of the web search proxy and the LLM proxy.

### UI

#### Fixed

- Corrected semantic errors in the intent status labels: `exhausted` "已穷尽" (exhausted) → "预算耗尽" (budget exhausted) (it actually means the step / time budget was hit and the work was cut off midway with only partial results written back, not that the direction has been fully explored), `blocked` "被拦截" (intercepted) → "执行出错" (execution error) (it actually means model / API / network failures ran out of retries and the intent was barely explored at all, not that the target / WAF blocked it), and added the previously missing `stopped` "已停止" (stopped) (a work stopped manually by the user).

### Dependencies

#### Changed

- Upgraded norma to v0.3.4: MCP tool output gains truncation and spill-to-disk (see `651b961`); the later v0.3.6 underpins the wall-clock in-place wrap-up.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.7] - 2026-08-31

### LLM

#### Added

- Model configs gain an "Output limit", with a choice of which request field name the limit uses: the limit caps how many tokens a single reply may generate and is sent with every request; 0 (default) = do not send the field and let the server default decide. It is a different thing from the "Context window" - the latter is the model's total capacity, used only locally to compute the compaction threshold, and never appears in requests. The field-name switch only matters for the `openai` (Chat Completions) format: empty (default) sends `max_tokens`, the only one the vast majority of compatible gateways accept; OpenAI's official reasoning models (o-series / GPT-5) conversely accept only `max_completion_tokens` and return `unsupported_parameter` outright on `max_tokens`, so such endpoints must be switched to the new field manually. The other two formats each hardwire their field name (`max_tokens` for Anthropic, `max_output_tokens` for the Responses API), so for non-openai formats the switch is grayed out and cleared on save. This also connects a path that had never been wired up: none of planner / worker / chat / main agent / goal decomposition ever set an output limit - the openai family sent nothing at all and Anthropic fell back to the SDK's 8192 - so adding the config option without wiring this up would have left the number without any effect; the value is now resolved from the config every round, like "Streaming output", and takes effect on the next round after a failover switch. Behavior is completely unchanged for old databases after upgrade (the new columns default to 0 and an empty field name).
- Clicking "Test" in the admin console now writes the status code and the gateway's raw response body of every HTTP attempt to the server log (body trimmed to 4K), making it easier to diagnose 401s, quota messages, empty frames, HTML pages being returned and the like, instead of seeing only the ok / err collapsed by the UI. This log does not depend on the "LLM records" switch.

#### Changed

- A task's LLM config chain can now be modified in any state, no longer limited to "running / paused / config chain exhausted": after a task ends (done / failed / timeout) the main agent conversation still uses this chain, and when a model on the chain had problems it previously could not be changed, so interaction could not continue. The terminal-state blocks in both the backend HTTP layer and the DB transaction are removed, and the frontend dialog also allows editing and saving for terminal-state tasks. Saving for a terminal-state task no longer reopens quota-blocked intents (those intents would be moved to open, with no worker to execute them and no longer meeting the conditions for "rerun intent"); to keep running, still use rerun intent / add goal, which pull the task back into the running state.

### Agent

#### Changed

- "Message a running Worker to redirect it" now reuses the existing pause / resume + transcript resume mechanism, behaving the same as the main agent conversation: it previously used a home-grown intervention persistence protocol that touched the scheduling barrier, the recovery flow and more than ten activity query filters. Messages are now injected via the next round's input, and the intent runs directly in a dedicated goroutine, not limited by the 3-slot worker pool and running as soon as the message is sent; the frontend keeps the message box and restores the "Continue directly" button, and sending is now carried over SSE. No DB schema change. Trade-offs: messages live only in memory with no crash recovery, and when a message is sent the task may momentarily exceed its concurrency by one worker (a low-frequency case, acceptable).

### Tasks

#### Fixed

- Fixed an out-of-memory (OOM) crash when archiving large tasks: archiving now writes the snapshot as a stream instead of reading the whole task into memory at once; three recovery gaps on the cold-archive path are closed as well - in modern installs traffic lives only in SQLite, and a crash between the PostgreSQL commit and the SQLite commit used to leave an archived task's traffic stranded in hot storage with no way to recover it; a staging journal is now written whether or not a legacy directory exists, and a missing `journal.json` is treated as discardable.
- Fixed the system becoming sluggish with many tasks: loading conversations and loading the UI had noticeable delays. Queries for the task list, task context and exploration records were optimized together, and the frontend dashboard, conversation page and task detail page now make fewer duplicate requests.

### Assets

#### Changed

- Removed the "at most 256 rules" limit on company asset scope: companies whose scope is entered IP by IP / domain by domain easily hit this cap, after which the only option was to split into several companies, while scope itself does not get slower with more entries. The cap is removed in all three places - frontend, backend and demo mock; a single rule is still limited to 1024 characters, and the 2 MiB request body limit remains as a backstop (roughly forty to fifty thousand rules).

### Skills

#### Fixed

- Fixed uploading a skill archive failing with "Upload failed: zip: unsupported compression": the Go standard library ships only the Store / Deflate decompressors, so bzip2 and Zstandard archives written by archivers at non-default levels could not be extracted at all. These two decompressors are now added (pure Go implementations, no new external dependency); methods that truly cannot be extracted, such as Deflate64 / LZMA / XZ / PPMd, as well as encrypted archives, now produce a Chinese-language message before extraction that names which file uses which compression method and how to repackage it, instead of throwing the low-level English error at the user.
- Fixed filename validation inside skills rejecting Chinese filenames as illegal: path validation used to be an ASCII allowlist of `[A-Za-z0-9-_./]`, so a single Chinese-named file in the archive (such as `参考/说明.md`) made the whole upload fail with "archive contains an illegal path". It is now a Unicode blocklist: filenames in any language and spaces are allowed, while control characters, invalid UTF-8, zero-width and bidirectional control characters (RLO filename spoofing), `\ % # ? * : " < > |` and `..` / absolute paths / empty path segments are still rejected, and directory traversal protection is unchanged. Skill names are relaxed likewise - ASCII is still limited to lowercase letters, digits and hyphens (the agentskills.io spec), and non-ASCII letters such as Chinese can be used directly as skill names, but spaces, dots and path separators are not accepted.
- Fixed Chinese archives produced by Windows archivers extracting with garbled filenames or being rejected wholesale: such zips do not set the UTF-8 flag and write filenames in GBK; they are now decoded as GBK as a fallback before path validation. Quoted frontmatter such as `name: "中文技能"` now also yields the skill name correctly.

### UI

#### Added

- The Findings page gains an "All findings" flat view, set as the default and switchable with the original "Grouped by task" view via page-header tabs: the grouped view requires expanding tasks one by one to see findings, which is a detour when you just want to scan the list across tasks. The flat view is one large cross-task table (10/20/50/100 per page) that behaves exactly like the grouped view - select to export, expand a row inline for evidence and the detailed report, edit name/category/severity inline, change triage status, dig deeper, delete. The two views share the stat cards and filter bar, switching does not lose filters, and the current view and filters are stored locally; polling hits only the current view, and inline edits are written to both caches, so switching over never shows stale data.
- The Findings page gains a "By asset" view: an asset tree on the left (company → root domain / IP → subdomain → service → endpoint; the company level appears only when an asset really has an owner) and, on the right, the findings under the entire subtree of the selected node, sharing the same table and the same filters as the other two views. The tree includes only assets that have findings, with ancestor chains filled in as needed (when a finding hangs only on the deepest endpoint, the levels above are still assembled), and node counts are subtree aggregates deduplicated by finding; findings whose asset was deleted or that never had a linked asset go into an "Unlinked assets" bucket. Unlike the other two views, the asset view does not poll - it queries only on entering the view, changing filters, adding/editing/deleting findings in the page, or clicking the refresh button on the tree; the left tree is a navigation structure and need not be recomputed every 5 seconds. Each tree level shows only the delta relative to the level above (subdomains drop the root-domain suffix, services show `https :443`, endpoints show only the path), with the full value in the hover tooltip and breadcrumb; when there are too many nodes, the endpoint/service levels are dropped wholesale with a notice, and their counts still roll up into the upper levels.

#### Fixed

- Fixed session records being squeezed and not fully visible in session details on mobile: on narrow screens the session list collapses, giving the width to the records themselves.

### Dependencies

#### Changed

- Upgraded norma v0.3.2 → v0.3.3: adds `Config.MaxTokensField`, letting the OpenAI Chat Completions output limit be sent as `max_completion_tokens` instead (the only key reasoning models accept). The two keys are mutually exclusive and only one is sent; the default is still `max_tokens`.
- Upgraded `golang.org/x/mod` v0.37.0 → v0.40.0, fixing two dependency security alerts.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@neouks](https://github.com/neouks)
- [@begininvoke](https://github.com/begininvoke)

## [0.3.6] - 2026-08-27

### LLM

#### Fixed

- Fixed the "non-streaming" switch reverting to "streaming" when reopened after saving (#69): the DTO of the config list endpoint was missing the `streaming` field, so the response never returned it and the frontend, reading `undefined`, always fell back to the streaming default; the actual write and DB storage were fine, the value just could not be read back. The field is restored in the DTO (without `omitempty`; `false` must also appear in the response).
- The connection test now verifies that the model actually replies, and tests in the config's real send/receive mode (#65): previously it only checked whether HTTP succeeded, so a request that went through but got zero reply from the model (thinking burned the budget / the body was swallowed by a safety policy / the compatibility layer dropped `content`) still reported "Connection successful", at odds with the "no reply" behavior seen in sessions; and the test always used streaming, so a non-streaming config was really testing a different path. An empty reply now fails outright, a success shows the model's reply in the notice, and the current streaming switch is carried into the test, so "test passed" and "sessions work" stay consistent.

### Agent

#### Changed

- `list_facts` now paginates with keyword filtering, so a single call no longer blows up the context when there are many facts (#74): it returns the latest 20 by default and supports `limit` (max 100) / a `before` cursor / a `q` summary keyword, returning `{facts, total, has_more, next_before}`; an overlong summary is truncated by character count (the full text is still available via `node_detail`). The worker / planner prompts are updated to pagination semantics. Old databases get a one-off migration that flushes the new parameter schema into the tool catalog table (`SeedTool` is first-insert only, otherwise the tool management page shows "No parameters").

### Tools

#### Added

- The "Tool executions" page gains tool call count statistics (#72): the toolbar "Statistics" button opens a dialog showing call count, share and failure count per tool (sorted by count, descending); it reuses the list's task / keyword filters, covers the whole result set rather than the current page, and fetches only when the dialog opens.

### Dependencies

#### Changed

- Upgraded norma v0.3.1 → v0.3.2: fixes reasoning/refusal being silently dropped from OpenAI-family responses (Responses adds `reasoning_text`), deduplication across the three reasoning field names, bounded retry on empty responses (zero content blocks), and gateway 400s caused by compaction boundaries cutting tool pairs apart (also repairing existing orphans already baked into transcripts).

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.5] - 2026-08-25

### LLM

#### Added

- Each LLM config supports switching between streaming and non-streaming (streaming by default): on uses streaming SSE; off uses true non-streaming (`stream:false`, returning the complete JSON in one shot), which sidesteps some gateways' poor SSE implementations (empty frames, dropped thinking-field frames) at the cost of losing live progress and live token counts during a run. worker / planner / mainagent / chat / goals all read the value dynamically from the currently active config; the three Provider wrapper layers `llmpool` / `llmrec` / task runtime are all compatible with non-streaming; `llm_profiles` gains a `streaming` column, with `ALTER` backfilling old databases (default `true`, transparent to old configs).
- Support for LLM configs in the OpenAI Responses API format: each config gains a third format, `openai-responses` (calling `POST /v1/responses`), alongside Chat Completions / Anthropic; `BaseURL` is normalized and the default model is `gpt-5`; the `format` constraint on `llm_profiles` adds `openai-responses`, with an idempotent migration for old databases; the frontend format dropdown gains "OpenAI (Responses API)". Dependency upgraded to norma v0.3.1 (including the `reasoning_content` round-trip fix).
- Recording of raw LLM request/response HTTP: the real wire body is captured at the HTTP transport layer, preserving the tool schemas, `tool_use` blocks and raw SSE frames that the normalized view cannot show; the multiple attempts of norma's internal retries are kept one by one; `llm_records` gains `raw_request` / `raw_response` columns, with `ALTER` backfilling old databases; the recording page's detail panel adds a "Raw" view toggle and request/response copy buttons (with an `execCommand` fallback for non-secure contexts).

### Conversations

#### Changed

- Session list multi-select becomes a "Multi-select" mode toggle: the default list no longer shows a permanent checkbox on every row (a cleaner look), and the header becomes an "N total + Multi-select" button; clicking "Multi-select" enters selection mode (checkboxes, select all, batch delete), "Done" exits and clears the selection, and the mode exits automatically once a batch delete fully succeeds (staying in selection mode if anything failed, for easy retry); single-item rename / pin / delete still go through each row's ⋯ menu.

### UI

#### Changed

- Improved the task management, session operations and traffic viewing experience (#57): the task list's column sort preference is persisted, inline rename is triggered directly by an icon (no longer via the menu), and drawer (sheet) interactions and traffic viewing details were polished.

#### Fixed

- Worker asset tags show only domains and IPs, and the empty-tag case is handled correctly.

### Agent

#### Added

- The planner gains a "quantitative acceptance check" when judging goals: when a goal contains quantifiable acceptance criteria (asset test coverage reaches X%, N flags captured, a certain privilege obtained), the measured values in `graph_overview` (`coverage.pct`, findings count, etc.) must be checked before `prove_goal`; if the measured value falls short, `prove_goal` is forbidden and intents are dispatched to close the gap instead, and the goal may not be marked met early on the grounds that "the main part is done / it is broadly achieved". This fixes a goal requiring 100% coverage being judged complete with only 40% measured.

#### Changed

- Rewrote the criteria for "0 intents this round" in the planner prompt: the original text described 0 intents as "the most common and most important principle", which made the Planner stop too early while the goal was unmet and untested surface remained in scope. It now says 0 intents should be produced only in two specific cases - ① every direction that comes to mind is already covered by open/running/recent_done intents; ② the next step depends on the output of a currently running work and that output has not appeared yet (wait for it to finish and plan on the next wakeup after the graph updates). A reverse constraint is added as well: when there really is a new direction that is uncovered and does not depend on a running work, or the goal is unmet and untested surface remains, do not stop just because "0 intents is common".
- Strengthened the "evidence threshold for negative conclusions" in the worker prompt: for negative conclusions such as "not injectable / port closed / no login entry" that could make the Planner abandon a whole direction, the reasonable techniques within the intent (other encodings/parameters/paths/methods) must be exhausted before concluding; if they are not exhausted or the evidence is weak, the conclusion must be marked `confidence=inferred`, so that a careless `observed` negative does not weld an entire route shut (a wrong negative early in a task is especially likely to lead the direction astray and is hard to self-heal).
- The planner / worker default prompt changes above are appended as a new version and made current via one-off migrations (`reseedPlannerPrompt` / `reseedWorkerPrompt`, each guarded by a settings flag); old versions stay in the version history, and users who customized their prompts can restore them from the version records.

### Operations

#### Added

- New `reset-password.sh` to reset the administrator password (username fixed as `ARTEX`): supports both local and docker deployments, with connection info specified explicitly or read automatically from `--dsn`/`$ARTEX_PG_DSN`/`config.json`; it generates a bcrypt hash compatible with backend login inside the database using `pgcrypto` and writes it back to `settings.auth.password_hash`, with no service restart needed after the reset. The password is passed via an environment variable, never enters the process argv, and is escaped to prevent injection.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@neouks](https://github.com/neouks)

## [0.3.4] - 2026-08-24

### UI

#### Added

- After selecting multiple tasks in the task list, their category can be changed in bulk; the target category supports "Uncategorized" to move tasks out of their current category; the whole batch is written in one transaction, and the only possible failures are tasks deleted after being selected.

#### Changed

- "Task category" for new tasks changes from a dropdown to a searchable input: typing filters existing categories, pressing Enter on a name not in the database (or clicking "Create" in the dropdown) creates and selects it instantly, and the selection is shown as a removable tag; still limited to a single category.

### Conversations

#### Fixed

- Fixed sending a message being blocked by "LLM not configured, cannot chat" when the session has an LLM config selected but there is no globally active config. The pre-send check looked only at the globally active config, while actual execution prefers the session's selected config, so the two resolution paths were inconsistent; they are now unified into one (session/Agent binding first, global as fallback).
- The cannot-chat notice is now broken down by state: with no config at all it says "Add a config", with configs but none active it says "Activate one or assign a config to this session", instead of always showing "Not configured", making the cause easier to locate.

### Agent

#### Added

- For tasks whose goals are all met, intents issued by the main agent via `add_intent` can be claimed and executed directly by a Worker and stop when done: the Planner no longer runs at this point (a task with no open goal does not enter planning), which avoids it re-judging the goals as met and canceling the intent just issued; once the frontier drains, the task returns to completed. Before issuing an intent, the main agent asks the user, based on the intent's content, whether to register it as a formal goal; if registered, the task resumes regular autonomous planning.

#### Changed

- When `step_ids` for `get_worker_trace` / `get_task_worker_trace` exceeds the per-call limit (5), it no longer errors out but returns the full content of the first 5 steps, with `returned_step_ids` / `omitted_step_ids` / `notice` in the result stating which were fetched and which remain, plus a hint that "if this is enough, there is no need to fetch more". Duplicate and invalid ids passed in are deduplicated and dropped before counting.

#### Fixed

- The task list is now sorted by task id descending (newest first), replacing the former sort by creation time. Tasks created at the same instant share a timestamp and sort unstably; combined with the 10-second polling and random iteration of the in-memory map, they kept swapping positions in the list.

### Assets

#### Fixed

- Fixed company attribution recalculation being brought down by a single dirty row: when `assets.ip` held a hostname (written by an Agent or the asset API), `a.ip::inet` threw `22P02`, so adding/editing a company asset scope and deleting a company all failed and rolled back. The safe conversion `try_inet()` is now used, skipping invalid values instead of aborting the whole statement.
- An unparseable `ip` is no longer skipped silently: after a company scope is saved, a clear notice states how many assets have an `ip` that is not a valid IP (with the specific ids and values), warning that these assets will not be attributed by IP/CIDR rules; the same information is written to the server log to cover paths with no frontend response (deleting a company, scopesentry sync, Agent writes).
- IP/CIDR matching for task test scope also switches to `try_inet()`, replacing the former regex guard that only checked the character set (hostnames made up entirely of hex letters, such as `abc.def`, still slipped through and triggered the same error).

#### Changed

- The `ip` field of `ip`, `service` and `endpoint` assets no longer accepts hostnames: `insert_assets` and the asset API return per-item errors with an `index` and state the fix directly (use `type=subdomain` with `domain`, or resolve the A/AAAA records first), so an Agent can correct itself and re-insert. Other assets in the same batch are unaffected and are stored as usual.

### Contributors

- [@neouks](https://github.com/neouks)

## [0.3.3] - 2026-08-23

### Worker

#### Added

- A running Worker can be paused, resumed and canceled individually; pausing keeps intents, facts and findings, while canceling transactionally cleans up the current intent and its direct outputs after execution exits.
- Added the `paused` intent state, an execution fence and named termination reasons to prevent late blackboard writes after a pause, cancel or task deletion.
- Added an optional task concurrency limit; new tasks, resumes, finding deep-dives and queue backfill all go through a single persistent FIFO admission path.

#### Changed

- The default wall-clock duration of a single Worker run changes from 600 to 1200 seconds; a timed-out task that is revived restarts its timer on the next real execution.
- The real execution state of the Worker, Planner and main agent now uniformly drives the task status badge, fixing "Idle" still being shown while a Worker is running.
- Worker sessions keep per-item controls and the current session's token summary, and the model name moves to the right of the current session title.

#### Removed

- Removed the Worker multi-select, select-all and batch pause/resume UI, along with the Worker batch control API and mock contract.
- Removed the token badge and tooltip from Worker list rows; the underlying full token ledger and task aggregation endpoint remain.

### LLM

#### Added

- Tasks support an ordered LLM config chain; when a provider quota shortfall is clearly identified, the next config is switched to automatically, and the current config, exhaustion state and error summary are persisted.
- Running and paused tasks support editing the full config chain, reordering it and manually switching the current config, effective from the next LLM call.
- Automatic switches, manual switches and whole-chain exhaustion are written as structured system activity and pop up deduplicable alerts via the task activity stream.
- Added a task role model resolution endpoint that uniformly resolves the config and model the Main Agent, Planner and Worker will use on their next call.
- Added an optional global LLM Pool supporting a configurable call order, fallback on failure for a specified model, health status, cooldown recovery and manual reset.
- Added an always-on LLM usage ledger aggregating input, output and cache tokens by task, session, model and config.

#### Changed

- Goal decomposition, Planner, Worker and main agent share a task-level LLM runtime (see below for the resolution priority).
- Failover responds only to explicit quota, balance or billing errors; ordinary rate limiting, auth, network, server-side and context errors do not trigger a wrong switch.
- The LLM config page becomes a list of config cards with a right-side drawer for editing, and offers the Pool failover order, priority, exclusions, health status and recovery actions.
- Config chain exhaustion info wraps on narrow screens; the current model changes from an icon in the session list to a text label next to the current session title with a full tooltip.
- The order in which roles within a task resolve their LLM changes to "Agent binding → task config chain → global": roles with an explicitly bound model always run on that model, unbound roles fall to the task chain, and only when the task chain is empty do they fall to global.
- An empty outbound proxy means a direct connection, no longer falling back to the `HTTP_PROXY`/`HTTPS_PROXY` environment variables (an explicit `ARTEX_LLM_PROXY` is unaffected); the proxy input supports `socks5://user:pass@host:port` with credentials.

#### Fixed

- Transient streaming failures before commit (before any output has been produced) are retried on the same provider with safe backoff, significantly reducing cases of "interrupted after only one or two tools, no summary, final state `model_error`"; quota exhaustion, overlong context and deterministic 4xx errors are not retried and are still handled by failover and compaction recovery respectively.

#### Removed

- Removed the LLM icon marker from the session list to avoid confusion with the Worker status icon.

### UI

#### Added

- Tasks support creating, renaming, deleting and filtering by global categories; a category can be chosen directly when creating a task.
- Added global task template CRUD and a right-side management drawer; a new task can load a preset description and goal, and the current content can be saved as a template.
- A new task can link multiple source tasks and multiple company asset scopes; company scope is entered in a single multi-line text box that automatically recognizes domains, URLs, IPs, CIDRs, ICP filings and company keywords.
- Task test assets can be added and removed in real time, recording sources such as manual, company, inherited or Agent-discovered; the current test assets and a source summary are shown next to the Worker session title.
- The Findings page groups by task with independent pagination within each group, and supports writing a description for a finding and creating a high-priority deep-dive intent.
- Conversations support rename, pin, unpin and delete; the task list supports batch pause and resume for the current page.
- Traffic details and tool execution details move to a right-side drawer; HTTP messages get a completed `Host` and highlighting for the request line, status code, headers, JSON and markup-language bodies.
- The task list's actions column gains "Details" and "Pause/Resume" buttons; the session send key is configurable in system settings (`Enter` / `Cmd+Enter`, etc.), and the Web search section gains a proxy input.
- The badge to the right of the session title now shows the LLM config name, with the model ID moved to a hover tooltip; tasks can be given an optional name, and the task list gains a name column and search (falling back to the description when empty).
- The Skills page adds usage count, last-used time and missing-dependency stats, making it easier to spot skills that are not taking effect or not installed.

#### Changed

- Task titles become focusable detail links; task assets, company assets, finding task groups and findings within groups use real server-side pagination, stable sorting and exact totals.
- Adding a company now uses a right-side drawer, with scope entry unified as multi-line text offering live recognition, validation and type preview.
- The main agent input box supports auto-grow, `Enter` to send and `Shift+Enter` for a newline, and avoids accidental sends while a Chinese IME is composing.
- Agent preview, task reports and related details uniformly use shared Markdown rendering; delete confirmations, long errors and mobile drawer width were fixed together.
- The task template selector displays and searches by template name and submits the template ID; the system's original font size is restored, and the application version is uniformly shown as `0.3.3`.
- Task totals and current-session tokens highlight only the input, cache read and output values; the send button uses a simpler up-arrow icon.
- The test scope in the task overview shows company names instead of IDs.

#### Fixed

- Plain text containing dots is no longer misidentified as an ICP filing number.
- Fixed duplicate keys being rendered in the Agent editor's variable list when a name in the variable catalog collided with a global runtime variable (such as `{{.Now}}`).

#### Removed

- Removed the standalone "Enter" button on task cards; clicking the task title is now the single way into details.
- Removed the dashboard "New task" button, the Logo URL input when adding a company, and the finding count badges at the top of the Findings page and in task groups.
- Reverted the style that enlarged the global font by 10%, and removed the Worker checkbox, model icon and row-level token stats from the session list.

### Agent

#### Added

- A new task can link directly to multiple existing tasks, inheriting live and read-only the goals, facts, findings, completed intents, asset scope and blackboard context of its direct source tasks.
- Blackboard read tools support on-demand queries of source task nodes, facts, findings and execution traces; inherited nodes carry a source marker and all write tools refuse to modify them.
- Company scope tools support domains, URLs, IPs, CIDRs, ICP filings and company keywords; keywords serve only as scope hints for the Agent and take no part in automatic asset attribution.
- The finding deep-dive action creates, in the original task, a high-priority manual Worker intent with asset anchors and a `derived_from` edge.
- The overview gains a "Goal management" card for manually viewing, adding, editing and deleting goals; adding or editing notifies the Planner and revives the task, while deleting hard-deletes the goal node (cascading to its edges and anchors) without reviving the task.
- The main agent gets the `steer_work` tool by default, which injects corrective instructions into a running Worker in real time without interrupting it or losing progress (the intent's ownership by this task is still verified).

#### Changed

- Source task intents do not enter the new task's frontier; the new task keeps using its own exploration, execution queue, working directory and conversation history.
- Deleting a running intent no longer destroys data: it is stopped into `stopped`, a deletion reason is required, the reason is attached to the intent as a fact and written into the payload, and the Planner is made aware via a `cancelled` trigger (the intent content and reason are kept).
- The main agent conversation is now driven by the server-side activity stream, and the input can be recovered when sending fails; opening a session sticks to the bottom automatically and keeps the last reply visible after details lazy-load.
- Pausing a task terminates the current Main Agent, Planner and Worker calls, but does not prevent the user from starting a new main agent orchestration session during the pause.
- Cancel, close and stream interruption uniformly preserve the real termination reason, generated content, rounds run, elapsed time, tokens and unreturned tool calls.

#### Removed

- Removed the main agent's client-side optimistic message echo to avoid duplicate messages and cross-session stream bleed under pause, failure or concurrent activity.

### Constraints

#### Added

- The goal decomposition stage extracts operating constraints (allow/deny) before decomposing goals, and the main agent can add more at runtime; constraints are injected into the Planner and Worker system prompts at the highest priority, and post-debiasing diversity and surface-expanding exploration explicitly yield to the constraints.
- The overview gains a "Constraint management" card and create/update/delete endpoints; the constraint injection scope can be toggled separately for Planner / Worker (both on by default, read every round, effective immediately). The new table `task_constraints` uses `CREATE TABLE IF NOT EXISTS`, so old databases get it created automatically.

### Intercept

#### Added

- Command intercept gains model fallback approval on top of regex/string rules: when no rule matches at all, the model makes an `ALLOW`/`ASK`/`DENY` semantic decision, with a configurable failure fallback action and approval timeout action; the intercept page is split into two tabs, "Intercept rules / Model config", and model verdicts are labeled with the `[模型]` (model) prefix and a rationale.

#### Fixed

- Hardened judge output parsing so that a correct `DENY` verdict is no longer mistaken for a pass.

### Traffic

#### Changed

- Traffic recording now stores each whole exchange in SQLite (large bodies overflow into a blob bucket deduplicated by hash), with a trigram full-text index on text bodies supporting arbitrary substring and Chinese search; deletion reduces to a single SQL transaction, dropping from hours to milliseconds without stalling recording in the meantime. Added streaming download of large bodies and a full-text parameter for `traffic_search`; legacy file-tree data needs no migration and remains readable, searchable and deletable.

### Tasks and assets

#### Added

- Task creation gains an "Asset coverage feature" switch (on by default): when off, coverage is not computed or displayed, the situation graph shows only assets, `task_scope` is not accumulated automatically, and the related tools are removed from the Planner / main agent; company linking is unaffected by this switch.
- `insert_assets` gains a per-asset `related` flag (default `true`): it takes effect only when coverage is on, and `false` adds the asset only to the shared asset store without counting it toward this task's coverage (e.g. sibling sites or unrelated assets found in passing).
- Company scope and task test assets uniformly support text recognition of domains, URLs, IPs, CIDRs, ICP filings and keywords; domains/IPs can create or reuse global assets, while CIDRs/ICP filings/keywords are kept as task scope context.

### Build

- `build.sh --release` builds Linux amd64/arm64, macOS amd64/arm64 and Windows amd64 in one go and produces zip release packages containing `skills/`, config examples and the README.
- Debug info is stripped with the Go linker and zip release packages are generated; UPX becomes explicitly opt-in, because its self-extracting ELF segfaults at startup in some Linux environments.
- The Release Workflow runs a startup smoke test on the Linux amd64 binary and ships `SHA256SUMS` with the release packages.

### Contributors

- [@neouks](https://github.com/neouks)

## [0.3.2] - 2026-08-20

### Added

- A new task can link to multiple existing tasks, inheriting live and read-only the facts, findings, completed intents, asset scope and blackboard context of its direct source tasks; the new task still uses its own execution queue, working directory and session history.
- Tasks support an ordered LLM config chain. When a provider quota shortfall is clearly identified, the next config is switched to automatically, and the current config, exhaustion state and structured audit activity are persisted.
- Running and paused tasks support editing the LLM config chain, reordering it and manually switching the current config; automatic switches, manual switches and whole-chain exhaustion are announced on the task detail page.
- A running Worker can be paused, resumed and canceled. Pausing keeps the blackboard data, while canceling transactionally cleans up the intent and the facts, findings and execution records it directly produced once the Worker has stopped writing.
- Deleting a task can optionally also clean up linked assets, traffic, findings and the task's on-disk files, with a deletion barrier, concurrency protection and auditable deletion stats added.
- Added an optional task concurrency limit; new tasks beyond the limit queue FIFO and start automatically once a run slot is released.
- Added task asset pagination, company asset pagination, task category stats and a paginated intent mock contract.
- Added the `build.sh` single-binary build script, supporting static frontend export, resource embedding, cross-platform targets and build version injection.

### Changed

- The task-level explicit LLM config chain coexists with the existing global LLM Pool; an explicit chain keeps strict quota-failover semantics, and without one the Agent binding and global config rules apply as before.
- The main agent input box supports auto-growing multi-line input, `Enter` to send and `Shift+Enter` for a newline, and avoids accidental sends while a Chinese IME is composing.
- Planner, Worker and main agent share a task-level LLM runtime; a task chain uses the smallest context window among its candidate models as the safe compaction threshold.
- The task detail page shows running, idle and paused states according to the real LLM call state; facts, findings, intents, asset references and graph nodes from source tasks are uniformly marked with their source and kept read-only.
- Agent preview, task reports and related detail views uniformly use the shared Markdown rendering component.
- LLM model configuration uses card and drawer interactions, and the model list scrolls independently inside the drawer.
- Pausing a task no longer blocks the main agent conversation: main agent orchestration sessions are independent of task pause, and new messages can still be sent while paused (pausing only terminates the round currently in progress).
- The default wall-clock duration of a single Worker run changes from 600 to 1200 seconds.

### Fixed

- Removed optimistic echo from the main agent console in favor of rendering purely from server data, fixing garbled messages and content from other sessions bleeding in after a pause, a failed send and similar cases.
- Opening a main agent session scrolls to the bottom by default and shows the last reply in full: once the last reply's full content lazy-loads and expands, the view sticks to the bottom automatically instead of being pushed off screen.
- Fixed the main agent session continuing to run after a task was paused, and the main agent not being stopped in sync when the orchestration Agent paused a task.
- Fixed the task status badge and action button states being out of sync when a task completes or while the Planner, Worker or main agent is actually running.
- Fixed late blackboard writes or leftover files that could arise between Worker cancellation, task deletion and concurrent writes.
- Fixed broken Markdown preview, long-text overflow in delete confirmations, mobile width, and misaligned buttons on parts of the task detail page.
- Added named termination reasons to all Agent cancellation paths; activity details can show who canceled, the final state, rounds run, elapsed time, token usage and unreturned tool calls.
- Fixed a race at backend shutdown where the parent context could preempt and overwrite the named `shutdown` reason, and garbled text caused by truncating Chinese activity summaries by bytes.
- Fixed cancellation events with partial streaming output losing the real termination reason, while keeping the content generated before the cancel.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@neouks](https://github.com/neouks)

[Unreleased]: https://github.com/Autumn-27/ARTEX/compare/v0.3.10...HEAD
[0.3.10]: https://github.com/Autumn-27/ARTEX/compare/v0.3.9...v0.3.10
[0.3.9]: https://github.com/Autumn-27/ARTEX/compare/v0.3.8...v0.3.9
[0.3.8]: https://github.com/Autumn-27/ARTEX/compare/v0.3.7...v0.3.8
[0.3.7]: https://github.com/Autumn-27/ARTEX/compare/v0.3.6...v0.3.7
[0.3.6]: https://github.com/Autumn-27/ARTEX/compare/v0.3.5...v0.3.6
[0.3.5]: https://github.com/Autumn-27/ARTEX/compare/v0.3.4...v0.3.5
[0.3.4]: https://github.com/Autumn-27/ARTEX/compare/v0.3.3...v0.3.4
[0.3.3]: https://github.com/Autumn-27/ARTEX/compare/v0.3.2...v0.3.3
[0.3.2]: https://github.com/Autumn-27/ARTEX/compare/v0.3.1...v0.3.2
