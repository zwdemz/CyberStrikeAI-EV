package dnslog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// Record contains provider DNS evidence. ClientIP is the recursive resolver's
// address, not necessarily the tested application's source address.
type Record struct {
	FullDomain string      `json:"FullDomain"`
	ClientIP   string      `json:"ClientIp"`
	Location   string      `json:"Location"`
	CreatedAt  string      `json:"CreatedAt"`
	UUID       string      `json:"UUID"`
	ID         interface{} `json:"ID,omitempty"`
}

// RecordsResult distinguishes a completed empty window from errors or limits.
// Truncated signals an early record/message cap, not a complete observation.
type RecordsResult struct {
	Status      string   `json:"status"`
	Domain      string   `json:"domain"`
	Records     []Record `json:"records"`
	RecordCount int      `json:"record_count"`
	Truncated   bool     `json:"truncated"`
}

// records keeps a single bounded subscription. Cancellation closes the socket;
// only the local observation deadline is treated as a successful empty result.
// An early disconnect, malformed frame or failed handshake returns a safe error.
func (c *Client) records(ctx context.Context, owner, id string, wait time.Duration) (*RecordsResult, error) {
	c.mu.Lock()
	entry, ok := c.sessions[id]
	if ok && !time.Now().Before(entry.expires) {
		delete(c.sessions, id)
		ok = false
	}
	c.mu.Unlock()
	if !ok || entry.owner != owner {
		return nil, errors.New("dig.pm session is unavailable for this caller; allocate a new session")
	}
	query := url.Values{"token": {entry.Token}, "subDomain": {entry.SubDomain}, "mainDomain": {entry.MainDomain}}
	endpoint := "wss" + strings.TrimPrefix(c.base, "https") + "/ws?" + query.Encode()
	connection, response, err := c.dialer.DialContext(ctx, endpoint, nil)
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if err != nil {
		return nil, errors.New("dig.pm WebSocket connection failed; check connectivity or allocate a new session")
	}
	defer connection.Close()
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stop()
	connection.SetReadLimit(maxResponseBytes)
	deadline := time.Now().Add(wait)
	if entry.expires.Before(deadline) {
		deadline = entry.expires
	}
	_ = connection.SetReadDeadline(deadline)
	result := &RecordsResult{Status: "no_records", Domain: strings.TrimSuffix(entry.FullDomain, "."), Records: []Record{}}
	seen := make(map[string]bool)
	bytesRead := 0
	for messages := 0; messages < 256; messages++ {
		kind, body, err := connection.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return nil, errors.New("dig.pm observation was cancelled")
			}
			if !time.Now().Before(entry.expires) {
				return nil, errors.New("dig.pm local session expired; allocate a new session")
			}
			var timeout net.Error
			if errors.As(err, &timeout) && timeout.Timeout() && !time.Now().Before(deadline) {
				return result, nil
			}
			return nil, errors.New("dig.pm record stream ended before the observation window completed; retry the session")
		}
		if kind != websocket.TextMessage {
			return nil, errors.New("dig.pm returned an unsupported WebSocket frame")
		}
		bytesRead += len(body)
		if bytesRead > 4*maxResponseBytes {
			return truncated(result), nil
		}
		records, err := decodeRecords(body)
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			domain, err := normalizeDomain(record.FullDomain)
			if err != nil || (domain != result.Domain && !strings.HasSuffix(domain, "."+result.Domain)) {
				continue
			}
			safe, err := sanitizeRecord(record, entry.Token)
			if err != nil {
				return nil, err
			}
			key := recordKey(safe)
			if seen[key] {
				continue
			}
			seen[key] = true
			result.Records = append(result.Records, safe)
			result.RecordCount++
			result.Status = "success"
			if result.RecordCount >= 100 {
				return truncated(result), nil
			}
		}
	}
	return truncated(result), nil
}

func truncated(result *RecordsResult) *RecordsResult {
	result.Status, result.Truncated = "partial", true
	return result
}

// decodeRecords accepts the three documented envelope types; malformed data,
// null payloads and excessive batches return a generic protocol error.
func decodeRecords(body []byte) ([]Record, error) {
	var message struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	invalid := errors.New("dig.pm returned an invalid record message")
	if json.Unmarshal(body, &message) != nil {
		return nil, invalid
	}
	var records []Record
	switch message.Type {
	case "history", "new_records":
		if string(message.Data) == "null" || decodeRecordData(message.Data, &records) != nil || len(records) > 10000 {
			return nil, invalid
		}
	case "new_record":
		var record Record
		if string(message.Data) == "null" || decodeRecordData(message.Data, &record) != nil {
			return nil, invalid
		}
		records = []Record{record}
	default:
		return nil, invalid
	}
	return records, nil
}

// decodeRecordData preserves numeric provider IDs without float64 rounding.
func decodeRecordData(body []byte, destination interface{}) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	return decoder.Decode(destination)
}

// sanitizeRecord bounds untrusted fields and removes any echoed session token.
// Only scalar IDs are allowed; structured IDs return a generic protocol error.
func sanitizeRecord(record Record, token string) (Record, error) {
	for _, field := range []*string{&record.FullDomain, &record.ClientIP, &record.Location, &record.CreatedAt, &record.UUID} {
		if len(*field) > 1024 {
			return Record{}, errors.New("dig.pm returned an oversized record field")
		}
		*field = strings.ReplaceAll(*field, token, "[redacted]")
	}
	switch id := record.ID.(type) {
	case nil, json.Number:
	case string:
		if len(id) > 1024 {
			return Record{}, errors.New("dig.pm returned an oversized record ID")
		}
		record.ID = strings.ReplaceAll(id, token, "[redacted]")
	default:
		return Record{}, errors.New("dig.pm returned an invalid record ID")
	}
	return record, nil
}

func recordKey(record Record) string {
	if record.UUID != "" {
		return "uuid:" + record.UUID
	}
	if record.ID != nil && record.ID != "" {
		encoded, _ := json.Marshal(record.ID)
		return "id:" + string(encoded)
	}
	encoded, _ := json.Marshal([]string{record.FullDomain, record.ClientIP, record.CreatedAt})
	return "fields:" + string(encoded)
}
