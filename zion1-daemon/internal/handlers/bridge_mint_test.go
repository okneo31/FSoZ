package handlers

import (
	"strings"
	"testing"

	"github.com/okneo31/zion1-daemon/internal/zionclient"
)

func TestRequireAddrAttr(t *testing.T) {
	ev := zionclient.TypedEvent{Attributes: map[string]string{
		"evm_addr": "0x1111111111111111111111111111111111111111",
	}}
	addr, err := requireAddrAttr(ev, "evm_addr")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(addr.Hex(), "0x1111111111111111111111111111111111111111") {
		t.Fatalf("got %s", addr.Hex())
	}
	if _, err := requireAddrAttr(ev, "missing"); err == nil {
		t.Fatal("expected error for missing attr")
	}
	zero := zionclient.TypedEvent{Attributes: map[string]string{
		"evm_addr": "0x0000000000000000000000000000000000000000",
	}}
	if _, err := requireAddrAttr(zero, "evm_addr"); err == nil {
		t.Fatal("expected error for zero addr")
	}
}

func TestRequireUintAttr(t *testing.T) {
	ev := zionclient.TypedEvent{Attributes: map[string]string{
		"amount": "1000000000000",
	}}
	n, err := requireUintAttr(ev, "amount")
	if err != nil {
		t.Fatal(err)
	}
	if n.String() != "1000000000000" {
		t.Fatalf("got %s", n.String())
	}
	if _, err := requireUintAttr(zionclient.TypedEvent{Attributes: map[string]string{"amount": "abc"}}, "amount"); err == nil {
		t.Fatal("expected error for non-decimal")
	}
}

func TestRequireUint64Attr_Overflow(t *testing.T) {
	// 2^64 = 18446744073709551616 → overflow
	ev := zionclient.TypedEvent{Attributes: map[string]string{"day": "18446744073709551616"}}
	if _, err := requireUint64Attr(ev, "day"); err == nil {
		t.Fatal("expected overflow error")
	}
	// 2^64 - 1 fits
	ev2 := zionclient.TypedEvent{Attributes: map[string]string{"day": "18446744073709551615"}}
	n, err := requireUint64Attr(ev2, "day")
	if err != nil {
		t.Fatal(err)
	}
	if n != ^uint64(0) {
		t.Fatalf("got %d", n)
	}
}

func TestRequireBoolAttr(t *testing.T) {
	cases := map[string]bool{"true": true, "false": false, "1": true, "0": false}
	for in, want := range cases {
		ev := zionclient.TypedEvent{Attributes: map[string]string{"k": in}}
		got, err := requireBoolAttr(ev, "k")
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got != want {
			t.Fatalf("%q: got %v want %v", in, got, want)
		}
	}
	ev := zionclient.TypedEvent{Attributes: map[string]string{"k": "yes"}}
	if _, err := requireBoolAttr(ev, "k"); err == nil {
		t.Fatal("expected error for 'yes'")
	}
}

func TestRequireBytes32Attr(t *testing.T) {
	ev := zionclient.TypedEvent{Attributes: map[string]string{
		"lock_id": "0x" + strings.Repeat("ab", 32),
	}}
	b, err := requireBytes32Attr(ev, "lock_id")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 32; i++ {
		if b[i] != 0xab {
			t.Fatalf("byte %d = %x", i, b[i])
		}
	}
	// 0x prefix 없어도 OK
	ev2 := zionclient.TypedEvent{Attributes: map[string]string{
		"lock_id": strings.Repeat("cd", 32),
	}}
	if _, err := requireBytes32Attr(ev2, "lock_id"); err != nil {
		t.Fatal(err)
	}
	// 너무 짧음
	ev3 := zionclient.TypedEvent{Attributes: map[string]string{"lock_id": "0xabc"}}
	if _, err := requireBytes32Attr(ev3, "lock_id"); err == nil {
		t.Fatal("expected error for short")
	}
}
