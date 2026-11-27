package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/Autumn-27/artex/db"
	actool "github.com/Autumn-27/norma/tool"
)

// assetInterceptCandidates extracts the domain/IP/URL candidate strings of one asset input item about to be inserted, for asset intercept matching.
// The URL's host is split out and classified, so service/endpoint assets that "carry only a URL" can also be matched by domain/IP rules.
func assetInterceptCandidates(item assetInputItem) (domains, ips, urls []string) {
	add := func(dst *[]string, s string) {
		if s = strings.TrimSpace(s); s != "" {
			*dst = append(*dst, s)
		}
	}
	add(&domains, item.Domain)
	for _, d := range item.BoundDomains {
		add(&domains, d)
	}
	add(&ips, item.IP)
	add(&ips, item.ServiceIP)
	add(&urls, item.URL)
	if item.URL != "" {
		if u, err := url.Parse(item.URL); err == nil {
			if h := u.Hostname(); h != "" {
				if net.ParseIP(h) != nil {
					add(&ips, h)
				} else {
					add(&domains, h)
				}
			}
		}
	}
	return domains, ips, urls
}

// assetInputLabel returns a short label for one asset about to be inserted, used in intercept explanation messages.
func assetInputLabel(item assetInputItem) string {
	typ := strings.TrimSpace(item.Type)
	var target string
	switch {
	case strings.TrimSpace(item.Domain) != "":
		target = strings.TrimSpace(item.Domain)
	case strings.TrimSpace(item.URL) != "":
		target = strings.TrimSpace(item.URL)
	case strings.TrimSpace(item.IP) != "":
		target = strings.TrimSpace(item.IP)
	case strings.TrimSpace(item.ServiceIP) != "":
		target = strings.TrimSpace(item.ServiceIP)
	default:
		target = "(unknown)"
	}
	if typ != "" {
		return fmt.Sprintf("[%s] %s", typ, target)
	}
	return target
}

// =====================================================================
// Unified asset insertion tools
// =====================================================================

// SetAssetStore wires the asset store and company store onto this ToolSet
// so the insert_assets, add_company_scope, and list_assets tools are active.
func (t *ToolSet) SetAssetStore(as *db.AssetStore, cs *db.CompanyStore) {
	t.as = as
	t.cs = cs
}

// assetInputItem is one element of the insert_assets "assets" array.
type assetInputItem struct {
	Type string `json:"type"` // root_domain|ip|subdomain|app|service|endpoint

	// ---- root_domain / subdomain ----
	Domain      string   `json:"domain"`
	ICP         string   `json:"icp"`
	RecordType  string   `json:"record_type"`
	RecordValue []string `json:"record_value"`

	// ---- ip ----
	IP           string           `json:"ip"`
	BoundDomains []string         `json:"bound_domains"`
	OpenPorts    []db.PortService `json:"open_ports"`

	// ---- app ----
	AppName     string `json:"app_name"`
	BundleID    string `json:"bundle_id"`
	Category    string `json:"category"`
	Description string `json:"description"`
	AppICP      string `json:"app_icp"`
	CompanyID   *int64 `json:"company_id"` // explicit company link (app only; others auto-attribute via scope)

	// ---- service (http) ----
	URL           string           `json:"url"`
	Technologies  []string         `json:"technologies"`
	StatusCode    *int             `json:"status_code"`
	ContentLength *int64           `json:"content_length"`
	PageTitle     string           `json:"page_title"`
	FaviconMMH3   string           `json:"favicon_mmh3"`
	Auth          []map[string]any `json:"auth"`
	ServiceName   string           `json:"service_name"`
	ServiceIP     string           `json:"service_ip"` // optional enrichment IP

	// ---- service (other) ----
	Port  int    `json:"port"`
	Proto string `json:"proto"`

	// ---- endpoint ----
	Method string           `json:"method"`
	Params []map[string]any `json:"params"`
}

