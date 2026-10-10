// Package testproxy manages opt-in account test proxies independently of model clients.
package testproxy

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Node is a proxy endpoint. Secret contains encrypted credentials and is never returned by the API.
type Node struct {
	ID       string `json:"id"`
	Address  string `json:"address"`
	Region   string `json:"region,omitempty"`
	SourceID string `json:"source_id,omitempty"`
	Enabled  bool   `json:"enabled"`
	Secret   string `json:"secret,omitempty"`
}

// ParseImport accepts Markdown, CSV, TSV, or one URL per line. Invalid/conflicting rows reject the entire import.
// Returned URLs may contain credentials and must be protected before persistence or response serialization.
func ParseImport(raw string) ([]Node, error) {
	if len(raw) == 0 || len(raw) > 256*1024 {
		return nil, fmt.Errorf("import must contain 1–262144 bytes")
	}
	raw = strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff"))
	var records [][]string
	first := strings.Split(raw, "\n")[0]
	isURLList := strings.HasPrefix(first, "http://") || strings.HasPrefix(first, "https://") || strings.HasPrefix(first, "socks5://")
	if !isURLList && strings.Contains(first, "|") {
		for _, line := range strings.Split(raw, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			row := strings.Split(strings.Trim(line, "|"), "|")
			separator := true
			for i := range row {
				row[i] = strings.TrimSpace(row[i])
				if strings.Trim(row[i], "-: ") != "" {
					separator = false
				}
			}
			if !separator {
				records = append(records, row)
			}
		}
	} else if !isURLList && (strings.Contains(first, "\t") || strings.Contains(first, ",")) {
		r := csv.NewReader(strings.NewReader(raw))
		r.FieldsPerRecord = -1
		r.TrimLeadingSpace = true
		if strings.Contains(first, "\t") {
			r.Comma = '\t'
		}
		var err error
		records, err = r.ReadAll()
		if err != nil {
			return nil, fmt.Errorf("invalid delimited table")
		}
	} else {
		for _, line := range strings.Split(raw, "\n") {
			if strings.TrimSpace(line) != "" {
				records = append(records, []string{strings.TrimSpace(line)})
			}
		}
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("empty import")
	}
	head := map[string]int{}
	for i, name := range records[0] {
		key := strings.ToLower(strings.TrimSpace(name))
		if key != "" {
			if _, exists := head[key]; exists {
				return nil, fmt.Errorf("duplicate column header")
			}
			head[key] = i
		}
	}
	_, table := head["proxyaddr"]
	if _, ok := head["host"]; ok {
		table = true
	}
	if table {
		records = records[1:]
	}
	seen := map[string]bool{}
	out := []Node{}
	for rowIndex, row := range records {
		get := func(keys ...string) string {
			for _, k := range keys {
				if i, ok := head[k]; ok && i < len(row) {
					return strings.TrimSpace(row[i])
				}
			}
			return ""
		}
		address := strings.TrimSpace(row[0])
		region, source, enabled := "", "", true
		if table {
			address = get("proxyaddr")
			host, port := get("host"), get("port")
			kind := strings.ToLower(get("类型", "type", "protocol"))
			if kind == "" {
				kind = "socks5"
			}
			if address == "" {
				address = kind + "://" + net.JoinHostPort(strings.Trim(host, "[]"), port)
			}
			u, err := url.Parse(address)
			if err != nil || u.Hostname() == "" {
				return nil, fmt.Errorf("invalid endpoint at row %d", rowIndex+1)
			}
			if host != "" && !strings.EqualFold(strings.Trim(host, "[]"), u.Hostname()) || port != "" && port != u.Port() || get("类型", "type", "protocol") != "" && kind != strings.ToLower(u.Scheme) {
				return nil, fmt.Errorf("conflicting endpoint columns at row %d", rowIndex+1)
			}
			user, pass := proxyTableCredentials(get("账号", "username", "user"), get("密码", "password"))
			if user != "" || pass != "" {
				if u.User != nil && u.User.String() != url.UserPassword(user, pass).String() {
					return nil, fmt.Errorf("conflicting credentials at row %d", rowIndex+1)
				}
				u.User = url.UserPassword(user, pass)
			}
			address = u.String()
			region = get("地区", "region")
			source = get("db_id", "source_id")
			switch strings.ToLower(get("状态", "status", "enabled")) {
			case "禁用", "停用", "false", "0", "disabled":
				enabled = false
			case "", "启用", "true", "1", "enabled":
			default:
				return nil, fmt.Errorf("invalid node status at row %d", rowIndex+1)
			}
		}
		u, err := url.Parse(address)
		if err != nil || u == nil {
			return nil, fmt.Errorf("invalid URL at row %d", rowIndex+1)
		}
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5") || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return nil, fmt.Errorf("unsupported proxy URL at row %d", rowIndex+1)
		}
		if strings.ContainsAny(u.Hostname(), " \t\r\n") || len(address) > 2048 || len(region) > 100 || len(source) > 100 {
			return nil, fmt.Errorf("invalid proxy fields at row %d", rowIndex+1)
		}
		u.Host = net.JoinHostPort(strings.ToLower(u.Hostname()), strconv.Itoa(port))
		u.Path = ""
		identity := u.Scheme + "://" + u.Host
		if seen[identity] {
			return nil, fmt.Errorf("duplicate endpoint at row %d", rowIndex+1)
		}
		seen[identity] = true
		hash := sha256.Sum256([]byte(identity))
		out = append(out, Node{ID: hex.EncodeToString(hash[:8]), Address: u.String(), Region: region, SourceID: source, Enabled: enabled})
	}
	if len(out) == 0 || len(out) > 200 {
		return nil, fmt.Errorf("a pool must have 1–200 unique nodes")
	}
	return out, nil
}

// proxyTableCredentials recognizes paired blank export markers only in table columns.
// If either field contains a real value, preserve both fields (including a literal dash
// password). URL userinfo is never normalized by this helper.
func proxyTableCredentials(user, password string) (string, string) {
	isBlank := func(value string) bool { return value == "" || value == "-" || value == "--" }
	if isBlank(user) && isBlank(password) {
		return "", ""
	}
	return user, password
}
