package vaultkv

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/secrets"
)

// The client against a real server: OpenBao or HashiCorp Vault in dev mode. CI starts both (see
// .github/workflows/ci.yml, job vault-compat); locally:
//
//	docker run --rm -d -p 8200:8200 openbao/openbao server -dev -dev-root-token-id=dev-root
//	VAULTKV_TEST_ADDR=http://127.0.0.1:8200 VAULTKV_TEST_ROOT_TOKEN=dev-root go test ./internal/secrets/vaultkv -run Real
//
// The test enables what it needs (a KV v1 mount, AppRole) under names of its own and leaves them there: it is meant
// for a throwaway dev server only.

func realServer(t *testing.T) (addr, root string) {
	addr, root = os.Getenv("VAULTKV_TEST_ADDR"), os.Getenv("VAULTKV_TEST_ROOT_TOKEN")
	if addr == "" || root == "" {
		t.Skip("VAULTKV_TEST_ADDR and VAULTKV_TEST_ROOT_TOKEN name a dev server")
	}
	return addr, root
}

// admin calls the server as root; a 400 for "already in use" is fine.
func admin(t *testing.T, addr, root, method, path string, body any) map[string]any {
	t.Helper()
	var rd *bytes.Reader
	raw, _ := json.Marshal(body)
	rd = bytes.NewReader(raw)
	req, _ := http.NewRequest(method, addr+"/v1/"+path, rd)
	req.Header.Set("X-Vault-Token", root)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode >= 300 && !strings.Contains(strings.ToLower(jsonOf(out)), "already in use") &&
		!strings.Contains(strings.ToLower(jsonOf(out)), "path is already") {
		t.Fatalf("%s %s: %d %v", method, path, resp.StatusCode, out)
	}
	return out
}

func jsonOf(v any) string { raw, _ := json.Marshal(v); return string(raw) }

func roundTrip(t *testing.T, c *Client) {
	t.Helper()
	ctx := context.Background()
	base := "harnessly-test/" + strings.ReplaceAll(t.Name(), "/", "-")
	if err := c.Write(ctx, base+"/gh-token", map[string]string{"value": "real-value-1"}); err != nil {
		t.Fatal(err)
	}
	got, err := c.Read(ctx, base+"/gh-token")
	if err != nil || got["value"] != "real-value-1" {
		t.Fatalf("read: %v %v", got, err)
	}
	keys, err := c.List(ctx, base)
	if err != nil || len(keys) != 1 || keys[0] != "gh-token" {
		t.Fatalf("list: %v %v", keys, err)
	}
	if err := c.Delete(ctx, base+"/gh-token"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Read(ctx, base+"/gh-token"); secrets.ReasonOf(err) != secrets.ReasonNotFound {
		t.Fatalf("read after delete: %v", err)
	}
	if keys, err := c.List(ctx, base); err != nil || len(keys) != 0 {
		t.Fatalf("list after delete: %v %v", keys, err)
	}
}

func TestRealKV2WithToken(t *testing.T) {
	addr, root := realServer(t)
	c, err := New(Config{Addr: addr, Mount: "secret", Auth: TokenFile{Path: writeFile(t, "tok", root)}})
	if err != nil {
		t.Fatal(err)
	}
	roundTrip(t, c)
	if c.kvVersion(context.Background()) != 2 {
		t.Fatal("dev mount secret/ is KV v2")
	}
}

func TestRealKV1WithToken(t *testing.T) {
	addr, root := realServer(t)
	admin(t, addr, root, http.MethodPost, "sys/mounts/kv1-test", map[string]any{"type": "kv", "options": map[string]string{"version": "1"}})
	c, err := New(Config{Addr: addr, Mount: "kv1-test", Auth: TokenFile{Path: writeFile(t, "tok", root)}})
	if err != nil {
		t.Fatal(err)
	}
	roundTrip(t, c)
	if c.kvVersion(context.Background()) != 1 {
		t.Fatal("kv1-test is KV v1")
	}
}

func TestRealAppRoleWithNarrowPolicy(t *testing.T) {
	addr, root := realServer(t)
	admin(t, addr, root, http.MethodPost, "sys/auth/approle-test", map[string]any{"type": "approle"})
	policy := `
path "secret/data/harnessly-test/*"     { capabilities = ["create", "read", "update", "delete"] }
path "secret/metadata/harnessly-test/*" { capabilities = ["list", "read", "delete"] }
`
	admin(t, addr, root, http.MethodPut, "sys/policies/acl/harnessly-test", map[string]any{"policy": policy})
	admin(t, addr, root, http.MethodPost, "auth/approle-test/role/harnessly", map[string]any{"token_policies": []string{"harnessly-test"}, "token_ttl": "15m"})
	roleID := admin(t, addr, root, http.MethodGet, "auth/approle-test/role/harnessly/role-id", nil)["data"].(map[string]any)["role_id"].(string)
	secretID := admin(t, addr, root, http.MethodPost, "auth/approle-test/role/harnessly/secret-id", map[string]any{})["data"].(map[string]any)["secret_id"].(string)
	c, err := New(Config{Addr: addr, Mount: "secret", Auth: AppRole{Mount: "approle-test", RoleID: roleID, SecretIDFile: writeFile(t, "sid", secretID)}})
	if err != nil {
		t.Fatal(err)
	}
	// The policy does not allow sys/internal/ui/mounts for this mount on every server: the client must not need it.
	roundTrip(t, c)
	if _, err := c.Read(context.Background(), "other/thing"); secrets.ReasonOf(err) != secrets.ReasonDenied {
		t.Fatalf("outside the policy: %v", err)
	}
}
