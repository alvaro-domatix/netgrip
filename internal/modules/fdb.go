package modules

import (
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// Tabla MAC dinamica (FDB) del bridge LAN, leida de `brctl showmacs` con
// los puertos resueltos via /sys/class/net/<bridge>/brif (mismo criterio
// que bridgeFdb de netinfo.go, pero conservando flag local y ageing para la
// vista de switch). Herramienta ausente o bridge inexistente = no
// aplicable, nunca error (CONTRIBUTING).

type FdbEntry struct {
	Port   string `json:"port"`
	MAC    string `json:"mac"`
	Local  bool   `json:"local"`
	Ageing string `json:"ageing"`
}

type FdbProbe struct {
	Applicable bool       `json:"applicable"`
	Bridge     string     `json:"bridge"`
	Entries    []FdbEntry `json:"entries"`
}

// brctlShowmacs es la costura de testeo del probe.
var brctlShowmacs = func(bridge string) (string, error) {
	out, err := exec.Command("brctl", "showmacs", bridge).Output()
	return string(out), err
}

// fdbBridges: el bridge configurado y los nombres convencionales de
// fallback, en orden (mismo criterio que bridgeFdb).
func fdbBridges() []string {
	var out []string
	for _, b := range []string{LANBridge(), "br-lan", "br0"} {
		if b == "" {
			continue
		}
		dup := false
		for _, x := range out {
			if x == b {
				dup = true
			}
		}
		if !dup {
			out = append(out, b)
		}
	}
	return out
}

// fdbPortNames mapea el numero de puerto de showmacs (byte bajo del
// port_id del kernel) al nombre de la boca, via /sys.
func fdbPortNames(bridge string) map[int]string {
	brifDir := "/sys/class/net/" + bridge + "/brif"
	entries, err := os.ReadDir(brifDir)
	if err != nil {
		return nil
	}
	portNames := map[int]string{}
	for _, e := range entries {
		data, err := os.ReadFile(brifDir + "/" + e.Name() + "/port_id")
		if err != nil {
			continue
		}
		id, err := strconv.ParseInt(strings.TrimSpace(string(data)), 0, 64)
		if err != nil {
			continue
		}
		portNames[int(id&0xff)] = e.Name()
	}
	return portNames
}

// ProbeFdb devuelve la tabla MAC del primer bridge que responde.
func ProbeFdb() FdbProbe {
	for _, bridge := range fdbBridges() {
		portNames := fdbPortNames(bridge)
		if portNames == nil {
			continue
		}
		out, err := brctlShowmacs(bridge)
		if err != nil {
			continue
		}
		return FdbProbe{Applicable: true, Bridge: bridge, Entries: parseShowmacsFull(out, portNames)}
	}
	return FdbProbe{Entries: []FdbEntry{}}
}

// parseShowmacsFull convierte la salida de `brctl showmacs` en entradas
// planas (boca, mac, local, ageing) ordenadas por boca y MAC. Bajo VLAN
// filtering la misma MAC aparece una vez por VLAN en la misma boca; como
// showmacs no imprime la VLAN, se deduplica por (boca, MAC) igual que el
// parser de clientes de netinfo.go.
func parseShowmacsFull(out string, portNames map[int]string) []FdbEntry {
	seen := map[string]bool{}
	var entries []FdbEntry
	for _, line := range strings.Split(out, "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		portNo, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		name, ok := portNames[portNo]
		if !ok {
			continue
		}
		key := name + "\x00" + fields[1]
		if seen[key] {
			continue
		}
		seen[key] = true
		entries = append(entries, FdbEntry{
			Port:   name,
			MAC:    fields[1],
			Local:  fields[2] == "yes",
			Ageing: fields[3],
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Port != entries[j].Port {
			return entries[i].Port < entries[j].Port
		}
		return entries[i].MAC < entries[j].MAC
	})
	return entries
}
