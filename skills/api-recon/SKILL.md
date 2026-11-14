---
name: api-recon
description: Use this skill when collecting a website's API endpoints.
---

# API Recon (front-end endpoint reconnaissance)

Under **explicit authorization**, discover as completely as possible: **back-end APIs** (paths, methods, parameters, response bodies), **front-end routes**, and **UI feature triggers** (tabs, dialogs, table actions, and so on).

---

## Boundaries and prohibitions (required reading)

This skill does **API / parameter-surface reconnaissance only**. It is not a vulnerability-hunting or exploitation phase. Breaking a rule below puts you out of bounds.

### Task boundaries

| Scope | Allowed | Forbidden |
|---|---|---|
| **Goal** | Enumerate paths, methods, parameters, routes, UI triggers | SQLi/XSS/privilege-escalation/brute-force/fuzzing for vulnerabilities, request-tampering attacks, destructive operations |
| **Authentication** | Hook + stub/mock to get past the **client-side** login gate | Asking the user for, or guessing, account passwords; attempting a real login form submission |
| **Runtime** | Hook endpoints without credentials and use mock responses to get the SPA into its post-login shell | Flows that can only continue with a real back-end session |

### Credential-less dynamic analysis (Phase 3 default)

1. Use `preload.js` / `runtime_harvest.js` to **intercept and stub** bootstrap endpoints such as login, permissions and menus;
2. For business query endpoints, return a mock body that is **structurally correct, has a success business code, and may contain empty data**;
3. This lets the front end render post-login pages with no back end, or in a 401 environment, which triggers more XHR/fetch/WebSocket calls;
4. **Empty data, blank tables and placeholder UI are all expected** — do not switch to real login or vulnerability testing because of them.

**In one sentence**: use mocks to open up front-end routes and component mounting, and **record outbound requests only**; what the back end returns does not matter, what matters is **which other endpoints the front end goes on to call**.

### Hard process prohibitions

| Forbidden | Do this instead |
|---|---|
| grep/curl/Read the main entry `index-*.js` to extract API paths before Phase 1 is complete | Run `OUTDIR/harvest_static.py` |
| Hand-writing `extract_apis.py` or similar scripts as a substitute for harvest | Edit `OUTDIR/harvest_static.py` and re-run |
| Repeating the same grep/command after it has failed ≥2 times | Change strategy: read tool_logs, edit harvest, check the reference |
| Skipping gate A/B and running the original `scripts/` directly | Copy to OUTDIR and adapt to the target |
| Authenticating with a real username/password, OTP, OAuth, etc. | Stub/mock (see above) |
| Skipping stubs "to get real data" and doing privilege-escalation/injection tests | Record outbound only; that is the recon boundary |
| Irreversible operations such as deleting, exporting sensitive data, bulk writes | The same applies to coverage clicks |
| Claiming to have all pages and endpoints without completing runtime + dynamic enumeration | See "Definition of done" or state the limitation |
| Claiming to know all parameters without completing the parameter trigger matrix + diff | Phase 3b matrix + Phase 5 diff |
| Inferring required/optional from a single runtime sample | Multi-sample diff, or infer from validation rules/errors |

---

## Two-layer model + run modes

| Layer | Output | Ceiling |
|---|---|---|
| **Static** (JS bundle) | Full set of endpoint paths, draft routes, candidate fields at request-building sites | No HTTP method; parameters need Phase 1b; misses URLs concatenated at runtime |
| **Runtime** (live session) | Method + body + response + dynamic URLs + WS/SSE; multi-sample diff completes the parameters | A page must actually render before it sends requests; one sample is not enough to decide required/optional |

| Run mode | Engine | Best for |
|---|---|---|
| **depth** | `runtime_harvest.js` (Puppeteer) | API inventory, METHOD/params/response bodies, WS/SSE, reproducible batch runs |
| **coverage** | browser + `preload.js` | Clicking tabs/dialogs/tables for deeper feature coverage |
| **both** | depth first, then coverage | Most complete, takes longest |

**Parameter methodology** (there is no universal script): use harvest/regex for paths; for parameters use **anchor-window expansion + UI binding chain + multi-sample diff + error back-inference** (grep recipes are in [reference.md](reference.md) section J).

---

## Definition of done

Recon may be called complete only when all of these hold:

