package config

import (
	"fmt"
	"math"
)

// ValidateAuditTemperature validates the optional audit-only sampling override.
// Nil retains the existing default; non-finite values or values outside [0,2]
// return a configuration error before a request or configuration mutation.
func (h HitlConfig) ValidateAuditTemperature() error {
	temperature := h.AuditModel.Temperature
	if temperature != nil && (math.IsNaN(*temperature) || math.IsInf(*temperature, 0) || *temperature < 0 || *temperature > 2) {
		return fmt.Errorf("hitl.audit_model.temperature must be finite and between 0 and 2")
	}
	return nil
}
