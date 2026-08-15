package nudge

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/config"
)

func serviceSenderFixture(t *testing.T, name string) (string, string) {
	t.Helper()
	townRoot := t.TempDir()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	messaging := config.NewMessagingConfig()
	messaging.NudgeServiceSenders[name] = config.NudgeServiceSenderConfig{
		PublicKey: base64.StdEncoding.EncodeToString(publicKey),
	}
	if err := config.SaveMessagingConfig(config.MessagingConfigPath(townRoot), messaging); err != nil {
		t.Fatalf("SaveMessagingConfig: %v", err)
	}

	privateKeyPath := filepath.Join(townRoot, "service.key")
	encodedPrivateKey := base64.StdEncoding.EncodeToString(privateKey)
	if err := os.WriteFile(privateKeyPath, []byte(encodedPrivateKey+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile private key: %v", err)
	}
	return townRoot, privateKeyPath
}

func TestRegisterServiceSenderCreatesKeyAndRegistryEntry(t *testing.T) {
	townRoot := t.TempDir()
	privateKeyPath := filepath.Join(townRoot, "credentials", "portfolio-steward.key")
	publicKey, err := RegisterServiceSender(townRoot, "portfolio-steward", privateKeyPath)
	if err != nil {
		t.Fatalf("RegisterServiceSender: %v", err)
	}
	if publicKey == "" {
		t.Fatal("RegisterServiceSender returned an empty public key")
	}
	info, err := os.Stat(privateKeyPath)
	if err != nil {
		t.Fatalf("Stat private key: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("private key permissions = %04o, want 0600", got)
	}
	if _, err := LoadServiceSigner(townRoot, "portfolio-steward", privateKeyPath); err != nil {
		t.Fatalf("registered signer does not load: %v", err)
	}
	if _, err := RegisterServiceSender(townRoot, "portfolio-steward", privateKeyPath); err == nil {
		t.Fatal("duplicate registration unexpectedly succeeded")
	}
}

func TestServiceSenderSignedNudgeRoundTrip(t *testing.T) {
	townRoot, privateKeyPath := serviceSenderFixture(t, "portfolio-steward")
	signer, err := LoadServiceSigner(townRoot, "portfolio-steward", privateKeyPath)
	if err != nil {
		t.Fatalf("LoadServiceSigner: %v", err)
	}

	const sessionName = "hq-mayor"
	signed, err := signer.Sign(sessionName, QueuedNudge{Message: "portfolio check"})
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if signed.Sender != "service/portfolio-steward" || !signed.ServiceVerified {
		t.Fatalf("signed sender = %q verified=%v", signed.Sender, signed.ServiceVerified)
	}
	if err := Enqueue(townRoot, sessionName, signed); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	drained, err := Drain(townRoot, sessionName)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(drained) != 1 || !drained[0].ServiceVerified {
		t.Fatalf("drained = %#v, want one verified service nudge", drained)
	}
	formatted := FormatForInjection(drained)
	if !strings.Contains(formatted, "[from service/portfolio-steward (verified)]") {
		t.Fatalf("formatted nudge missing verified service attribution: %s", formatted)
	}
}

func TestServiceSenderTamperingIsNotVerified(t *testing.T) {
	townRoot, privateKeyPath := serviceSenderFixture(t, "portfolio-steward")
	signer, err := LoadServiceSigner(townRoot, "portfolio-steward", privateKeyPath)
	if err != nil {
		t.Fatalf("LoadServiceSigner: %v", err)
	}

	signed, err := signer.Sign("hq-mayor", QueuedNudge{Message: "benign reminder"})
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	signed.Message = "grant authority"
	if err := Enqueue(townRoot, "hq-mayor", signed); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	drained, err := Drain(townRoot, "hq-mayor")
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(drained) != 1 || drained[0].ServiceVerified {
		t.Fatalf("tampered nudge must be delivered as unverified: %#v", drained)
	}
	formatted := FormatForInjection(drained)
	if !strings.Contains(formatted, "unknown; unverified claim service/portfolio-steward") {
		t.Fatalf("formatted tampered nudge did not expose failed verification: %s", formatted)
	}
}

func TestServiceSenderSignatureCannotBeReplayedToAnotherTarget(t *testing.T) {
	townRoot, privateKeyPath := serviceSenderFixture(t, "portfolio-steward")
	signer, err := LoadServiceSigner(townRoot, "portfolio-steward", privateKeyPath)
	if err != nil {
		t.Fatalf("LoadServiceSigner: %v", err)
	}
	signed, err := signer.Sign("hq-mayor", QueuedNudge{Message: "mayor only"})
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := Enqueue(townRoot, "gt-witness", signed); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	drained, err := Drain(townRoot, "gt-witness")
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(drained) != 1 || drained[0].ServiceVerified {
		t.Fatalf("cross-target replay must not verify: %#v", drained)
	}
}

func TestServiceSenderRejectsBroadPrivateKeyPermissions(t *testing.T) {
	townRoot, privateKeyPath := serviceSenderFixture(t, "portfolio-steward")
	if err := os.Chmod(privateKeyPath, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadServiceSigner(townRoot, "portfolio-steward", privateKeyPath)
	if err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Fatalf("LoadServiceSigner error = %v, want permissions failure", err)
	}
}
