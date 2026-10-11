package modules

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Helpers tc compartidos por las funciones de trafico por boca (storm,
// espejo y banda ancha, #443). Todos conviven en el mismo qdisc clsact de
// cada boca, repartidos por prioridades: asi el espejo no pisa las reglas
// de tormentas ni al reves, y la banda ancha de entrada suma sin conflictos.
const (
	prioStormBroadcast = 1
	prioStormMulticast = 2
	prioMirrorIngress  = 10
	prioMirrorEgress   = 11
	prioRateIngress    = 20
)

// tcHasClsact reporta si la boca ya tiene el qdisc clsact (el gancho
// ingress/egress que usan todas las funciones de trafico).
func tcHasClsact(port string) bool {
	return strings.Contains(tcQdiscShow(port), "clsact")
}

// tcQdiscShow devuelve la salida de `tc qdisc show dev <port>` ("" si tc
// falla, p. ej. binario ausente).
func tcQdiscShow(port string) string {
	out, err := exec.Command("tc", "qdisc", "show", "dev", port).CombinedOutput()
	if err != nil {
		return ""
	}
	return string(out)
}

// tcFilterShow devuelve la salida de `tc filter show dev <port> <dir>`.
func tcFilterShow(port, direction string) string {
	out, err := exec.Command("tc", "filter", "show", "dev", port, direction).CombinedOutput()
	if err != nil {
		return ""
	}
	return string(out)
}

// tcDelPrio borra los filtros de una prioridad concreta (best effort: no
// existe = exito). Borrar por prioridad, no el qdisc entero, es lo que
// permite que storm, espejo y banda ancha coexistan en la misma boca.
func tcDelPrio(port, direction string, prio int) {
	exec.Command("tc", "filter", "del", "dev", port, direction, "prio", strconv.Itoa(prio)).Run()
}

// addClsactRaw intenta crear el qdisc clsact tal cual.
func addClsactRaw(port string) (string, error) {
	out, err := exec.Command("tc", "qdisc", "add", "dev", port, "clsact").CombinedOutput()
	return string(out), err
}

// ensureClsact garantiza el qdisc clsact de una boca. Si existe un qdisc
// `ingress` plano (storm o un restore antiguos, que comparten bloque y son
// excluyentes con clsact), lo migra: borra el qdisc plano y re-aplica los
// filtros de storm que vivian en el, si los habia.
func ensureClsact(port string) error {
	if tcHasClsact(port) {
		return nil
	}
	out, err := addClsactRaw(port)
	if err != nil {
		if strings.Contains(out, "File exists") {
			return nil // ya existe (carrera con otro apply): nada que hacer
		}
		if !strings.Contains(out, "Exclusivity") {
			return fmt.Errorf("add clsact on %s: %v (%s)", port, err, strings.TrimSpace(out))
		}
		// Exclusividad: hay un qdisc ingress plano. Lo migro.
		exec.Command("tc", "qdisc", "del", "dev", port, "ingress").Run()
		if out2, err2 := addClsactRaw(port); err2 != nil {
			return fmt.Errorf("migrate ingress qdisc to clsact on %s: %v (%s)", port, err2, strings.TrimSpace(out2))
		}
		// La migracion ha borrado los filtros storm que hubiera: los
		// re-aplico para no dejar la boca sin control en caliente.
		if pct := loadStormConfigs()[port]; pct > 0 {
			if err3 := applyStormFilters(port, pct); err3 != nil {
				return err3
			}
		}
	}
	return nil
}

// portTcInUse reporta si la boca sigue necesitando el qdisc clsact: filtros
// activos, cola raiz tbf (banda ancha de salida) o config persistida que
// hay que re-aplicar.
func portTcInUse(port string) bool {
	if strings.TrimSpace(tcFilterShow(port, "ingress")) != "" {
		return true
	}
	if strings.TrimSpace(tcFilterShow(port, "egress")) != "" {
		return true
	}
	if strings.Contains(tcQdiscShow(port), "qdisc tbf") {
		return true
	}
	if pct := loadStormConfigs()[port]; pct > 0 {
		return true
	}
	if c, ok := loadRateConfigs()[port]; ok && (c.ingress > 0 || c.egress > 0) {
		return true
	}
	if mirrorConfHasSource(port) {
		return true
	}
	return false
}

// maybeDropClsact quita el qdisc clsact si la boca ya no tiene nada que
// ejecutar en el. Un clsact vacio es inofensivo, pero dejarlo creado en
// las 52 bocas ensucia `tc qdisc show` sin motivo.
func maybeDropClsact(port string) {
	if portTcInUse(port) {
		return
	}
	exec.Command("tc", "qdisc", "del", "dev", port, "clsact").Run()
}

// linkSpeedMbps lee la velocidad de enlace de una boca (1000 por defecto,
// el mismo criterio que storm: una boca caida no debe impedir aplicar).
func linkSpeedMbps(port string) int {
	speed := 1000
	data, err := os.ReadFile("/sys/class/net/" + port + "/speed")
	if err == nil {
		if v, err2 := strconv.Atoi(strings.TrimSpace(string(data))); err2 == nil && v > 0 {
			speed = v
		}
	}
	return speed
}
