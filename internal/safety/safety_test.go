package safety

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHashIsDeterministicWithinWindow(t *testing.T) {
	req := map[string]any{
		"op": "order_send", "symbol": "EURUSD", "side": "buy", "volume": 0.10,
		"sl": 1.08, "tp": 1.10,
	}
	a, err := Hash(req)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Hash(req)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("hash changed within window: %s vs %s", a, b)
	}
}

func TestVerifyAcceptsCurrentAndPreviousWindow(t *testing.T) {
	req := map[string]any{"op": "close_all", "filter": "profit<0"}
	h, _ := Hash(req)

	ok, err := Verify(h, req)
	if err != nil || !ok {
		t.Fatalf("current-window verify failed: ok=%v err=%v", ok, err)
	}
	other := map[string]any{"op": "close_all", "filter": "profit<-100"}
	ok2, _ := Verify(h, other)
	if ok2 {
		t.Fatal("hash verified for a different request — collision/bug")
	}
}

func TestWindowConstant(t *testing.T) {
	if Window != 60*time.Second {
		t.Fatalf("spec says hash expires after 60s; got %s", Window)
	}
}

func TestKillSwitchBlocks(t *testing.T) {
	f := filepath.Join(t.TempDir(), "STOP")
	if err := os.WriteFile(f, []byte("stop"), 0644); err != nil {
		t.Fatal(err)
	}
	g := Guardrails{KillSwitchFile: f}
	if err := CheckGuardrails(g, nil); err == nil {
		t.Fatal("kill switch present but writes allowed")
	}
}

func TestKillSwitchAbsentAllows(t *testing.T) {
	g := Guardrails{KillSwitchFile: filepath.Join(t.TempDir(), "definitely-absent")}
	if err := CheckGuardrails(g, nil); err != nil {
		t.Fatalf("absent kill switch should be permissive: %v", err)
	}
}