- [ ] **Static**: Phase 1 harvest produced `api_static.txt`, `routes.txt`, `js/`
- [ ] **Runtime**: at least one of depth or coverage; coverage/both require **hooks in effect + the dynamic enumeration loop**
- [ ] **In the shell**: visiting a business path does not land on `/login` (watch for hash routing)
- [ ] **Parameters**: coverage/both completed the parameter trigger matrix + `param_samples.json`; Phase 5 merged `params_merged.json`
- [ ] **Depth** (if module pages are blank): Phase 4 permission-tree reconstruction and a re-run, until **module-level APIs** appear (not just locale/bootstrap)
- [ ] **Delivery**: all Phase 5 outputs present (see the Phase 5 output table); `insert_assets` has written the service and endpoint assets

---

## Scripts and gates

`scripts/` contains reference templates only. Running the originals unchanged and treating the result as final is **forbidden**.

**Rule**: read first → adapt to the target → write into `OUTDIR` (for example `recon/`) → record changes in `CHANGES.md`; if a script does not fit, rewrite it following the methodology and borrow only the structure.

| Gate | When | Reference script → OUTDIR copy | Commonly required changes |
|---|---|---|---|
| **A (static)** | After Phase 0, before the **first** harvest/spider run | `harvest_static.py` / `spider_mpa.py` | **The default regex runs as is on most sites**; only when the manifest/dialect does not match, change the endpoint regex, webpack/Vite `publicPath`, MPA exclude/cookie |
| **B (runtime)** | After Phase 2, before running depth/coverage | `runtime_harvest.js` / `preload.js` + `config.json` | Cookie/localStorage keys, neutralize success value, stubs, login regex, api prefix, hash/history |

**Mandatory SPA order** (not interchangeable; Phase numbering takes priority over "explore first, script later"):

| Step | Required | Forbidden |
|---|---|---|
| After Phase 0 | Next Bash command = `python3 OUTDIR/harvest_static.py <URL> OUTDIR` | curl/grep/Read the main entry `index-*.js` (usually >500KB) |
| Gate A | Copy the script → small edits if needed → **run immediately** | Extracting APIs by hand first and then deciding whether to harvest |
| Before Phase 1 completes | Verify output with `wc -l`; on 404, edit harvest and retry | Hand-written extract scripts; repeatedly grepping URLs that were never downloaded |
| From Phase 1b | grep only `OUTDIR/js/*.js` | Using the main bundle in place of harvest |

- ✅ Copy `harvest_static.py` → (optional) edit the regex → **run immediately**
- ❌ curl the main bundle → grep several times → write a temporary extract → harvest only at the end
- **MPA**: after Phase 0 the next Bash command = `python3 OUTDIR/spider_mpa.py ...`

---

## Tool and output constraints

| Constraint | Detail |
|---|---|
| Large files | `index-*.js` over 100KB must **not** be Read/grepped into context; batch-process with the OUTDIR scripts |
| grep output | Must use `\| head -20` or `-m 5`; keep only path summaries in the conversation, do not paste bundle fragments |
| Verification | Use `wc -l`, `ls \| wc -l`; do not Read whole directories |
| Initial regex probe | Optional, at most once, only on small chunks ≤50KB or HTML; harvest is authoritative for static results |
| reference | Recipes/templates/troubleshooting are in [reference.md](reference.md); do not repeat the full text inline |

---

## Execution roadmap

```
Phase 0 classify + OUTDIR
  → Gate A → Phase 1 harvest (★ run immediately ★)
  → Phase 1b parameter reverse-engineering
  → Phase 2 three authentication gates → config.json
  → Gate B → Phase 3 runtime + parameter matrix
  → Phase 4 permission tree (if needed) → re-run Phase 3
  → Phase 5 merged report + insert_assets: bulk-insert every discovered service and endpoint API asset; no discovered asset may be left out of the insert under any circumstances
```

Tick these off in order; **do not enter the next Phase until the previous item is complete**.

