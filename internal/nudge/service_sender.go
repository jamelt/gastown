package nudge

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/steveyegge/gastown/internal/config"
)

const serviceSignatureVersion = 1

// ServiceSigner signs nudges for a non-agent sender registered in the town's
// messaging configuration.
type ServiceSigner struct {
	name       string
	privateKey ed25519.PrivateKey
}

// RegisterServiceSender creates a private key and registers the corresponding
// public key in config/messaging.json. It refuses to overwrite either an
// existing registration or key file.
func RegisterServiceSender(townRoot, name, privateKeyPath string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("service sender name is required")
	}
	if privateKeyPath == "" {
		return "", fmt.Errorf("private key file path is required")
	}

	messagingPath := config.MessagingConfigPath(townRoot)
	messaging, err := config.LoadOrCreateMessagingConfig(messagingPath)
	if err != nil {
		return "", fmt.Errorf("loading service sender registry: %w", err)
	}
	if _, exists := messaging.NudgeServiceSenders[name]; exists {
		return "", fmt.Errorf("nudge service sender %q is already registered", name)
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generating service sender key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(privateKeyPath), 0o700); err != nil {
		return "", fmt.Errorf("creating private key directory: %w", err)
	}
	keyFile, err := os.OpenFile(privateKeyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // Private key requires 0600.
	if err != nil {
		return "", fmt.Errorf("creating private key: %w", err)
	}
	encodedPrivateKey := base64.StdEncoding.EncodeToString(privateKey) + "\n"
	if _, err := keyFile.WriteString(encodedPrivateKey); err != nil {
		_ = keyFile.Close()
		_ = os.Remove(privateKeyPath)
		return "", fmt.Errorf("writing private key: %w", err)
	}
	if err := keyFile.Close(); err != nil {
		_ = os.Remove(privateKeyPath)
		return "", fmt.Errorf("closing private key: %w", err)
	}

	encodedPublicKey := base64.StdEncoding.EncodeToString(publicKey)
	messaging.NudgeServiceSenders[name] = config.NudgeServiceSenderConfig{PublicKey: encodedPublicKey}
	if err := config.SaveMessagingConfig(messagingPath, messaging); err != nil {
		_ = os.Remove(privateKeyPath)
		return "", fmt.Errorf("saving service sender registry: %w", err)
	}
	return encodedPublicKey, nil
}

// LoadServiceSigner validates a registered service sender and loads its private
// key. Key files must not be accessible to group or other users.
func LoadServiceSigner(townRoot, name, privateKeyPath string) (*ServiceSigner, error) {
	if name == "" {
		return nil, fmt.Errorf("service sender name is required")
	}
	if privateKeyPath == "" {
		return nil, fmt.Errorf("private key file is required for service sender %q", name)
	}

	messaging, err := config.LoadMessagingConfig(config.MessagingConfigPath(townRoot))
	if err != nil {
		return nil, fmt.Errorf("loading service sender registry: %w", err)
	}
	registered, ok := messaging.NudgeServiceSenders[name]
	if !ok {
		return nil, fmt.Errorf("nudge service sender %q is not registered", name)
	}

	info, err := os.Lstat(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("reading private key metadata: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("private key file must not be a symbolic link")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("private key file permissions %04o are too broad; require 0600 or stricter", info.Mode().Perm())
	}

	privateKeyData, err := os.ReadFile(privateKeyPath) //nolint:gosec // Explicit operator-provided credential path.
	if err != nil {
		return nil, fmt.Errorf("reading private key: %w", err)
	}
	privateKey, err := decodeServicePrivateKey(strings.TrimSpace(string(privateKeyData)))
	if err != nil {
		return nil, err
	}

	registeredPublicKey, err := base64.StdEncoding.DecodeString(registered.PublicKey)
	if err != nil || len(registeredPublicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("registered public key for service sender %q is invalid", name)
	}
	derivedPublicKey := privateKey.Public().(ed25519.PublicKey)
	if subtle.ConstantTimeCompare(derivedPublicKey, registeredPublicKey) != 1 {
		return nil, fmt.Errorf("private key does not match registered service sender %q", name)
	}

	return &ServiceSigner{name: name, privateKey: privateKey}, nil
}

func decodeServicePrivateKey(encoded string) (ed25519.PrivateKey, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("private key must be base64-encoded: %w", err)
	}
	switch len(key) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(key), nil
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(key), nil
	default:
		return nil, fmt.Errorf("private key must contain an Ed25519 seed or private key")
	}
}

