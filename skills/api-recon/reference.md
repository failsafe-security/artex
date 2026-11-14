# api-recon — Reference manual

Grep recipes, the `config.json` template, and troubleshooting. All greps run against the `js/` directory. When a bundle is on a single line, run `js-beautify` or `sed 's/}/}\n/g'` first; usually a raw grep with a context window is enough.

## About the scripts

Every file under `scripts/` is a **reference template** and must be adapted to the target site before it is run. Typical change points:

| Script | Commonly adjusted |
|---|---|
| `harvest_static.py` | endpoint regex, webpack/Vite manifest parsing, micro-frontend publicPath, retry/concurrency |
| `runtime_harvest.js` | neutralize field names and success value, stub match rules and body structure, routes source, WS recording, `waitUntil`/`routeTimeout`/`proxy` |
| `preload.js` | `loginPathRe`, L1 stubs, `neutralize.fields`, `apiPattern`, whether to enable L3, `recordDetail`, `observe.*`, `neutralizeVueRouter` |
| `spider_mpa.py` | `--exclude` destructive links, cookie, depth/max, same-origin filtering |
| `extract_route_map.py` | `routeMap` / `routeLink` regex, KEY naming pattern |
| `build_perm_tree.py` | `userRouteAuth` parsing, `ROOTS`/`PREFIX_PARENT` hierarchy heuristics, stub outer field name |
| `config.json` | the single entry point for all of the site-specific parameters above |

Put the adjusted files in the task working directory (for example `recon/`), and note the specific changes relative to the reference scripts in the report.

---

## A. Reverse-engineering the three gates

### A1. Render gate — "How does the app decide the user is logged in?"

```bash
grep -rhoaE '.{0,40}(isLogin|isAuthenticated|loggedIn|hasLogin|requireAuth)\b.{0,80}' js | head
grep -rhoaE 'function (getUser|getToken|getAuth)[0-9]?\([^)]*\)\{.{0,200}' js | head
grep -rhoaE '(localStorage|sessionStorage)\.getItem\("[^"]+"\)' js | sort -u
grep -rhoaE '(Cookies?|cookie)\.(get|load)\("[^"]+"\)' js | sort -u
grep -rhoaE '\batob\(|JSON\.parse\(|jwt|decode' js | head
```

Follow the chain `isLogin = f(getUser())` → `getUser = decode(storage.read(KEY))` and determine the **storage key**, the **container** (Cookie vs localStorage), and the **encoding**:

| Encoding | How to forge it in config |
|---|---|
| Plain string / `"1"` / token | `"value": "anything-truthy"` |
| `JSON.parse(x)` | `"value": "json:{\"id\":1,\"username\":\"admin\"}"` |
| `JSON.parse(atob(x))` | `"value": "b64json:{\"id\":1,\"username\":\"admin\"}"` |
| JWT | Unsigned/`alg:none` JWT, or sign with a key found in the bundle |
| Encrypted (SM2/AES/RSA) | Find a hard-coded key; you can forge when the render gate only needs a decodable blob, otherwise fall back to static |

→ Write into `cookies` / `localStorage`.

### A2. Interceptor gate — "What triggers the redirect to /login?"

```bash
grep -rhoaE '.{0,60}(interceptors\.response|axios|request\.use).{0,120}' js | head
grep -rhoaE '.{0,40}(response_code|errcode|errno|\bcode\b|\bret\b|\bstatus\b)\s*[=!]==?\s*[\-0-9]{1,4}.{0,60}' js | head -20
grep -rhoaE '.{0,40}(未登录|请重新登录|登录已过期|unauthorized|登录失效|授权|token.{0,10}invalid).{0,40}' js | head
grep -rhoaE '.{0,30}(location\.href|router\.(push|replace)|navigate)\([^)]*login[^)]*\)' js | head
```

Determine: the **field name**, the **success value** (usually `0` or `200`), and the **failure value that triggers the redirect**. Verify with a junk session:

