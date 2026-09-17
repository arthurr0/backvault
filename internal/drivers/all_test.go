package drivers

import (
	"regexp"
	"strings"
	"testing"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/dest"
	"github.com/arthurr0/backvault/internal/notify"
	"github.com/arthurr0/backvault/internal/source"
)

var snakeCase = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func TestAllSourcesRegistered(t *testing.T) {
	Register()
	want := []string{"postgres", "mysql", "mongodb", "redis", "sqlite", "files", "ssh", "docker", "command", "push"}
	for _, kind := range want {
		if _, ok := source.Get(kind); !ok {
			t.Errorf("source driver %q is not registered", kind)
		}
	}
	if len(source.All()) != len(want) {
		t.Errorf("registered %d source drivers, want %d", len(source.All()), len(want))
	}
}

func TestAllDestinationsRegistered(t *testing.T) {
	Register()
	want := []string{"local", "s3", "sftp", "webdav"}
	for _, kind := range want {
		if _, ok := dest.Get(kind); !ok {
			t.Errorf("destination driver %q is not registered", kind)
		}
	}
	if len(dest.All()) != len(want) {
		t.Errorf("registered %d destination drivers, want %d", len(dest.All()), len(want))
	}
}

func TestAllNotifiersRegistered(t *testing.T) {
	Register()
	want := []string{"email", "webhook", "slack", "discord", "telegram", "ntfy"}
	for _, kind := range want {
		if _, ok := notify.Get(kind); !ok {
			t.Errorf("notifier %q is not registered", kind)
		}
	}
	if len(notify.All()) != len(want) {
		t.Errorf("registered %d notifiers, want %d", len(notify.All()), len(want))
	}
}

func TestSpecsAreWellFormed(t *testing.T) {
	Register()
	specs := append(append(Sources(), Destinations()...), Notifiers()...)
	if len(specs) != 20 {
		t.Fatalf("expected 20 driver specs, got %d", len(specs))
	}
	for _, spec := range specs {
		t.Run(spec.Kind, func(t *testing.T) {
			if spec.Label == "" || spec.Description == "" || spec.Icon == "" || spec.Category == "" {
				t.Errorf("spec %s is missing label, description, icon or category", spec.Kind)
			}
			if !spec.Has(core.CapTest) {
				t.Errorf("spec %s does not declare the test capability", spec.Kind)
			}
			if strings.Contains(spec.Description, "!") {
				t.Errorf("spec %s description uses an exclamation mark", spec.Kind)
			}
			names := map[string]bool{}
			for _, f := range spec.Fields {
				if !snakeCase.MatchString(f.Name) {
					t.Errorf("field %q in %s is not snake_case", f.Name, spec.Kind)
				}
				if names[f.Name] {
					t.Errorf("field %q in %s is declared twice", f.Name, spec.Kind)
				}
				names[f.Name] = true
				if f.Label == "" {
					t.Errorf("field %q in %s has no label", f.Name, spec.Kind)
				}
				if f.Group == "" {
					t.Errorf("field %q in %s has no group", f.Name, spec.Kind)
				}
				if f.Type == core.FieldSelect && len(f.Options) == 0 {
					t.Errorf("select field %q in %s has no options", f.Name, spec.Kind)
				}
				if f.Type != core.FieldSelect && len(f.Options) > 0 {
					t.Errorf("field %q in %s has options but is not a select", f.Name, spec.Kind)
				}
				if f.Secret && f.Type != core.FieldSecret && f.Type != core.FieldText {
					t.Errorf("secret field %q in %s should be a secret or text field", f.Name, spec.Kind)
				}
			}
			for _, f := range spec.Fields {
				for key := range f.ShowIf {
					if !names[key] {
						t.Errorf("field %q in %s has showIf on unknown field %q", f.Name, spec.Kind, key)
					}
				}
			}
			if f := spec.Fields; len(f) == 0 && spec.Kind != "push" {
				t.Errorf("spec %s has no fields", spec.Kind)
			}
		})
	}
}

func TestSourceCapabilitiesMatchRestorers(t *testing.T) {
	Register()
	for _, d := range source.All() {
		_, isRestorer := d.(source.Restorer)
		declared := d.Spec().Has(core.CapRestore)
		if isRestorer != declared {
			t.Errorf("source %s: restorer implemented=%v, capability declared=%v", d.Spec().Kind, isRestorer, declared)
		}
	}
}

func TestDestinationsDeclareBrowse(t *testing.T) {
	Register()
	for _, d := range dest.All() {
		if !d.Spec().Has(core.CapBrowse) {
			t.Errorf("destination %s does not declare the browse capability", d.Spec().Kind)
		}
	}
}

func TestPushDeclaresIngest(t *testing.T) {
	Register()
	d, ok := source.Get("push")
	if !ok {
		t.Fatal("push source is not registered")
	}
	if !d.Spec().Has(core.CapIngest) {
		t.Error("push source does not declare the ingest capability")
	}
	if _, err := d.Backup(t.Context(), core.Config{}, testLogger()); err == nil {
		t.Error("push source Backup should return an error")
	}
	if err := d.Test(t.Context(), core.Config{}, testLogger()); err != nil {
		t.Errorf("push source Test should succeed, got %v", err)
	}
}

func TestToolsReportsKnownBinaries(t *testing.T) {
	Register()
	tools := Tools()
	if len(tools) == 0 {
		t.Fatal("no tools reported")
	}
	found := map[string]bool{}
	for _, tool := range tools {
		found[tool.Name] = true
		if len(tool.UsedBy) == 0 {
			t.Errorf("tool %s reports no users", tool.Name)
		}
	}
	for _, want := range []string{"pg_dump", "mysqldump", "mongodump", "redis-cli", "sqlite3", "docker"} {
		if !found[want] {
			t.Errorf("tool %s is not reported", want)
		}
	}
}
