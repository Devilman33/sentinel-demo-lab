// Package logs guarda los logs de los servicios simulados y los entrega al
// envío hacia Sentinel. Todo log queda en un NDJSON por servicio, se envíe o no.
package logs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	Info     = "info"
	Warning  = "warning"
	Error    = "error"
	Critical = "critical"
)

// Weight ordena los niveles para filtrar por mínimo.
func Weight(level string) int {
	switch level {
	case Critical:
		return 3
	case Error:
		return 2
	case Warning:
		return 1
	}
	return 0
}

type HTTPInfo struct {
	Method string `json:"method"`
	Route  string `json:"route"`
	Status int    `json:"status"`
}

// Record es el log canónico. A Sentinel se envía renderizado en el formato del
// servicio (json, texto plano o xml); en disco siempre queda así.
type Record struct {
	Time      time.Time `json:"timestamp"`
	Level     string    `json:"severity"`
	Platform  string    `json:"platform"`
	Service   string    `json:"service"`
	Message   string    `json:"message"`
	TraceID   string    `json:"trace_id,omitempty"`
	SpanID    string    `json:"span_id,omitempty"`
	HTTP      *HTTPInfo `json:"http,omitempty"`
	LatencyMs int64     `json:"latency_ms,omitempty"`
	ErrorKind string    `json:"error_kind,omitempty"`
	Format    string    `json:"format"` // json | text | xml
}

const (
	maxFileBytes = 5 << 20
	ringSize     = 2000
)

type Logger struct {
	dir string

	mu    sync.Mutex
	files map[string]*os.File
	sinks []func(Record)
	ring  []Record
}

func New(dir string) *Logger {
	return &Logger{dir: dir, files: map[string]*os.File{}}
}

// Close cierra los archivos abiertos.
func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for path, f := range l.files {
		_ = f.Close()
		delete(l.files, path)
	}
}

// Subscribe registra quien recibe cada log nuevo (el envío a Sentinel).
func (l *Logger) Subscribe(fn func(Record)) {
	l.mu.Lock()
	l.sinks = append(l.sinks, fn)
	l.mu.Unlock()
}

// Emit guarda el log y lo entrega a los suscriptores. Un fallo de disco no
// debe tumbar al servicio: el log sigue en memoria y se envía igual.
func (l *Logger) Emit(r Record) {
	if r.Time.IsZero() {
		r.Time = time.Now().UTC()
	}
	line, _ := json.Marshal(r)

	l.mu.Lock()
	l.write(r, line)
	l.ring = append(l.ring, r)
	if len(l.ring) > ringSize {
		l.ring = l.ring[len(l.ring)-ringSize:]
	}
	sinks := append([]func(Record){}, l.sinks...)
	l.mu.Unlock()

	for _, s := range sinks {
		s(r)
	}
}

func (l *Logger) write(r Record, line []byte) {
	if l.dir == "" {
		return
	}
	path := filepath.Join(l.dir, r.Platform, r.Service+".ndjson")
	f := l.files[path]
	if f == nil {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return
		}
		var err error
		if f, err = os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err != nil {
			return
		}
		l.files[path] = f
	}
	if st, err := f.Stat(); err == nil && st.Size() > maxFileBytes {
		f.Close()
		delete(l.files, path)
		_ = os.Rename(path, path+".1")
		if f, err = os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err != nil {
			return
		}
		l.files[path] = f
	}
	_, _ = f.Write(append(line, '\n'))
}

// Filter selecciona logs; los campos vacíos no filtran.
type Filter struct {
	Platform, Service string
	MinLevel          string
	Since, Until      time.Time
	Limit             int
}

func (f Filter) match(r Record) bool {
	switch {
	case f.Platform != "" && r.Platform != f.Platform,
		f.Service != "" && r.Service != f.Service,
		f.MinLevel != "" && Weight(r.Level) < Weight(f.MinLevel),
		!f.Since.IsZero() && r.Time.Before(f.Since),
		!f.Until.IsZero() && r.Time.After(f.Until):
		return false
	}
	return true
}

// Recent devuelve los últimos logs en memoria que cumplen el filtro, del más viejo al más nuevo.
func (l *Logger) Recent(f Filter) []Record {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []Record
	for _, r := range l.ring {
		if f.match(r) {
			out = append(out, r)
		}
	}
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[len(out)-f.Limit:]
	}
	return out
}

// FromDisk lee los logs guardados (incluida la rotación anterior) que cumplen el filtro.
func (l *Logger) FromDisk(f Filter) ([]Record, error) {
	var out []Record
	for _, suffix := range []string{".ndjson.1", ".ndjson"} {
		matches, err := filepath.Glob(filepath.Join(l.dir, "*", "*"+suffix))
		if err != nil {
			return nil, err
		}
		for _, path := range matches {
			recs, err := readFile(path)
			if err != nil {
				return nil, err
			}
			for _, r := range recs {
				if f.match(r) {
					out = append(out, r)
				}
			}
		}
	}
	return out, nil
}

func readFile(path string) ([]Record, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Record
	start := 0
	for i, b := range raw {
		if b != '\n' {
			continue
		}
		var r Record
		if json.Unmarshal(raw[start:i], &r) == nil {
			out = append(out, r)
		}
		start = i + 1
	}
	return out, nil
}
