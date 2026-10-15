package modules

import (
	"testing"
)

// Salida sintética de `poe-util status` con el formato que el parser espera:
// línea de presupuesto, una boca activa, una deshabilitada y una apagada.
const poeUtilFixture = `Total Power Budget: 130 W
lan1 Enabled 4.2W
lan2 Disabled 0W
lan3 off 0
`

func TestParsePoeUtilStatus(t *testing.T) {
	pp := &PoEProbe{Ports: []PoEPort{}}
	parsePoeUtilStatus(pp, poeUtilFixture)
	if !pp.Applicable {
		t.Fatal("applicable no marcado")
	}
	if pp.TotalBudgetW != 130 {
		t.Fatalf("budget = %v, want 130", pp.TotalBudgetW)
	}
	if len(pp.Ports) != 3 {
		t.Fatalf("ports = %d, want 3: %+v", len(pp.Ports), pp.Ports)
	}
	if pp.Ports[0].Name != "lan1" || !pp.Ports[0].Enabled || pp.Ports[0].PowerW != 4.2 {
		t.Fatalf("lan1 mal parseada: %+v", pp.Ports[0])
	}
	if pp.Ports[1].Enabled {
		t.Fatalf("lan2 Disabled debía quedar !Enabled: %+v", pp.Ports[1])
	}
	if pp.Ports[2].Enabled {
		t.Fatalf("lan3 off debía quedar !Enabled: %+v", pp.Ports[2])
	}
	if pp.UsedW != 4.2 {
		t.Fatalf("used = %v, want 4.2", pp.UsedW)
	}
}

func TestParsePoeUtilStatusGarbage(t *testing.T) {
	pp := &PoEProbe{Ports: []PoEPort{}}
	parsePoeUtilStatus(pp, "\n\n  \n")
	// Líneas sin campos suficientes se descartan, pero el probe queda aplicable.
	if len(pp.Ports) != 0 {
		t.Fatalf("ports = %d con entrada vacía: %+v", len(pp.Ports), pp.Ports)
	}
}