// insertAssets is the unified insert_assets agent tool.
func (t *ToolSet) insertAssets() actool.CoreTool {
	return writeTool(
		"insert_assets",
		"Record newly discovered assets in bulk; one call may mix several types (see the type enum).\n"+
			"Required fields per type: root_domain→domain; ip→ip (must be IPv4/IPv6, not a hostname); subdomain→domain; app→app_name; service(HTTP)→url; service(non-HTTP)→service_name+port (at least one of ip/domain); endpoint→url+method. See each field's own description for the rest.\n"+
			"auth/technologies/params are merged by appending and do not overwrite existing values.\n"+
			"Returns: {results:[{index,id,type}], errors:[{index,error}]}",
		obj(map[string]any{
			// task_id is not exposed to the model: which task a worker belongs to is set authoritatively by the program via SetTaskID (see the handler).
			"assets": map[string]any{
				"type":        "array",
				"description": "Array of assets; each element is one asset record",
				"items": obj(map[string]any{
					"type": map[string]any{
						"type":        "string",
						"enum":        []string{"root_domain", "ip", "subdomain", "app", "service", "endpoint"},
						"description": "Asset type",
					},
					// root_domain / subdomain
					"domain":      str("Root domain or subdomain (required for root_domain/subdomain)"),
					"icp":         str("ICP filing number (optional)"),
					"record_type": str("DNS record type: A/AAAA/CNAME/MX, etc. (optional for subdomain)"),
					"record_value": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "List of DNS record values (optional for subdomain, e.g. [\"1.2.3.4\",\"2.3.4.5\"])",
					},
					// ip
					"ip": str("IP address; must be an IPv4/IPv6 address, never a hostname (for a hostname use the domain field with type=subdomain); required for type ip; optional for types service/endpoint, where it links the IP"),
					"bound_domains": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "List of domains bound to this IP (optional for type ip)",
					},
					"open_ports": map[string]any{
						"type":        "array",
						"description": "List of open ports (optional for type ip)",
						"items": obj(map[string]any{
							"port":    intp("Port number"),
							"service": str("Service name, e.g. http/ssh/mysql (optional)"),
						}, "port"),
					},
					// app
					"app_name":    str("App name (required for type app)"),
					"bundle_id":   str("Bundle ID (optional for type app)"),
					"category":    str("App category (optional)"),
					"description": str("App description (optional)"),
					"app_icp":     str("App ICP filing (optional)"),
					"company_id":  intp("Id of the owning company (optional for type app; an app cannot be attributed automatically through scope, so it must be specified explicitly. The id is returned by add_company_scope)"),
					// service (http)
					"url":         str("Full URL including scheme and port (required for an HTTP service; service_type is set to http automatically)"),
					"status_code": intp("HTTP response status code, e.g. 200/301/403/404 (optional)"),
					"content_length": map[string]any{
						"type":        "integer",
						"description": "HTTP response body size in bytes (optional)",
					},
					"page_title":   str("Content of the page <title> (optional)"),
					"favicon_mmh3": str("favicon MMH3 hash (optional)"),
					"technologies": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "List of fingerprints / tech stack, e.g. [\"Nginx\",\"Vue\",\"Bootstrap\"] (optional)",
					},
					"auth": map[string]any{
						"type":        "array",
						"description": "List of discovered credentials; each entry has fields such as type/username/password (optional; appended, not overwritten)",
						"items":       map[string]any{"type": "object"},
					},
					// service (other, non-HTTP)
					"service_name": str("Service name, e.g. ssh/mysql/redis (required for a non-HTTP service)"),
					"port":         intp("Port number (required for a non-HTTP service)"),
					// endpoint
					"method": str("HTTP method: GET/POST/PUT/PATCH/DELETE, etc. (required for endpoint)"),
					"params": map[string]any{
						"type":        "array",
						"description": "List of request parameters; each entry has location(query/body/header/path)/name/value/type (optional; appended, not overwritten)",
						"items":       map[string]any{"type": "object"},
					},
				}, "type"),
			},
		}, "assets"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			if t.as == nil {
				return actool.Errorf("insert_assets not enabled: AssetStore is not initialized"), nil
			}
			var a struct {
				Assets []assetInputItem `json:"assets"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("invalid input: " + err.Error()), nil
			}
			// task_id is set authoritatively by the program (worker: SetTaskID) and is not accepted from the model - this avoids the model omitting or mis-passing it
			// and leaving the asset unattributed or attributed to the wrong task. Callers without a task context (auto/pentest/chat) have t.taskID=0.
			taskID := t.taskID

			type result struct {
				Index int    `json:"index"`
				ID    int64  `json:"id"`
				Type  string `json:"type"`
			}
			type errEntry struct {
				Index int    `json:"index"`
				Error string `json:"error"`
			}

			var results []result
			var errs []errEntry

			// Asset gate rules are loaded once; if reading fails the check is skipped (insertion is not blocked).
			// Intercept rules = global ∪ task-level block; allow rules = task-level allow.
			blockRules, _ := t.as.ListAssetInterceptRules()
			var allowRules []db.AssetInterceptRule
			if t.taskID > 0 {
				if tb, ta, err := t.as.TaskInterceptRulesSplit(t.taskID); err == nil {
					blockRules = append(blockRules, tb...)
					allowRules = ta
				}
			}

			// Inside a task an app can only be attributed to a company the operator linked
			// to the task; loaded on first use. Other companies in the shared store are
			// not this task's to write to.
			var linkedCompanies map[int64]bool

			for i, item := range a.Assets {
				// Asset gate: block first, then allow; a rejected asset must not be inserted (skip the Upsert and its subsequent side effects).
				domains, ips, urls := assetInterceptCandidates(item)
				if d := db.EvaluateAssetGate(blockRules, allowRules, domains, ips, urls); !d.Allowed {
					errs = append(errs, errEntry{
						Index: i,
						Error: fmt.Sprintf("asset %s %s; insertion forbidden", assetInputLabel(item), d.Reason),
					})
					continue
				}

				typ := strings.TrimSpace(item.Type)
				var id int64
				var err error

				switch typ {
				case "root_domain":
					id, err = t.as.UpsertRootDomain(db.UpsertRootDomainReq{
						Domain: item.Domain,
						ICP:    item.ICP,
						TaskID: taskID,
					})

				case "ip":
					id, err = t.as.UpsertIP(db.UpsertIPReq{
						IP:           item.IP,
						BoundDomains: item.BoundDomains,
						OpenPorts:    item.OpenPorts,
						TaskID:       taskID,
					})

				case "subdomain":
					id, err = t.as.UpsertSubdomain(db.UpsertSubdomainReq{
						Domain:      item.Domain,
						RecordType:  item.RecordType,
						RecordValue: item.RecordValue,
						ICP:         item.ICP,
						TaskID:      taskID,
					})

				case "app":
					if taskID > 0 && item.CompanyID != nil && *item.CompanyID > 0 {
						if linkedCompanies == nil {
							if linkedCompanies, err = t.as.TaskCompanyIDs(taskID); err != nil {
								errs = append(errs, errEntry{Index: i, Error: "failed to read the task scope: " + err.Error()})
								linkedCompanies = nil
								continue
							}
						}
						if !linkedCompanies[*item.CompanyID] {
							errs = append(errs, errEntry{Index: i, Error: fmt.Sprintf(
								"company_id %d is not linked to this task; an app can only be attributed to a company the operator linked to the task (see list_companies), or leave company_id out", *item.CompanyID)})
							continue
						}
					}
					id, err = t.as.UpsertApp(db.UpsertAppReq{
						Name:        item.AppName,
						BundleID:    item.BundleID,
						Category:    item.Category,
						Description: item.Description,
						ICP:         item.AppICP,
						CompanyID:   item.CompanyID,
						TaskID:      taskID,
					})

				case "service":
					// distinguish HTTP vs other by presence of url
					if item.URL != "" {
						// agent may send "ip" or "service_ip" for the enrichment IP; accept both
						svcIP := item.ServiceIP
						if svcIP == "" {
							svcIP = item.IP
						}
						id, err = t.as.UpsertHTTPService(db.UpsertHTTPServiceReq{
							URL:           item.URL,
							Technologies:  item.Technologies,
							StatusCode:    item.StatusCode,
							ContentLength: item.ContentLength,
							PageTitle:     item.PageTitle,
							FaviconMMH3:   item.FaviconMMH3,
							Auth:          item.Auth,
							IP:            svcIP,
							TaskID:        taskID,
						})
					} else {
						id, err = t.as.UpsertOtherService(db.UpsertOtherServiceReq{
							Domain:      item.Domain,
							IP:          item.IP,
							Port:        item.Port,
							ServiceName: item.ServiceName,
							Auth:        item.Auth,
							TaskID:      taskID,
						})
					}

				case "endpoint":
					id, err = t.as.UpsertEndpoint(db.UpsertEndpointReq{
						URL:    item.URL,
						Method: item.Method,
						Params: item.Params,
						IP:     item.ServiceIP,
						TaskID: taskID,
					})

				default:
					errs = append(errs, errEntry{Index: i, Error: "unknown type: " + typ})
					continue
				}

				if err != nil {
					errs = append(errs, errEntry{Index: i, Error: err.Error()})
					continue
				}
				results = append(results, result{Index: i, ID: id, Type: typ})
				t.writes.Assets++
				t.anchorOwner(id)
				if taskID > 0 {
					var sourceNodeID *int64
					if t.ownerNode > 0 {
						nodeID := t.ownerNode
						sourceNodeID = &nodeID
					}
					summary := "Recorded by an agent via insert_assets"
					if t.ownerNode > 0 {
						summary = fmt.Sprintf("Recorded by Worker intent #%d via insert_assets", t.ownerNode)
					}
					_ = t.as.SetTaskAssetSource(taskID, id, "agent", summary, sourceNodeID)
				}
				// Automatic addition to the test scope (source='auto'): only for this one item explicitly inserted at the top level by the worker, adding a conservative scope
				// by its type; assets derived as side effects do not pass through here, so the scope does not widen blindly. No-op when taskID=0.
				// Independent of the coverage switch: task_scope is the task's range boundary (the filter basis for list/query),
				// and the coverage switch only decides whether to use it as the denominator for metrics, not whether to accumulate the scope itself.
				{
					svcIP := item.ServiceIP
					if svcIP == "" {
						svcIP = item.IP
					}
					_ = t.as.AddAutoScope(taskID, typ, item.Domain, item.URL, svcIP)
				}
			}

			return jsonResult(map[string]any{
				"results": results,
				"errors":  errs,
			})
		},
	)
}

// addCompanyScope writes to company_scope table and triggers asset attribution.
func (t *ToolSet) addCompanyScope() actool.CoreTool {
	return writeTool(
		"add_company_scope",
		"Add a domain/IP/CIDR/ICP filing/company keyword to a company's ASSET SCOPE - domains, networks and ICPs automatically claim matching assets, while keywords are only given to agents as scope hints.\n"+
			"Company names are unique: if company does not exist it is created, otherwise it is reused (the scope is simply merged in).\n"+
			"scope has one entry per line, recognized automatically by the system: root domain / URL / single IP / CIDR range / ICP filing / company keyword.\n"+
			"Always give a reason stating the basis for ownership (whois/certificate/ASN, etc.).\n"+
			"Guardrails: bare TLDs and over-wide ranges are rejected (IPv4 prefix must be /16-/32, IPv6 prefix must be /32-/128); invalid lines are skipped and returned in errors.\n"+
			"Inside a task: only a company the operator linked to this task can be changed (no company is created), and each line must be something this task found "+
			"(recorded as an asset by this task) or that the task goal/description names; other lines are rejected and returned in errors.",
		obj(map[string]any{
			"company": str("Company name (created if missing, reused if it exists; names are unique)"),
			"scope":   str("Asset scope, one entry per line: domain / URL / IP / CIDR / ICP filing / company keyword"),
			"reason":  str("Basis for ownership (evidence/source); always fill this in"),
			"logo":    str("Company logo URL (optional; only takes effect when a new company is created)"),
		}, "company", "scope"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			if t.cs == nil {
				return actool.Errorf("add_company_scope not enabled: CompanyStore is not initialized"), nil
			}
			var a struct {
				Company string `json:"company"`
				Scope   string `json:"scope"`
				Reason  string `json:"reason"`
				Logo    string `json:"logo"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf(err.Error()), nil
			}
			if strings.TrimSpace(a.Company) == "" {
				return actool.Errorf("company must not be empty"), nil
			}
			lines := splitLines(a.Scope)
			// In a task the agent works only on the companies the operator linked to it,
			// and adds only what the task itself has a basis for (same rule as
			// add_task_scope). A company's scope claims matching assets store-wide, so
			// without this a task could pull another engagement's assets into its own.
			var rejected []string
			if t.taskID > 0 && t.as != nil {
				linked, err := t.as.TaskCompanyIDs(t.taskID)
				if err != nil {
					return actool.Errorf("failed to read the task scope: " + err.Error()), nil
				}
				comp, err := t.cs.GetCompanyByName(a.Company)
				if err != nil {
					return actool.Errorf("failed to look up the company: " + err.Error()), nil
				}
				if comp == nil || !linked[comp.ID] {
					return actool.Errorf("add_company_scope: inside a task, scope can only be added to a company the operator linked to this task (list_companies shows them); " +
						"creating a company or changing another company's scope is not available here. Use add_task_scope for targets this task has found"), nil
				}
				allowed := lines[:0:0]
				for _, line := range lines {
					if err := t.as.CheckAgentScopeLine(t.taskID, line); err != nil {
						rejected = append(rejected, err.Error())
						continue
					}
					allowed = append(allowed, line)
				}
				lines = allowed
			}
			companyID, _, err := t.cs.UpsertCompany(a.Company, a.Logo)
			if err != nil {
				return actool.Errorf("failed to create/get company: " + err.Error()), nil
			}
			added, skipped, invalid, errMsgs := 0, 0, 0, []string(nil)
			if len(lines) > 0 {
				added, skipped, invalid, errMsgs = t.cs.AddScope(companyID, lines, a.Reason)
			}
			errMsgs = append(errMsgs, rejected...)
			out := map[string]any{
				"company_id": companyID,
				"added":      added,
				"skipped":    skipped,
				"invalid":    invalid,
			}
			if len(rejected) > 0 {
				out["rejected"] = len(rejected)
			}
			if len(errMsgs) > 0 {
				out["errors"] = errMsgs
			}
			return jsonResult(out)
		},
	)
}

