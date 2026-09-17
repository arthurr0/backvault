package mongodb

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/source"
	"github.com/arthurr0/backvault/internal/source/internal/procstream"
)

type Driver struct{}

func New() *Driver { return &Driver{} }

func init() { source.Register(New()) }

func (d *Driver) Spec() core.DriverSpec {
	return core.DriverSpec{
		Kind:        "mongodb",
		Label:       "MongoDB",
		Description: "Archive produced by mongodump, restorable with mongorestore.",
		Icon:        "database",
		Category:    "Database",
		Tools:       []string{"mongodump", "mongorestore"},
		Capabilities: []string{
			core.CapTest,
			core.CapRestore,
		},
		Fields: []core.Field{
			{
				Name: "uri", Label: "Connection URI", Type: core.FieldSecret, Secret: true,
				Required: true, Group: "Connection",
				Placeholder: "mongodb://user:password@127.0.0.1:27017/?authSource=admin",
				Help:        "Full mongodb:// or mongodb+srv:// URI. Stored encrypted and scrubbed from the run log.",
			},
			{
				Name: "database", Label: "Database", Type: core.FieldString, Group: "Options",
				Placeholder: "app", Help: "Leave empty to dump every database the user can read.",
			},
			{
				Name: "collection", Label: "Collection", Type: core.FieldString, Group: "Options",
				Placeholder: "events", Help: "Only used together with a database. Leave empty for all collections.",
			},
			{
				Name: "extra_args", Label: "Extra arguments", Type: core.FieldStringList,
				Group: "Advanced", Advanced: true, Placeholder: "--numParallelCollections=2",
				Help: "Appended verbatim to mongodump, one argument per line.",
			},
			{
				Name: "binary_path", Label: "Binary path", Type: core.FieldPath,
				Group: "Advanced", Advanced: true, Placeholder: "/usr/bin",
				Help: "Directory holding mongodump and mongorestore, or the full path to mongodump. Leave empty to use PATH.",
			},
		},
	}
}

func (d *Driver) Validate(cfg core.Config) error {
	if err := core.ValidateRequired(d.Spec(), cfg); err != nil {
		return err
	}
	raw := cfg.String("uri")
	if !strings.HasPrefix(raw, "mongodb://") && !strings.HasPrefix(raw, "mongodb+srv://") {
		return fmt.Errorf("connection uri must start with mongodb:// or mongodb+srv://")
	}
	if _, err := url.Parse(raw); err != nil {
		return fmt.Errorf("connection uri is not a valid url")
	}
	if cfg.Has("collection") && !cfg.Has("database") {
		return fmt.Errorf("a database is required when a collection is set")
	}
	return nil
}

func (d *Driver) Test(ctx context.Context, cfg core.Config, log *slog.Logger) error {
	bin, err := procstream.Lookup(cfg.String("binary_path"), "mongodump")
	if err != nil {
		return err
	}
	probe := func(args []string) error {
		return procstream.Run(ctx, log, procstream.Command{
			Name: bin, Args: args, Redact: redact(cfg), Label: "mongodump", Quiet: true,
		})
	}
	base := []string{"--uri=" + cfg.String("uri"), "--archive=/dev/null", "--quiet"}
	err = probe(append(base, "--db=admin", "--collection=system.version"))
	if err == nil {
		return nil
	}
	if db := cfg.String("database"); db != "" {
		if fallback := probe(append(base, "--db="+db, "--collection=backvault_connection_probe")); fallback == nil {
			return nil
		}
	}
	return err
}

func (d *Driver) Backup(ctx context.Context, cfg core.Config, log *slog.Logger) (*source.Stream, error) {
	if err := d.Validate(cfg); err != nil {
		return nil, err
	}
	bin, err := procstream.Lookup(cfg.String("binary_path"), "mongodump")
	if err != nil {
		return nil, err
	}
	args := []string{"--uri=" + cfg.String("uri"), "--archive"}
	if db := cfg.String("database"); db != "" {
		args = append(args, "--db="+db)
		if coll := cfg.String("collection"); coll != "" {
			args = append(args, "--collection="+coll)
		}
	}
	args = append(args, cfg.StringList("extra_args")...)
	log.Info("dumping mongodb", "target", target(cfg))
	p, err := procstream.Start(ctx, log, procstream.Command{
		Name: bin, Args: args, Redact: redact(cfg), Label: "mongodump",
	})
	if err != nil {
		return nil, err
	}
	return &source.Stream{
		Reader:    p,
		Extension: "archive",
		Meta:      map[string]string{"target": target(cfg)},
	}, nil
}

func (d *Driver) Restore(ctx context.Context, cfg core.Config, r io.Reader, opts source.RestoreOptions, log *slog.Logger) error {
	bin, err := procstream.Lookup(cfg.String("binary_path"), "mongorestore")
	if err != nil {
		return err
	}
	params := opts.Params
	if params == nil {
		params = core.Config{}
	}
	args := []string{"--uri=" + cfg.String("uri"), "--archive"}
	if params.Bool("drop", false) {
		args = append(args, "--drop")
	}
	if db := params.StringOr("database", cfg.String("database")); db != "" {
		args = append(args, "--nsInclude="+db+".*")
	}
	if from := params.String("rename_from"); from != "" && params.Has("rename_to") {
		args = append(args, "--nsFrom="+from+".*", "--nsTo="+params.String("rename_to")+".*")
	}
	log.Info("restoring mongodb archive", "drop", params.Bool("drop", false))
	return procstream.Run(ctx, log, procstream.Command{
		Name: bin, Args: args, Stdin: r, Redact: redact(cfg), Label: "mongorestore",
	})
}

func redact(cfg core.Config) []string {
	raw := cfg.String("uri")
	if raw == "" {
		return nil
	}
	out := []string{raw}
	if u, err := url.Parse(raw); err == nil && u.User != nil {
		if pw, ok := u.User.Password(); ok && pw != "" {
			out = append(out, pw)
		}
	}
	return out
}

func target(cfg core.Config) string {
	db := cfg.String("database")
	if db == "" {
		return "all databases"
	}
	if coll := cfg.String("collection"); coll != "" {
		return db + "." + coll
	}
	return db
}

var (
	_ source.Driver   = (*Driver)(nil)
	_ source.Restorer = (*Driver)(nil)
)