```bash
curl -sk -X POST -H 'Cookie: <fakekey>=junk' https://target/api/<protected> -d '{}' -H 'Content-Type: application/json'
```

→ Write into `neutralize.fields` + `neutralize.success`.

### A3. Content gate — "Where do menus/permissions come from?"

```bash
grep -rhoaE '"/api[^"]*(permission|perm|role|menu|acl|resource|nav)[^"]*"' js | sort -u
grep -rhoaE '.{0,30}(menus|permissions|menuList|routeList|authList|role_permissions)\b.{0,120}' js | head
grep -rhoaE 'userRouteAuth|getResultTree|routeMap|routeLink|hasPermission|checkAuth' js | head
grep -rhoaE '([A-Z_][A-Z0-9_]*):\{name:"[^"]*",link:"/[^"]+"\}' js | head
```

**Two layers of data** (common in enterprise back offices):

| API | Typical payload | Consumer |
|---|---|---|
| `.../role_permissions` | `{ permissions: string[], role_type }` | Route guards, button-level ACL |
| `.../permissions/all` | `tree[{ code, position, children }]` | Sidebar menu rendering |
| `userRouteAuth` in the bundle | `{ CODE: { url, name? } }` | code → front-end path |
| `routeMap` in the bundle | `{ KEY: { name, link } }` | Alias resolution (webpack `o.DASHBOARD`) |

Read the consumer code to confirm: how `getResultTree(tree, permissions)` filters, and which field `v-if` / `hasAuth(code)` checks.

**Forge by hand** (small sites): build a permissive payload → `stubs`.

**Full permission-tree reconstruction** (large sites where the sidebar/sub-modules are still blank): see **section I**.

---

## B. config.json template

```json
{
  "baseUrl": "https://target/",
  "runtimeMode": "both",
  "chromium": "/usr/bin/chromium",

  "cookies": [
    { "name": "auth", "value": "b64json:{\"id\":1,\"username\":\"admin\",\"role\":\"admin\",\"func\":{},\"permissions\":[\"*\"]}" }
  ],
  "localStorage": { "token": "faketoken", "isLogin": "1" },

  "neutralize": {
    "fields": ["response_code", "code", "errno", "ret", "status"],
    "success": 0,
    "flags": { "success": true, "message": "ok" }
  },
  "forward": true,
  "loginUrlPattern": "/login",
  "apiPattern": "/api/|/rest/|/graphql",

  "mockTier": "L1+L2",
  "recordDetail": true,
  "observe": {
    "storageReads": false,
    "cookieReads": false,
    "xhrHeaders": true
  },
  "neutralizeVueRouter": true,
  "stubs": [
    {
      "match": "permissions/all|/menu|role_permissions",
      "body": {
        "response_code": 0, "code": 0,
        "data": {
          "permissions": ["*"],
          "menus": [
            { "name": "dashboard", "path": "/dashboard", "show": true, "children": [] },
            { "name": "alert", "path": "/alert", "show": true, "children": [] }
          ]
        }
      }
    }
  ],

  "explore": {
    "clickTabs": true,
    "clickTables": true,
    "pushStateFallback": true,
    "maxMenuItems": 50
  },

  "routes": ["/dashboard", "/alert", "/asset", "/device", "/report", "/config", "/system"],
  "waitMs": 1500, "perRouteMs": 900, "headless": true,
  "waitUntil": "domcontentloaded",
  "routeTimeout": 12000,
  "proxy": "",

  "captureResponses": true, "recordWs": true, "respMax": 600
}
```

