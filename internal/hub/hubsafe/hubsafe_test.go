package hubsafe

import (
	"context"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/secrets"
	"github.com/ly-solutions/harnessly/internal/secrets/secretstest"
)

func TestBrokerKeepsEachOwnerInTheirFolder(t *testing.T) {
	kv := secretstest.New()
	b := New(kv, "/harnessly/", []string{"shared/ci"})
	ctx := context.Background()
	if names, _, err := b.Do(ctx, "u1", hubproto.SecretsRequest{Op: hubproto.SecretsList}); err != nil || len(names) != 0 || names == nil {
		t.Fatalf("empty list: %v %v", names, err)
	}
	if _, _, err := b.Do(ctx, "u1", hubproto.SecretsRequest{Op: hubproto.SecretsWrite, Name: "gh-token", Data: map[string]string{"value": "v1"}}); err != nil {
		t.Fatal(err)
	}
	if kv.Data["harnessly/users/u1/gh-token"]["value"] != "v1" {
		t.Fatalf("stored at %v", kv.Data)
	}
	kv.Data["harnessly/users/u1/nested/x"] = map[string]string{"value": "n"}
	names, _, err := b.Do(ctx, "u1", hubproto.SecretsRequest{Op: hubproto.SecretsList})
	if err != nil || strings.Join(names, ",") != "gh-token" {
		t.Fatalf("list %v %v", names, err)
	}
	_, data, err := b.Do(ctx, "u1", hubproto.SecretsRequest{Op: hubproto.SecretsRead, Name: "gh-token"})
	if err != nil || data["value"] != "v1" {
		t.Fatalf("read %v %v", data, err)
	}
	if _, _, err := b.Do(ctx, "u2", hubproto.SecretsRequest{Op: hubproto.SecretsRead, Name: "gh-token"}); secrets.ReasonOf(err) != secrets.ReasonNotFound {
		t.Fatalf("another owner read it: %v", err)
	}
	if _, _, err := b.Do(ctx, "u1", hubproto.SecretsRequest{Op: hubproto.SecretsDelete, Name: "gh-token"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := kv.Data["harnessly/users/u1/gh-token"]; ok {
		t.Fatal("not deleted")
	}
}

func TestBrokerRefusesWhatLeavesTheFolder(t *testing.T) {
	kv := secretstest.New()
	kv.Data["harnessly/users/u2/gh-token"] = map[string]string{"value": "theirs"}
	kv.Data["shared/ci/deploy"] = map[string]string{"key": "ok"}
	kv.Data["other/x"] = map[string]string{"key": "no"}
	b := New(kv, "harnessly", []string{"shared/ci", "harnessly"})
	ctx := context.Background()
	for _, name := range []string{"", "../u2/gh-token", "a/b", "..", ".", "gh token", strings.Repeat("a", 300)} {
		for _, op := range []string{hubproto.SecretsRead, hubproto.SecretsWrite, hubproto.SecretsDelete} {
			req := hubproto.SecretsRequest{Op: op, Name: name, Data: map[string]string{"value": "x"}}
			if _, _, err := b.Do(ctx, "u1", req); secrets.ReasonOf(err) != secrets.ReasonBadName {
				t.Errorf("%s %q: %v", op, name, err)
			}
		}
	}
	for _, owner := range []string{"", "u1/../u2", "a/b", ".."} {
		if _, _, err := b.Do(ctx, owner, hubproto.SecretsRequest{Op: hubproto.SecretsList}); secrets.ReasonOf(err) != secrets.ReasonDenied {
			t.Errorf("owner %q: %v", owner, err)
		}
	}
	for _, p := range []string{"harnessly/users/u2/gh-token", "harnessly/users", "/harnessly/users/u2/gh-token/", "other/x", "shared/ci/../../other/x", "shared/cix/y"} {
		if _, _, err := b.Do(ctx, "u1", hubproto.SecretsRequest{Op: hubproto.SecretsRef, Path: p}); secrets.ReasonOf(err) != secrets.ReasonDenied {
			t.Errorf("ref %q: %v", p, err)
		}
	}
	if _, data, err := b.Do(ctx, "u1", hubproto.SecretsRequest{Op: hubproto.SecretsRef, Path: "shared/ci/deploy"}); err != nil || data["key"] != "ok" {
		t.Fatalf("ref: %v %v", data, err)
	}
	if _, _, err := New(kv, "harnessly", nil).Do(ctx, "u1", hubproto.SecretsRequest{Op: hubproto.SecretsRef, Path: "shared/ci/deploy"}); secrets.ReasonOf(err) != secrets.ReasonDenied {
		t.Fatalf("a hub without reference folders let one through: %v", err)
	}
	if !New(kv, "harnessly", []string{"*"}).RefAllowed("other/x") || New(kv, "harnessly", []string{"*"}).RefAllowed("harnessly/users/u2/x") {
		t.Fatal("* must reach any path but the persons' folders")
	}
}

func TestBrokerWritesOneValueOfBoundedSize(t *testing.T) {
	b := New(secretstest.New(), "harnessly", nil)
	ctx := context.Background()
	for name, data := range map[string]map[string]string{
		"none":      nil,
		"other key": {"token": "x"},
		"two keys":  {"value": "x", "extra": "y"},
	} {
		if _, _, err := b.Do(ctx, "u1", hubproto.SecretsRequest{Op: hubproto.SecretsWrite, Name: "n", Data: data}); secrets.ReasonOf(err) != secrets.ReasonEmpty {
			t.Errorf("%s: %v", name, err)
		}
	}
	big := strings.Repeat("x", secrets.MaxValue+1)
	if _, _, err := b.Do(ctx, "u1", hubproto.SecretsRequest{Op: hubproto.SecretsWrite, Name: "n", Data: map[string]string{"value": big}}); secrets.ReasonOf(err) != secrets.ReasonTooLarge {
		t.Fatalf("too large: %v", err)
	}
	if _, _, err := b.Do(ctx, "u1", hubproto.SecretsRequest{Op: "rotate"}); secrets.ReasonOf(err) != secrets.ReasonFailed {
		t.Fatalf("unknown op: %v", err)
	}
}
