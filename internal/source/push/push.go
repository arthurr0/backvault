package push

import (
	"context"
	"errors"
	"log/slog"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/source"
)

type Driver struct{}

func New() *Driver { return &Driver{} }

func init() { source.Register(New()) }

func (d *Driver) Spec() core.DriverSpec {
	return core.DriverSpec{
		Kind:        "push",
		Label:       "Push (remote script)",
		Description: "A placeholder source for machines that create their own dumps and upload them to Backvault through the ingest API.",
		Icon:        "upload",
		Category:    "Custom",
		Capabilities: []string{
			core.CapTest,
			core.CapIngest,
		},
		Fields: []core.Field{
			{
				Name: "expected_filename_extension", Label: "Expected file extension", Type: core.FieldString,
				Default: "bin", Group: "Options", Placeholder: "sql.gz",
				Help: "Only a hint for the artifact name when the pushing client does not send X-Backvault-Filename.",
			},
			{
				Name: "note", Label: "Note", Type: core.FieldText, Group: "Options",
				Placeholder: "Pushed nightly by backup.sh on db01",
				Help:        "Free text describing who pushes to this job and from where.",
			},
		},
	}
}

func (d *Driver) Validate(cfg core.Config) error {
	return core.ValidateRequired(d.Spec(), cfg)
}

func (d *Driver) Test(ctx context.Context, cfg core.Config, log *slog.Logger) error {
	log.Info("push source is ready to receive artifacts through the ingest API")
	return nil
}

func (d *Driver) Backup(ctx context.Context, cfg core.Config, log *slog.Logger) (*source.Stream, error) {
	return nil, errors.New("push sources receive data through the ingest API")
}

var _ source.Driver = (*Driver)(nil)
