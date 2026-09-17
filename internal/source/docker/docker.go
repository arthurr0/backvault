package docker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/source"
	"github.com/arthurr0/backvault/internal/source/internal/procstream"
)

const defaultImage = "alpine:3.20"

type Driver struct{}

func New() *Driver { return &Driver{} }

func init() { source.Register(New()) }

func (d *Driver) Spec() core.DriverSpec {
	return core.DriverSpec{
		Kind:        "docker",
		Label:       "Docker volume / container",
		Description: "Archives a named Docker volume with a throwaway helper container, or streams the output of a command run inside a running container.",
		Icon:        "container",
		Category:    "Container",
		Tools:       []string{"docker"},
		Capabilities: []string{
			core.CapTest,
			core.CapRestore,
		},
		Fields: []core.Field{
			{
				Name: "mode", Label: "Mode", Type: core.FieldSelect, Default: "volume", Group: "Mode",
				Options: []core.FieldOption{
					{Value: "volume", Label: "Named volume (tar of its contents)"},
					{Value: "exec", Label: "Command inside a running container"},
				},
				Help: "Volume mode mounts the volume read-only into a helper container and tars it. Exec mode runs your command in an existing container.",
			},
			{
				Name: "volume", Label: "Volume", Type: core.FieldString, Required: true, Group: "Mode",
				Placeholder: "postgres_data", ShowIf: map[string]any{"mode": "volume"},
				Help: "Name of the Docker volume as shown by docker volume ls.",
			},
			{
				Name: "image", Label: "Helper image", Type: core.FieldString, Default: defaultImage,
				Group: "Advanced", Advanced: true, ShowIf: map[string]any{"mode": "volume"},
				Placeholder: defaultImage,
				Help:        "Small image that provides tar. It must already be pullable from this host.",
			},
			{
				Name: "container", Label: "Container", Type: core.FieldString, Required: true, Group: "Mode",
				Placeholder: "app-db-1", ShowIf: map[string]any{"mode": "exec"},
				Help: "Name or id of a running container.",
			},
			{
				Name: "command", Label: "Command", Type: core.FieldText, Required: true, Group: "Mode",
				ShowIf: map[string]any{"mode": "exec"}, Placeholder: "pg_dump -Fc -U postgres app",
				Help: "Runs through the container shell. It must write the backup to standard output and exit with code 0.",
			},
			{
				Name: "extension", Label: "File extension", Type: core.FieldString, Default: "dump",
				Group: "Mode", ShowIf: map[string]any{"mode": "exec"},
				Help: "Used in the artifact filename, for example dump, sql or tar.",
			},
			{
				Name: "exec_user", Label: "Run as user", Type: core.FieldString, Group: "Advanced",
				Advanced: true, ShowIf: map[string]any{"mode": "exec"}, Placeholder: "postgres",
			},
			{
				Name: "shell", Label: "Container shell", Type: core.FieldString, Default: "/bin/sh",
				Group: "Advanced", Advanced: true, ShowIf: map[string]any{"mode": "exec"},
			},
			{
				Name: "docker_host", Label: "Docker host", Type: core.FieldString, Group: "Advanced",
				Advanced: true, Placeholder: "unix:///var/run/docker.sock",
				Help: "Sets DOCKER_HOST for the docker calls. Leave empty to use the host default.",
			},
			{
				Name: "binary_path", Label: "Binary path", Type: core.FieldPath, Group: "Advanced",
				Advanced: true, Placeholder: "/usr/bin/docker",
				Help: "Full path to the docker binary, or the directory holding it. Leave empty to use PATH.",
			},
		},
	}
}

func (d *Driver) Validate(cfg core.Config) error {
	if err := core.ValidateRequired(d.Spec(), cfg); err != nil {
		return err
	}
	switch cfg.StringOr("mode", "volume") {
	case "volume":
		if !cfg.Has("volume") {
			return fmt.Errorf("a volume name is required in volume mode")
		}
	case "exec":
		if !cfg.Has("container") {
			return fmt.Errorf("a container is required in exec mode")
		}
		if !cfg.Has("command") {
			return fmt.Errorf("a command is required in exec mode")
		}
	default:
		return fmt.Errorf("mode must be volume or exec")
	}
	return nil
}

