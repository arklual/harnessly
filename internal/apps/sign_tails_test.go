package apps

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/egress"
)

func signedPublicPackage(t *testing.T) (raw []byte, sig Signature, pub string) {
	t.Helper()
	dir := publicPackage(t, "acme.demo", "1.0.0", nil)
	raw, _, err := Pack(dir)
	if err != nil {
		t.Fatal(err)
	}
	priv, pub := keyPair(t)
	sig, err = Sign(raw, "Банк", priv)
	if err != nil {
		t.Fatal(err)
	}
	return raw, sig, pub
}

// A .hlapp published to the team's catalog straight from the file: the signature goes with it, and a machine that requires
// signatures takes the app from the catalog.
func TestPublishArchiveToTheTeamCarriesTheSignature(t *testing.T) {
	raw, sig, pub := signedPublicPackage(t)
	withNetwork(t, DeployNetwork{RequireSigned: true, Publishers: []egress.Publisher{{Name: "Банк", Key: pub}}})
	owner := newRegistry(t)
	if err := owner.SetTeam("sp1", RoleOwner, teamNow); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.PublishArchive(raw, nil, ChannelStable, PublishOptions{}); !errors.Is(err, ErrUnsigned) {
		t.Fatalf("unsigned: %v", err)
	}
	pv, _, v, err := owner.PublishArchive(raw, &sig, ChannelStable, PublishOptions{})
	if err != nil || !v.Signed || pv.Version != "1.0.0" {
		t.Fatalf("publish: %+v %v", v, err)
	}
	e, err := owner.PublishToTeam("acme.demo", "1.0.0", ChannelStable, "ann@team.test", TemplateVersion, teamNow)
	if err != nil || e.Sum != sig.SHA256 {
		t.Fatalf("the catalog holds the signed archive: %+v %v", e, err)
	}
	if sigOf(owner.catalogArchive("acme.demo", "1.0.0")) == nil {
		t.Fatal("the catalog keeps the signature")
	}
	if _, err := owner.InstallForTeam("acme.demo", ChannelStable, TeamInstallOptions{HostVersion: TemplateVersion, By: "ann@team.test", Now: teamNow}); err != nil {
		t.Fatalf("install for the team: %v", err)
	}
}

// A pre-release channel rewrites the version, which the signature does not cover: no signature is kept.
func TestPublishArchiveBetaIsUnsigned(t *testing.T) {
	raw, sig, _ := signedPublicPackage(t)
	r := newRegistry(t)
	pv, _, _, err := r.PublishArchive(raw, &sig, ChannelBeta, PublishOptions{})
	if err != nil || pv.Version == "1.0.0" {
		t.Fatalf("beta: %+v %v", pv, err)
	}
	if r.versionSig("acme.demo", pv.Version) != nil {
		t.Fatal("a rewritten version is not signed")
	}
	// A changed archive is refused whatever the channel.
	if _, _, _, err := r.PublishArchive(append(append([]byte{}, raw...), 0), &sig, ChannelStable, PublishOptions{}); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("changed: %v", err)
	}
}

// The public catalog carries the publisher's signature; where signatures are required the signed app installs from it and
// an entry that lost its signature does not.
func TestPublicCatalogCarriesTheSignature(t *testing.T) {
	raw, sig, pub := signedPublicPackage(t)
	author := newRegistry(t)
	if _, _, _, err := author.PublishArchive(raw, &sig, ChannelStable, PublishOptions{}); err != nil {
		t.Fatal(err)
	}
	e, err := author.PublishPublic("acme.demo", "1.0.0", "ann@acme.test", teamNow)
	if err != nil || e.Sum != sig.SHA256 {
		t.Fatalf("public entry is the signed archive: %+v %v", e, err)
	}
	snap, err := author.PublicSnapshot()
	if err != nil || snap.Entries[0].Signature == nil {
		t.Fatalf("snapshot carries the signature: %v", err)
	}
	withNetwork(t, DeployNetwork{RequireSigned: true, Publishers: []egress.Publisher{{Name: "Банк", Key: pub}}})

	reader := newRegistry(t)
	if problems, err := reader.ApplyPublic(snap, TemplateVersion, teamNow); err != nil || len(problems) != 0 {
		t.Fatalf("apply: %v %v", err, problems)
	}
	if _, _, err := reader.InstallFromPublic("acme.demo", PublicInstallOptions{HostVersion: TemplateVersion, Sum: e.Sum}); err != nil {
		t.Fatalf("signed app installs from the public catalog: %v", err)
	}

	snap.Entries[0].Signature = nil
	bare := newRegistry(t)
	if _, err := bare.ApplyPublic(snap, TemplateVersion, teamNow); err != nil {
		t.Fatal(err)
	}
	if _, _, err := bare.InstallFromPublic("acme.demo", PublicInstallOptions{HostVersion: TemplateVersion, Sum: e.Sum}); !errors.Is(err, ErrUnsigned) {
		t.Fatalf("an entry without a signature is refused: %v", err)
	}
}

// What the checks looked at is what is published: a kept archive that differs from the published folder is not used.
func TestPublishedArchiveMustMatchTheFolder(t *testing.T) {
	raw, sig, _ := signedPublicPackage(t)
	r := newRegistry(t)
	pv, _, _, err := r.PublishArchive(raw, &sig, ChannelStable, PublishOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got, sum, err := r.publishedArchive("acme.demo", "1.0.0", pv.Dir); err != nil || sum != sig.SHA256 || ArchiveSum(got) != sig.SHA256 {
		t.Fatalf("the folder is the archive: %v", err)
	}
	// The folder changed after publishing (or a kept archive of another folder stands beside it): the pack of the folder.
	if err := os.WriteFile(filepath.Join(pv.Dir, "extra.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, sum, err := r.publishedArchive("acme.demo", "1.0.0", pv.Dir); err != nil || sum == sig.SHA256 {
		t.Fatalf("a different folder is not published as the signed archive: %v", err)
	}
}
