package attacks

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// TC-12: un objetivo que no es un servicio del módulo se rechaza sin enviar ninguna petición.
func TestRechazaObjetivosFueraDeLaLista(t *testing.T) {
	var hits atomic.Int64
	ext := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer ext.Close()

	g := New()
	for _, target := range []string{ext.URL, "example.com", "127.0.0.1:7104", "localhost", "http://10.0.0.1/", "../auth-banca", ""} {
		if _, err := g.Launch("banco", target, "brute_force", 5, 10, "test"); err == nil {
			t.Errorf("el objetivo %q debía rechazarse", target)
		}
	}
	if _, err := g.Launch("mercado", "auth-banca", "brute_force", 5, 10, "test"); err == nil {
		t.Error("un servicio de otra plataforma debía rechazarse")
	}
	if hits.Load() != 0 || len(g.List()) != 0 {
		t.Fatalf("no debió salir ninguna petición ni crearse ataque (peticiones %d, ataques %d)", hits.Load(), len(g.List()))
	}
}

func TestRechazaTipoOIntensidadInvalidos(t *testing.T) {
	g := New()
	cases := []struct {
		name                   string
		platform, target, kind string
		intensity, durationS   int
	}{
		{"tipo desconocido", "banco", "auth-banca", "ddos", 1, 5},
		{"tipo que no aplica al servicio", "banco", "core-bancario", "brute_force", 1, 5},
		{"intensidad", "banco", "auth-banca", "brute_force", MaxIntensity + 1, 5},
		{"duración", "banco", "auth-banca", "brute_force", 1, MaxDuration + 1},
		{"plataforma", "nada", "auth-banca", "brute_force", 1, 5},
	}
	for _, c := range cases {
		if _, err := g.Launch(c.platform, c.target, c.kind, c.intensity, c.durationS, "test"); err == nil {
			t.Errorf("%s: debía rechazarse", c.name)
		}
	}
}

func TestCadaTipoTieneObjetivosDelCatalogo(t *testing.T) {
	for _, k := range Kinds() {
		found := 0
		for _, p := range []string{"mercado", "banco", "clinica"} {
			for _, target := range k.Targets {
				if ValidKind(p, target, k.Name) == nil {
					found++
				}
			}
		}
		if found == 0 {
			t.Errorf("%s no tiene ningún objetivo válido en el catálogo", k.Name)
		}
	}
}