// addTaskScope lets the plan agent add test scope to THE CURRENT TASK — the coverage
// denominator and the task's authorization edge. Worker discoveries are auto-scoped
// (precise host) by insertAssets; this tool widens from there: a whole root domain, a
// specific subdomain / ip / small range. Every entry must be backed by the task
// itself (db.CheckAgentScope); a company is linked by the operator, never added here.
func (t *ToolSet) addTaskScope() actool.CoreTool {
	return writeTool(
		"add_task_scope",
		"Add test scope to THIS TASK - this is the task's authorization boundary and also the denominator of asset test coverage.\n"+
			"Supported kind values: root_domain (the whole root domain, including every subdomain) / subdomain (one exact subdomain) / ip / cidr / icp / keyword.\n"+
			"Note: hosts that workers touch one by one are added to the scope AUTOMATICALLY by the system (exact subdomain); this tool is for DELIBERATELY WIDENING it - bringing in a whole root domain, or adding a specific subdomain/IP.\n"+
			"Scope can only grow from what THIS TASK has a basis for: a target named in the task goal/description, or one this task has already recorded as an asset (insert_assets). "+
			"Anything else is rejected - have a Worker confirm and record the target first. A range added from a discovered address may be at most /24 (IPv4) or /64 (IPv6).\n"+
			"Companies cannot be added with this tool: a company is linked to the task by the operator.\n"+
			"value: for root_domain/subdomain pass the domain; for ip/cidr pass the IP or range; for icp/keyword pass the ICP filing number or company keyword.\n"+
			"Always give a reason stating the basis (auditable). For several entries use the entries array.",
		obj(map[string]any{
			"entries": map[string]any{"type": "array", "description": "Batch: [{kind, value}]. kind is one of root_domain/subdomain/ip/cidr/icp/keyword.", "items": map[string]any{"type": "object"}},
			"kind":    str("[Single] root_domain / subdomain / ip / cidr / icp / keyword"),
			"value":   str("[Single] domain / IP / CIDR / ICP / keyword"),
			"reason":  str("Basis for adding it (for auditing); always fill this in"),
		}),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			if t.as == nil {
				return actool.Errorf("add_task_scope not enabled: AssetStore is not initialized"), nil
			}
			if t.taskID <= 0 {
				return actool.Errorf("add_task_scope requires a task context (there is no current task)"), nil
			}
			type scopeEntry struct {
				Kind  string `json:"kind"`
				Value string `json:"value"`
			}
			var a struct {
				Entries    []scopeEntry `json:"entries"`
				scopeEntry              // single mode
				Reason     string       `json:"reason"`
			}
			_ = json.Unmarshal(in, &a)
			items := a.Entries
			if len(items) == 0 {
				items = []scopeEntry{a.scopeEntry}
			}
			var added []map[string]any
			errs := map[string]string{}
			for i, e := range items {
				ts, err := t.as.AddAgentScope(t.taskID, strings.TrimSpace(e.Kind), e.Value, a.Reason, "agent")
				if err != nil {
					errs[strconv.Itoa(i)] = err.Error()
					continue
				}
				added = append(added, map[string]any{"kind": ts.Kind, "domain": ts.Domain, "net": ts.Net, "value": ts.Value, "company_id": ts.CompanyID})
			}
			out := map[string]any{"added": added}
			if len(errs) > 0 {
				out["errors"] = errs
			}
			return jsonResult(out)
		},
	)
}

