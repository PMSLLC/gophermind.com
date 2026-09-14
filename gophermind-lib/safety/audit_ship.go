package safety

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"
)

// SetShipper attaches a function called with each recorded entry, so the audit
// trail can be streamed to a central collector (HTTP/OTLP/file) for monitoring.
// The shipper runs synchronously within Record; use a non-blocking shipper (e.g.
// HTTPShipper) to avoid adding latency to tool calls.
func (al *AuditLog) SetShipper(ship func(AuditEntry)) {
	if al == nil {
		return
	}
	al.ship = ship
}

// HTTPShipper returns a non-blocking shipper that POSTs each audit entry as JSON
// to a collector URL (best-effort; delivery failures are ignored).
func HTTPShipper(url string) func(AuditEntry) {
	client := &http.Client{Timeout: 5 * time.Second}
	return func(e AuditEntry) {
		b, err := json.Marshal(e)
		if err != nil {
			return
		}
		go func() {
			req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
			if err != nil {
				return
			}
			req.Header.Set("Content-Type", "application/json")
			if resp, err := client.Do(req); err == nil {
				resp.Body.Close()
			}
		}()
	}
}
