package modules

import (
	"testing"
)

// Salida real de `brctl showmacs br-lan` en un bridge con VLAN filtering:
// la misma MAC repetida por VLAN, una entrada local (del propio bridge) y
// una boca no mapeada en /sys (se descarta).
const showmacsFullFixture = `port no	mac addr		is local?	ageing timer
  1	aa:bb:cc:00:00:01	no		   4.21
  1	aa:bb:cc:00:00:01	no		  12.87
  2	aa:bb:cc:00:00:02	no		   0.00
  2	aa:bb:cc:00:00:03	yes		   0.00
  9	aa:bb:cc:00:00:04	no		  55.10
`

func TestParseShowmacsFull(t *testing.T) {
	entries := parseShowmacsFull(showmacsFullFixture, map[int]string{1: "lan2", 2: "lan1"})
	if len(entries) != 3 {
		t.Fatalf("entries = %d, esperaba 3 (dedup por boca+MAC): %+v", len(entries), entries)
	}
	// Ordenadas por boca y luego MAC: lan1(x2), lan2.
	if entries[0].Port != "lan1" || entries[0].MAC != "aa:bb:cc:00:00:02" || entries[0].Local || entries[0].Ageing != "0.00" {
		t.Fatalf("entrada 0 mal parseada: %+v", entries[0])
	}
	if entries[1].Port != "lan1" || entries[1].MAC != "aa:bb:cc:00:00:03" || !entries[1].Local {
		t.Fatalf("entrada local mal parseada: %+v", entries[1])
	}
	if entries[2].Port != "lan2" || entries[2].MAC != "aa:bb:cc:00:00:01" || entries[2].Ageing != "4.21" {
		t.Fatalf("entrada 2 mal parseada: %+v", entries[2])
	}
}

func TestParseShowmacsFullEmpty(t *testing.T) {
	const header = "port no	mac addr		is local?	ageing timer\n"
	if entries := parseShowmacsFull(header, map[int]string{1: "lan1"}); len(entries) != 0 {
		t.Fatalf("esperaba 0 entradas, got %+v", entries)
	}
	if entries := parseShowmacsFull("", nil); len(entries) != 0 {
		t.Fatalf("esperaba 0 entradas con salida vacia, got %+v", entries)
	}
}
