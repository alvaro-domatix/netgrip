package modules

import (
	"testing"
)

func TestParseTcRateValue(t *testing.T) {
	cases := map[string]int{
		"100Mbit":   100000,
		"50000Kbit": 50000,
		"50000kbit": 50000,
		"2500Mbit":  2500000,
		"bogus":     0,
		"":          0,
	}
	for in, want := range cases {
		if got := parseTcRateValue(in); got != want {
			t.Fatalf("parseTcRateValue(%q) = %d, want %d", in, got, want)
		}
	}
}

// Salida real de `tc qdisc show dev lan20`: el tbf con rate y un fq_codel.
const tcQdiscShowFixture = `qdisc noqueue 0: root refcnt 2
qdisc tbf 8003: root rate 50Mbit burst 5000b latency 50ms
qdisc fq_codel 0: parent 1:3 limit 10240p flows 1024 quantum 1514 target 5ms interval 100ms
`

func TestParseTbfRateKbps(t *testing.T) {
	if got := parseTbfRateKbps(tcQdiscShowFixture); got != 50000 {
		t.Fatalf("parseTbfRateKbps = %d, want 50000", got)
	}
	if got := parseTbfRateKbps("qdisc noqueue 0: root\n"); got != 0 {
		t.Fatalf("parseTbfRateKbps sin tbf = %d, want 0", got)
	}
}

func TestParseRateConfigs(t *testing.T) {
	text := "# comentario\nlan20=10,25\n\nlan21=0,0\nlan22=101,5\nlan23=5\n=1,1\nlan24= 5 , 6 \n"
	cfg := parseRateConfigs(text)
	if len(cfg) != 3 {
		t.Fatalf("configs = %d, want 3 (lan20, lan21, lan24): %+v", len(cfg), cfg)
	}
	if c := cfg["lan20"]; c.ingress != 10 || c.egress != 25 {
		t.Fatalf("lan20 = %+v", c)
	}
	if c := cfg["lan21"]; c.ingress != 0 || c.egress != 0 {
		t.Fatalf("lan21 = %+v", c)
	}
	if c := cfg["lan24"]; c.ingress != 5 || c.egress != 6 {
		t.Fatalf("lan24 (con espacios) = %+v", c)
	}
	if _, ok := cfg["lan22"]; ok {
		t.Fatal("101% fuera de rango no descartado")
	}
	if _, ok := cfg["lan23"]; ok {
		t.Fatal("entrada sin coma no descartada")
	}
	if _, ok := cfg[""]; ok {
		t.Fatal("clave vacía no descartada")
	}
}
