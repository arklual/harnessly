package apps

import (
	"errors"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/egress"
)

// requireFor sets a deployment that requires signatures from one publisher, and returns that publisher's key pair.
func requireFor(t *testing.T) (priv, pub string) {
	t.Helper()
	priv, pub = keyPair(t)
	withNetwork(t, DeployNetwork{RequireSigned: true, Publishers: []egress.Publisher{{Name: "Банк", Key: pub}}})
	return priv, pub
}

func signedArchive(t *testing.T, id, version, priv string) ([]byte, Signature) {
	t.Helper()
	raw, _, err := Pack(publicPackage(t, id, version, nil))
	if err != nil {
		t.Fatal(err)
	}
	sig, err := Sign(raw, "Банк", priv)
	if err != nil {
		t.Fatal(err)
	}
	return raw, sig
}

func wantUnsigned(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, ErrUnsigned) || !strings.Contains(err.Error(), "подпис") {
		t.Fatalf("%s must be refused in words, got %v", what, err)
	}
}

// Path 1: a folder. Refused where signatures are required; a development install is the one exception.
func TestRequiredSignatureFolderInstall(t *testing.T) {
	requireFor(t)
	r := newRegistry(t)
	dir := publicPackage(t, "acme.demo", "1.0.0", nil)
	_, _, err := r.Install(dir, InstallOptions{HostVersion: TemplateVersion})
	wantUnsigned(t, "a folder install", err)
	if list, _ := r.List(); len(list) != 0 {
		t.Fatalf("nothing installed: %+v", list)
	}
	if _, _, err := r.Install(dir, InstallOptions{HostVersion: TemplateVersion, Dev: true}); err != nil {
		t.Fatalf("the explicit dev install is the exception: %v", err)
	}
}

// Path 2: a channel (install --channel, sync). Only a version published from a signed .hlapp installs.
func TestRequiredSignatureChannelInstallAndSync(t *testing.T) {
	priv, _ := requireFor(t)
	r := newRegistry(t)
	opt := InstallOptions{HostVersion: TemplateVersion}

	// Published as a folder: unsigned.
	publishStable(t, r, publicPackage(t, "acme.demo", "1.0.0", nil))
	_, _, err := r.InstallChannel("acme.demo", ChannelStable, opt)
	wantUnsigned(t, "a channel version published from a folder", err)

	// Published from a signed file: installs, and following the channel takes a signed update but not an unsigned one.
	raw, sig := signedArchive(t, "acme.demo", "1.1.0", priv)
	if _, _, _, err := r.PublishArchive(raw, &sig, ChannelStable, PublishOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.InstallChannel("acme.demo", ChannelStable, opt); err != nil {
		t.Fatalf("signed channel version: %v", err)
	}
	publishStable(t, r, publicPackage(t, "acme.demo", "1.2.0", nil))
	res, err := r.SyncChannels(SyncOptions{HostVersion: TemplateVersion, Confirm: []string{"*"}})
	if err == nil && len(res) > 0 && res[0].Applied {
		t.Fatalf("an unsigned update must not be applied: %+v", res)
	}
	if list, _ := r.List(); len(list) != 1 || list[0].Version != "1.1.0" {
		t.Fatalf("still on the signed version: %+v", list)
	}
}

// Path 3: the team's catalog, also when the host version is left empty.
func TestRequiredSignatureTeamCatalog(t *testing.T) {
	priv, _ := requireFor(t)
	owner := newRegistry(t)
	if err := owner.SetTeam("sp1", RoleOwner, teamNow); err != nil {
		t.Fatal(err)
	}
	// An unsigned entry came to the catalog before the rule (or by a folder publish).
	pv := publishStable(t, owner, publicPackage(t, "acme.demo", "1.0.0", nil))
	if _, err := owner.PublishToTeam("acme.demo", pv.Version, ChannelStable, "ann@team.test", TemplateVersion, teamNow); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{TemplateVersion, ""} {
		_, err := owner.InstallForTeam("acme.demo", ChannelStable, TeamInstallOptions{HostVersion: host, By: "ann@team.test", Now: teamNow})
		wantUnsigned(t, "an unsigned catalog entry (host "+host+")", err)
	}
	// The signed one goes through.
	raw, sig := signedArchive(t, "acme.signed", "1.0.0", priv)
	if _, _, _, err := owner.PublishArchive(raw, &sig, ChannelStable, PublishOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.PublishToTeam("acme.signed", "1.0.0", ChannelStable, "ann@team.test", TemplateVersion, teamNow); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.InstallForTeam("acme.signed", ChannelStable, TeamInstallOptions{HostVersion: "", By: "ann@team.test", Now: teamNow}); err != nil {
		t.Fatalf("signed entry: %v", err)
	}
}

// Path 4: the public catalog, also when the host version is left empty.
func TestRequiredSignaturePublicCatalog(t *testing.T) {
	requireFor(t)
	author := newRegistry(t)
	pv := publishStable(t, author, publicPackage(t, "acme.demo", "1.0.0", nil))
	e, err := author.PublishPublic("acme.demo", pv.Version, "ann@acme.test", teamNow)
	if err != nil {
		t.Fatal(err)
	}
	snap, _ := author.PublicSnapshot()
	reader := newRegistry(t)
	if _, err := reader.ApplyPublic(snap, TemplateVersion, teamNow); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{TemplateVersion, ""} {
		_, _, err := reader.InstallFromPublic("acme.demo", PublicInstallOptions{HostVersion: host, Sum: e.Sum})
		wantUnsigned(t, "an unsigned public entry (host "+host+")", err)
	}
}

// Path 5: the environment's apps are folders of the hub's list and carry no signature.
func TestRequiredSignatureEnvironmentApp(t *testing.T) {
	requireFor(t)
	r := newRegistry(t)
	dir := publicPackage(t, "acme.demo", "1.0.0", nil)
	_, _, err := r.InstallForEnv(dir, EnvInstallOptions{Link: "l", ItemID: "i", HostVersion: TemplateVersion, Now: teamNow})
	wantUnsigned(t, "an environment app", err)
}