func (d *Driver) Test(ctx context.Context, cfg core.Config, log *slog.Logger) error {
	if err := d.Validate(cfg); err != nil {
		return err
	}
	bin, err := procstream.Lookup(cfg.String("binary_path"), "docker", "podman")
	if err != nil {
		return err
	}
	out, err := procstream.Output(ctx, log, procstream.Command{
		Name: bin, Args: []string{"version", "--format", "{{.Server.Version}}"},
		Env: env(cfg), Label: "docker", Quiet: true,
	})
	if err != nil {
		return fmt.Errorf("docker daemon not reachable: %w", err)
	}
	log.Info("docker daemon reachable", "version", strings.TrimSpace(string(out)))
	if cfg.StringOr("mode", "volume") == "volume" {
		return procstream.Run(ctx, log, procstream.Command{
			Name: bin, Args: []string{"volume", "inspect", "--format", "{{.Mountpoint}}", cfg.String("volume")},
			Env: env(cfg), Label: "docker", Quiet: true,
		})
	}
	state, err := procstream.Output(ctx, log, procstream.Command{
		Name: bin, Args: []string{"inspect", "--format", "{{.State.Running}}", cfg.String("container")},
		Env: env(cfg), Label: "docker", Quiet: true,
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(state)) != "true" {
		return fmt.Errorf("container %s is not running", cfg.String("container"))
	}
	return nil
}

func (d *Driver) Backup(ctx context.Context, cfg core.Config, log *slog.Logger) (*source.Stream, error) {
	if err := d.Validate(cfg); err != nil {
		return nil, err
	}
	bin, err := procstream.Lookup(cfg.String("binary_path"), "docker", "podman")
	if err != nil {
		return nil, err
	}
	var args []string
	ext := "tar"
	meta := map[string]string{}
	if cfg.StringOr("mode", "volume") == "volume" {
		vol := cfg.String("volume")
		args = []string{"run", "--rm", "--network", "none", "-v", vol + ":/data:ro",
			cfg.StringOr("image", defaultImage), "tar", "-C", "/data", "-cf", "-", "."}
		meta["volume"] = vol
		log.Info("archiving docker volume", "volume", vol, "image", cfg.StringOr("image", defaultImage))
	} else {
		ext = strings.TrimPrefix(cfg.StringOr("extension", "tar"), ".")
		args = []string{"exec"}
		if u := cfg.String("exec_user"); u != "" {
			args = append(args, "--user", u)
		}
		args = append(args, cfg.String("container"), cfg.StringOr("shell", "/bin/sh"), "-c", cfg.String("command"))
		meta["container"] = cfg.String("container")
		log.Info("running command in container", "container", cfg.String("container"))
	}
	p, err := procstream.Start(ctx, log, procstream.Command{
		Name: bin, Args: args, Env: env(cfg), Label: "docker",
	})
	if err != nil {
		return nil, err
	}
	return &source.Stream{Reader: p, Extension: ext, Meta: meta}, nil
}

func (d *Driver) Restore(ctx context.Context, cfg core.Config, r io.Reader, opts source.RestoreOptions, log *slog.Logger) error {
	if cfg.StringOr("mode", "volume") != "volume" {
		return fmt.Errorf("restore is only supported for docker volumes, not for exec sources")
	}
	bin, err := procstream.Lookup(cfg.String("binary_path"), "docker", "podman")
	if err != nil {
		return err
	}
	vol := cfg.String("volume")
	if opts.Params != nil && opts.Params.Has("volume") {
		vol = opts.Params.String("volume")
	}
	if vol == "" {
		return fmt.Errorf("no target volume for the restore")
	}
	script := "tar -C /data -xf -"
	if opts.Overwrite {
		script = "rm -rf /data/..?* /data/.[!.]* /data/* 2>/dev/null; " + script
		log.Info("clearing docker volume before restore", "volume", vol)
	}
	args := []string{"run", "--rm", "-i", "--network", "none", "-v", vol + ":/data",
		cfg.StringOr("image", defaultImage), "/bin/sh", "-c", script}
	log.Info("restoring docker volume", "volume", vol)
	return procstream.Run(ctx, log, procstream.Command{
		Name: bin, Args: args, Env: env(cfg), Stdin: r, Label: "docker",
	})
}

func env(cfg core.Config) []string {
	if h := cfg.String("docker_host"); h != "" {
		return []string{"DOCKER_HOST=" + h}
	}
	return nil
}

var (
	_ source.Driver   = (*Driver)(nil)
	_ source.Restorer = (*Driver)(nil)
)
