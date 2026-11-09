package apps

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/egress"
)

func keyPair(t *testing.T) (private, public string) {
	t.Helper()
	priv, pub, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return priv, pub
}

func signedPackage(t *testing.T, id, version string) (dir string, raw []byte, priv, pub string, sig Signature) {
	t.Helper()
	dir = versioned(t, id, version)
	raw, _, err := Pack(dir)
	if err != nil {
		t.Fatal(err)
	}
	priv, pub = keyPair(t)
	sig, err = Sign(raw, "Банк", priv)
	if err != nil {
		t.Fatal(err)
	}
	return dir, raw, priv, pub, sig
}

func TestSignVerifyAndTheWordsOfARefusal(t *testing.T) {
	_, raw, priv, pub, sig := signedPackage(t, "acme.demo", "1.0.0")
	if got, err := PublicOf(priv); err != nil || got != pub {
		t.Fatalf("public of private: %v", err)
	}
	trusting := &egress.Policy{RequireSigned: true, Publishers: []egress.Publisher{{Name: "Банк (ИБ)", Key: pub}}}

	v, err := VerifyArchive(raw, &sig, trusting)
	if err != nil || !v.Signed || v.Publisher != "Банк (ИБ)" {
		t.Fatalf("trusted publisher: %+v %v", v, err)
	}

	// Required, but the package has no signature.
	if _, err := VerifyArchive(raw, nil, trusting); !errors.Is(err, ErrUnsigned) || !strings.Contains(err.Error(), "не подписан") || !strings.Contains(err.Error(), "app pack --sign") {
		t.Fatalf("unsigned: %v", err)
	}
	// Signed by a key the deployment does not list.
	_, otherPub := keyPair(t)
	other := &egress.Policy{RequireSigned: true, Publishers: []egress.Publisher{{Name: "Другой", Key: otherPub}}}
	if _, err := VerifyArchive(raw, &sig, other); !errors.Is(err, ErrUntrustedKey) || !strings.Contains(err.Error(), "«Банк»") || !strings.Contains(err.Error(), KeyID(pub)) {
		t.Fatalf("wrong publisher: %v", err)
	}
	// Changed after signing, always refused, required or not.
	changed := append(append([]byte{}, raw...), 0)
	for _, p := range []*egress.Policy{trusting, nil, {}} {
		if _, err := VerifyArchive(changed, &sig, p); !errors.Is(err, ErrBadSignature) || !strings.Contains(err.Error(), "изменили") {
			t.Fatalf("changed package: %v", err)
		}
	}
	// A signature with the sum rewritten still does not fit: the signed bytes differ.
	forged := sig
	forged.SHA256 = ArchiveSum(changed)
	if _, err := VerifyArchive(changed, &forged, nil); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("forged sum: %v", err)
	}
	// Not required: unsigned and unknown-key packages install as they are.
	if v, err := VerifyArchive(raw, nil, &egress.Policy{}); err != nil || v.Signed {
		t.Fatalf("open deployment, unsigned: %+v %v", v, err)
	}
	if v, err := VerifyArchive(raw, &sig, &egress.Policy{Publishers: other.Publishers}); err != nil || v.Signed || v.Publisher != "Банк" {
		t.Fatalf("open deployment, unknown key: %+v %v", v, err)
	}
	// The file round-trips.
	back, err := ParseSignature(sig.Marshal())
	if err != nil || *back != sig {
		t.Fatalf("round trip: %v", err)
	}
	if _, err := ParseSignature([]byte(`{"alg":"rsa"}`)); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("garbage: %v", err)
	}
	if _, err := Sign(raw, "", priv); err == nil {
		t.Error("a signature names its publisher")
	}
	if _, err := Sign(raw, "x", "not a key"); err == nil {
		t.Error("a bad key is told")
	}
}

func TestInstallArchiveChecksTheSignatureUnderThePolicy(t *testing.T) {
	_, raw, _, pub, sig := signedPackage(t, "acme.demo", "1.0.0")
	opt := InstallOptions{HostVersion: TemplateVersion}

	withNetwork(t, DeployNetwork{RequireSigned: true, Publishers: []egress.Publisher{{Name: "Банк", Key: pub}}})
	r := newRegistry(t)
	if _, _, _, err := r.InstallArchive(raw, nil, opt); !errors.Is(err, ErrUnsigned) {
		t.Fatalf("unsigned: %v", err)
	}
	if list, _ := r.List(); len(list) != 0 {
		t.Fatalf("a refused package is not installed: %+v", list)
	}
	got, _, v, err := r.InstallArchive(raw, &sig, opt)
	if err != nil || got.ID != "acme.demo" || !v.Signed {
		t.Fatalf("signed: %+v %v", got, err)
	}
}

func TestTheTeamCatalogCarriesTheSignatureOn(t *testing.T) {
	dir, raw, _, pub, sig := signedPackage(t, "acme.demo", "1.0.0")
	withNetwork(t, DeployNetwork{RequireSigned: true, Publishers: []egress.Publisher{{Name: "Банк", Key: pub}}})
	owner := newRegistry(t)
	if err := owner.SetTeam("sp1", RoleOwner, teamNow); err != nil {
		t.Fatal(err)
	}
	_ = dir
	if _, _, _, err := owner.PublishArchive(raw, &sig, ChannelStable, PublishOptions{}); err != nil {
		t.Fatal(err)
	}
	e, err := owner.PublishToTeam("acme.demo", "1.0.0", ChannelStable, "ann@team.test", TemplateVersion, teamNow)
	if err != nil || e.Sum != sig.SHA256 {
		t.Fatalf("the catalog holds the signed archive: %+v %v", e, err)
	}
	if _, err := owner.InstallForTeam("acme.demo", ChannelStable, TeamInstallOptions{HostVersion: TemplateVersion, By: "ann@team.test", Now: teamNow.Add(time.Minute)}); err != nil {
		t.Fatalf("install for the team: %v", err)
	}
	snap, err := owner.Snapshot()
	if err != nil || snap.Catalog[0].Signature == nil || snap.Installs[0].Signature == nil {
		t.Fatalf("the snapshot carries the signature: %v", err)
	}
	// A member of a deployment that requires signatures gets the app.
	snap.Role = RoleMember
	member := newRegistry(t)
	if problems, err := member.ApplyTeam(snap, TemplateVersion, teamNow.Add(2*time.Minute)); err != nil || len(problems) > 0 {
		t.Fatalf("member: %v %v", err, problems)
	}
	if list, _ := member.List(); len(list) != 1 {
		t.Fatalf("the member has the app: %+v", list)
	}
	// The same snapshot without the signatures: the app does not come, the others' machines do not take unsigned code.
	for i := range snap.Catalog {
		snap.Catalog[i].Signature, snap.Catalog[i].PreviousSignature = nil, nil
	}
	for i := range snap.Installs {
		snap.Installs[i].Signature = nil
	}
	bare := newRegistry(t)
	problems, err := bare.ApplyTeam(snap, TemplateVersion, teamNow.Add(2*time.Minute))
	list, _ := bare.List()
	if len(list) != 0 || (err == nil && len(problems) == 0) {
		t.Fatalf("unsigned snapshot must not install: list=%+v err=%v problems=%v", list, err, problems)
	}
}
