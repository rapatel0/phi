package serve

import "testing"

func TestRequireLoopback(t *testing.T) {
	if err := requireLoopback("127.0.0.1:38765"); err != nil {
		t.Fatal(err)
	}
	if err := requireLoopback("[::1]:38765"); err != nil {
		t.Fatal(err)
	}
	if err := requireLoopback("0.0.0.0:38765"); err == nil {
		t.Fatal("expected reject")
	}
	if err := requireLoopback("192.168.1.1:80"); err == nil {
		t.Fatal("expected reject")
	}
}
