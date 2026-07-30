package openai

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bytedance/sonic"
	"go.uber.org/zap"
)

const (
	requestDiagRespMaxBytes  = 8192
	requestDiagRawExcerptBytes = 4096
)

// AttachRequestErrorDiagTransport wraps client.Transport to log the request body
// (structured summary + raw excerpt) and the parsed gateway error whenever a
// /chat/completions call returns HTTP >= 400. Success responses (including SSE
// streaming) are passed through untouched, so streaming is never disturbed.
//
// Why this exists: the ARK coding-plan gateway occasionally returns a GENERIC 400
// ("Invalid request body" / "A parameter specified in the request is not valid")
// carrying no field name, whereas field-specific violations normally do. Such
// generic rejections cannot be reproduced with synthetic requests, so capturing
// the real failing request body is the only way to see which structural shape the
// gateway rejects. The transport only buffers bodies on >= 400 (small non-streaming
// JSON error payloads); 200 responses are never read here.
func AttachRequestErrorDiagTransport(client *http.Client, logger *zap.Logger) {
	if client == nil || logger == nil {
		return
	}
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Transport = &requestErrorDiagRoundTripper{base: base, logger: logger}
}

type requestErrorDiagRoundTripper struct {
	base   http.RoundTripper
	logger *zap.Logger
}

func (rt *requestErrorDiagRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	isChat := req != nil &&
		req.Method == http.MethodPost &&
		strings.HasSuffix(req.URL.Path, "/chat/completions")

	var reqBody []byte
	if isChat && req.Body != nil {
		b, err := io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			req.Body = nil
			req.ContentLength = 0
		} else {
			reqBody = b
			req.Body = io.NopCloser(bytes.NewReader(b))
			req.ContentLength = int64(len(b))
			req.Header.Set("Content-Length", strconv.Itoa(len(b)))
		}
	}

	resp, err := rt.base.RoundTrip(req)
	if err != nil || resp == nil {
		return resp, err
	}
	// Success (incl. streaming 2xx): do NOT touch the body — preserve SSE.
	if resp.StatusCode < 400 || !isChat {
		return resp, nil
	}

	// >= 400 on chat/completions: errors are small non-streaming JSON; safe to buffer.
	respBody, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		resp.Body = io.NopCloser(bytes.NewReader(nil))
		resp.ContentLength = 0
		return resp, nil
	}
	// Restore body for upstream consumers (SDK / sanitizer / claude bridge).
	resp.Body = io.NopCloser(bytes.NewReader(respBody))
	resp.ContentLength = int64(len(respBody))

	rt.logFailure(req, resp, reqBody, respBody)
	return resp, nil
}

func (rt *requestErrorDiagRoundTripper) logFailure(req *http.Request, resp *http.Response, reqBody, respBody []byte) {
	if rt.logger == nil {
		return
	}
	fields := []zap.Field{
		zap.Int("status", resp.StatusCode),
		zap.String("url", req.URL.String()),
	}
	if code, msg, typ, ok := parseAPIErrorFields(respBody); ok {
		fields = append(fields,
			zap.String("err_code", code),
			zap.String("err_message", msg),
			zap.String("err_type", typ),
		)
	} else {
		fields = append(fields, zap.String("resp_body", truncateForLog(string(respBody), requestDiagRespMaxBytes)))
	}
	if sum, ok := summarizeChatRequestBody(reqBody); ok {
		fields = append(fields, zap.Any("req_summary", sum))
	}
	fields = append(fields,
		zap.Int("req_body_bytes", len(reqBody)),
		zap.String("req_body_head", truncateForLog(string(reqBody), requestDiagRawExcerptBytes)),
	)
	if dumpPath, derr := dumpRequestBodyForReplay(reqBody); derr == nil {
		fields = append(fields, zap.String("req_body_dump", dumpPath))
	} else {
		fields = append(fields, zap.String("req_body_dump_err", derr.Error()))
	}
	rt.logger.Warn("eino chat/completions rejected by gateway (4xx request-body diag)", fields...)
}

