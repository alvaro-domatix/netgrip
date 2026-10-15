package modules

import (
	"testing"
)

// En la máquina de tests no hay bridge (bridgePorts() vacío): cualquier
// boca se rechaza, que es el defecto seguro esperado en la frontera.
// (parseStaticMAC y los .conf ya tienen cobertura en portsec_test.go.)
func TestPortSecurityValidation(t *testing.T) {
	if err := SetPortIsolation(PortIsolationSetRequest{Port: "", Isolated: true}); err == nil {
		t.Fatal("boca vacía no rechazada (isolation)")
	}
	if err := SetPortIsolation(PortIsolationSetRequest{Port: "lan99", Isolated: true}); err == nil {
		t.Fatal("boca fuera del bridge no rechazada (isolation)")
	}
	if err := SetPortLearning(PortSecurityLearningRequest{Port: "lan1", Learning: true}); err == nil {
		t.Fatal("boca fuera del bridge no rechazada (learning)")
	}
	if err := SetPortStaticMAC(PortSecurityFdbRequest{Port: "lan1", MAC: "zz:zz", Add: true}); err == nil {
		t.Fatal("boca fuera del bridge no rechazada (fdb)")
	}
}