Field notes:
- `runtimeMode`: `depth` (Puppeteer), `coverage` (browser MCP), `both`
- `cookies[].value` prefixes: `b64json:` → base64(JSON); `json:` → raw JSON; no prefix → literal
- `forward: true` forwards the real request and rewrites the code field; `false` is a fully offline stub
- `mockTier`: which tiers the coverage-mode preload enables, such as `L1+L2`, `L1+L2+L3`
- `routes` comes from `routes.txt`; after you forge the menu the harness appends `<a href>` automatically
- `captureResponses` / `recordWs` apply to depth mode only
- `waitUntil`: use `domcontentloaded` for large SPAs to avoid `networkidle2` hanging
- `routeTimeout`: per-route `page.goto` timeout (milliseconds)
- `proxy`: Puppeteer `--proxy-server`; `HTTP_PROXY` / `HTTPS_PROXY` also work

### B1. Double-stub template (role_permissions + permissions/all)

```json
"stubs": [
  {
    "match": "role_permissions",
    "body": {
      "response_code": 0,
      "data": {
        "permissions": ["MONITOR", "MONITOR_ALERT", "THREAT", "ASSETS_RISK"],
        "role_type": "SUPER_ADMIN"
      }
    }
  },
  {
    "match": "permissions/all",
    "body": {
      "response_code": 0,
      "data": [
        {
          "code": "MONITOR",
          "position": 1,
          "children": [
            { "code": "MONITOR_ALERT", "position": 1, "children": [] }
          ]
        }
      ]
    }
  }
]
```

The outer field names (`response_code` / `code` / `data`) must match the A2 interceptor gate; `permissions` must cover every leaf code in the tree.

---

## C. Coverage mode: preload configuration

Edit the `CONFIG` object at the top of `scripts/preload.js`, or replace it before injecting via CDP:

```javascript
const CONFIG = {
  loginPathRe: /\/(login|signin)(\/|$|\?)/i,
  mockTier: 'L1+L2',
  forward: true,
  recordDetail: true,
  extractUrlsFromResponse: true,
  neutralizeVueRouter: true,
  observe: { storageReads: false, cookieReads: false, xhrHeaders: true },
  neutralize: { fields: ['response_code', 'code'], success: 0 },
  stubs: [ /* same as config.json stubs */ ],
  apiPattern: /\/(api|apis|v\d+|dev|internal|graphql)\//i,
};
```

Verify: `window.__API_RECON_PRELOAD__ === true` and the pathname is stable.

Export the recording:

```javascript
JSON.stringify({
  apis: [...window.__API_RECON_LOG__],
  detail: window.__API_RECON_DETAIL__,
  routes: [...(window.__API_RECON_ROUTES__ || [])],
  observe: window.__API_RECON_OBSERVE__,
}, null, 2)
```

---

## D. preload / runtime hook capabilities

The browser hooks built into preload (coverage) and runtime_harvest (depth), and their coverage:

| Hook capability | Value for API discovery | Coverage |
|---|---|---|
| Hook fetch / XHR.open | Records request URL/method | ✅ `recordDetail` + `__API_RECON_LOG__` |
| Hook XHR.setRequestHeader | Discovers headers such as Authorization | ✅ `observe.xhrHeaders` |
| Hook localStorage/cookie reads | Confirms session key names | ⚠️ optional `observe.storageReads/cookieReads` |
| Get Vue routes | Completes frontendRoutes | ✅ `__API_RECON_ROUTES__` (already-loaded routes) |
| Neutralize Vue route guards / block login redirects | Opens modules up to trigger APIs | ✅ `neutralizeVueRouter` + native navigation neutralization |
| Get React routes | Supplements routes | ⚠️ static + clicking; no dedicated hook |
| Block page navigation (login path) | Stays on the page to analyze | ⚠️ blocks the login path only, to avoid blocking business navigation |
| Hook crypto libraries (CryptoJS/SM etc.) | Encrypted params → plaintext API body | ❌ must hook the crypto function's input by hand; record the conclusion in config |
| Anti-debug bypass | Otherwise runtime records no APIs | ❌ must be handled by hand; static still works |

---

## E. Endpoint extraction regex (when static is too sparse)