// listUntestedAssets lets the plan agent pull the current + directly inherited
// scope's not-yet-tested assets on demand (filter by type, paginated).
func (t *ToolSet) listUntestedAssets() actool.CoreTool {
	return readTool(
		"list_untested_assets",
		"Query assets within the scope of THIS TASK AND DIRECTLY LINKED TASKS that are not yet covered by a fact anchor (linked scope is read-only; this is for you to judge whether to test them and does not decide for you).\n"+
			"Optionally filter by asset type: root_domain/subdomain/service/app/endpoint/ip.\n"+
			"Pagination: page starts at 1, page_size defaults to 10. Returns {assets:[{id,type,label}], total, page, page_size}. Available in a task context only.",
		obj(map[string]any{
			"type":      str("Asset type filter (optional): root_domain/subdomain/service/app/endpoint/ip"),
			"page":      intp("Page number, starting at 1 (default 1)"),
			"page_size": intp("Items per page (default 10)"),
		}),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			if t.as == nil {
				return actool.Errorf("list_untested_assets not enabled: AssetStore is not initialized"), nil
			}
			if t.taskID <= 0 || t.ts == nil {
				return actool.Errorf("list_untested_assets requires a task context"), nil
			}
			var a struct {
				Type     string `json:"type"`
				Page     int    `json:"page"`
				PageSize int    `json:"page_size"`
			}
			_ = json.Unmarshal(in, &a)
			if a.Page <= 0 {
				a.Page = 1
			}
			if a.PageSize <= 0 {
				a.PageSize = 10
			}
			offset := (a.Page - 1) * a.PageSize
			assets, total, err := t.as.ListUntestedAssetsWithSources(t.taskID, strings.TrimSpace(a.Type), a.PageSize, offset)
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			return jsonResult(map[string]any{
				"assets": assets, "total": total, "page": a.Page, "page_size": a.PageSize,
			})
		},
	)
}

