package store

import (
	"context"
	"errors"
	"testing"

	"github.com/arthurr0/backvault/internal/core"
)

func TestHostsCRUD(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	created, err := s.Hosts.Create(ctx, core.Host{
		Name: "db-01", Description: "database box", Address: "10.0.0.5", Port: 2222,
		User: "backup", Auth: core.HostAuthKey, PrivateKey: "enc:v1:secret", PublicKey: "ssh-ed25519 AAAA",
		HostKey: "SHA256:abc", Sudo: true, ConnectTimeout: 5, Tags: []string{"prod", "eu"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.CreatedAt.IsZero() {
		t.Fatalf("create returned %+v", created)
	}

	got, err := s.Hosts.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "db-01" || got.Address != "10.0.0.5" || got.Port != 2222 || got.User != "backup" {
		t.Fatalf("unexpected host %+v", got)
	}
	if got.Auth != core.HostAuthKey || got.PrivateKey != "enc:v1:secret" || got.PublicKey != "ssh-ed25519 AAAA" {
		t.Fatalf("auth fields not round tripped: %+v", got)
	}
	if !got.Sudo || got.ConnectTimeout != 5 || got.HostKey != "SHA256:abc" {
		t.Fatalf("options not round tripped: %+v", got)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "prod" {
		t.Fatalf("tags = %v", got.Tags)
	}

	byName, err := s.Hosts.GetByName(ctx, "db-01")
	if err != nil || byName.ID != created.ID {
		t.Fatalf("get by name: %v %+v", err, byName)
	}
	if _, err := s.Hosts.GetByName(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}

	if _, err := s.Hosts.Create(ctx, core.Host{Name: "db-01", Address: "1.2.3.4", User: "root"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict on duplicate name, got %v", err)
	}

	got.Description = "renamed"
	got.Address = "10.0.0.6"
	got.Auth = core.HostAuthPassword
	got.Password = "enc:v1:pw"
	if _, err := s.Hosts.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	updated, err := s.Hosts.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Address != "10.0.0.6" || updated.Auth != core.HostAuthPassword || updated.Password != "enc:v1:pw" {
		t.Fatalf("update not applied: %+v", updated)
	}

	if err := s.Hosts.SetTestResult(ctx, created.ID, true, "", "Linux 6.1 x86_64", []string{"tar", "pg_dump"}); err != nil {
		t.Fatal(err)
	}
	tested, err := s.Hosts.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tested.LastTestAt == nil || tested.LastTestOK == nil || !*tested.LastTestOK {
		t.Fatalf("test result not stored: %+v", tested)
	}
	if tested.LastSeenOS != "Linux 6.1 x86_64" || len(tested.Tools) != 2 {
		t.Fatalf("probe result not stored: %+v", tested)
	}

	if err := s.Hosts.Delete(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Hosts.Get(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found after delete, got %v", err)
	}
	if err := s.Hosts.Delete(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found on second delete, got %v", err)
	}
}

func TestHostsListFilters(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	for _, h := range []core.Host{
		{Name: "alpha", Address: "10.0.0.1", User: "root", Tags: []string{"prod"}},
		{Name: "beta", Address: "10.0.0.2", User: "root", Tags: []string{"staging"}},
		{Name: "gamma", Description: "alpha replica", Address: "10.0.0.3", User: "root", Tags: []string{"prod"}},
	} {
		if _, err := s.Hosts.Create(ctx, h); err != nil {
			t.Fatal(err)
		}
	}

	all, total, err := s.Hosts.List(ctx, HostFilter{})
	if err != nil || total != 3 || len(all) != 3 {
		t.Fatalf("list all: %v total=%d len=%d", err, total, len(all))
	}
	if all[0].Name != "alpha" || all[2].Name != "gamma" {
		t.Fatalf("expected order by name, got %s %s", all[0].Name, all[2].Name)
	}

	tagged, total, err := s.Hosts.List(ctx, HostFilter{Tag: "prod"})
	if err != nil || total != 2 || len(tagged) != 2 {
		t.Fatalf("tag filter: %v total=%d len=%d", err, total, len(tagged))
	}

	matched, total, err := s.Hosts.List(ctx, HostFilter{Q: "alpha"})
	if err != nil || total != 2 {
		t.Fatalf("q filter: %v total=%d", err, total)
	}
	if matched[0].Name != "alpha" || matched[1].Name != "gamma" {
		t.Fatalf("unexpected q results %+v", matched)
	}

	page, total, err := s.Hosts.List(ctx, HostFilter{Page: Page{Limit: 1, Offset: 1}})
	if err != nil || total != 3 || len(page) != 1 || page[0].Name != "beta" {
		t.Fatalf("pagination: %v total=%d %+v", err, total, page)
	}
}

func TestSourcesCarryHost(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	host, err := s.Hosts.Create(ctx, core.Host{Name: "web-01", Address: "10.0.0.9", User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	remote, err := s.Sources.Create(ctx, core.Source{Name: "remote files", Kind: "files", HostID: host.ID, Config: core.Config{"paths": "/etc"}})
	if err != nil {
		t.Fatal(err)
	}
	local, err := s.Sources.Create(ctx, core.Source{Name: "local files", Kind: "files", Config: core.Config{"paths": "/srv"}})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.Sources.Get(ctx, remote.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.HostID != host.ID || got.HostName != "web-01" {
		t.Fatalf("host not joined: %+v", got)
	}
	plain, err := s.Sources.Get(ctx, local.ID)
	if err != nil {
		t.Fatal(err)
	}
	if plain.HostID != "" || plain.HostName != "" {
		t.Fatalf("local source has a host: %+v", plain)
	}

	filtered, total, err := s.Sources.List(ctx, SourceFilter{HostID: host.ID})
	if err != nil || total != 1 || len(filtered) != 1 || filtered[0].ID != remote.ID {
		t.Fatalf("host filter: %v total=%d %+v", err, total, filtered)
	}
	if filtered[0].HostName != "web-01" {
		t.Fatalf("host name missing in list: %+v", filtered[0])
	}

	withCount, err := s.Hosts.Get(ctx, host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if withCount.SourceCount != 1 {
		t.Fatalf("source count = %d", withCount.SourceCount)
	}

	if err := s.Hosts.Delete(ctx, host.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict while sources reference the host, got %v", err)
	}

	got.HostID = ""
	if _, err := s.Sources.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	detached, err := s.Sources.Get(ctx, remote.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detached.HostID != "" || detached.HostName != "" {
		t.Fatalf("host not detached: %+v", detached)
	}
	if err := s.Hosts.Delete(ctx, host.ID); err != nil {
		t.Fatalf("delete after detaching: %v", err)
	}
}
