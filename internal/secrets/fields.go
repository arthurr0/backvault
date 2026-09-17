package secrets

import (
	"fmt"

	"github.com/arthurr0/backvault/internal/core"
)

func (c *Cipher) EncryptConfig(cfg core.Config, secretFields []string) (core.Config, error) {
	if cfg == nil {
		return nil, nil
	}
	out := cfg.Clone()
	for _, f := range secretFields {
		raw, ok := out[f]
		if !ok || raw == nil {
			continue
		}
		s, isString := raw.(string)
		if !isString || s == "" {
			continue
		}
		enc, err := c.Encrypt(s)
		if err != nil {
			return nil, fmt.Errorf("encrypt field %s: %w", f, err)
		}
		out[f] = enc
	}
	return out, nil
}

func (c *Cipher) DecryptConfig(cfg core.Config, secretFields []string) (core.Config, error) {
	if cfg == nil {
		return nil, nil
	}
	out := cfg.Clone()
	for _, f := range secretFields {
		raw, ok := out[f]
		if !ok || raw == nil {
			continue
		}
		s, isString := raw.(string)
		if !isString || s == "" {
			continue
		}
		plain, err := c.Decrypt(s)
		if err != nil {
			return nil, fmt.Errorf("decrypt field %s: %w", f, err)
		}
		out[f] = plain
	}
	return out, nil
}

func (c *Cipher) DecryptConfigLenient(cfg core.Config, secretFields []string) core.Config {
	out, err := c.DecryptConfig(cfg, secretFields)
	if err != nil {
		return cfg.Clone()
	}
	return out
}