// listAssets lets an agent query the asset table.
func (t *ToolSet) listAssets() actool.CoreTool {
	return readTool(
		"list_assets",
		"Query the asset store: search with a DSL expression, or fetch directly by id/ids; pagination is supported. Returns only assets within the test scope of THIS TASK AND DIRECTLY LINKED TASKS.\n"+
			"DSL: field=value fuzzy (ILIKE) | field==value exact | field!=value exclude | numeric fields support > >= < <= | bare word = full-text fuzzy; combine with AND/OR (AND binds tighter), parentheses may be used for grouping. The asset type goes in the separate type parameter, not in the DSL.\n"+
			"When id/ids is not passed, dsl must be non-empty (an unconditional full query is not allowed).\n"+
			"Available fields: domain (root/sub/service domain), root_domain, ip, url, page_title, icp, service_name, app_name, method (e.g. GET/POST), service_type (http|other), record_type (e.g. A/CNAME), technology (array; = fuzzy, == exact), port/status_code/company_id (integers).\n"+
			"Examples: status_code>=400 AND technology=shiro ; (port==80 OR port==443) AND technology=nginx",
		obj(map[string]any{
			"dsl":    str(`DSL query expression (see the tool description for syntax/fields). Must be non-empty when id/ids is not passed.`),
			"type":   str("Asset type filter: root_domain|ip|subdomain|app|service|endpoint (a separate field that can be combined with dsl; type alone is not enough to query, dsl is still required)"),
			"id":     intp("Fetch directly by a single asset id (optional; mutually exclusive with dsl/type)"),
			"ids":    map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "Fetch directly by several asset ids (optional; mutually exclusive with dsl/type)"},
			"limit":  intp("Maximum number of results, default 10 (optional)"),
			"offset": intp("Pagination offset, default 0 (optional)"),
		}),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			if t.as == nil {
				return actool.Errorf("list_assets not enabled: AssetStore is not initialized"), nil
			}
			var a struct {
				DSL    string  `json:"dsl"`
				Type   string  `json:"type"`
				ID     int64   `json:"id"`
				IDs    []int64 `json:"ids"`
				Limit  int     `json:"limit"`
				Offset int     `json:"offset"`
			}
			_ = json.Unmarshal(in, &a)
			if a.Limit <= 0 {
				a.Limit = 10
			}

			var assets []*db.Asset
			var err error
			switch {
			case a.ID > 0:
				assets, err = t.as.GetByIDsInScope(t.taskID, []int64{a.ID})
			case len(a.IDs) > 0:
				assets, err = t.as.GetByIDsInScope(t.taskID, a.IDs)
			case a.DSL != "":
				assets, err = t.as.QueryDSLInScope(a.DSL, a.Type, t.taskID, a.Limit, a.Offset)
			default:
				return actool.Errorf("dsl must not be empty when id/ids is not passed: querying all assets unconditionally is not allowed, please provide a query condition"), nil
			}
			if err != nil {
				return actool.Errorf("DSL error: " + err.Error()), nil
			}
			return jsonResult(map[string]any{
				"count":  len(assets),
				"assets": assets,
			})
		},
	)
}

