package modules

import (
	"strings"
	"testing"
)

// Los payloads son los mismos que la auditoría usó para demostrar la
// inyección; ahora deben ser rechazados antes de tocar cron, nft o init.d.

func TestPoeCronSchedule(t *testing.T) {
	if got := poeCronSchedule("08:30"); got != "30 8 * * *" {
		t.Fatalf("poeCronSchedule(08:30) = %q", got)
	}
	if got := poeCronSchedule("00:05"); got != "5 0 * * *" {
		t.Fatalf("poeCronSchedule(00:05) = %q", got)
	}
}

func TestSetPoEScheduleValidates(t *testing.T) {
	cases := []PoESchedule{
		{Port: "lan2' ; reboot ; #", OnTime: "08:00"},
		{Port: "../../etc", OnTime: "08:00"},
		{Port: "lan2", OnTime: "* * * * *\n* * * * * id #"},
		{Port: "lan2", OnTime: "25:00"},
		{Port: "lan2", OffTime: "8pm"},
	}
	for _, c := range cases {
		if _, err := SetPoESchedule(c); err == nil {
			t.Fatalf("SetPoESchedule(%+v) no fue rechazado", c)
		}
	}
}

func TestSetMACACLValidates(t *testing.T) {
	// bridgePorts() lee /sys: en la máquina de tests no hay bridge, así que
	// cualquier puerto se rechaza (defecto seguro); y las MACs malas se
	// rechazan con independencia del puerto.
	if err := SetMACACL(MACACLSetRequest{Port: "lan2\" } {\n chain pwn {\n    #", Mode: "allow", MACs: []string{"aa:bb:cc:dd:ee:ff"}}); err == nil {
		t.Fatal("puerto inyectado no rechazado")
	}
	if err := SetMACACL(MACACLSetRequest{Port: "lan2", Mode: "allow", MACs: []string{"x accept\n ether saddr 11:22:33:44:55:66 drop\n #"}}); err == nil {
		t.Fatal("MAC inyectada no rechazada")
	}
}

func TestSetNftQoSValidatesIP(t *testing.T) {
	_, _, err := SetNftQoSLimit(NftQoSSetRequest{
		MAC:    "aa:bb:cc:dd:ee:ff",
		IP:     "192.0.2.1 limit rate over 1 kbytes/second accept\n ip saddr 203.0.113.7 drop\n #",
		Upload: 10,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid ip") {
		t.Fatalf("IP inyectada no rechazada: %v", err)
	}
}

func TestSetStorageServiceValidatesName(t *testing.T) {
	if err := SetStorageService("../../sbin/reboot", "enable"); err == nil {
		t.Fatal("traversal no rechazado")
	}
	if err := SetStorageService("cron", "enable"); err == nil {
		t.Fatal("servicio fuera de la allowlist no rechazado")
	}
	if err := SetStorageService("", "enable"); err == nil {
		t.Fatal("nombre vacío no rechazado")
	}
}