Loosen `extract_endpoints` in `harvest_static.py`, or by hand:

```bash
grep -rhoaE '"/[a-z][A-Za-z0-9_/\-]{3,}"' js | sort -u
grep -rhoaE '/api/[a-zA-Z0-9_./-]+' js | sort -u
```

---

## F. Troubleshooting

| Symptom | Cause → action |
|---|---|
| Very few static APIs | Endpoint dialect does not match → loosen the regex (section D) |
| Chunk count ≪ manifest | CSS-only or undeployed chunks; 404s already retried |
| Runtime still shows the login page | Render gate wrong → recheck A1: key name, container, encoding, domain |
| In the shell but modules blank | Content gate → forge the menu (A3); the `routes` paths may be wrong |
| Every route has only bootstrap/locale | Permission codes incomplete → section I permission-tree reconstruction; check the `role_permissions` + `permissions/all` double stub |
| Sidebar has items but sub-pages are blank | Tree is missing intermediate nodes, or the code does not match `userRouteAuth` |
| Every API redirects to login | Interceptor gate → confirm `neutralize`; nested fields need the walk logic extended |
| WS frames are 0 | Subscription happens only after user interaction; increase `perRouteMs` |
| Empty response bodies | Real responses only with `forward: true` |
| Chromium missing | Install chromium or set `config.chromium` / `CHROMIUM` |
| Lots of mocks but still back to login | Hook too late or missing a `location.href` setter → document-start + preload |
| Lists all empty | L3 empty arrays are normal; keep clicking tabs/settings/detail |
| Mistaking a Redux action for a route | Filter out internal paths containing get/set/change/clear/toggle/upload |
| Vue still redirects to login | preload not at document-start → change injection timing; or clear the guards by hand when `neutralizeVueRouter: false` |
| URL in a response but not in the log | Enable `extractUrlsFromResponse`; or extract from `__API_RECON_DETAIL__` by hand |
| Do not know the Authorization header name | Enable `observe.xhrHeaders` or inspect the request headers in DevTools |
| Runtime very slow / timing out | Set `waitUntil: domcontentloaded`; lower `routeTimeout`; do not use `networkidle2` |
| Proxy connection fails | Check `proxy` / environment variables; Puppeteer and curl must use the same proxy port |

---

## G. Hardened targets

When the server validates the session step by step (an unforgeable signed cookie, a server-rendered menu that cannot be stubbed), runtime gets stuck at the shell. Expected behaviour:

- **Static is enough for endpoint enumeration** — the module paths are in the code
- If authorized, run the same harness with a **real session**: `forward: true`, no neutralize needed, capturing real methods/params/responses

---

## H. Single-task checklist

1. Confirm the authorized scope
2. **Read** `scripts/harvest_static.py` → adapt to the target → run → review `api_static.txt`, `routes.txt`
3. **Phase 1b**: path anchor-window expansion + binding layer → `param_candidates.json` (section J)
4. Reverse-engineer A1/A2/A3 → write a site-specific `config.json`
5. **Read and adjust** `runtime_harvest.js` / `preload.js` before running them
6. `runtimeMode=depth`: `npm ci --ignore-scripts` (preinstalled in the image, so this can be skipped) → run the adjusted harvest script
7. `runtimeMode=coverage/both`: inject the adjusted preload at document-start → browser MCP dynamic enumeration + **parameter trigger matrix**
8. Modules do not render → **section I permission-tree reconstruction** → patch stubs → re-run
9. Parameter multi-sample diff + error back-inference → `params_merged.json`
10. Merge → `site_map.json` + `api_merged.txt`, honestly noting coverage, gaps, and the script change points

---

## I. Permission-tree reconstruction (Phase 4 deep dive)

Use this when forging a simple `menus: [{ path, show: true }]` does not work and sub-modules still do not mount.

### I1. Locate the auth module

