package modules

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// Seguridad de bocas del puente (#449): aislamiento (isolated), aprendizaje
// (learning) y FDB estatica (MAC fija -> boca) sobre el bridge del kernel.
// Todo son flags sysfs del bridge port o entradas netlink AF_BRIDGE, sin
// dependencias de tc ni de paquetes extra. Verificado en el LGS352C
// (rtl931x, kernel 6.12): los tres mecanismos funcionan en hardware DSA.
//
// Persistencia en /etc/netgrip/{isolation,learning,fdb}.conf; el hook
// /usr/libexec/netgrip-restore-rules las re-aplica en el arranque (la FDB
// estatica via el propio binario: `netgrip -restore-fdb`, porque no hay
// herramienta `bridge` en los targets y el re-aplicado es netlink puro).
//
// Limite de MACs por boca: el bridge del kernel NO lo soporta (ni sysfs,
// ni netlink, ni tc). nftables solo cuenta conexiones o paquetes, no MACs
// distintas por boca, asi que la capacidad se reporta como no soportada y
// la UI lo dice limpio (decision documentada en #449).

// portsecSysRoot es la costura de testeo de las rutas sysfs (los tests la
// redirigen a un tmpdir; en produccion es /sys/class/net).
var portsecSysRoot = "/sys/class/net"

// Las rutas de los conf son variables (no constantes) para poder
// redirigirlas a un tmpdir en los tests de persistencia, igual que hacen
// otras costuras del paquete.
var (
	isolationConfPath = "/etc/netgrip/isolation.conf"
	learningConfPath  = "/etc/netgrip/learning.conf"
	fdbConfPath       = "/etc/netgrip/fdb.conf"
)

// fdbProbeMAC es una MAC localmente administrada de test usada solo para
// el probe de capacidad (add + del transitorios, milisegundos, en la
// ultima boca del puente, igual que el probe mirred de #443).
const fdbProbeMAC = "02:4e:65:74:67:72" // "Netgr...", no asignable a fabricantes

// ---------------------------------------------------------------------------
// Aislamiento de bocas

type PortIsolationPort struct {
	Port string `json:"port"`
	// Isolated es el flag real del kernel (sysfs brport/isolated).
	Isolated bool `json:"isolated"`
	// Configured: la boca figura en isolation.conf (puede no estar
	// re-aplicada aun, p. ej. justo despues del arranque).
	Configured bool `json:"configured"`
}

type PortIsolationProbe struct {
	Applicable bool                `json:"applicable"`
	Ports      []PortIsolationPort `json:"ports"`
	// IsolatedSupported: el kernel expone brport/isolated (>= 5.10).
	IsolatedSupported bool `json:"isolated_supported"`
}

type PortIsolationSetRequest struct {
	Port     string `json:"port"`
	Isolated bool   `json:"isolated"`
}

func ProbePortIsolation() PortIsolationProbe {
	portMap := bridgePorts()
	if len(portMap) == 0 {
		return PortIsolationProbe{Applicable: false}
	}

	configured := loadPortListConf(isolationConfPath)
	// El flag isolated existe desde el kernel 5.10; basta con comprobar
	// que el sysfs lo expone en alguna boca del puente (es un atributo
	// del bridge port, identico en todas).
	supported := false
	for port := range portMap {
		if _, err := os.Stat(portsecSysRoot + "/" + port + "/brport/isolated"); err == nil {
			supported = true
			break
		}
	}

	var ports []PortIsolationPort
	for _, port := range sortedBridgePorts() {
		ports = append(ports, PortIsolationPort{
			Port:       port,
			Isolated:   readBrportBool(port, "isolated"),
			Configured: configured[port],
		})
	}

	return PortIsolationProbe{
		Applicable:        true,
		Ports:             ports,
		IsolatedSupported: supported,
	}
}

func SetPortIsolation(req PortIsolationSetRequest) error {
	if req.Port == "" {
		return fmt.Errorf("invalid port")
	}
	portMap := bridgePorts()
	if !portMap[req.Port] {
		return fmt.Errorf("port not in bridge")
	}
	if _, err := os.Stat(portsecSysRoot + "/" + req.Port + "/brport/isolated"); err != nil {
		return fmt.Errorf("port isolation is not supported by this kernel (no brport/isolated)")
	}

	if err := writeBrportBool(req.Port, "isolated", req.Isolated); err != nil {
		return err
	}

	configured := loadPortListConf(isolationConfPath)
	configured[req.Port] = req.Isolated
	savePortListConf(isolationConfPath, configured)
	return nil
}

// ---------------------------------------------------------------------------
// Seguridad por boca: aprendizaje + FDB estatica + limite MAC (no soportado)

