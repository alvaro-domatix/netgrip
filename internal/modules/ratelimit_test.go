package modules

import (
	"strings"
	"testing"
)

func TestParseRateConfigs(t *testing.T) {
	// Formato real escrito por writeRateConfigs.
	text := "lan20=10,25\nlan21=100,0\n"
	configs := parseRateConfigs(text)
	if c := configs["lan20"]; c.ingress != 10 || c.egress != 25 {
		t.Fatalf("lan20: se obtuvo %+v, se esperaba {10 25}", c)
	}
	if c := configs["lan21"]; c.ingress != 100 || c.egress != 0 {
		t.Fatalf("lan21: se obtuvo %+v, se esperaba {100 0}", c)
	}

	// Comentarios, basura y porcentajes fuera de rango se toleran/saltan.
	text = "# limites\n\nlan20=10,25\ngarbage\nlan21=10\ntrans=150,10\nlan22=-5,3\n"
	configs = parseRateConfigs(text)
	if len(configs) != 1 || configs["lan20"].ingress != 10 {
		t.Fatalf("tolerancia: se obtuvo %v", configs)
	}

	if got := parseRateConfigs(""); len(got) != 0 {
		t.Fatalf("vacio: se obtuvo %v", got)
	}
}

func TestRatePercentToKbps(t *testing.T) {
	cases := []struct {
		speed, percent, want int
	}{
		{1000, 10, 100000}, // 10% de 1 Gb/s
		{1000, 100, 1000000},
		{1000, 1, 10000},
		{100, 50, 50000}, // 100 Mb/s (boca con enlace rapido/feo)
		{0, 10, 100000},  // sin enlace: se asume 1000 Mb/s, como storm
		{-1, 10, 100000},
		{1000, 0, 0},
	}
	for _, tc := range cases {
		if got := ratePercentToKbps(tc.speed, tc.percent); got != tc.want {
			t.Fatalf("ratePercentToKbps(%d, %d): se obtuvo %d, se esperaba %d", tc.speed, tc.percent, got, tc.want)
		}
	}
}

func TestParseTbfRateKbps(t *testing.T) {
	// `tc qdisc show` imprime "rate 100Mbit" aunque se configure 100000kbit
	// (verificado en el LGS352C).
	show := "qdisc tbf 1: root refcnt 2 rate 100Mbit burst 32Kb lat 100ms \n"
	if got := parseTbfRateKbps(show); got != 100000 {
		t.Fatalf("100Mbit debe parsearse como 100000 kbit, se obtuvo %d", got)
	}
	if got := parseTbfRateKbps("qdisc noqueue 0: root refcnt 2 \n"); got != 0 {
		t.Fatalf("sin tbf debe dar 0, se obtuvo %d", got)
	}
	if got := parseTbfRateKbps("qdisc tbf 1: root rate 50000Kbit burst 32Kb lat 100ms \n"); got != 50000 {
		t.Fatalf("50000Kbit debe mantenerse, se obtuvo %d", got)
	}
}

func TestParseTcRateValue(t *testing.T) {
	cases := map[string]int{
		"100Mbit":   100000,
		"50Mbit":    50000,
		"50000Kbit": 50000,
		"50000kbit": 50000,
		"32k":       0, // sin sufijo de bits no es un rate de tc
		"abc":       0,
	}
	for in, want := range cases {
		if got := parseTcRateValue(in); got != want {
			t.Fatalf("parseTcRateValue(%q): se obtuvo %d, se esperaba %d", in, got, want)
		}
	}
}

func TestPoliceFilterArgs(t *testing.T) {
	// Con matchall la accion debe llevar "action" delante: sin ella tc
	// responde 'What is "police"?' (verificado en el LGS352C).
	args := policeFilterArgs("lan20", "ingress", prioRateIngress, "100000kbit")
	want := "filter add dev lan20 ingress protocol all prio 20 matchall action police rate 100000kbit burst 32k drop"
	if strings.Join(args, " ") != want {
		t.Fatalf("se obtuvo %q, se esperaba %q", strings.Join(args, " "), want)
	}
}

func TestRateSupportCachedAndSeam(t *testing.T) {
	oldProbe := probeTcRate
	t.Cleanup(func() { probeTcRate = oldProbe; resetRateSupportForTest() })

	calls := 0
	probeTcRate = func() (bool, bool) { calls++; return true, false }
	resetRateSupportForTest()

	i, e := rateSupport()
	if !i || e || calls != 1 {
		t.Fatalf("primera llamada: (%v,%v) calls=%d", i, e, calls)
	}
	rateSupport()
	if calls != 1 {
		t.Fatalf("la cache debe evitar re-probar, calls=%d", calls)
	}
}
