package db

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
)

// ErrScopeNotDiscovered marks an agent scope write that this task has no basis for:
// the entry was neither named by the operator nor found by the task itself.
var ErrScopeNotDiscovered = errors.New("scope entry is not backed by this task")

// Widest network an agent may add on the strength of one discovered address. A
// range the operator wrote into the task goal/description is not capped here.
const (
	agentScopeMinPrefixV4 = 24
	agentScopeMinPrefixV6 = 64
)

var cidrLiteral = regexp.MustCompile(`[0-9A-Fa-f:.]+/[0-9]{1,3}`)

// TaskCompanyIDs returns the companies that are in the scope of the task and of its
// direct source tasks. This is the set of companies an agent working on the task is
// allowed to see and to attribute assets to.
func (s *AssetStore) TaskCompanyIDs(taskID int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	if taskID <= 0 {
		return out, nil
	}
	rows, err := s.db.Query(`WITH `+directTaskContextCTE+`
SELECT DISTINCT ts.company_id
FROM task_scope ts
JOIN context_tasks ctx ON ctx.task_id=ts.task_id
WHERE ts.kind='company' AND ts.company_id IS NOT NULL`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// CheckAgentScope decides whether an agent may add one scope entry to a task. The
// asset store is shared by every task, so without this check an agent could pull
// another engagement's company, domain or network into its own test scope simply
// because the rows exist. An entry is accepted only when the task itself gives a
// basis for it:
//
//   - the operator named it: the value appears in the task name, goal or description; or
//   - the task found it: an asset recorded by this task (assets.task_ids) matches it.
//
// kind is root_domain | subdomain | ip | cidr | icp | keyword, with domain / cidr /
// value already normalized the way task_scope stores them. Companies are never added
// by an agent; see AddAgentScope. The returned error wraps ErrScopeNotDiscovered and
// is written for the model: it says what to do instead.
func (s *AssetStore) CheckAgentScope(taskID int64, kind, domain, cidr, value string) error {
	if taskID <= 0 {
		return fmt.Errorf("task_id is required")
	}
	text, err := s.taskOperatorText(taskID)
	if err != nil {
		return err
	}
	shown := domain
	found := false
	switch kind {
	case "root_domain", "subdomain":
		if mentionsToken(text, domain) {
			return nil
		}
		cond := `a.domain=$2`
		if kind == "root_domain" {
			cond = `(a.root_domain=$2 OR a.domain=$2)`
		}
		found, err = s.taskRecordedAsset(taskID, cond, domain)
	case "ip", "cidr":
		shown = cidr
		_, network, perr := net.ParseCIDR(cidr)
		if perr != nil {
			return fmt.Errorf("invalid ip/cidr: %s", cidr)
		}
		ones, bits := network.Mask.Size()
		if ones == bits {
			if mentionsToken(text, network.IP.String()) {
				return nil
			}
		} else {
			if mentionsNetwork(text, network) {
				return nil
			}
			if (bits == 32 && ones < agentScopeMinPrefixV4) || (bits == 128 && ones < agentScopeMinPrefixV6) {
				return fmt.Errorf("%w: %s is wider than an agent may add from a discovered address (IPv4 /%d, IPv6 /%d at most); add the specific addresses, or ask the operator to add the range on the task page",
					ErrScopeNotDiscovered, cidr, agentScopeMinPrefixV4, agentScopeMinPrefixV6)
			}
		}
		found, err = s.taskRecordedAsset(taskID, `($2::cidr >>= try_inet(a.ip) OR $2::cidr >>= try_inet(a.domain))`, cidr)
	case "icp":
		shown = value
		if value != "" && strings.Contains(NormalizeICP(text), value) {
			return nil
		}
		found, err = s.taskRecordedAsset(taskID, `(lower(regexp_replace(COALESCE(a.icp,''), '[[:space:]]+', '', 'g'))=$2
   OR lower(regexp_replace(COALESCE(a.app_icp,''), '[[:space:]]+', '', 'g'))=$2)`, value)
	case "keyword":
		shown = value
		if value != "" && strings.Contains(strings.ToLower(text), strings.ToLower(value)) {
			return nil
		}
	default:
		return fmt.Errorf("unsupported kind: %s", kind)
	}
	if err != nil {
		return err
	}
	if found {
		return nil
	}
	return fmt.Errorf("%w: %s %q was neither named in the task goal/description nor recorded as an asset by this task. "+
		"Scope can only grow from what this task actually finds: have a Worker confirm the target and record it with insert_assets first, "+
		"or ask the operator to add it on the task page", ErrScopeNotDiscovered, kind, shown)
}

// CheckAgentScopeLine applies CheckAgentScope to one free-form company-scope line
// (domain / URL / IP / CIDR / ICP / keyword, classified the way the company store
// does). A line that does not parse is left to the store's own validation.
func (s *AssetStore) CheckAgentScopeLine(taskID int64, line string) error {
	parsed, err := ParseAutoScopeLine(line)
	if err != nil {
		return nil
	}
	kind := parsed.Kind
	if kind == "domain" {
		kind = "root_domain"
	}
	return s.CheckAgentScope(taskID, kind, parsed.Domain, parsed.Net, parsed.Value)
}

// taskOperatorText is the text the operator wrote when creating the task. A target
// named there counts as given by the operator.
func (s *AssetStore) taskOperatorText(taskID int64) (string, error) {
	var name, goal, description string
	err := s.db.QueryRow(`SELECT COALESCE(name,''), COALESCE(goal,''), COALESCE(description,'') FROM tasks WHERE id=$1`, taskID).
		Scan(&name, &goal, &description)
	if err != nil {
		return "", fmt.Errorf("load task %d: %w", taskID, err)
	}
	return name + "\n" + goal + "\n" + description, nil
}

// taskRecordedAsset reports whether an asset recorded by the task satisfies cond.
// cond is a fixed SQL fragment over the alias a, with $2 bound to arg.
func (s *AssetStore) taskRecordedAsset(taskID int64, cond, arg string) (bool, error) {
	var ok bool
	err := s.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM assets a WHERE $1 = ANY(a.task_ids) AND `+cond+`)`, taskID, arg).Scan(&ok)
	return ok, err
}

func isHostChar(c byte) bool {
	return c == '-' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// mentionsToken reports whether text names token (a hostname or an IP address) as a
// whole name. "example.com" is named by "https://app.example.com/x" (a parent of a
// named host) but not by "notexample.com" or "example.com.evil.test"; "10.0.0.1" is
// not named by "110.0.0.1" or "10.0.0.12".
func mentionsToken(text, token string) bool {
	text, token = strings.ToLower(text), strings.ToLower(strings.TrimSpace(token))
	if token == "" {
		return false
	}
	isIP := net.ParseIP(token) != nil
	for from := 0; ; {
		i := strings.Index(text[from:], token)
		if i < 0 {
			return false
		}
		start := from + i
		end := start + len(token)
		before := start == 0 || !isHostChar(text[start-1])
		if before && isIP && start > 0 && (text[start-1] == '.' || text[start-1] == ':') {
			before = false // the tail of a longer dotted or colon-separated value, not this address
		}
		after := end == len(text) || !isHostChar(text[end])
		if after && end+1 < len(text) && text[end] == '.' && isHostChar(text[end+1]) {
			after = false // the name continues: token is only a prefix of a longer host
		}
		if after && end < len(text) && text[end] == ':' && strings.Contains(token, ":") {
			after = false // an IPv6 address continues
		}
		if before && after {
			return true
		}
		from = start + 1
	}
}

// mentionsNetwork reports whether text contains a CIDR literal for the same network,
// however the operator wrote it ("10.0.0.5/24" names 10.0.0.0/24).
func mentionsNetwork(text string, network *net.IPNet) bool {
	for _, literal := range cidrLiteral.FindAllString(text, -1) {
		if _, parsed, err := net.ParseCIDR(literal); err == nil && parsed.String() == network.String() {
			return true
		}
	}
	return false
}
