package cluster

import (
	"os"
	"testing"
)

func TestParseSSHConfig(t *testing.T) {
	f, err := os.Open("testdata/ssh_config")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	hosts, err := ParseSSHConfig(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 3 {
		t.Fatalf("hosts = %d, want 3", len(hosts))
	}
	cp := hosts["controlplane"]
	want := SSHHost{Host: "controlplane", HostName: "192.168.121.10", User: "vagrant", Port: 22,
		IdentityFile: "/home/u/lab/.vagrant/machines/controlplane/libvirt/private_key"}
	if cp != want {
		t.Fatalf("controlplane = %+v", cp)
	}
	n2 := hosts["node02"]
	if n2.Port != 2222 || n2.IdentityFile != "/home/u/my lab/.vagrant/machines/node02/libvirt/private_key" {
		t.Fatalf("node02 = %+v", n2)
	}
}

func TestParseSSHConfig_Empty(t *testing.T) {
	f, _ := os.Open(os.DevNull)
	defer f.Close()
	if _, err := ParseSSHConfig(f); err == nil {
		t.Fatal("want error for empty config")
	}
}
