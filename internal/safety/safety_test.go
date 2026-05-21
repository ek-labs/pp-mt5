package safety

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
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
	cmd := dummyWriteCmd()
	err := PrecheckWrite(cmd, Guardrails{KillSwitchFile: f}, 0)
	if err == nil {
		t.Fatal("kill switch present but writes allowed")
	}
}

func TestKillSwitchAbsentAllowsDemo(t *testing.T) {
	cmd := dummyWriteCmd()
	g := Guardrails{KillSwitchFile: filepath.Join(t.TempDir(), "definitely-absent")}
	if err := PrecheckWrite(cmd, g, 0); err != nil {
		t.Fatalf("demo write should pass kill-switch + live gates: %v", err)
	}
}

func TestRealAccountRequiresEnvAndFlag(t *testing.T) {
	os.Unsetenv("MT5_LIVE")
	cmd := dummyWriteCmd()
	if err := PrecheckWrite(cmd, Guardrails{}, 2); err == nil {
		t.Fatal("real account with no MT5_LIVE should be rejected")
	}
	t.Setenv("MT5_LIVE", "1")
	if err := PrecheckWrite(cmd, Guardrails{}, 2); err == nil {
		t.Fatal("real account with MT5_LIVE but no --i-understand-this-is-live should be rejected")
	}
	cmd.SetArgs([]string{"--i-understand-this-is-live"})
	if err := cmd.ParseFlags([]string{"--i-understand-this-is-live"}); err != nil {
		t.Fatal(err)
	}
	if err := PrecheckWrite(cmd, Guardrails{}, 2); err != nil {
		t.Fatalf("real account with both gates set should pass: %v", err)
	}
}

func TestMaxVolumeGuardrail(t *testing.T) {
	g := Guardrails{MaxVolumePerOrder: 1.0}
	if err := CheckGuardrails(g, 0.5); err != nil {
		t.Fatalf("0.5 under 1.0 should pass: %v", err)
	}
	if err := CheckGuardrails(g, 2.0); err == nil {
		t.Fatal("2.0 over 1.0 should fail")
	}
	if err := CheckGuardrails(Guardrails{}, 1e6); err != nil {
		t.Fatal("MaxVolumePerOrder=0 disables the limit")
	}
}

func dummyWriteCmd() *cobra.Command {
	c := &cobra.Command{Use: "test"}
	AddWriteFlags(c)
	return c
}