// Sign binds service attribution to the nudge payload and target session.
func (s *ServiceSigner) Sign(session string, queued QueuedNudge) (QueuedNudge, error) {
	queued.Sender = "service/" + s.name
	queued.ServiceSender = s.name
	queued.ServiceSignatureVersion = serviceSignatureVersion
	prepareQueuedNudge(session, &queued)

	payload, err := serviceSignaturePayload(queued)
	if err != nil {
		return QueuedNudge{}, err
	}
	queued.ServiceSignature = base64.StdEncoding.EncodeToString(ed25519.Sign(s.privateKey, payload))
	queued.ServiceVerified = true
	return queued, nil
}

func verifyServiceNudge(townRoot string, queued *QueuedNudge) bool {
	if queued.ServiceSender == "" || queued.ServiceSignature == "" {
		return false
	}
	if queued.ServiceSignatureVersion != serviceSignatureVersion {
		return false
	}
	if queued.Sender != "service/"+queued.ServiceSender {
		return false
	}

	messaging, err := config.LoadMessagingConfig(config.MessagingConfigPath(townRoot))
	if err != nil {
		return false
	}
	registered, ok := messaging.NudgeServiceSenders[queued.ServiceSender]
	if !ok {
		return false
	}
	publicKey, err := base64.StdEncoding.DecodeString(registered.PublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return false
	}
	signature, err := base64.StdEncoding.DecodeString(queued.ServiceSignature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return false
	}
	payload, err := serviceSignaturePayload(*queued)
	if err != nil {
		return false
	}
	return ed25519.Verify(publicKey, payload, signature)
}

func serviceSignaturePayload(queued QueuedNudge) ([]byte, error) {
	payload := struct {
		Version      int    `json:"version"`
		Sender       string `json:"sender"`
		Service      string `json:"service"`
		Target       string `json:"target"`
		Message      string `json:"message"`
		Priority     string `json:"priority"`
		Kind         string `json:"kind,omitempty"`
		ThreadID     string `json:"thread_id,omitempty"`
		Severity     string `json:"severity,omitempty"`
		Timestamp    int64  `json:"timestamp_unix_nano"`
		ExpiresAt    int64  `json:"expires_at_unix_nano"`
		DeliverAfter int64  `json:"deliver_after_unix_nano"`
	}{
		Version:      queued.ServiceSignatureVersion,
		Sender:       queued.Sender,
		Service:      queued.ServiceSender,
		Target:       queued.Target,
		Message:      queued.Message,
		Priority:     queued.Priority,
		Kind:         queued.Kind,
		ThreadID:     queued.ThreadID,
		Severity:     queued.Severity,
		Timestamp:    queued.Timestamp.UnixNano(),
		ExpiresAt:    queued.ExpiresAt.UnixNano(),
		DeliverAfter: queued.DeliverAfter.UnixNano(),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encoding service nudge signature payload: %w", err)
	}
	return encoded, nil
}

// SenderAttribution returns the trust-aware sender text rendered to an agent.
func SenderAttribution(queued QueuedNudge) string {
	if queued.ServiceSender == "" {
		return queued.Sender
	}
	if queued.ServiceVerified {
		return queued.Sender + " (verified)"
	}
	return "unknown; unverified claim " + queued.Sender
}
