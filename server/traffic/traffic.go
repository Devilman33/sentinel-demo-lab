// Package traffic genera usuarios sintéticos que recorren las acciones de cada
// plataforma contra su BFF, para que los servicios tengan actividad (y logs).
package traffic

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"demo-lab/server/catalog"
	"demo-lab/server/runtime"
)

const MaxRPS = 50

type Generator struct {
	client *http.Client

	mu      sync.Mutex
	cancels map[string]context.CancelFunc
	rps     map[string]float64
}

func New() *Generator {
	return &Generator{client: &http.Client{Timeout: 10 * time.Second}, cancels: map[string]context.CancelFunc{}, rps: map[string]float64{}}
}

// Set fija los usuarios por segundo de una plataforma; 0 detiene el tráfico.
func (g *Generator) Set(platform string, rps float64) error {
	p, ok := catalog.BySlug(platform)
	if !ok {
		return fmt.Errorf("plataforma %q desconocida", platform)
	}
	if rps < 0 || rps > MaxRPS {
		return fmt.Errorf("rps debe estar entre 0 y %d", MaxRPS)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if cancel := g.cancels[platform]; cancel != nil {
		cancel()
		delete(g.cancels, platform)
	}
	g.rps[platform] = rps
	if rps == 0 || len(p.Actions) == 0 {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	g.cancels[platform] = cancel
	go g.loop(ctx, p, rps)
	return nil
}

func (g *Generator) RPS(platform string) float64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.rps[platform]
}

func (g *Generator) StopAll() {
	for slug := range g.rps {
		_ = g.Set(slug, 0)
	}
}

func (g *Generator) loop(ctx context.Context, p catalog.Platform, rps float64) {
	t := time.NewTicker(time.Duration(float64(time.Second) / rps))
	defer t.Stop()
	entry := runtime.BaseURL(p.EntryService())
	for i := 0; ; i++ {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			action := p.Actions[i%len(p.Actions)].Name
			go g.hit(ctx, entry, action)
		}
	}
}

func (g *Generator) hit(ctx context.Context, entry, action string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, entry+"/v1/action/"+action, nil)
	if err != nil {
		return
	}
	if res, err := g.client.Do(req); err == nil {
		res.Body.Close()
	}
}
