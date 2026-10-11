package modules

import (
	"testing"
)

// --- parsePortValueConf / formatos de isolation.conf ---

func TestParsePortValueConf(t *testing.T) {
	text := `
# comentario
lan20
lan21=1
lan22=0

  lan23  `
	got := parsePortValueConf(text)
	want := map[string]bool{"lan20": true, "lan21": true, "lan22": false, "lan23": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %v, want %v", k, got[k], v)
		}
	}
}

func TestParsePortValueConfEmpty(t *testing.T) {
	if got := parsePortValueConf(""); len(got) != 0 {
		t.Fatalf("expected empty map, got %v", got)
	}
}

func TestPortListConfRoundTrip(t *testing.T) {
	// Solo las bocas verdaderas se escriben; parsear de nuevo da el mismo set.
	dir := t.TempDir()
	path := dir + "/isolation.conf"
	savePortListConf(path, map[string]bool{"lan21": true, "lan20": true, "lan22": false})
	got := loadPortListConf(path)
	// Las entradas falsas se borran del fichero: no vuelven en la carga.
	want := map[string]bool{"lan20": true, "lan21": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %v, want %v", k, got[k], v)
		}
	}
}

// --- learning.conf ---

func TestParseLearningConf(t *testing.T) {
	text := `
# aprendizaje desactivado
lan20=0
lan21=1
malformado
lan22=0`
	got := parseLearningConf(text)
	want := map[string]bool{"lan20": true, "lan22": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k := range want {
		if !got[k] {
			t.Errorf("%s deberia estar marcada como learning-off", k)
		}
	}
}

func TestLearningConfRoundTrip(t *testing.T) {
	dir := t.TempDir()
	old := learningConfPath
	learningConfPath = dir + "/learning.conf"
	t.Cleanup(func() { learningConfPath = old })

	saveLearningConf(map[string]bool{"lan21": true, "lan20": true, "lan22": false})
	got := loadLearningConf()
	want := map[string]bool{"lan20": true, "lan21": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k := range want {
		if !got[k] {
			t.Errorf("%s deberia estar marcada como learning-off", k)
		}
	}
}

// --- fdb.conf ---

func TestParseFdbConf(t *testing.T) {
	text := `
# MACs fijas
lan20=AA:BB:CC:DD:EE:FF 11:22:33:44:55:66
lan21=aa:bb:cc:dd:ee:ff
malformado
lan22=zz:zz:zz:zz:zz:zz 66:55:44:33:22:11
=01:02:03:04:05:06`
	got := parseFdbConf(text)
	if len(got) != 3 {
		t.Fatalf("expected 3 ports, got %v", got)
	}
	if len(got["lan20"]) != 2 || got["lan20"][0] != "aa:bb:cc:dd:ee:ff" || got["lan20"][1] != "11:22:33:44:55:66" {
		t.Errorf("lan20: %v", got["lan20"])
	}
	// MAC duplicada en minusculas: se conserva tal cual (dedupe en el set).
	if len(got["lan21"]) != 1 {
		t.Errorf("lan21: %v", got["lan21"])
	}
	if len(got["lan22"]) != 1 || got["lan22"][0] != "66:55:44:33:22:11" {
		t.Errorf("lan22 (invalida ignorada): %v", got["lan22"])
	}
}

func TestFormatFdbConfSorted(t *testing.T) {
	conf := map[string][]string{
		"lan21": {"bb:bb:bb:bb:bb:bb", "aa:aa:aa:aa:aa:aa"},
		"lan20": {"cc:cc:cc:cc:cc:cc"},
	}
	got := formatFdbConf(conf)
	want := "lan20=cc:cc:cc:cc:cc:cc\nlan21=aa:aa:aa:aa:aa:aa bb:bb:bb:bb:bb:bb\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if formatFdbConf(map[string][]string{}) != "" {
		t.Error("conf vacio debe serializar a cadena vacia")
	}
}

func TestFdbConfRoundTrip(t *testing.T) {
	dir := t.TempDir()
	old := fdbConfPath
	fdbConfPath = dir + "/fdb.conf"
	t.Cleanup(func() { fdbConfPath = old })

	in := map[string][]string{
		"lan20": {"aa:aa:aa:aa:aa:aa", "bb:bb:bb:bb:bb:bb"},
		"lan3":  {"cc:cc:cc:cc:cc:cc"},
	}
	saveFdbConf(in)
	got := loadFdbConf()
	if len(got) != 2 || len(got["lan20"]) != 2 || len(got["lan3"]) != 1 {
		t.Fatalf("round trip: %v", got)
	}
}

// --- parseStaticMAC ---

func TestParseStaticMAC(t *testing.T) {
	ok := []string{"aa:bb:cc:dd:ee:ff", "AA:BB:CC:DD:EE:FF", " aa:bb:cc:dd:ee:ff "}
	for _, s := range ok {
		mac, err := parseStaticMAC(s)
		if err != nil {
			t.Errorf("%q deberia ser valida: %v", s, err)
			continue
		}
		if mac.String() != "aa:bb:cc:dd:ee:ff" {
			t.Errorf("%q: normalizada a %s", s, mac)
		}
	}
	// net.ParseMAC tambien acepta el formato Cisco "aabb.ccdd.eeff": se
	// normaliza igual que el resto.
	mac, err := parseStaticMAC("aabb.ccdd.eeff")
	if err != nil || mac.String() != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("formato Cisco: %v %v", mac, err)
	}
	bad := []string{"", "aa:bb:cc:dd:ee", "aa:bb:cc:dd:ee:ff:11", "gg:bb:cc:dd:ee:ff"}
	for _, s := range bad {
		if _, err := parseStaticMAC(s); err == nil {
			t.Errorf("%q deberia ser invalida", s)
		}
	}
}

// --- capacidad: limite de MACs siempre no soportado (decision #449) ---

func TestMacLimitAlwaysUnsupported(t *testing.T) {
	var probe PortSecurityProbe
	if probe.MacLimitSupported {
		t.Error("mac_limit_supported debe ser false siempre (el bridge del kernel no lo soporta)")
	}
}
