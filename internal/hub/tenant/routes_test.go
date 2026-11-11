package tenant

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// registered reads the hub's source and returns every route it registers on a router, as "<dir> <pattern>", and
// whether the handler is made by a Guard.
func registered(t *testing.T) map[string]bool {
	t.Helper()
	root := filepath.Join("..", "..", "..")
	found := map[string]bool{}
	fset := token.NewFileSet()
	for _, dir := range []string{"internal/hub", "cmd/hub"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, filepath.Dir(path))
			if err != nil {
				return err
			}
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) != 2 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || (sel.Sel.Name != "Handle" && sel.Sel.Name != "HandleFunc") {
					return true
				}
				if p, ok := pattern(call.Args[0]); ok {
					found[filepath.ToSlash(rel)+" "+p] = viaGuard(call.Args[1])
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return found
}

// pattern is a route's pattern as the source writes it: string literals as they are, a name as <name>.
func pattern(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		return s, err == nil
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		l, ok := pattern(v.X)
		if !ok {
			return "", false
		}
		r, ok := pattern(v.Y)
		return l + r, ok
	case *ast.Ident:
		return "<" + v.Name + ">", true
	case *ast.SelectorExpr:
		if x, ok := v.X.(*ast.Ident); ok {
			return "<" + x.Name + "." + v.Sel.Name + ">", true
		}
	}
	return "", false
}

// viaGuard says whether a handler is written as <guard>.Space(tenant.<Level>, h), <guard>.Machine(tenant.<Level>, h),
// <guard>.Org(h), or <service>.OrgGate(h), the organizations' wrapper of Guard.Org.
func viaGuard(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch {
	case len(call.Args) == 1 && (sel.Sel.Name == "Org" || sel.Sel.Name == "OrgGate"):
		return true
	case len(call.Args) != 2 || (sel.Sel.Name != "Space" && sel.Sel.Name != "Machine"):
		return false
	}
	lvl, ok := call.Args[0].(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := lvl.X.(*ast.Ident)
	return ok && pkg.Name == "tenant" && (lvl.Sel.Name == "Member" || lvl.Sel.Name == "Owner" || lvl.Sel.Name == "Viewer")
}

// tenantParams are the path wildcards that carry a tenant's id, with the source a route that has one must name.
var tenantParams = map[string]Source{
	"{spaceId}": Space, "{org}": Org, "{orgId}": Org, "{machineId}": Machine,
}

func TestEveryRouteIsInTheInventory(t *testing.T) {
	reg := registered(t)
	// The reader of the source is checked against routes known to be there, so that it cannot go blind quietly.
	for _, known := range []string{
		"internal/hub/sshinstall GET /api/spaces/{spaceId}/machines/ssh",
		"internal/hub/orgs GET /api/orgs/{org}",
		"internal/hub/machines GET /api/machines/{machineId}",
		"internal/hub/docs GET <p>/{slug}",
	} {
		if _, ok := reg[known]; !ok {
			t.Fatalf("the reader of the source did not find %q", known)
		}
	}
	listed := map[string]bool{}
	for _, r := range Routes {
		key := r.Pkg + " " + r.Pattern
		if listed[key] {
			t.Errorf("%s is in the inventory twice", key)
		}
		listed[key] = true
	}
	var missing, stale []string
	for k := range reg {
		if !listed[k] {
			missing = append(missing, k)
		}
	}
	for k := range listed {
		if _, ok := reg[k]; !ok {
			stale = append(stale, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	if len(missing) > 0 {
		t.Errorf("routes the inventory does not name:\n  %s\nAdd each to tenant.Routes with the check that guards it (see internal/hub/tenant/routes.go)", strings.Join(missing, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("routes of the inventory that are registered nowhere:\n  %s", strings.Join(stale, "\n  "))
	}
}

func TestEveryRouteNamesItsCheck(t *testing.T) {
	for _, r := range Routes {
		key := r.Pkg + " " + r.Pattern
		if r.Tenant == "" {
			t.Errorf("%s: no tenant source", key)
		}
		if (r.Check == "") == (r.Open == "") {
			t.Errorf("%s: name the check that guards the route, or say in Open why it needs none (one of the two)", key)
		}
		var want []string
		ok := true
		for param, src := range tenantParams {
			if strings.Contains(r.Pattern, param) {
				want = append(want, string(src))
				ok = false
			}
		}
		for param, src := range tenantParams {
			if strings.Contains(r.Pattern, param) && r.Tenant == src {
				ok = true
			}
		}
		if !ok {
			sort.Strings(want)
			t.Errorf("%s: the path carries a tenant's id, the tenant source must be one of %v, not %q", key, want, r.Tenant)
		}
	}
}

func TestPatternOfTheSource(t *testing.T) {
	for src, want := range map[string]string{
		`"GET /api/x/{id}"`:          "GET /api/x/{id}",
		`"GET "+p+"/rules/{id}"`:     "GET <p>/rules/{id}",
		`"POST "+hubproto.StorePath`: "POST <hubproto.StorePath>",
		`p+"-guest/{token}"`:         "<p>-guest/{token}",
	} {
		e, err := parser.ParseExpr(src)
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := pattern(e); !ok || got != want {
			t.Errorf("%s: %q %v, want %q", src, got, ok, want)
		}
	}
	if e, _ := parser.ParseExpr(`f(x)`); e != nil {
		if _, ok := pattern(e); ok {
			t.Error("a call was read as a pattern")
		}
	}
}

// carriesTenant says whether a pattern has the id of a space, a machine or an organization.
func carriesTenant(pattern string) bool {
	for _, p := range []string{SpaceParam, MachineParam, OrgParam} {
		if strings.Contains(pattern, "{"+p+"}") {
			return true
		}
	}
	return false
}

// A person's route with a tenant's id in its path (a space, a machine, an organization) goes through Guard, or the
// inventory says why not yet; and the inventory names Guard exactly for the routes that are behind it.
func TestSpaceRoutesAreBehindTheGuard(t *testing.T) {
	reg := registered(t)
	for _, r := range Routes {
		key := r.Pkg + " " + r.Pattern
		via, ok := reg[key]
		if !ok {
			continue // TestEveryRouteIsInTheInventory reports it
		}
		if names := strings.HasPrefix(r.Check, "tenant.Guard"); names != via {
			t.Errorf("%s: the inventory says behind Guard %v, the source %v", key, names, via)
		}
		personal := r.Tenant == Space || r.Tenant == Machine || r.Tenant == Org
		if personal && carriesTenant(r.Pattern) && r.Check != "" && !via && r.Later == "" {
			t.Errorf("%s: put the route behind tenant.Guard, or say in Later why it checks by hand", key)
		}
		if via && r.Later != "" {
			t.Errorf("%s: behind Guard already, drop Later", key)
		}
	}
}
