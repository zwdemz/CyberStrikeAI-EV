package dnslog

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
)

type request struct {
	operation, mainDomain, sessionID string
	wait                             time.Duration
}

// parseRequest rejects unused, unknown and mistyped fields before networking.
// Integer seconds are accepted from JSON numbers and native Go integer callers.
func parseRequest(args map[string]interface{}) (request, error) {
	result := request{wait: 5 * time.Second}
	for key, value := range args {
		if key == "wait_time" {
			var seconds float64
			switch number := value.(type) {
			case float64:
				seconds = number
			case int:
				seconds = float64(number)
			case json.Number:
				var err error
				seconds, err = number.Float64()
				if err != nil {
					return result, errors.New("wait_time must be an integer from 1 to 30")
				}
			default:
				return result, errors.New("wait_time must be an integer from 1 to 30")
			}
			if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 1 || seconds > 30 || math.Trunc(seconds) != seconds {
				return result, errors.New("wait_time must be an integer from 1 to 30")
			}
			result.wait = time.Duration(seconds) * time.Second
			continue
		}
		text, ok := value.(string)
		if !ok {
			return result, errors.New("dig.pm string parameter has an invalid type")
		}
		switch key {
		case "operation":
			result.operation = text
		case "main_domain":
			result.mainDomain = text
		case "session_id":
			result.sessionID = text
		default:
			return result, errors.New("unknown dig.pm parameter")
		}
	}
	switch result.operation {
	case "list_domains":
		if len(args) != 1 {
			return result, errors.New("list_domains accepts only operation")
		}
	case "get_domain":
		_, hasSession := args["session_id"]
		if _, exists := args["wait_time"]; exists || hasSession {
			return result, errors.New("get_domain accepts only main_domain")
		}
		if result.mainDomain != "" {
			var err error
			result.mainDomain, err = normalizeDomain(result.mainDomain)
			if err != nil {
				return result, err
			}
		}
	case "get_records":
		_, hasDomain := args["main_domain"]
		if _, err := uuid.Parse(result.sessionID); err != nil || hasDomain {
			return result, errors.New("get_records requires a valid session_id and optional wait_time")
		}
	default:
		return result, errors.New("operation must be list_domains, get_domain or get_records")
	}
	return result, nil
}

// normalizeDomain validates ASCII DNS labels (including punycode) and removes
// one optional root dot. It intentionally does not assume a fixed prefix length.
func normalizeDomain(value string) (string, error) {
	value = strings.ToLower(strings.TrimSuffix(value, "."))
	if len(value) < 1 || len(value) > 253 {
		return "", errors.New("invalid DNS domain")
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) < 1 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("invalid DNS label")
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
				return "", errors.New("invalid DNS label")
			}
		}
	}
	return value, nil
}