// dumpRequestBodyForReplay writes the full request body to tmp/diag_4xx_<unix_ms>.json
// so the exact failing request can be replayed (curl) and bisected offline.
func dumpRequestBodyForReplay(body []byte) (string, error) {
	if len(body) == 0 {
		return "", nil
	}
	dir := "tmp"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("diag_4xx_%d.json", time.Now().UnixNano()/int64(time.Millisecond))
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// parseAPIErrorFields extracts code/message/type from a standard OpenAI-style
// {"error":{"code":...,"message":...,"type":...}} body. code may be string or number.
func parseAPIErrorFields(body []byte) (code, message, typ string, ok bool) {
	var er struct {
		Error struct {
			Code    any    `json:"code"`
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := sonic.Unmarshal(body, &er); err != nil {
		return "", "", "", false
	}
	if er.Error.Message == "" && er.Error.Type == "" {
		return "", "", "", false
	}
	switch c := er.Error.Code.(type) {
	case string:
		code = c
	case float64:
		code = strconv.FormatFloat(c, 'f', -1, 64)
	}
	return code, er.Error.Message, er.Error.Type, true
}

// summarizeChatRequestBody builds a compact diagnostic view of a chat/completions
// request: top-level fields, reasoning/tool_choice values, the message role
// sequence (with tool_call/tool_call_id annotations), orphan-tool detection,
// empty/null-content flags, and a tools schema sanity check.
func summarizeChatRequestBody(body []byte) (map[string]any, bool) {
	var raw map[string]any
	if err := sonic.Unmarshal(body, &raw); err != nil {
		return nil, false
	}
	sum := map[string]any{}
	if m, ok := raw["model"].(string); ok {
		sum["model"] = m
	}
	if v, ok := raw["stream"].(bool); ok {
		sum["stream"] = v
	}
	for _, k := range []string{"reasoning_effort", "thinking", "tool_choice", "max_tokens", "temperature", "top_p", "parallel_tool_calls"} {
		if v, ok := raw[k]; ok {
			sum[k] = v
		}
	}
	// All present top-level field names (sorted), excluding the bulky arrays.
	keys := make([]string, 0, len(raw))
	for k := range raw {
		if k == "messages" || k == "tools" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sum["fields"] = keys

	if msgs, ok := raw["messages"].([]any); ok {
		provided := make(map[string]struct{}, 8)
		for _, mi := range msgs {
			m, _ := mi.(map[string]any)
			if m == nil {
				continue
			}
			if r, _ := m["role"].(string); r == "assistant" {
				if tcs, ok := m["tool_calls"].([]any); ok {
					for _, tci := range tcs {
						tc, _ := tci.(map[string]any)
						if id, _ := tc["id"].(string); id != "" {
							provided[id] = struct{}{}
						}
					}
				}
			}
		}
		seq := make([]string, 0, len(msgs))
		emptyContent := []int{}
		nullContent := []int{}
		orphan := []int{}
		for i, mi := range msgs {
			m, _ := mi.(map[string]any)
			if m == nil {
				seq = append(seq, "<nil>")
				continue
			}
			role, _ := m["role"].(string)
			tag := role
			if tcs, ok := m["tool_calls"].([]any); ok && len(tcs) > 0 {
				tag = role + "(tc=" + strconv.Itoa(len(tcs)) + ")"
			}
			if tcID, _ := m["tool_call_id"].(string); tcID != "" {
				tag = role + "(tci=" + tcID + ")"
				if _, ok := provided[tcID]; !ok {
					orphan = append(orphan, i)
				}
			}
			if c, hasC := m["content"]; hasC {
				if c == nil {
					nullContent = append(nullContent, i)
				} else if s, ok := c.(string); ok && s == "" {
					emptyContent = append(emptyContent, i)
				}
			}
			seq = append(seq, tag)
		}
		sum["messages_count"] = len(msgs)
		sum["messages_seq"] = seq
		if len(emptyContent) > 0 {
			sum["messages_empty_content_idx"] = emptyContent
		}
		if len(nullContent) > 0 {
			sum["messages_null_content_idx"] = nullContent
		}
		if len(orphan) > 0 {
			sum["messages_orphan_tool_idx"] = orphan
		}
	}

	if tools, ok := raw["tools"].([]any); ok {
		names := make([]string, 0, len(tools))
		badSchema := []string{}
		for _, ti := range tools {
			t, _ := ti.(map[string]any)
			if t == nil {
				continue
			}
			fn, _ := t["function"].(map[string]any)
			if fn == nil {
				continue
			}
			name, _ := fn["name"].(string)
			if name == "" {
				name = "<unnamed>"
			}
			names = append(names, name)
			if p, ok := fn["parameters"]; ok {
				if pm, ok := p.(map[string]any); !ok {
					badSchema = append(badSchema, name+"(params_not_object)")
				} else if _, ok := pm["type"].(string); !ok {
					badSchema = append(badSchema, name+"(no_type)")
				}
			} else {
				badSchema = append(badSchema, name+"(no_params)")
			}
		}
		sum["tools_count"] = len(tools)
		sum["tool_names"] = names
		if len(badSchema) > 0 {
			sum["tools_bad_schema"] = badSchema
		}
	}
	return sum, true
}
