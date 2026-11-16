package kitadd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

// KitRootID is the provider a kit made by `kit add` gets when the package needs a provider to draw anything and
// the provider needs props a board cannot give (a theme object, a design system value): a small component the bundle
// carries, which wraps its children in the package's own provider with the package's default theme. It is also where
// the theme object is read from: window.__kitTheme.
const KitRootID = "KitRoot"

// rootRecipe is the code a bundle adds to make KitRoot for a package.
type rootRecipe struct {
	code string
	desc string
}

// The recipes see K (the package's exports) and ns (the window global); React is the page's.
var rootRecipes = map[string]rootRecipe{
	"@mui/material": {desc: "MUI's ThemeProvider with the default theme (CSS variables, light and dark) and CssBaseline", code: `
(function(){var R=window.React;if(!K.ThemeProvider||!K.createTheme)return;
var th;try{th=K.createTheme({cssVariables:{colorSchemeSelector:'[data-theme="%s"]'},colorSchemes:{light:true,dark:true}});}catch(e){th=K.createTheme({});}
var vars={};function flat(o,p){for(var k in o){var v=o[k];if(v&&typeof v==='object')flat(v,p+k+'-');else if(typeof v==='string'||typeof v==='number')vars[p+k]=String(v);}}
try{flat(th.palette,'--mui-palette-');if(th.shape)vars['--mui-shape-borderRadius']=th.shape.borderRadius+'px';if(th.typography){vars['--mui-fontFamily']=th.typography.fontFamily;}}catch(e){}
window.__kitTheme={vars:vars};
ns.KitRoot=function(p){return R.createElement(K.ThemeProvider,{theme:th},K.CssBaseline?R.createElement(K.CssBaseline):null,p.children);};})();`},
	"antd": {desc: "Ant Design's ConfigProvider with CSS variables on", code: `
(function(){var R=window.React;if(!K.ConfigProvider)return;
function kebab(s){return s.replace(/[A-Z]/g,function(m){return '-'+m.toLowerCase();});}
function Tok(){var t=K.theme&&K.theme.useToken&&K.theme.useToken();if(t&&t.token){var v={};for(var k in t.token){var x=t.token[k];if(typeof x==='string')v['--ant-'+kebab(k)]=x;else if(typeof x==='number')v['--ant-'+kebab(k)]=/^(line-height|opacity|z-index|font-weight|motion-base|motion-unit)/.test(kebab(k))?String(x):x+'px';}window.__kitTheme={vars:v};}return null;}
ns.KitRoot=function(p){return R.createElement(K.ConfigProvider,{theme:{cssVar:true,hashed:false}},R.createElement(Tok),p.children);};})();`},
	"@chakra-ui/react": {desc: "Chakra's ChakraProvider with the default system", code: `
(function(){var R=window.React;if(!K.ChakraProvider)return;
ns.KitRoot=function(p){return R.createElement(K.ChakraProvider,K.defaultSystem?{value:K.defaultSystem}:{},p.children);};})();`},
	"@fluentui/react-components": {desc: "Fluent's FluentProvider with the light web theme", code: `
(function(){var R=window.React;if(!K.FluentProvider||!K.webLightTheme)return;
ns.KitRoot=function(p){return R.createElement(K.FluentProvider,{theme:K.webLightTheme},p.children);};})();`},
	"@radix-ui/themes": {desc: "Radix Themes' Theme", code: `
(function(){var R=window.React;if(!K.Theme)return;
ns.KitRoot=function(p){return R.createElement(K.Theme,{},p.children);};})();`},
	"@primer/react": {desc: "Primer's ThemeProvider and BaseStyles", code: `
(function(){var R=window.React;if(!K.ThemeProvider)return;
ns.KitRoot=function(p){return R.createElement(K.ThemeProvider,{},K.BaseStyles?R.createElement(K.BaseStyles,null,p.children):p.children);};})();`},
	"@mantine/core": {desc: "Mantine's MantineProvider", code: `
(function(){var R=window.React;if(!K.MantineProvider)return;
ns.KitRoot=function(p){return R.createElement(K.MantineProvider,{},p.children);};})();`},
}

// providerNames are the exports that are a provider by name: tried, in this order, when no recipe knows the package.
var providerNames = []string{"ChakraProvider", "MantineProvider", "ConfigProvider", "HeroUIProvider", "NextUIProvider",
	"FluentProvider", "CssVarsProvider", "ThemeProvider", "StyletronProvider", "TamaguiProvider", "Provider"}

// rootFor is what makes the package's provider: a recipe (the bundle gets KitRoot), or an export of its own that is
// a provider and needs no props. "" for neither.
func rootFor(pkg string, comps []genComp) (recipe *rootRecipe, export string) {
	if r, ok := rootRecipes[pkg]; ok {
		return &r, KitRootID
	}
	have := map[string]*genComp{}
	for i := range comps {
		have[comps[i].ID] = &comps[i]
	}
	for _, n := range providerNames {
		c := have[n]
		if c == nil {
			continue
		}
		ok := true
		for _, p := range c.Props {
			if p.Required && p.Name != "children" {
				ok = false
			}
		}
		if ok {
			return nil, n
		}
	}
	return nil, ""
}

var minVerRe = regexp.MustCompile(`\d+`)

// reactPeer is the React major version a package asks for when the lowest one it accepts is 19 or newer: "19", else "".
// A package that accepts 18 or older runs on the page's default React.
func reactPeer(dir, pkg string) string {
	raw, err := os.ReadFile(filepath.Join(dir, "node_modules", filepath.FromSlash(pkg), "package.json"))
	if err != nil {
		return ""
	}
	var pj struct {
		Peer map[string]string `json:"peerDependencies"`
		Deps map[string]string `json:"dependencies"`
	}
	if json.Unmarshal(raw, &pj) != nil {
		return ""
	}
	spec := pj.Peer["react"]
	if spec == "" {
		spec = pj.Deps["react"]
	}
	if spec == "" {
		return ""
	}
	low := -1
	for _, alt := range strings.Split(spec, "||") {
		if m := minVerRe.FindString(alt); m != "" {
			n, _ := strconv.Atoi(m)
			if low < 0 || n < low {
				low = n
			}
		}
	}
	if low >= 19 {
		return "19"
	}
	return ""
}

var react19API = regexp.MustCompile(`\.(useActionState|useOptimistic)\b`)

// needsReact19 says a bundle uses what only React 19 has.
func needsReact19(js []byte) bool { return react19API.Match(js) }

// rootComp is KitRoot as a component record of the kit.
func rootComp(r *rootRecipe) genComp {
	return genComp{ID: KitRootID, Part: designkit.PartProvider, Description: "The kit's provider: " + r.desc + ". Every board wraps its content in it."}
}