// listCompanies lets an agent enumerate companies with their scope + asset count.
func (t *ToolSet) listCompanies() actool.CoreTool {
	return readTool(
		"list_companies",
		"List the COMPANIES in the asset store together with their asset scope and the number of assets attributed to them. Use it to see which companies exist and "+
			"to get a company_id (used when insert_assets links an app, or when list_assets filters by company_id). "+
			"Inside a task this returns only the companies the operator linked to this task (and to its directly linked tasks). "+
			"Optionally search does a fuzzy filter on the company name (case-insensitive); leave it empty to return all.",
		obj(map[string]any{
			"search": str("Fuzzy filter on the company name (optional, case-insensitive); leave empty to return all"),
		}),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			if t.cs == nil {
				return actool.Errorf("list_companies not enabled: CompanyStore is not initialized"), nil
			}
			var a struct {
				Search string `json:"search"`
			}
			_ = json.Unmarshal(in, &a)
			cos, err := t.cs.ListCompanies()
			if err != nil {
				return actool.Errorf("failed to query companies: " + err.Error()), nil
			}
			// The store is shared by every task. Inside a task the agent sees only the
			// companies in that task's scope; other engagements stay invisible.
			var visible map[int64]bool
			if t.taskID > 0 && t.as != nil {
				if visible, err = t.as.TaskCompanyIDs(t.taskID); err != nil {
					return actool.Errorf("failed to read the task scope: " + err.Error()), nil
				}
			}
			q := strings.ToLower(strings.TrimSpace(a.Search))
			type companyOut struct {
				ID         int64    `json:"id"`
				Name       string   `json:"name"`
				AssetCount int      `json:"asset_count"`
				Scope      []string `json:"scope"`
			}
			out := make([]companyOut, 0, len(cos))
			for _, c := range cos {
				if visible != nil && !visible[c.ID] {
					continue
				}
				if q != "" && !strings.Contains(strings.ToLower(c.Name), q) {
					continue
				}
				scope := make([]string, 0, len(c.Scope))
				for _, r := range c.Scope {
					scope = append(scope, r.Raw)
				}
				out = append(out, companyOut{ID: c.ID, Name: c.Name, AssetCount: c.AssetCount, Scope: scope})
			}
			return jsonResult(map[string]any{"count": len(out), "companies": out})
		},
	)
}

