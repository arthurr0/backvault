package server

import (
	"fmt"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/dest"
	"github.com/arthurr0/backvault/internal/notify"
	"github.com/arthurr0/backvault/internal/source"
)

type preparedConfig struct {
	Plain     core.Config
	Encrypted core.Config
	Spec      core.DriverSpec
}

func (s *Server) prepareConfig(spec core.DriverSpec, incoming, previous core.Config, validate func(core.Config) error) (preparedConfig, error) {
	fields := spec.SecretFields()
	if incoming == nil {
		incoming = core.Config{}
	}
	merged := incoming.MergeSecrets(previous, fields)
	plain, err := s.secrets.DecryptConfig(merged, fields)
	if err != nil {
		return preparedConfig{}, err
	}
	if err := core.ValidateRequired(spec, plain); err != nil {
		return preparedConfig{}, newValidationError(err.Error()).field("config", err.Error())
	}
	if validate != nil {
		if err := validate(plain); err != nil {
			return preparedConfig{}, newValidationError(err.Error()).field("config", err.Error())
		}
	}
	encrypted, err := s.secrets.EncryptConfig(plain, fields)
	if err != nil {
		return preparedConfig{}, err
	}
	return preparedConfig{Plain: plain, Encrypted: encrypted, Spec: spec}, nil
}

func sourceSpec(kind string) (core.DriverSpec, source.Driver, error) {
	driver, ok := source.Get(kind)
	if !ok {
		return core.DriverSpec{}, nil, newValidationError(fmt.Sprintf("unknown source kind %q", kind)).field("kind", "unknown")
	}
	return driver.Spec(), driver, nil
}

func destinationSpec(kind string) (core.DriverSpec, dest.Driver, error) {
	driver, ok := dest.Get(kind)
	if !ok {
		return core.DriverSpec{}, nil, newValidationError(fmt.Sprintf("unknown destination kind %q", kind)).field("kind", "unknown")
	}
	return driver.Spec(), driver, nil
}

func notifierSpec(kind string) (core.DriverSpec, notify.Driver, error) {
	driver, ok := notify.Get(kind)
	if !ok {
		return core.DriverSpec{}, nil, newValidationError(fmt.Sprintf("unknown notifier kind %q", kind)).field("kind", "unknown")
	}
	return driver.Spec(), driver, nil
}

func maskSource(src core.Source) core.Source {
	if driver, ok := source.Get(src.Kind); ok {
		src.Config = src.Config.Masked(driver.Spec().SecretFields())
	} else {
		src.Config = core.Config{}
	}
	return src
}

func maskDestination(d core.Destination) core.Destination {
	if driver, ok := dest.Get(d.Kind); ok {
		d.Config = d.Config.Masked(driver.Spec().SecretFields())
	} else {
		d.Config = core.Config{}
	}
	return d
}

func maskChannel(c core.NotificationChannel) core.NotificationChannel {
	if driver, ok := notify.Get(c.Kind); ok {
		c.Config = c.Config.Masked(driver.Spec().SecretFields())
	} else {
		c.Config = core.Config{}
	}
	return c
}

func maskJob(j core.Job) core.Job {
	if j.EncryptionPassphrase != "" {
		j.EncryptionPassphrase = core.SecretMask
	}
	return j
}

func maskJobs(jobs []core.Job) []core.Job {
	out := make([]core.Job, len(jobs))
	for i, j := range jobs {
		out[i] = maskJob(j)
	}
	return out
}
