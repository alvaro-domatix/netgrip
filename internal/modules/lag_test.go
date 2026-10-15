package modules

import (
	"strings"
	"testing"
)

func TestUciListValues(t *testing.T) {
	cases := []struct {
		name string
		line string
		want []string
	}{
		{"escalar", "network.cfg05dc9f.ports='lan1'", []string{"lan1"}},
		{"lista", "network.cfg05dc9f.ports='lan1' 'lan2' 'lan3'", []string{"lan1", "lan2", "lan3"}},
		{"sin igual", "network.cfg05dc9f", nil},
		{"vacio", "network.cfg05dc9f.ports=''", []string{""}},
		{"valor con espacios", "netgrip.x.description='Sala de estar'", []string{"Sala de estar"}},
	}
	for _, c := range cases {
		got := uciListValues(c.line)
		if len(got) != len(c.want) {
			t.Fatalf("%s: uciListValues(%q) = %v, want %v", c.name, c.line, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("%s: uciListValues(%q)[%d] = %q, want %q", c.name, c.line, i, got[i], c.want[i])
			}
		}
	}
}

func TestLagDeviceName(t *testing.T) {
	if got := lagDeviceName("lag0"); got != "bond-lag0" {
		t.Fatalf("lagDeviceName = %q", got)
	}
}

func TestSetLAGValidation(t *testing.T) {
	// En la máquina de tests no hay bridge ni uci: ProbeLAGs devuelve vacío y
	// lagPhysicalPorts no ve bocas. Las validaciones previas al sistema son
	// las que se ejercitan aquí (rechazos en la frontera, sin tocar nada).
	if _, _, err := SetLAG(LAGConfig{Name: "0mal", Mode: "802.3ad", Slaves: []string{"lan1", "lan2"}}); err == nil {
		t.Fatal("nombre inválido no rechazado")
	}
	if _, _, err := SetLAG(LAGConfig{Name: "lag0", Mode: "balance-alb", Slaves: []string{"lan1", "lan2"}}); err == nil {
		t.Fatal("modo no soportado no rechazado")
	}
	if _, _, err := SetLAG(LAGConfig{Name: "lag0", Mode: "802.3ad", Slaves: []string{"lan1"}}); err == nil {
		t.Fatal("menos de dos bocas no rechazado")
	}
	// Sin bridge en el entorno de tests, cualquier boca es desconocida.
	if _, _, err := SetLAG(LAGConfig{Name: "lag0", Mode: "802.3ad", Slaves: []string{"lan1", "lan2"}}); err == nil || !strings.Contains(err.Error(), "physical bridge port") {
		t.Fatalf("boca fuera del bridge no rechazada como esperaba: %v", err)
	}
}