1. [ ] **Phase 0**: initial SPA/MPA probe; create `OUTDIR` → [Phase 0](#phase-0--classification)
2. [ ] **Gate A + Phase 1**: copy the script → harvest **immediately** → verify with `wc -l` → [Phase 1](#phase-1--static)
3. [ ] **Phase 1b**: anchor-window expansion + binding layer → `param_candidates.json` → [Phase 1b](#phase-1b--parameter-reverse-engineering)
4. [ ] **Phase 2**: three authentication gates → `config.json` → [Phase 2](#phase-2--three-authentication-gates)
5. [ ] **Gate B**: adjust the runtime scripts → [Phase 3](#phase-3--runtime)
6. [ ] **Phase 3**: depth / coverage / both; confirm you are in the shell; parameter trigger matrix → `param_samples.json`
7. [ ] **Phase 4** (if needed): permission tree → patch stubs → re-run Phase 3 → [Phase 4](#phase-4--permission-tree-reconstruction)
8. [ ] **Phase 5**: merged outputs + report + `insert_assets` → [Phase 5](#phase-5--merge-and-report)

---

## Phase 0 — Classification

Fetch the entry HTML and **create `OUTDIR`** (do not modify `scripts/` inside the skill):

- **SPA**: empty shell + `<div id=app>` + chunks → Phases 1–5
- **MPA**: SSR + `<form>`, no endpoint bundle → after gate A:

```bash
python3 recon/spider_mpa.py <BASE_URL> <OUTDIR> [--cookie "session=..."] [--max 300] [--depth 5] [--exclude "logout|delete|destroy"]
```

Produces `forms.txt`, `links.txt`, `api_inline.txt`. For an SPA, if forms ≈ 0 → switch to Phase 1.

---

## Phase 1 — Static

Follow [Scripts and gates](#scripts-and-gates) · [Tool and output constraints](#tool-and-output-constraints).

```bash
python3 recon/harvest_static.py <BASE_URL> <OUTDIR>
```

harvest: parse the HTML scripts → webpack/Vite manifest → download every lazy chunk → produce `js/`, `api_static.txt`, `routes.txt`, `chunkmap.txt`.

```bash
wc -l OUTDIR/api_static.txt OUTDIR/routes.txt
ls OUTDIR/js | wc -l
```

- Chunk count vs manifest: on 404, edit harvest and retry; do not curl chunks one by one by hand
- `api_static.txt` too small → loosen the endpoint regex inside OUTDIR and re-run (see reference)

### Phase 1b — Parameter reverse-engineering

Paths come from Phase 1; parameter fields need their own recon. grep rules are in [Tool and output constraints](#tool-and-output-constraints).

**Completion criterion**: for the important endpoints you can state the field name, where it is transported, the inferred type, whether it is required, a sample value, and the confidence.

#### 1b.0 — Transport shape

| Shape | Where the parameters are | Look here first, statically |
|---|---|---|
| REST JSON | body + query | `(params\|data\|body)\s*:\s*\{` next to the path anchor |
| GraphQL | `variables` | gql templates, `$page: Int` |
| Traditional form | urlencoded | `<form>`, `FormData` |
| File upload | multipart | `FormData.append` |
| Path parameters | `/user/:id` | Route table + `useParams` / `$route.params` |
| Encrypted/signed | Wrapped into `sign`/`data` | Hook the encryption function's input (reference section D) |

Output: annotate each endpoint with `transport: query|json|form|graphql|encrypted`.

#### 1b.1 — Anchor-window expansion

Take a known path as the anchor and widen the window to find the object that builds the request:

```bash
grep -n '"/api/user/list"' OUTDIR/js/*.js | head -20
grep -rhoaE '.{0,120}("/api[^"]+").{0,200}' OUTDIR/js/*.js | head -20
grep -rhoaE '(params|data|body|payload)\s*:\s*\{' OUTDIR/js/*.js | head -20
```

| Wrapper layer | Parameter clue |
|---|---|
| axios instance | `data` / `params` |
| Unified request helper | Global fields injected by an interceptor |
| OpenAPI client | Generated method signatures |
| React Query / SWR | The hook's second argument |
| Vue composable | The composable's arguments |

Type remnants: `yup`/`zod`/rules, `Form.Item name=`, embedded Swagger.

→ `param_candidates.json`: `{ path, fields[], source: "static-callsite", confidence }`

#### 1b.2 — Binding layer

```
Form field → onFinish/handleSubmit → transform → API payload
```

| Binding source | Technique |
|---|---|
| Form submit | Follow submit → transform → API |
| Table search | `getFieldsValue()` → `params` |
| Route | `:id` / `?tab=` |
| Interceptor | Global `tenantId`, pagination, sign |
| Enum select | `options` → API enum values |

In DevTools, trace the call stack upward from `fetch`/`XHR.send` to the function that builds the request.

#### 1b.3 — Three request-building questions (≠ the three authentication gates of Phase 2)

| Question | What to answer |
|---|---|
| **Assembly** | Where the payload is built, traces of transforms |
| **Validation** | required, pattern, enum |
| **Transport** | path / query / body / multipart / headers |

At the interceptor gate (Phase 2), also read the globally injected fields (Authorization, `X-Tenant-Id`, sign).

#### 1b.4 — Hand-off to Phase 3

Candidate fields come from the static/binding layers; **required/optional/conditional dependencies** need the Phase 3 parameter matrix + diff + Phase 5 error back-inference.

---

## Phase 2 — Three authentication gates

grep in `OUTDIR/js/` (with `head`) and write the findings into `config.json` (recipes in reference):

| Gate | Question | Keywords |
|---|---|---|
| **Render gate** | How does the app decide the user is logged in? | `isLogin`, `getToken`, Cookie/localStorage |
| **Interceptor gate** | What triggers the redirect to `/login`? | `response_code`, `errno`, axios interceptor |
| **Content gate** | Where do menus/permissions come from? | `menu`, `permission`, `role`, `acl`, `routes` |

Do not treat a localStorage key name as a credential — confirm it from the chunk/request chain.

**Exit = gate B**: record the conclusions in `config.json`, and edit `OUTDIR/runtime_harvest.js` / `preload.js`.

### Phase 2b — API observation (optional)

Use `preload.js` inside OUTDIR to confirm session key names, Authorization, and nested API URLs:

| Setting | Output |
|---|---|
| `recordDetail: true` | `__API_RECON_DETAIL__` |
| `observe.xhrHeaders: true` | Header observations |
| `extractUrlsFromResponse: true` | Child APIs found inside responses |
| `observe.storageReads/cookieReads: true` | Back-fill config |
| `neutralizeVueRouter: true` | `__API_RECON_ROUTES__` |

In coverage, export each round: `__API_RECON_LOG__`, `__API_RECON_DETAIL__`, `__API_RECON_ROUTES__`, `__API_RECON_OBSERVE__`.

---

## Phase 3 — Runtime

Gate B must already be passed; follow [Boundaries and prohibitions](#boundaries-and-prohibitions-required-reading) and the credential-less mock strategy.

Set `"runtimeMode": "depth" | "coverage" | "both"` in `config.json` (template in reference).

### Hooks and stubs (shared by depth + coverage)

| Layer | Scope | Purpose |
|---|---|---|
| L1 exact | auth/permission/bootstrap stubs | Pass first-screen authentication |
| L2 negative correction | All JSON responses | Not-logged-in code → success |
| L3 fallback | `/api` etc. not matched by L1 | Empty success body to open up the UI |

- **depth**: fake auth + `forward` with rewritten business code + `stubs`; iterate over `routes` (hash/history); produces `runtime_api.json`
- **coverage**: inject `preload.js` at **document-start** (CDP `addScriptToEvaluateOnNewDocument` or a userscript)

Verify: `window.__API_RECON_PRELOAD__` exists; business paths do not bounce back to `/login`.

```bash
# puppeteer-core is preinstalled in the image (NODE_PATH). Outside the image, copy
# package.json + package-lock.json + .npmrc from scripts/ and install from the lock:
cd recon && { node -e "require('puppeteer-core')" 2>/dev/null || npm ci --ignore-scripts; }
node runtime_harvest.js config.json
```

### 3b — coverage dynamic enumeration (mandatory)

1. Main navigation/sidebar — click every item, wait 1–3s for the network
2. Tabs — `role=tab`, `.ant-tabs-tab`
3. Tables — view/edit/detail on the first row
4. Toolbar — export, filter, create (**avoid irreversible deletes**)
5. On entering each module — merge APIs/routes
6. SPA — controlled `pushState` to paths in `routes.txt` not yet covered (forbidden for MPA)

**Parameter trigger matrix** (mandatory): record each module once per operation type and **diff the samples**:

| Operation | Parameters that usually appear |
|---|---|
| List first screen | Pagination + default filters |
| Click search | keyword, filter |
| Advanced filter | More optional fields |
| Create/edit | The full entity |
| Bulk/export/sort | `ids[]`, `exportType`, `sortField` |

**Under stubs the outbound body/headers are still real** — the request is authoritative. Record → `scan_raw.json`, `param_samples.json`, `api_detail.json`.

- **Vue**: `neutralizeVueRouter: true` + document-start preload
- **React**: `routes.txt` + sidebar clicks + `pushState`
- **both**: 3a depth first, then 3b coverage

---

## Phase 4 — Permission-tree reconstruction

**Trigger**: module pages are blank / every route shows only bootstrap calls (such as locale) → the content gate has not been passed.

| Symptom | Meaning |
|---|---|
| Got into the shell | Render gate + interceptor gate passed |
| Sidebar items missing / blank on click | Stub shape or permission codes incomplete |
| Every route has the same, very few APIs | `v-if permission` did not pass |
| `routes.txt` far smaller than the bundle | Must be completed from the auth module |

```bash
grep -rhoaE '"/api[^"]*(permission|perm|role|menu|acl)[^"]*"' OUTDIR/js/*.js | sort -u | head -30
grep -rhoaE 'userRouteAuth|getResultTree|routeMap|routeLink|menuList|authList' OUTDIR/js/*.js | head -20
```

Typical chain: `role_permissions` (flat codes) + `permissions/all` (tree) → `getResultTree` → `userRouteAuth[CODE].url`.

```bash
python3 recon/extract_route_map.py recon/js recon/
python3 recon/build_perm_tree.py recon/js recon/ --config recon/config.json
```

Intermediate outputs: `route_map.json`, `userRouteAuth.json`, `permissions_tree.json`, `*_stub.json`, `perm_codes_all.txt`.

Stub checks: the outer `response_code` matches the interceptor gate; flat codes line up with the tree; `routes` covers every link in `route_map`.

After updating `config.json`, **re-run Phase 3**. For large SPAs you can tune `waitUntil`, `routeTimeout`, `perRouteMs` (see reference sections A3/I).

---

## Phase 5 — Merge and report

### Output table

| File | Phase | Contents |
|---|---|---|
| `js/`, `api_static.txt`, `routes.txt`, `chunkmap.txt` | 1 | Static bundles and paths |
| `param_candidates.json` | 1b | Static candidate parameter fields |
| `config.json` | 2 | The three gates + runtime configuration |
| `runtime_api.json` | 3a | Detailed depth recording (including WS/SSE) |
| `param_samples.json`, `scan_raw.json`, `api_detail.json` | 3b | Multiple samples, click log, detail |
| `route_map.json` etc. | 4 | Permission-tree intermediate files (if run) |
| `params_merged.json` | 5 | Merged parameter fields + confidence |
| `api_merged.txt` | 5 | `METHOD /path [params] [static\|runtime\|both]` |
| `site_map.json` | 5 | Routes, APIs, params, feature points, limitations |
| **insert_assets** | 5 | Write all service and endpoint assets to the asset store |

### 5b — Parameter merge

Diff from `param_samples.json`; **there is no universal merge script**. Confidence rules are in reference J7 (high/medium/low/pending trigger).

### 5c — Error back-inference

Within the authorized scope you may send incomplete requests and read the 400 (**this is parameter recon, not vulnerability testing**): `field 'x' is required`, enum errors, and so on. Watch for `data` wrappers, `variables`, and `bizData` before encryption.

The report must state: runtimeMode, the number of static/runtime APIs, parameter confidence, modules not covered, and a `CHANGES.md` summary relative to the reference scripts.

Suggested structure for `site_map.json`:

```json
{
  "site": "https://example.com",
  "runtimeMode": "both",
  "appType": "vue-spa",
  "routeGuardStrategy": ["nav-neutralize", "L1-auth", "L2-patch", "forward"],
  "apisFromStatic": [],
  "apisFromRuntime": [],
  "apis": [],
  "params": [{ "method": "POST", "path": "/api/user/list", "transport": "json", "fields": [] }],
  "frontendRoutes": [],
  "routesVerifiedByClick": [],
  "featuresTriggered": [],
  "limitations": ""
}
```

More fields and grep recipes are in [reference.md](reference.md).

---

## General notes

- **Framework-agnostic**: the method is the same for webpack/Vite/Angular lazy loading
- **Transport**: REST/JSON, GraphQL, WebSocket, SSE; gRPC-web is out of scope
- **SSR**: client-side fetches can be recorded; RSC/Server Actions cannot be fully enumerated
- **Blind spots**: JSVMP, WASM, strong HMAC/mTLS verification → static only + state the limitation
- **Parameter blind spots**: conditional linkage, hidden params, request building in WASM → mark "pending trigger" / "unreachable"
- **Static is the safety net**: when runtime is blocked, static can still enumerate endpoints

---

## Additional resources

- Grep recipes, the `config.json` template, troubleshooting, hooks, parameter reverse-engineering (section J), the site_map template: **[reference.md](reference.md)**
- Reference script paths are in the [Scripts and gates](#scripts-and-gates) table