```bash
grep -l 'userRouteAuth' js/*.js
grep -l 'routeMap\|routeLink' js/*.js
grep -rhoaE 'getResultTree|role_permissions|permissions/all' js | head
```

Record: the **permission API path**, the **response field names**, and the **consuming chunk file name**.

### I2. Extract the routeMap

```bash
python3 scripts/extract_route_map.py recon/js recon/
# produces recon/route_map.json
```

If `[!] no routeMap pattern found`: loosen the regex in `extract_route_map.py`, or grep by hand:

```bash
grep -rhoaE '([A-Z_][A-Z0-9_]*):\{name:"[^"]*",link:"/[^"]+"\}' js | head -20
```

### I3. Build the permission tree + stubs

```bash
python3 scripts/build_perm_tree.py recon/js recon/ --config recon/config.json
```

Script logic:
1. Parse `userRouteAuth={MONITOR:{url:...},...}` (including the webpack alias `He=o.DASHBOARD`)
2. Use `route_map.json` to resolve alias → real path
3. Infer the parent from the code prefix (`MONITOR_ALERT` → `MONITOR`)
4. Output `permissions_tree.json`, `permissions_all_stub.json`, `role_permissions_stub.json`
5. With `--config`, automatically write the `stubs` and extended `routes` into `config.json`

**Adjust for the target** (at the top of the script):
- `DEFAULT_ROOTS`: the list of top-level module codes
- `DEFAULT_PREFIX_PARENT`: `PREFIX_` → parent mapping
- `DEFAULT_EXTRA_PARENT`: orphan nodes with no prefix relationship

### I4. Verify stub consistency

```bash
# the permissions count should ≈ the number of userRouteAuth entries
wc -l recon/perm_codes_all.txt
# routes should cover every link in route_map
python3 -c "import json; m=json.load(open('recon/route_map.json')); r=set(json.load(open('recon/config.json'))['routes']); print('missing', [v['link'] for v in m.values() if v['link'] not in r])"
```

### I5. Re-run runtime and compare

```bash
node recon/runtime_harvest.js recon/config.json
# compare the runtime_api.json counts before and after the forge; check whether module APIs such as /attack, /asset now appear
```

| Before forge | After forge (success) |
|---|---|
| The same 3–5 bootstrap calls on every route | Different routes trigger different module APIs |
| Only `/api/locale/language` | `/api/web/...` module endpoints appear |
| Single-digit routes in `routes.txt` | 80–110+ `routes` from route_map |

### I6. When it still fails

- **Coverage mode**: click the sidebar + tabs; permission gating may only request after interaction
- **Stub fields**: compare a real API (curl + a real session) with the stub's nesting
- **Extra guards**: grep for button-level checks such as `hasPermission|checkRole|func.` and extend `role_permissions.permissions`
- **Static fallback**: the module API paths are still in `api_static.txt`; runtime only adds METHOD/body; keep parameters in `param_candidates.json` + the samples already recorded

---

## J. Parameter reverse-engineering (Phase 1b / 5b / 5c)

**A methodology, not a universal script.** Find paths with regex; find parameters with anchor-window expansion + the UI binding chain + multi-sample diff + error back-inference.

### J1. Anchor-window expansion — find the request-building object from a path

```bash
# anchor on a path known from Phase 1
grep -n '"/api/user/list"' js/*.js
grep -rhoaE '.{0,120}("/api[^"]+").{0,200}' js | head
grep -rhoaE '(params|data|body|payload)\s*:\s*\{' js | head
grep -rhoaE '(get|post|put|delete|patch)\([^,]+,\s*\{' js | head
```

### J2. Wrapper layer and transport shape

