package modules

import (
	"testing"
)

// Salida real de `lldpcli -f json0 show neighbors` en un lldpd 1.0.x: cada
// TLV envuelto en arrays de {"value": ...}, dos bocas, una con vecino CDP
// (sin mgmt-ip) y chassis sin nombre (cae al chassis id).
const lldpcliJson0 = `{
  "lldp": {
    "interface": [
      {
        "name": "lan1",
        "via": "LLDP",
        "rid": "1",
        "age": "0 day, 00:04:12",
        "chassis": [
          {
            "id": {"type": "mac", "value": "aa:bb:cc:dd:ee:01"},
            "name": [{"value": "core-sw"}],
            "descr": [{"value": "MikroTik CRS326"}],
            "mgmt-ip": [{"value": "192.168.1.2"}]
          }
        ],
        "port": [
          {
            "id": {"type": "ifname", "value": "ether1"},
            "descr": [{"value": "uplink"}],
            "ttl": [{"value": "120"}]
          }
        ]
      },
      {
        "name": "lan2",
        "via": "CDP",
        "rid": "2",
        "age": "1 day, 02:00:00",
        "chassis": [
          {
            "id": {"type": "mac", "value": "aa:bb:cc:dd:ee:02"}
          }
        ],
        "port": [
          {
            "id": {"type": "mac", "value": "aa:bb:cc:dd:ee:99"},
            "ttl": [{"value": "180"}]
          }
        ]
      }
    ]
  }
}`

func TestParseLldpcliNeighbors(t *testing.T) {
	ns := parseLldpcliNeighbors(lldpcliJson0)
	if len(ns) != 2 {
		t.Fatalf("vecinos = %d, esperaba 2", len(ns))
	}
	a := ns[0]
	if a.Interface != "lan1" || a.Protocol != "LLDP" || a.ChassisName != "core-sw" ||
		a.ChassisID != "aa:bb:cc:dd:ee:01" || a.MgmtIP != "192.168.1.2" ||
		a.PortID != "ether1" || a.PortDescr != "uplink" || a.TTL != "120" {
		t.Fatalf("vecino lan1 mal parseado: %+v", a)
	}
	b := ns[1]
	if b.Interface != "lan2" || b.Protocol != "CDP" || b.ChassisID != "aa:bb:cc:dd:ee:02" ||
		b.ChassisName != "" || b.MgmtIP != "" || b.PortID != "aa:bb:cc:dd:ee:99" || b.TTL != "180" {
		t.Fatalf("vecino lan2 mal parseado: %+v", b)
	}
}

func TestParseLldpcliNeighborsEmpty(t *testing.T) {
	if ns := parseLldpcliNeighbors(`{"lldp":{"interface":[]}}`); len(ns) != 0 {
		t.Fatalf("esperaba 0 vecinos, got %d", len(ns))
	}
	if ns := parseLldpcliNeighbors(`no json`); ns != nil {
		t.Fatalf("esperaba nil con entrada rota, got %+v", ns)
	}
	// lldpd apagado: documento sin interfaces.
	if ns := parseLldpcliNeighbors(`{"lldp":{}}`); len(ns) != 0 {
		t.Fatalf("esperaba 0 vecinos con documento vacio, got %d", len(ns))
	}
}

// Tolerancia a valores planos por si lldpd cambia el envoltorio json0.
func TestParseLldpcliNeighborsFlatValues(t *testing.T) {
	const flat = `{"lldp":{"interface":[{"name":"lan3","via":"LLDP",
		"chassis":[{"id":{"type":"mac","value":"aa:bb:cc:dd:ee:03"},"name":[{"value":"ap-atico"}]}],
		"port":[{"id":{"type":"ifname","value":"wlan0"},"ttl":[{"value":"120"}]}]}]}}`
	ns := parseLldpcliNeighbors(flat)
	if len(ns) != 1 || ns[0].ChassisName != "ap-atico" || ns[0].PortID != "wlan0" {
		t.Fatalf("mal parseado: %+v", ns)
	}
}