type PortSecurityPort struct {
	Port string `json:"port"`
	// Learning es el flag real del kernel (sysfs brport/learning).
	Learning bool `json:"learning"`
	// LearningConfigured: learning.conf fija esta boca (off; el on es el
	// valor por defecto del kernel y no se persiste).
	LearningConfigured bool `json:"learning_configured"`
	// StaticMACs: MACs fijas de la boca (fdb.conf, formato
	// "<boca>=<mac> <mac>..." por linea).
	StaticMACs []string `json:"static_macs"`
}

type PortSecurityProbe struct {
	Applicable bool               `json:"applicable"`
	Ports      []PortSecurityPort `json:"ports"`
	// LearningSupported: el kernel expone brport/learning.
	LearningSupported bool `json:"learning_supported"`
	// FdbSupported: el kernel acepta entradas estaticas FDB por netlink
	// (probe real, add + del transitorios).
	FdbSupported bool `json:"fdb_supported"`
	// MacLimitSupported es SIEMPRE false: el bridge del kernel no tiene
	// limite de MACs por boca y nftables no cuenta MACs distintas (decision
	// documentada en #449). La UI lo muestra como no soportado, limpio.
	MacLimitSupported bool `json:"mac_limit_supported"`
}

type PortSecurityLearningRequest struct {
	Port     string `json:"port"`
	Learning bool   `json:"learning"`
}

type PortSecurityFdbRequest struct {
	Port string `json:"port"`
	MAC  string `json:"mac"`
	Add  bool   `json:"add"`
}

var probeFdbSupport = probeFdbSupportReal

var (
	fdbSupportMu   sync.Mutex
	fdbSupportDone bool
	fdbSupportOK   bool
)

func fdbSupport() bool {
	fdbSupportMu.Lock()
	defer fdbSupportMu.Unlock()
	if !fdbSupportDone {
		fdbSupportOK = probeFdbSupport()
		fdbSupportDone = true
	}
	return fdbSupportOK
}

// resetFdbSupportForTest invalida la cache de capacidad (tests).
func resetFdbSupportForTest() {
	fdbSupportMu.Lock()
	fdbSupportDone = false
	fdbSupportMu.Unlock()
}

// probeFdbSupportReal comprueba de verdad que el kernel acepta una entrada
// estatica FDB: add + del transitorios de la MAC de probe en la ultima boca
// del puente (mismo criterio que el probe mirred de #443: dura milisegundos
// y no llega a ver paquetes). No necesita tc ni ningun paquete.
func probeFdbSupportReal() bool {
	ports := sortedBridgePorts()
	if len(ports) == 0 {
		return false
	}
	port := ports[len(ports)-1]
	mac, err := net.ParseMAC(fdbProbeMAC)
	if err != nil {
		return false
	}
	if err := fdbAddMAC(port, mac); err != nil {
		return false
	}
	// El del es best-effort: si fallara quedaria una entrada estatica de
	// una MAC inexistente, inofensiva pero sucia; se reintenta una vez.
	if err := fdbDelMAC(port, mac); err != nil {
		_ = fdbDelMAC(port, mac)
	}
	return true
}

func ProbePortSecurity() PortSecurityProbe {
	portMap := bridgePorts()
	if len(portMap) == 0 {
		return PortSecurityProbe{Applicable: false}
	}

	learningOff := loadLearningConf()
	staticMacs := loadFdbConf()

	learningSupported := false
	for port := range portMap {
		if _, err := os.Stat(portsecSysRoot + "/" + port + "/brport/learning"); err == nil {
			learningSupported = true
			break
		}
	}

	var ports []PortSecurityPort
	for _, port := range sortedBridgePorts() {
		macs := staticMacs[port]
		if macs == nil {
			macs = []string{}
		}
		ports = append(ports, PortSecurityPort{
			Port:               port,
			Learning:           readBrportBool(port, "learning"),
			LearningConfigured: learningOff[port],
			StaticMACs:         macs,
		})
	}

	return PortSecurityProbe{
		Applicable:        true,
		Ports:             ports,
		LearningSupported: learningSupported,
		FdbSupported:      fdbSupport(),
		MacLimitSupported: false,
	}
}

