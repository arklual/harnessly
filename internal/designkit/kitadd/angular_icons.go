package kitadd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Taiga draws its icons from files (assets/taiga-ui/icons/<name>.svg) that sit next to the app. A kit has no such
// folder, so the icons the package itself uses and the commonly asked ones go into the bundle as data addresses, and
// the icon resolver reads them from there. Any other name is looked up beside the bundle (icons/<name>.svg).
var taigaIconRe = regexp.MustCompile(`@tui\.([a-z0-9-]+(?:\.[a-z0-9-]+)*)`)

var taigaCommonIcons = strings.Fields(`activity alert-circle archive arrow-down arrow-up arrow-up-right bell book bookmark calendar
calendar-days camera chart-bar chart-line check chevron-up chevrons-up-down circle-plus clipboard credit-card
download external-link file-text filter folder globe grid-2x2 house image inbox key layout-dashboard link list lock
log-in log-out mail map-pin menu message-circle message-square moon package paperclip pencil phone receipt refresh-cw
save send settings share shield shopping-bag shopping-cart square-pen sun tag ticket trash-2 triangle-alert upload user
user-plus users wallet x-circle zap`)

// taigaIcons returns JS imports and statements that give the Taiga icon resolver its embedded icons.
func taigaIcons(dir, pkg string) (imports, providers string) {
	if !strings.HasPrefix(pkg, "@taiga-ui/") {
		return "", ""
	}
	src := filepath.Join(dir, "node_modules", "@taiga-ui", "icons", "src")
	if st, err := os.Stat(src); err != nil || !st.IsDir() {
		return "", ""
	}
	names := map[string]bool{}
	for _, n := range taigaCommonIcons {
		names[n] = true
	}
	for _, sub := range []string{"core", "kit", "layout", "addon-table", "addon-commerce", "addon-doc"} {
		root := filepath.Join(dir, "node_modules", "@taiga-ui", sub, "fesm2022")
		ents, _ := os.ReadDir(root)
		for _, e := range ents {
			if !strings.HasSuffix(e.Name(), ".mjs") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(root, e.Name()))
			if err != nil {
				continue
			}
			for _, m := range taigaIconRe.FindAllStringSubmatch(string(raw), -1) {
				names[m[1]] = true
			}
		}
	}
	icons := map[string]string{}
	var keys []string
	for n := range names {
		raw, err := os.ReadFile(filepath.Join(src, filepath.FromSlash(strings.ReplaceAll(n, ".", "/"))+".svg"))
		if err != nil {
			continue
		}
		icons[n] = strings.TrimSpace(string(raw))
		keys = append(keys, n)
	}
	if len(icons) == 0 {
		return "", ""
	}
	sort.Strings(keys)
	js, _ := json.Marshal(icons)
	imports = "import { tuiIconResolverProvider } from '@taiga-ui/core';\n"
	providers = fmt.Sprintf(`
(function () {
  var icons = %s;
  var base = document.currentScript && document.currentScript.src ? document.currentScript.src.replace(/[^/]*$/, '') : '';
  providers.push(tuiIconResolverProvider(function (icon) {
    var name = icon.replace(/@[a-z]+\./i, '');
    var svg = icons[name];
    if (svg) return 'data:image/svg+xml;charset=UTF-8,' + encodeURIComponent(svg).replace(/\(/g, '%%28').replace(/\)/g, '%%29').replace(/'/g, '%%27');
    return base + 'icons/' + name.replace(/\./g, '/') + '.svg';
  }));
})();
`, js)
	return imports, providers
}
