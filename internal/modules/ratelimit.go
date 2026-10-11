package modules

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Banda ancha por boca (#443): limite de velocidad por boca en % del
// ancho de enlace. La entrada se frena con un police en el gancho ingress
// (mismo estilo que el control de tormentas); la salida con un qdisc raiz
// tbf (los puertos DSA traen noqueue y lo aceptan, verificado en el
// LGS352C). Persistencia en /etc/netgrip/rate.conf; el hook
// /usr/libexec/netgrip-restore-rules lo re-aplica en el arranque.

const rateConfPath = "/etc/netgrip/rate.conf"

// ratePortConfig guarda los porcentajes configurados de una boca
// (0 = sin limite en ese sentido).
type ratePortConfig struct {
	ingress int
	egress  int
}

type RatePort struct {
	Port           string `json:"port"`
	LinkSpeedMbps  int    `json:"link_speed_mbps"`
	IngressPercent int    `json:"ingress_percent"`
	EgressPercent  int    `json:"egress_percent"`
	Active         bool   `json:"active"`
}

type RateProbe struct {
	Applicable       bool       `json:"applicable"`
	Ports            []RatePort `json:"ports"`
	TcInstalled      bool       `json:"tc_installed"`
	IngressSupported bool       `json:"ingress_supported"`
	EgressSupported  bool       `json:"egress_supported"`
}

type RateSetRequest struct {
	// Port aplica a una boca; Ports a una lista; All a todas las del
	// puente (mismo contrato que storm, #487).
	Port    string   `json:"port"`
	Ports   []string `json:"ports,omitempty"`
	All     bool     `json:"all,omitempty"`
	Ingress int      `json:"ingress_percent"`
	Egress  int      `json:"egress_percent"`
}

var probeTcRate = probeRateSupport

var (
	rateSupportMu      sync.Mutex
	rateSupportDone    bool
	rateSupportIngress bool
	rateSupportEgress  bool
)

func rateSupport() (ingress, egress bool) {
	rateSupportMu.Lock()
	defer rateSupportMu.Unlock()
	if !rateSupportDone {
		rateSupportIngress, rateSupportEgress = probeTcRate()
		rateSupportDone = true
	}
	return rateSupportIngress, rateSupportEgress
}

// resetRateSupportForTest invalida la cache de capacidad (tests).
func resetRateSupportForTest() {
	rateSupportMu.Lock()
	rateSupportDone = false
	rateSupportMu.Unlock()
}

// probeRateSupport comprueba de verdad que el kernel acepta un police en
// ingress y un tbf como qdisc raiz (add + del transitorios en la primera
// boca disponible, sin efecto en el trafico). El tbf solo se prueba en una
// boca cuya cola raiz sea noqueue: si hubiera otra cola puesta (ajenio),
// no la tocamos ni para sondear.
func probeRateSupport() (ingress, egress bool) {
	if !tcInstalled() {
		return false, false
	}
	ports := sortedBridgePorts()
	if len(ports) == 0 {
		return false, false
	}

	// Ingress: filtro police temporal en la ULTIMA boca del orden
	// alfabetico (lan9 en los chasis lanN): no es la tipica boca de
	// gestion (lan1) y el add/del dura milisegundos.
	src := ports[len(ports)-1]
	hadClsact := tcHasClsact(src)
	if !hadClsact {
		if out, err := addClsactRaw(src); err != nil {
			if !strings.Contains(out, "File exists") && !strings.Contains(out, "Exclusivity") {
				return false, false
			}
		}
	}
	_, ingress = policeFilterAdd(src, "ingress", 99, "1mbit")
	tcDelPrio(src, "ingress", 99)
	if !hadClsact && !portTcInUse(src) {
		exec.Command("tc", "qdisc", "del", "dev", src, "clsact").Run()
	}

	// Egress: tbf raiz temporal en la primera boca con cola raiz noqueue.
	for _, port := range ports {
		if !strings.Contains(tcQdiscShow(port), "noqueue") {
			continue
		}
		if _, err := exec.Command("tc", "qdisc", "add", "dev", port, "root",
			"handle", "99:", "tbf", "rate", "1mbit", "burst", "32k", "latency", "100ms").CombinedOutput(); err == nil {
			egress = true
			exec.Command("tc", "qdisc", "del", "dev", port, "root").Run()
		}
		break
	}
	return ingress, egress
}

// policeFilterArgs construye los argumentos del filtro matchall + police
// (funcion pura, testeable). Con matchall la accion lleva la palabra clave
// "action" (verificado en el LGS352C: sin ella tc responde
// 'What is "police"?'); el estilo "police ..." a pelo solo vale tras un
// selector como "basic match".
func policeFilterArgs(port, direction string, prio int, rate string) []string {
	return []string{"filter", "add", "dev", port, direction,
		"protocol", "all", "prio", strconv.Itoa(prio), "matchall",
		"action", "police", "rate", rate, "burst", "32k", "drop"}
}

// policeFilterAdd crea el filtro; devuelve la salida y el exito.
func policeFilterAdd(port, direction string, prio int, rate string) (string, bool) {
	out, err := exec.Command("tc", policeFilterArgs(port, direction, prio, rate)...).CombinedOutput()
	return string(out), err == nil
}

func ProbeRateLimit() RateProbe {
	portMap := bridgePorts()
	if len(portMap) == 0 {
		return RateProbe{Applicable: false}
	}

	configs := loadRateConfigs()
	ingressOK, egressOK := rateSupport()

	var ports []RatePort
	for _, port := range sortedBridgePorts() {
		cfg := configs[port]
		rp := RatePort{
			Port:           port,
			LinkSpeedMbps:  linkSpeedMbps(port),
			IngressPercent: cfg.ingress,
			EgressPercent:  cfg.egress,
			Active:         ratePortActive(port, cfg),
		}
		ports = append(ports, rp)
	}

	return RateProbe{
		Applicable:       true,
		Ports:            ports,
		TcInstalled:      tcInstalled(),
		IngressSupported: ingressOK,
		EgressSupported:  egressOK,
	}
}

// ratePortActive reporta si la boca tiene aplicado lo que dice su config:
// police prio 20 en ingress y/o tbf como cola raiz.
func ratePortActive(port string, cfg ratePortConfig) bool {
	active := false
	if cfg.ingress > 0 {
		show := tcFilterShow(port, "ingress")
		active = strings.Contains(show, "police") && strings.Contains(show, "pref "+strconv.Itoa(prioRateIngress))
	}
	if cfg.egress > 0 {
		active = active || strings.Contains(tcQdiscShow(port), "qdisc tbf")
	}
	return active
}

func SetRateLimit(req RateSetRequest) error {
	if req.Ingress < 0 || req.Ingress > 100 || req.Egress < 0 || req.Egress > 100 {
		return fmt.Errorf("invalid percent")
	}

	targets, err := rateTargets(req)
	if err != nil {
		return err
	}

	// Aplicar con un limite exige tc; el 0 (revertir) solo borra de
	// rate.conf y hace un best-effort de las reglas, igual que storm.
	if req.Ingress > 0 || req.Egress > 0 {
		if err := ensureTc(); err != nil {
			return err
		}
	}

	for _, port := range targets {
		if err := setRatePort(port, req.Ingress, req.Egress); err != nil {
			return err
		}
	}
	return nil
}

// rateTargets resuelve la lista de bocas destino (misma logica que storm).
func rateTargets(req RateSetRequest) ([]string, error) {
	portMap := bridgePorts()
	if req.All {
		ports := make([]string, 0, len(portMap))
		for p := range portMap {
			ports = append(ports, p)
		}
		sort.Strings(ports)
		return ports, nil
	}
	if len(req.Ports) > 0 {
		for _, p := range req.Ports {
			if _, ok := portMap[p]; !ok {
				return nil, fmt.Errorf("port %s not in bridge", p)
			}
		}
		return req.Ports, nil
	}
	if req.Port == "" {
		return nil, fmt.Errorf("invalid port")
	}
	if _, ok := portMap[req.Port]; !ok {
		return nil, fmt.Errorf("port not in bridge")
	}
	return []string{req.Port}, nil
}

func setRatePort(port string, ingressPct, egressPct int) error {
	// Entrada: police en el gancho ingress (comparte clsact con storm y
	// el espejo; cada funcion borra solo su prioridad).
	if ingressPct == 0 {
		tcDelPrio(port, "ingress", prioRateIngress)
	} else {
		if err := ensureClsact(port); err != nil {
			return err
		}
		tcDelPrio(port, "ingress", prioRateIngress)
		rate := ratePercentToKbps(linkSpeedMbps(port), ingressPct)
		if out, ok := policeFilterAdd(port, "ingress", prioRateIngress, fmt.Sprintf("%dkbit", rate)); !ok {
			return fmt.Errorf("add ingress rate limit on %s: %s", port, strings.TrimSpace(out))
		}
	}

	// Salida: tbf como cola raiz. Solo tocamos la raiz si la cola actual
	// es noqueue o un tbf nuestro: una cola ajena no se pisa.
	if egressPct == 0 {
		if strings.Contains(tcQdiscShow(port), "qdisc tbf") {
			exec.Command("tc", "qdisc", "del", "dev", port, "root").Run()
		}
	} else {
		if !egressRootReplaceable(port) {
			return fmt.Errorf("root qdisc on %s is not replaceable (not noqueue/tbf)", port)
		}
		rate := ratePercentToKbps(linkSpeedMbps(port), egressPct)
		exec.Command("tc", "qdisc", "del", "dev", port, "root").Run()
		if out, err := exec.Command("tc", "qdisc", "add", "dev", port, "root",
			"handle", "1:", "tbf", "rate", fmt.Sprintf("%dkbit", rate),
			"burst", "32k", "latency", "100ms").CombinedOutput(); err != nil {
			return fmt.Errorf("add egress rate limit on %s: %v (%s)", port, err, strings.TrimSpace(string(out)))
		}
	}

	if ingressPct == 0 && egressPct == 0 {
		removeRateConfig(port)
		maybeDropClsact(port)
	} else {
		saveRateConfig(port, ingressPct, egressPct)
	}
	return nil
}

// egressRootReplaceable reporta si la cola raiz de la boca se puede
// reemplazar con seguridad: noqueue (estado de fabrica DSA) o un tbf
// puesto previamente por este modulo.
func egressRootReplaceable(port string) bool {
	show := tcQdiscShow(port)
	return strings.Contains(show, "noqueue") || strings.Contains(show, "qdisc tbf")
}

// ratePercentToKbps convierte un porcentaje del ancho de enlace a kbit/s
// (funcion pura, testeable). Velocidades <= 0 se tratan como 1000 Mb/s
// (boca sin enlace, mismo criterio que storm).
func ratePercentToKbps(speedMbps, percent int) int {
	if speedMbps <= 0 {
		speedMbps = 1000
	}
	return speedMbps * 1000 * percent / 100
}

// parseTbfRateKbps extrae el rate de la salida de `tc qdisc show` en
// kbit/s (0 si no hay tbf). `tc` imprime "rate 100Mbit" aunque se
// configure 100000kbit (funcion pura, testeable).
func parseTbfRateKbps(show string) int {
	for _, line := range strings.Split(show, "\n") {
		if !strings.Contains(line, "qdisc tbf") {
			continue
		}
		fields := strings.Fields(line)
		for i, f := range fields {
			if f == "rate" && i+1 < len(fields) {
				return parseTcRateValue(fields[i+1])
			}
		}
	}
	return 0
}

// parseTcRateValue interpreta "100Mbit", "50000Kbit", "50000kbit" como
// numero entero de kbit/s (funcion pura, testeable).
func parseTcRateValue(val string) int {
	mult := 1
	if strings.HasSuffix(val, "Mbit") {
		mult = 1000
		val = strings.TrimSuffix(val, "Mbit")
	}
	val = strings.TrimSuffix(val, "Kbit")
	val = strings.TrimSuffix(val, "kbit")
	v, err := strconv.Atoi(val)
	if err != nil {
		return 0
	}
	return v * mult
}

// loadRateConfigs lee /etc/netgrip/rate.conf, una boca por linea:
//
//	lan20=10,25
//
// (entrada,salida en %; una entrada 0,0 no se guarda).
func loadRateConfigs() map[string]ratePortConfig {
	configs := map[string]ratePortConfig{}
	data, err := os.ReadFile(rateConfPath)
	if err != nil {
		return configs
	}
	return parseRateConfigs(string(data))
}

// parseRateConfigs interpreta el contenido del fichero (funcion pura,
// testeable).
func parseRateConfigs(text string) map[string]ratePortConfig {
	configs := map[string]ratePortConfig{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		ing, eg, ok := strings.Cut(strings.TrimSpace(val), ",")
		if !ok {
			continue
		}
		ingN, err1 := strconv.Atoi(strings.TrimSpace(ing))
		egN, err2 := strconv.Atoi(strings.TrimSpace(eg))
		if err1 != nil || err2 != nil || ingN < 0 || ingN > 100 || egN < 0 || egN > 100 {
			continue
		}
		port := strings.TrimSpace(key)
		if port != "" {
			configs[port] = ratePortConfig{ingress: ingN, egress: egN}
		}
	}
	return configs
}

func saveRateConfig(port string, ingress, egress int) {
	configs := loadRateConfigs()
	if ingress == 0 && egress == 0 {
		delete(configs, port)
	} else {
		configs[port] = ratePortConfig{ingress: ingress, egress: egress}
	}
	writeRateConfigs(configs)
}

func removeRateConfig(port string) {
	configs := loadRateConfigs()
	delete(configs, port)
	writeRateConfigs(configs)
}

func writeRateConfigs(configs map[string]ratePortConfig) {
	os.MkdirAll(filepath.Dir(rateConfPath), 0755)
	ports := make([]string, 0, len(configs))
	for p := range configs {
		ports = append(ports, p)
	}
	sort.Strings(ports)
	var lines []string
	for _, p := range ports {
		c := configs[p]
		lines = append(lines, fmt.Sprintf("%s=%d,%d", p, c.ingress, c.egress))
	}
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	os.WriteFile(rateConfPath, []byte(content), 0600)
}
