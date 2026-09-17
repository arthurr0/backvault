package ssh

import (
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/arthurr0/backvault/internal/core"

	cryptossh "golang.org/x/crypto/ssh"
)

func dial(cfg core.Config, log *slog.Logger) (*cryptossh.Client, error) {
	timeout := time.Duration(cfg.Int("connect_timeout", 15)) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	auths, err := authMethods(cfg)
	if err != nil {
		return nil, err
	}
	hostKey, err := hostKeyCallback(cfg, log)
	if err != nil {
		return nil, err
	}
	clientCfg := &cryptossh.ClientConfig{
		User:            cfg.String("user"),
		Auth:            auths,
		HostKeyCallback: hostKey,
		Timeout:         timeout,
	}
	addr := net.JoinHostPort(cfg.String("host"), strconv.Itoa(cfg.Int("port", 22)))
	client, err := cryptossh.Dial("tcp", addr, clientCfg)
	if err != nil {
		return nil, fmt.Errorf("ssh connect to %s: %w", addr, scrub(cfg, err))
	}
	return client, nil
}

func authMethods(cfg core.Config) ([]cryptossh.AuthMethod, error) {
	switch cfg.StringOr("auth", "key") {
	case "password":
		pw := cfg.String("password")
		if pw == "" {
			return nil, fmt.Errorf("password authentication selected but no password is set")
		}
		return []cryptossh.AuthMethod{
			cryptossh.Password(pw),
			cryptossh.KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = pw
				}
				return answers, nil
			}),
		}, nil
	case "key":
		pem := strings.TrimSpace(cfg.String("private_key"))
		if pem == "" {
			return nil, fmt.Errorf("key authentication selected but no private key is set")
		}
		var signer cryptossh.Signer
		var err error
		if pass := cfg.String("key_passphrase"); pass != "" {
			signer, err = cryptossh.ParsePrivateKeyWithPassphrase([]byte(pem), []byte(pass))
		} else {
			signer, err = cryptossh.ParsePrivateKey([]byte(pem))
		}
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
		return []cryptossh.AuthMethod{cryptossh.PublicKeys(signer)}, nil
	default:
		return nil, fmt.Errorf("auth must be password or key")
	}
}

func hostKeyCallback(cfg core.Config, log *slog.Logger) (cryptossh.HostKeyCallback, error) {
	expected := strings.TrimSpace(cfg.String("host_key"))
	if expected == "" {
		return func(hostname string, remote net.Addr, key cryptossh.PublicKey) error {
			if log != nil {
				log.Warn("accepting unverified ssh host key, copy it into the host key field to pin it",
					"host", hostname, "type", key.Type(), "fingerprint", cryptossh.FingerprintSHA256(key))
			}
			return nil
		}, nil
	}
	if !strings.HasPrefix(expected, "SHA256:") {
		return nil, fmt.Errorf("host key fingerprint must look like SHA256:abc...")
	}
	return func(hostname string, remote net.Addr, key cryptossh.PublicKey) error {
		got := cryptossh.FingerprintSHA256(key)
		if got != expected {
			return fmt.Errorf("host key mismatch for %s: expected %s, got %s", hostname, expected, got)
		}
		return nil
	}, nil
}

func scrub(cfg core.Config, err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	for _, secret := range []string{cfg.String("password"), cfg.String("key_passphrase"), cfg.String("private_key")} {
		if len(secret) >= 3 {
			msg = strings.ReplaceAll(msg, secret, "***")
		}
	}
	if msg == err.Error() {
		return err
	}
	return fmt.Errorf("%s", msg)
}
