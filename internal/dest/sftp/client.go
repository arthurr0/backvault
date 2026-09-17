package sftp

import (
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/arthurr0/backvault/internal/core"

	"golang.org/x/crypto/ssh"
)

func dial(cfg core.Config, log *slog.Logger) (*ssh.Client, error) {
	timeout := time.Duration(cfg.Int("connect_timeout", 20)) * time.Second
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	auths, err := authMethods(cfg)
	if err != nil {
		return nil, err
	}
	hostKey, err := hostKeyCallback(cfg, log)
	if err != nil {
		return nil, err
	}
	addr := net.JoinHostPort(cfg.String("host"), strconv.Itoa(cfg.Int("port", 22)))
	client, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User:            cfg.String("user"),
		Auth:            auths,
		HostKeyCallback: hostKey,
		Timeout:         timeout,
	})
	if err != nil {
		return nil, fmt.Errorf("ssh connect to %s: %w", addr, scrub(cfg, err))
	}
	return client, nil
}

func authMethods(cfg core.Config) ([]ssh.AuthMethod, error) {
	switch cfg.StringOr("auth", "password") {
	case "password":
		pw := cfg.String("password")
		if pw == "" {
			return nil, fmt.Errorf("password authentication selected but no password is set")
		}
		return []ssh.AuthMethod{
			ssh.Password(pw),
			ssh.KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
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
		var signer ssh.Signer
		var err error
		if pass := cfg.String("key_passphrase"); pass != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(pem), []byte(pass))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(pem))
		}
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	default:
		return nil, fmt.Errorf("auth must be password or key")
	}
}

func hostKeyCallback(cfg core.Config, log *slog.Logger) (ssh.HostKeyCallback, error) {
	expected := strings.TrimSpace(cfg.String("host_key"))
	if expected == "" {
		return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			if log != nil {
				log.Warn("accepting unverified ssh host key, copy it into the host key field to pin it",
					"host", hostname, "type", key.Type(), "fingerprint", ssh.FingerprintSHA256(key))
			}
			return nil
		}, nil
	}
	if !strings.HasPrefix(expected, "SHA256:") {
		return nil, fmt.Errorf("host key fingerprint must look like SHA256:abc...")
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		got := ssh.FingerprintSHA256(key)
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
	original := msg
	for _, secret := range []string{cfg.String("password"), cfg.String("key_passphrase"), cfg.String("private_key")} {
		if len(secret) >= 3 {
			msg = strings.ReplaceAll(msg, secret, "***")
		}
	}
	if msg == original {
		return err
	}
	return fmt.Errorf("%s", msg)
}
