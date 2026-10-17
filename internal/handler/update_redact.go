package handler

import (
	"bytes"
	"cyberstrike-ai/internal/update"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http"
)

// safeUpdateOutput copies JSON transport data and redacts every string, including
// nested errors and job results. Serialization errors fail closed, never returning
// the original value. JSON numbers retain their exact representation.
func safeUpdateOutput(value any, source string) any {
	encoded, err := json.Marshal(value)
	if err != nil {
		return gin.H{"error": "update response unavailable"}
	}
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if decoder.Decode(&decoded) != nil {
		return gin.H{"error": "update response unavailable"}
	}
	var clean func(any) any
	clean = func(value any) any {
		switch item := value.(type) {
		case string:
			return update.RedactSourceText(item, source)
		case []any:
			for i := range item {
				item[i] = clean(item[i])
			}
			return item
		case map[string]any:
			result := make(map[string]any, len(item))
			for key, entry := range item {
				result[update.RedactSourceText(key, source)] = clean(entry)
			}
			return result
		default:
			return value
		}
	}
	return clean(decoded)
}

// respond centralizes redaction for all update HTTP responses without changing
// the internal source used for Git operations or saving configuration.
func (h *UpdateHandler) respond(c *gin.Context, status int, value any) {
	source := h.options().Repo
	safe := safeUpdateOutput(value, source)
	if safe == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "update response unavailable"})
		return
	}
	c.JSON(status, safe)
}