```bash
# axios / unified request
grep -rhoaE '(axios|request)\.(get|post|put|delete|patch)\(' js | head
grep -rhoaE 'interceptors\.(request|response)' js | head

# GraphQL
grep -rhoaE '(query|mutation)\s+\w+|gql`|graphql\(' js | head
grep -rhoaE '\$[a-zA-Z_]+\s*:\s*(Int|String|Boolean|\[)' js | head

# FormData / multipart
grep -rhoaE 'FormData|\.append\(' js | head

# path parameters
grep -rhoaE 'path:\s*"/[^"]*:[^"]+"' js | head
grep -rhoaE 'useParams|route\.params|\$route\.params' js | head
```

### J3. Validation gate — required / format / enum

```bash
grep -rhoaE '(required|message|pattern|enum|validator)\s*:' js | head
grep -rhoaE 'yup\.|zod\.|async-validator|Form\.Item|a-form-item|el-form-item' js | head
grep -rhoaE 'rules\s*:\s*\[|name:\s*["\'][a-zA-Z_]+["\']' js | head
grep -rhoaE 'label.*value|options\s*:\s*\[' js | head
```

### J4. Binding layer — form → API

```bash
grep -rhoaE 'onFinish|handleSubmit|getFieldsValue|validateFields' js | head
grep -rhoaE '(pick|omit|transform|dayjs|moment)\(' js | head
```

Runtime fallback: DevTools → Network → request → **Initiator** (call stack), tracing upward from `fetch`/`send` to the request-building function.

### J5. Encrypted parameters

```bash
grep -rhoaE 'encrypt|decrypt|sign|CryptoJS|sm2|sm3|sm4|RSA|AES' js | head
```

**Do not guess fields from the ciphertext** — hook the crypto function's **input** and record the plaintext payload before encryption; write the conclusion into `config.json` / `param_candidates.json`.

### J6. Parameter trigger matrix (mandatory in Phase 3)

Record each module once per operation, and diff the request body/query:

| Operation | What to watch |
|---|---|
| List first screen | Pagination defaults |
| Search | keyword, filters |
| Advanced filter | optional fields |
| Create/edit | the full entity |
| Bulk/export | `ids[]`, `exportType` |
| Sort/paginate | `sortField`, `order` |

Produces `param_samples.json`: `[{ "path", "method", "action": "search", "body", "query", "headers" }]`

### J7. Confidence rules

| Confidence | Condition |
|---|---|
| **High** | Static callsite + runtime, ≥2 consistent samples |
| **Medium** | Static only, or a single runtime sample |
| **Low** | Back-inferred from a response/error, not re-verified |
| **Pending trigger** | Field known statically, but the UI/permission was never reached |

### J8. Scenario quick-reference

| Scenario | Order |
|---|---|
| REST list page | J1 request-building object → J6 four diffs → J3 rules |
| Create/edit form | J3 Form name → J4 submit chain → runtime submit + deliberately leave fields empty to read the 400 |
| GraphQL | J2 variables declaration → runtime records variables for each operation |
| Encrypted body | J5 hook the input → the pre-encryption fields are the real params |

### J9. Mapping to api-recon phases

| api-recon | Parameter recon |
|---|---|
| Phase 1 static | J1 anchor-window expansion |
| Phase 2 A2 interceptor | Globally injected fields (tenantId, sign) |
| Phase 3 runtime | J6 trigger matrix + `param_samples.json` |
| Phase 4 permission tree | Different modules have different forms → full fields trigger only with enough permission |
| Phase 5 merge | `params_merged.json` + confidence; do not decide required from a single sample |

### J10. Troubleshooting

| Symptom | Action |
|---|---|
| A field name is in static but never appears at runtime | Mark "pending trigger"; complete the permission tree / click advanced filter / each option of a linked select |
| Same path, different body shapes | Normal — record separately by `action`, do not force one merged schema |
| Stub response is fake but you want the params | **Look at the outbound request** body/headers, do not back-infer from the stub response |
| 400 reports a nested field | Watch for an outer wrapper `data`/`bizData`/`variables` |
| GraphQL shows only the operation name | Expand the `variables` JSON; find `$var: Type` statically |

---