// splitLines splits a multi-line string into non-empty trimmed lines.
func splitLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// WorkerTools returns the tool set for a work agent.
func (t *ToolSet) WorkerTools() []actool.CoreTool {
	return []actool.CoreTool{
		// list_findings is kept: before reporting a finding, check this task's confirmed findings first so the same finding is not reported twice.
		t.listFindings(),
		t.addFinding(), t.recordFact(),
		// asset management (handlers guard nil store internally).
		// add_company_scope is not given to workers: defining a company's asset scope is the job of the planner/main agent/Auto, a worker only carries out exploration.
		t.insertAssets(), t.listAssets(),
		// Looking back across works: a worker can also reuse the observations of other works to avoid duplicated effort.
		// search_all_worker_traces: no need to know an intent_id first; pulls matching steps globally by keyword;
		// get_worker_trace: once a work is pinned down, list its steps / search within it / fetch the full content.
		t.searchAllWorkerTraces(), t.getWorkerTrace(),
		// node_detail: once a worker has an intent_id/node id it can look up that node's full detail (complements the look-back above).
		t.nodeDetail(),
		// The following tools are still NOT given to workers and stay with planner/main (reading context and reviewing across works is a planning duty;
		// a worker only executes a single intent and writes back): list_facts / list_companies / list_worker_traces.
	}
}

