package docker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/source"
	"github.com/arthurr0/backvault/internal/source/internal/remoteexec"
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
			core.CapRemote,
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
	r, err := remoteexec.Open(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer r.Close()
	bin, err := r.Tool(ctx, cfg.String("binary_path"), "docker", "podman")
	if err != nil {
		return err
	}
	out, err := r.Output(ctx, remoteexec.Command{
		Argv: []string{bin, "version", "--format", "{{.Server.Version}}"},
		Env:  env(cfg), Label: "docker", Quiet: true,
	})
	if err != nil {
		return fmt.Errorf("docker daemon not reachable: %w", err)
	}
	log.Info("docker daemon reachable", "version", strings.TrimSpace(string(out)), "where", where(r))
	if cfg.StringOr("mode", "volume") == "volume" {
		return r.Run(ctx, remoteexec.Command{
			Argv: []string{bin, "volume", "inspect", "--format", "{{.Mountpoint}}", cfg.String("volume")},
			Env:  env(cfg), Label: "docker", Quiet: true,
		})
	}
	state, err := r.Output(ctx, remoteexec.Command{
		Argv: []string{bin, "inspect", "--format", "{{.State.Running}}", cfg.String("container")},
		Env:  env(cfg), Label: "docker", Quiet: true,
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
	r, err := remoteexec.Open(ctx, cfg, log)
	if err != nil {
		return nil, err
	}
	bin, err := r.Tool(ctx, cfg.String("binary_path"), "docker", "podman")
	if err != nil {
		r.Close()
		return nil, err
	}
	ext := "tar"
	meta := map[string]string{}
	if r.Remote() {
		meta["host"] = r.HostLabel()
	}
	if cfg.StringOr("mode", "volume") == "volume" {
		meta["volume"] = cfg.String("volume")
		log.Info("archiving docker volume", "volume", cfg.String("volume"),
			"image", cfg.StringOr("image", defaultImage), "where", where(r))
	} else {
		ext = strings.TrimPrefix(cfg.StringOr("extension", "tar"), ".")
		meta["container"] = cfg.String("container")
		log.Info("running command in container", "container", cfg.String("container"), "where", where(r))
	}
	reader, err := r.Start(ctx, remoteexec.Command{
		Argv: backupArgv(bin, cfg), Env: env(cfg), Label: "docker",
	})
	if err != nil {
		r.Close()
		return nil, err
	}
	return &source.Stream{Reader: reader, Extension: ext, Meta: meta}, nil
}

func (d *Driver) Restore(ctx context.Context, cfg core.Config, src io.Reader, opts source.RestoreOptions, log *slog.Logger) error {
	if cfg.StringOr("mode", "volume") != "volume" {
		return fmt.Errorf("restore is only supported for docker volumes, not for exec sources")
	}
	vol := cfg.String("volume")
	if opts.Params != nil && opts.Params.Has("volume") {
		vol = opts.Params.String("volume")
	}
	if vol == "" {
		return fmt.Errorf("no target volume for the restore")
	}
	r, err := remoteexec.Open(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer r.Close()
	bin, err := r.Tool(ctx, cfg.String("binary_path"), "docker", "podman")
	if err != nil {
		return err
	}
	if opts.Overwrite {
		log.Info("clearing docker volume before restore", "volume", vol)
	}
	log.Info("restoring docker volume", "volume", vol, "where", where(r))
	return r.Run(ctx, remoteexec.Command{
		Argv: restoreArgv(bin, vol, opts.Overwrite, cfg), Env: env(cfg), Stdin: src, Label: "docker",
	})
}

func backupArgv(bin string, cfg core.Config) []string {
	if cfg.StringOr("mode", "volume") == "volume" {
		return []string{bin, "run", "--rm", "--network", "none", "-v", cfg.String("volume") + ":/data:ro",
			cfg.StringOr("image", defaultImage), "tar", "-C", "/data", "-cf", "-", "."}
	}
	argv := []string{bin, "exec"}
	if u := cfg.String("exec_user"); u != "" {
		argv = append(argv, "--user", u)
	}
	return append(argv, cfg.String("container"), cfg.StringOr("shell", "/bin/sh"), "-c", cfg.String("command"))
}

func restoreArgv(bin, vol string, overwrite bool, cfg core.Config) []string {
	script := "tar -C /data -xf -"
	if overwrite {
		script = "rm -rf /data/..?* /data/.[!.]* /data/* 2>/dev/null; " + script
	}
	return []string{bin, "run", "--rm", "-i", "--network", "none", "-v", vol + ":/data",
		cfg.StringOr("image", defaultImage), "/bin/sh", "-c", script}
}

func where(r *remoteexec.Runner) string {
	if r.Remote() {
		return "host " + r.HostLabel()
	}
	return "this server"
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