func SetPortLearning(req PortSecurityLearningRequest) error {
	if req.Port == "" {
		return fmt.Errorf("invalid port")
	}
	portMap := bridgePorts()
	if !portMap[req.Port] {
		return fmt.Errorf("port not in bridge")
	}
	if _, err := os.Stat(portsecSysRoot + "/" + req.Port + "/brport/learning"); err != nil {
		return fmt.Errorf("port learning toggle is not supported by this kernel (no brport/learning)")
	}

	if err := writeBrportBool(req.Port, "learning", req.Learning); err != nil {
		return err
	}

	// Solo se persiste el off, explicito ("lan20=0"): el on es el valor por
	// defecto del kernel y el formato coincide con el flag que se re-aplica
	// en el arranque (netgrip-restore-rules hace echo del valor).
	off := loadLearningConf()
	off[req.Port] = !req.Learning
	saveLearningConf(off)
	return nil
}

func SetPortStaticMAC(req PortSecurityFdbRequest) error {
	if req.Port == "" {
		return fmt.Errorf("invalid port")
	}
	portMap := bridgePorts()
	if !portMap[req.Port] {
		return fmt.Errorf("port not in bridge")
	}
	mac, err := parseStaticMAC(req.MAC)
	if err != nil {
		return err
	}
	if !fdbSupport() {
		return fmt.Errorf("static FDB entries are not supported by this kernel")
	}

	configured := loadFdbConf()
	macs := configured[req.Port]

	if req.Add {
		for _, m := range macs {
			if strings.EqualFold(m, mac.String()) {
				return fmt.Errorf("mac %s is already pinned on %s", mac, req.Port)
			}
		}
		if err := fdbAddMAC(req.Port, mac); err != nil {
			return fmt.Errorf("pin %s on %s: %w", mac, req.Port, err)
		}
		configured[req.Port] = append(macs, mac.String())
	} else {
		found := false
		var remaining []string
		for _, m := range macs {
			if strings.EqualFold(m, mac.String()) {
				found = true
				continue
			}
			remaining = append(remaining, m)
		}
		if !found {
			return fmt.Errorf("mac %s is not pinned on %s", mac, req.Port)
		}
		// Quitar del kernel primero: si el del falla, el conf sigue
		// reflejando la realidad y el restore no deja la entrada a medias.
		if err := fdbDelMAC(req.Port, mac); err != nil {
			return fmt.Errorf("unpin %s on %s: %w", mac, req.Port, err)
		}
		if len(remaining) == 0 {
			delete(configured, req.Port)
		} else {
			configured[req.Port] = remaining
		}
	}

	saveFdbConf(configured)
	return nil
}