// MainAgentTools returns the human-interface tool set.
func (t *ToolSet) MainAgentTools() []actool.CoreTool {
	return []actool.CoreTool{
		t.graphOverview(), t.listFindings(), t.listFacts(), t.nodeDetail(),
		t.expandDigest(), // cold-digest §6.1
		t.getWorkerOutput(), t.getWorkerTrace(), t.searchAllWorkerTraces(), t.addHint(), t.addIntent(),
		// steer_work: a human can inject a course-correction instruction into a running intent (work) in real time (no interruption, no lost progress).
		t.steerWorkTool(),
		// set_goals: a human can add a new final goal to this task at runtime (the Planner re-judges completion accordingly).
		t.setGoals(),
		// set_constraints: a human can add/change this task's operating constraints (allow/deny) at runtime, bounding the planner/worker exploration.
		t.setConstraints(),
		// asset management (handlers guard nil store internally)
		t.insertAssets(), t.addCompanyScope(), t.listAssets(),
		t.addFinding(), t.recordFact(),
		t.addTaskScope(),
		// list_untested_assets: look up untested assets within this task's scope on demand (type + pagination) and decide independently whether to test them.
		t.listUntestedAssets(),
	}
}

// AllDomainTools returns the union of all domain tools across all agent types,
// deduped by name (mainagent order wins). Used by the server to build a registry
// for injecting domain tools into agents (Auto, custom) that don't own a per-task
// ToolSet. The caller provides real stores; tools are callable at taskID=0 scope.
func (t *ToolSet) AllDomainTools() []actool.CoreTool {
	seen := map[string]bool{}
	var out []actool.CoreTool
	all := append(append(t.MainAgentTools(), t.PlannerTools()...), t.WorkerTools()...)
	for _, tool := range all {
		if !seen[tool.Name()] {
			seen[tool.Name()] = true
			out = append(out, tool)
		}
	}
	return out
}
