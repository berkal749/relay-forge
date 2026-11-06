package handlers

import (
	"net"
	"testing"
)

func TestCIDRAllowed(t *testing.T) {
	ip := net.ParseIP("203.0.113.7")
	if !cidrAllowed(ip, []string{"203.0.113.0/24"}) {
		t.Fatal("cidrAllowed() = false, want true")
	}
	if cidrAllowed(ip, []string{"10.0.0.0/8"}) {
		t.Fatal("cidrAllowed() = true, want false")
	}
	if cidrAllowed(ip, []string{"not-a-cidr"}) {
		t.Fatal("cidrAllowed() accepted an invalid CIDR")
	}
}
