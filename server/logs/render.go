package logs

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"time"
)

// Rendered es un log listo para el campo source/format/payload de /api/ingest/batch.
type Rendered struct {
	Source  string
	Format  string
	Payload string
}

// Render convierte el log canónico al formato propio del servicio. Los
// formatos texto y xml salen como fuente "legacy": así se ejercita la
// normalización de FEAT-001 y no se les exigen los campos de una fuente cloud.
func Render(r Record) Rendered {
	ts := r.Time.UTC().Format(time.RFC3339Nano)
	switch r.Format {
	case "text":
		// Sin campo service: lo normal en un log de texto plano. Sentinel debe
		// atribuirlo por component_id (ver spike S-3 del plan).
		line := fmt.Sprintf("%s %s %s", ts, upper(r.Level), r.Message)
		if r.TraceID != "" {
			line += " trace_id=" + r.TraceID
		}
		return Rendered{Source: "legacy", Format: "text", Payload: line}
	case "xml":
		var b bytes.Buffer
		b.WriteString("<log>")
		for _, kv := range [][2]string{{"timestamp", ts}, {"severity", r.Level}, {"service", r.Service}, {"message", r.Message}, {"trace_id", r.TraceID}} {
			if kv[1] == "" {
				continue
			}
			b.WriteString("<" + kv[0] + ">")
			_ = xml.EscapeText(&b, []byte(kv[1]))
			b.WriteString("</" + kv[0] + ">")
		}
		b.WriteString("</log>")
		return Rendered{Source: "legacy", Format: "xml", Payload: b.String()}
	}
	body := map[string]any{
		"timestamp": ts, "severity": r.Level, "service": r.Service, "platform": r.Platform, "message": r.Message,
	}
	if r.TraceID != "" {
		body["trace_id"], body["span_id"] = r.TraceID, r.SpanID
	}
	if r.HTTP != nil {
		body["http"] = r.HTTP
	}
	if r.LatencyMs > 0 {
		body["latency_ms"] = r.LatencyMs
	}
	if r.ErrorKind != "" {
		body["error"] = map[string]string{"kind": r.ErrorKind}
	}
	raw, _ := json.Marshal(body)
	return Rendered{Source: "cloud", Format: "json", Payload: string(raw)}
}

func upper(level string) string {
	switch level {
	case Warning:
		return "WARN"
	case Error:
		return "ERROR"
	case Critical:
		return "CRITICAL"
	}
	return "INFO"
}
