package secrets

import (
	"fmt"

	"github.com/arthurr0/backvault/internal/core"
)

func (c *Cipher) EncryptHost(h core.Host) (core.Host, error) {
	out := h
	for _, field := range []struct {
		name  string
		value *string
	}{
		{"privateKey", &out.PrivateKey},
		{"keyPassphrase", &out.KeyPassphrase},
		{"password", &out.Password},
	} {
		if *field.value == "" {
			continue
		}
		enc, err := c.Encrypt(*field.value)
		if err != nil {
			return h, fmt.Errorf("encrypt host field %s: %w", field.name, err)
		}
		*field.value = enc
	}
	return out, nil
}

func (c *Cipher) DecryptHost(h core.Host) (core.Host, error) {
	out := h
	for _, field := range []struct {
		name  string
		value *string
	}{
		{"privateKey", &out.PrivateKey},
		{"keyPassphrase", &out.KeyPassphrase},
		{"password", &out.Password},
	} {
		if *field.value == "" {
			continue
		}
		plain, err := c.Decrypt(*field.value)
		if err != nil {
			return h, fmt.Errorf("decrypt host field %s: %w", field.name, err)
		}
		*field.value = plain
	}
	return out, nil
}

func (c *Cipher) DecryptHostLenient(h core.Host) core.Host {
	out, err := c.DecryptHost(h)
	if err != nil {
		return h
	}
	return out
}
