package preview

import (
	"bytes"
	"encoding/json"
	"strings"

	"golang.org/x/net/html"
)

// A module loader can compute a specifier at run time. The browser's import map can
// route its final same-host URL, including template expressions, without evaluating
// or modifying the loader. Preserve an application's authored map and CSP nonce.
func addSlotImportMap(doc []byte, p pathRewriter) []byte {
	z := html.NewTokenizer(bytes.NewReader(doc))
	firstModule, offset := -1, 0
	var nonce string
	var authored bytes.Buffer
	lastMapEnd := 0
	hasMap := false
	for {
		token := z.Next()
		if token == html.ErrorToken {
			break
		}
		start := offset
		offset += len(z.Raw())
		if token != html.StartTagToken {
			continue
		}
		tag, attr := z.TagName()
		if !bytes.EqualFold(tag, []byte("script")) {
			continue
		}
		var kind, scriptNonce string
		for attr {
			k, v, more := z.TagAttr()
			attr = more
			switch string(k) {
			case "type":
				kind = strings.ToLower(strings.TrimSpace(string(v)))
			case "nonce":
				scriptNonce = string(v)
			}
		}
		if kind == "importmap" {
			hasMap = true
			if z.Next() != html.TextToken {
				offset += len(z.Raw())
				continue
			}
			raw := z.Raw()
			next := rewriteAppImportMap(raw, p)
			authored.Write(doc[lastMapEnd:offset])
			authored.Write(next)
			offset += len(raw)
			lastMapEnd = offset
			continue
		}
		if kind == "module" && firstModule < 0 {
			firstModule, nonce = start, scriptNonce
		}
	}
	if hasMap {
		if lastMapEnd == 0 {
			return doc
		}
		authored.Write(doc[lastMapEnd:])
		return authored.Bytes()
	}
	if firstModule < 0 {
		return doc
	}
	root := p.prefix + "/"
	config, _ := json.Marshal(map[string]map[string]string{"imports": {"/": root, root: root}})
	var out bytes.Buffer
	out.Grow(len(doc) + len(config) + 64)
	out.Write(doc[:firstModule])
	out.WriteString(`<script type="importmap"`)
	if nonce != "" {
		out.WriteString(` nonce="` + html.EscapeString(nonce) + `"`)
	}
	out.WriteByte('>')
	out.Write(config)
	out.WriteString("</script>")
	out.Write(doc[firstModule:])
	return out.Bytes()
}

// Import-map addresses are resolved once against the original document. Bare
// names, external providers, and null (blocked) entries keep their meanings.
func rewriteAppImportMap(raw []byte, p pathRewriter) []byte {
	var config map[string]json.RawMessage
	if json.Unmarshal(raw, &config) != nil || config == nil {
		return raw
	}
	rewriteTable := func(table map[string]json.RawMessage) map[string]json.RawMessage {
		out := make(map[string]json.RawMessage, len(table))
		for name, value := range table {
			var target string
			if json.Unmarshal(value, &target) == nil && modulePath(target) {
				value, _ = json.Marshal(p.fix(target))
			}
			out[name] = value
			if modulePath(name) {
				if _, exists := table[p.fix(name)]; !exists {
					out[p.fix(name)] = value
				}
			}
		}
		return out
	}
	var imports map[string]json.RawMessage
	if json.Unmarshal(config["imports"], &imports) == nil && imports != nil {
		config["imports"], _ = json.Marshal(rewriteTable(imports))
	}
	var scopes map[string]map[string]json.RawMessage
	if json.Unmarshal(config["scopes"], &scopes) == nil && scopes != nil {
		rewritten := make(map[string]map[string]json.RawMessage, len(scopes))
		for name, table := range scopes {
			values := rewriteTable(table)
			rewritten[name] = values
			if _, exists := scopes[p.fix(name)]; !exists {
				rewritten[p.fix(name)] = values
			}
		}
		config["scopes"], _ = json.Marshal(rewritten)
	}
	var integrity map[string]json.RawMessage
	if json.Unmarshal(config["integrity"], &integrity) == nil && integrity != nil {
		scoped := make(map[string]json.RawMessage, len(integrity))
		for key, value := range integrity {
			scoped[p.fix(key)] = value
		}
		config["integrity"], _ = json.Marshal(scoped)
	}
	out, err := json.Marshal(config)
	if err != nil {
		return raw
	}
	return out
}
func modulePath(s string) bool {
	return strings.HasPrefix(s, "/") || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../")
}
