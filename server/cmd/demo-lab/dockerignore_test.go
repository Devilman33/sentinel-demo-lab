package main

import (
	"os"
	"strings"
	"testing"
)

// R-6: las capturas de portales reales, el estado con tokens y los perfiles con
// secretos no pueden entrar a la imagen. La garantía es que el .dockerignore sea
// una lista de permitidos: todo excluido salvo lo que el Dockerfile copia.
func TestDockerignoreEsUnaListaDePermitidos(t *testing.T) {
	raw, err := os.ReadFile("../../../.dockerignore")
	if err != nil {
		t.Fatal(err)
	}
	var reglas []string
	for _, l := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			reglas = append(reglas, l)
		}
	}
	if len(reglas) == 0 || reglas[0] != "*" {
		t.Fatalf("la primera regla debe excluirlo todo (*): %v", reglas)
	}
	permitidos := map[string]bool{"!go.mod": true, "!go.sum": true, "!server/": true}
	for _, r := range reglas[1:] {
		if !permitidos[r] {
			t.Errorf("regla no esperada en el .dockerignore: %q", r)
		}
	}
}
