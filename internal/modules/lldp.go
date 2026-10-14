package modules

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/gnacho/netgrip/internal/executor"
)

// Vecinos LLDP/CDP: tabla de equipos descubiertos por lldpd, leida de
// `lldpcli -f json0 show neighbors`, mas el toggle del daemon. Sigue el
// patron de mdns.go: probe honesto cuando la herramienta no esta (el switch
// no pierde nada, solo no ve vecinos), instalacion bajo demanda, y
// snapshot + healthcheck + rollback en la escritura.
//
// El formato json0 de lldpcli envuelve cada TLV en arrays de objetos
// {"value": ...}; el parser es una funcion pura con fixtures (CONTRIBUTING)
// y tolera tambien valores planos por si la version cambia el envoltorio.

type LldpNeighbor struct {
	Interface   string `json:"interface"`
	Protocol    string `json:"protocol"`
	ChassisID   string `json:"chassis_id"`
	ChassisName string `json:"chassis_name"`
	SysDescr    string `json:"sys_descr,omitempty"`
	MgmtIP      string `json:"mgmt_ip,omitempty"`
	PortID      string `json:"port_id"`
	PortDescr   string `json:"port_descr,omitempty"`
	TTL         string `json:"ttl,omitempty"`
}

type LldpProbe struct {
	Installed bool           `json:"installed"`
	Enabled   bool           `json:"enabled"`
	Running   bool           `json:"running"`
	TxEnabled bool           `json:"tx_enabled"`
	Neighbors []LldpNeighbor `json:"neighbors"`
}

func lldpdInstalled() bool {
	if _, err := os.Stat("/etc/init.d/lldpd"); err == nil {
		return true
	}
	_, err := exec.LookPath("lldpcli")
	return err == nil
}

// lldpcliNeighbors es la costura de testeo del probe de vecinos.
var lldpcliNeighbors = func() (string, error) {
	out, err := exec.Command("lldpcli", "-f", "json0", "show", "neighbors").Output()
	return string(out), err
}

// ProbeLLDP lee el estado de lldpd y, si corre, la tabla de vecinos.
func ProbeLLDP() *LldpProbe {
	p := &LldpProbe{Installed: lldpdInstalled(), Neighbors: []LldpNeighbor{}}
	if !p.Installed {
		return p
	}
	p.Enabled = executor.ServiceEnabled("lldpd")
	p.Running = executor.ServiceRunning("lldpd")
	p.TxEnabled = uciGet("lldpd.config.enable_tx") == "1"
	if !p.Running {
		return p
	}
	out, err := lldpcliNeighbors()
	if err != nil {
		return p
	}
	p.Neighbors = parseLldpcliNeighbors(out)
	return p
}

// SetLLDP activa o desactiva lldpd (instalando el paquete si hace falta al
// activar) con snapshot, healthcheck y rollback, como el resto de toggles.
func SetLLDP(enabled bool) (*LldpProbe, bool, error) {
	InvalidateKey("lldp")
	probe := ProbeLLDP()
	if !probe.Installed && !enabled {
		return probe, false, nil
	}

	snap := ""
	if _, err := os.Stat("/etc/config/lldpd"); err == nil {
		if s, err := executor.Snapshot("lldpd"); err == nil {
			snap = s
		}
	}

	rollback := func() {
		if snap != "" {
			_ = executor.Restore("lldpd", snap)
		}
		_ = executor.Run(executor.Op{Kind: "initd", Args: []string{"lldpd", "restart"}})
	}

	var ops []executor.Op
	if !probe.Installed {
		ops = append(ops, executor.Op{Kind: "pkg_add", Args: []string{"lldpd"}})
	}
	tx := "0"
	action := "stop"
	enable := "disable"
	if enabled {
		tx = "1"
		action = "start"
		enable = "enable"
	}
	ops = append(ops,
		executor.Op{Kind: "uci_set", Args: []string{"lldpd.config.enable_tx", tx}},
		executor.Op{Kind: "uci_commit", Args: []string{"lldpd"}},
		executor.Op{Kind: "initd", Args: []string{"lldpd", enable}},
		executor.Op{Kind: "initd", Args: []string{"lldpd", action}},
	)

	if err := executor.Apply(ops, nil); err != nil {
		rollback()
		return ProbeLLDP(), true, err
	}

	ok := func() bool {
		for range 5 {
			p := ProbeLLDP()
			if enabled {
				if p.Enabled && p.Running {
					return true
				}
			} else if !p.Enabled && !p.Running {
				return true
			}
			time.Sleep(time.Second)
		}
		return false
	}
	if !ok() {
		rollback()
		return ProbeLLDP(), true, fmt.Errorf("lldpd healthcheck failed after apply (enabled=%v), rolled back", enabled)
	}
	return ProbeLLDP(), false, nil
}

// --- parser puro del json0 de lldpcli ---

// tlv desenvuelve el formato json0 (arrays de {"value": x}) tolerando
// valores planos.
type tlv json.RawMessage

// UnmarshalJSON guarda los bytes crudos del TLV (type tlv no hereda el
// UnmarshalJSON de json.RawMessage; sin este metodo el decoder esperaba
// base64 y rompia el documento entero).
func (t *tlv) UnmarshalJSON(b []byte) error {
	*t = tlv(append((*t)[:0], b...))
	return nil
}

func (t tlv) first() string {
	var list []struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(json.RawMessage(t), &list); err == nil && len(list) > 0 {
		return list[0].Value
	}
	var s string
	if err := json.Unmarshal(json.RawMessage(t), &s); err == nil {
		return s
	}
	return ""
}

type lldpcliID struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type lldpcliIface struct {
	Name    string `json:"name"`
	Via     string `json:"via"`
	Chassis []struct {
		ID      lldpcliID `json:"id"`
		Name    tlv       `json:"name"`
		Descr   tlv       `json:"descr"`
		MgmtIP  tlv       `json:"mgmt-ip"`
	} `json:"chassis"`
	Port []struct {
		ID    lldpcliID `json:"id"`
		Descr tlv       `json:"descr"`
		TTL   tlv       `json:"ttl"`
	} `json:"port"`
}

// parseLldpcliNeighbors aplana la tabla json0 en una lista por (boca, vecino).
func parseLldpcliNeighbors(out string) []LldpNeighbor {
	var doc struct {
		Lldp struct {
			Interface []lldpcliIface `json:"interface"`
		} `json:"lldp"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		return nil
	}
	var neighbors []LldpNeighbor
	for _, iface := range doc.Lldp.Interface {
		proto := iface.Via
		if proto == "" {
			proto = "LLDP"
		}
		for _, ch := range iface.Chassis {
			n := LldpNeighbor{
				Interface: iface.Name,
				Protocol:  proto,
				ChassisID: ch.ID.Value,
			}
			n.ChassisName = ch.Name.first()
			n.SysDescr = ch.Descr.first()
			n.MgmtIP = ch.MgmtIP.first()
			for _, port := range iface.Port {
				n.PortID = port.ID.Value
				n.PortDescr = port.Descr.first()
				n.TTL = port.TTL.first()
			}
			neighbors = append(neighbors, n)
		}
	}
	return neighbors
}
