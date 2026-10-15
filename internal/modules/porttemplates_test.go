package modules

import (
	"testing"
)

// Salida real de `uci show netgrip` con dos plantillas y secciones ajenas.
const uciShowNetgripFixture = `netgrip.main=main
netgrip.main.port='8090'
netgrip.uplink=port_template
netgrip.uplink.description='Uplink al router'
netgrip.uplink.admin_up='1'
netgrip.uplink.speed_mbps='2500'
netgrip.uplink.vlans_json='[{"vlan_id":1,"tagged":false}]'
netgrip.camara=port_template
netgrip.camara.description='Cámara'
netgrip.camara.admin_up='0'
netgrip.poe=poe
netgrip.poe.lan2=poeport
`

func TestListPortTemplatesFrom(t *testing.T) {
	tpls := listPortTemplatesFrom(uciShowNetgripFixture)
	if len(tpls) != 2 {
		t.Fatalf("templates = %d, want 2: %+v", len(tpls), tpls)
	}
	byName := map[string]PortTemplate{}
	for _, tp := range tpls {
		byName[tp.Name] = tp
	}
	up, ok := byName["uplink"]
	if !ok {
		t.Fatalf("falta uplink: %+v", tpls)
	}
	if up.Description != "Uplink al router" || !up.AdminUp || up.SpeedMbps != 2500 || len(up.VLANs) != 1 {
		t.Fatalf("uplink mal parseado: %+v", up)
	}
	cam, ok := byName["camara"]
	if !ok || cam.AdminUp || cam.SpeedMbps != 0 || len(cam.VLANs) != 0 {
		t.Fatalf("camara mal parseado: %+v", cam)
	}
}

func TestListPortTemplatesFromEmpty(t *testing.T) {
	if got := listPortTemplatesFrom("netgrip.main=main\n"); len(got) != 0 {
		t.Fatalf("esperaba 0 plantillas, got %+v", got)
	}
	if got := listPortTemplatesFrom(""); len(got) != 0 {
		t.Fatalf("esperaba 0 plantillas con salida vacía, got %+v", got)
	}
}

func TestParseInt(t *testing.T) {
	if v, err := parseInt("2500"); err != nil || v != 2500 {
		t.Fatalf("parseInt(2500) = %d, %v", v, err)
	}
	if _, err := parseInt("abc"); err == nil {
		t.Fatal("parseInt(abc) no falló")
	}
}