// RestoreStaticFdb re-aplica las entradas estaticas FDB de fdb.conf. La
// invoca `netgrip -restore-fdb` desde netgrip-restore-rules en el arranque:
// no hay herramienta `bridge` en los targets (busybox no la trae) y el
// re-aplicado es netlink puro desde el binario.
func RestoreStaticFdb() error {
	configured := loadFdbConf()
	if len(configured) == 0 {
		return nil
	}
	var failed []string
	for _, port := range sortedKeys(configured) {
		for _, macStr := range configured[port] {
			mac, err := net.ParseMAC(macStr)
			if err != nil {
				failed = append(failed, fmt.Sprintf("%s=%s", port, macStr))
				continue
			}
			// NeighSet (REPLACE) en vez de ADD: en el arranque la entrada
			// puede haber sobrevivido (FDB estatica no caduca), y un ADD
			// con EXCL fallaria con "file exists".
			if err := fdbSetMAC(port, mac); err != nil {
				failed = append(failed, fmt.Sprintf("%s=%s", port, macStr))
			}
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("could not restore static FDB entries: %s", strings.Join(failed, ", "))
	}
	return nil
}

// ---------------------------------------------------------------------------
// sysfs helpers

// readBrportBool lee un flag 0/1 del bridge port (ausente o ilegible = 0).
func readBrportBool(port, flag string) bool {
	data, err := os.ReadFile(fmt.Sprintf("%s/%s/brport/%s", portsecSysRoot, port, flag))
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == "1"
}

func writeBrportBool(port, flag string, value bool) error {
	v := "0"
	if value {
		v = "1"
	}
	path := fmt.Sprintf("%s/%s/brport/%s", portsecSysRoot, port, flag)
	if err := os.WriteFile(path, []byte(v), 0644); err != nil {
		return fmt.Errorf("set %s on %s: %w", flag, port, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// netlink FDB (AF_BRIDGE)

// fdbAddMAC anade una entrada estatica MAC -> boca en la FDB del puente
// (equivalente a `bridge fdb add <mac> dev <port> master static`).
func fdbAddMAC(port string, mac net.HardwareAddr) error {
	return fdbNeigh(port, mac, netlink.NeighAdd)
}

// fdbSetMAC crea o reemplaza la entrada (idempotente, para el restore).
func fdbSetMAC(port string, mac net.HardwareAddr) error {
	return fdbNeigh(port, mac, netlink.NeighSet)
}

func fdbDelMAC(port string, mac net.HardwareAddr) error {
	return fdbNeigh(port, mac, netlink.NeighDel)
}

func fdbNeigh(port string, mac net.HardwareAddr, op func(*netlink.Neigh) error) error {
	link, err := netlink.LinkByName(port)
	if err != nil {
		return fmt.Errorf("link %s: %w", port, err)
	}
	n := &netlink.Neigh{
		LinkIndex:    link.Attrs().Index,
		Family:       unix.AF_BRIDGE,
		State:        netlink.NUD_NOARP, // "static"
		Flags:        netlink.NTF_MASTER,
		HardwareAddr: mac,
	}
	if err := op(n); err != nil {
		return fmt.Errorf("bridge fdb on %s: %w", port, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Persistencia

// loadPortListConf lee un conf con una boca por linea: la boca presente es
// verdadera (isolation.conf). Las lineas con "=0" cuentan como falsas.
func loadPortListConf(path string) map[string]bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]bool{}
	}
	return parsePortValueConf(string(data))
}

// parsePortValueConf interpreta el contenido (funcion pura, testeable).
func parsePortValueConf(text string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, hasVal := strings.Cut(line, "=")
		port := strings.TrimSpace(key)
		if port == "" {
			continue
		}
		v := true
		if hasVal && strings.TrimSpace(val) == "0" {
			v = false
		}
		out[port] = v
	}
	return out
}

// savePortListConf escribe una lista de bocas con valor verdadero, una por
// linea (las falsas desaparecen del fichero).
func savePortListConf(path string, values map[string]bool) {
	var ports []string
	for port, v := range values {
		if v {
			ports = append(ports, port)
		}
	}
	sort.Strings(ports)
	content := ""
	if len(ports) > 0 {
		content = strings.Join(ports, "\n") + "\n"
	}
	writeNetgripConf(path, content)
}

// loadLearningConf lee learning.conf: "<boca>=0" por linea marca bocas con
// el aprendizaje desactivado de forma persistente.
func loadLearningConf() map[string]bool {
	data, err := os.ReadFile(learningConfPath)
	if err != nil {
		return map[string]bool{}
	}
	return parseLearningConf(string(data))
}

// parseLearningConf interpreta el contenido (funcion pura, testeable).
func parseLearningConf(text string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		port := strings.TrimSpace(key)
		if port == "" || !ok || strings.TrimSpace(val) != "0" {
			continue
		}
		out[port] = true
	}
	return out
}

func saveLearningConf(off map[string]bool) {
	var ports []string
	for port, isOff := range off {
		if isOff {
			ports = append(ports, port)
		}
	}
	sort.Strings(ports)
	content := ""
	for _, p := range ports {
		content += p + "=0\n"
	}
	writeNetgripConf(learningConfPath, content)
}

// loadFdbConf lee fdb.conf ("<boca>=<mac> <mac>..." por linea).
func loadFdbConf() map[string][]string {
	data, err := os.ReadFile(fdbConfPath)
	if err != nil {
		return map[string][]string{}
	}
	return parseFdbConf(string(data))
}

// parseFdbConf interpreta el contenido (funcion pura, testeable). Las MACs
// se normalizan a minusculas y se ignoran las entradas invalidas.
func parseFdbConf(text string) map[string][]string {
	out := map[string][]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		port := strings.TrimSpace(key)
		if port == "" || !ok {
			continue
		}
		for _, m := range strings.Fields(val) {
			mac, err := parseStaticMAC(m)
			if err != nil {
				continue
			}
			out[port] = append(out[port], mac.String())
		}
	}
	return out
}

// formatFdbConf serializa el mapa (funcion pura, testeable).
func formatFdbConf(conf map[string][]string) string {
	var lines []string
	for _, port := range sortedKeys(conf) {
		macs := append([]string{}, conf[port]...)
		sort.Strings(macs)
		lines = append(lines, port+"="+strings.Join(macs, " "))
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func saveFdbConf(conf map[string][]string) {
	writeNetgripConf(fdbConfPath, formatFdbConf(conf))
}

func writeNetgripConf(path, content string) {
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, []byte(content), 0600)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// parseStaticMAC valida y normaliza una MAC (funcion pura, testeable).
func parseStaticMAC(s string) (net.HardwareAddr, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	mac, err := net.ParseMAC(s)
	if err != nil {
		return nil, fmt.Errorf("invalid mac %q", s)
	}
	if len(mac) != 6 {
		return nil, fmt.Errorf("invalid mac %q (expected 48 bits)", s)
	}
	return mac, nil
}
