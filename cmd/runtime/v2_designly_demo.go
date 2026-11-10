package main

import (
	"context"
	"embed"
	"io/fs"

	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// demoProjects are the projects every library starts with: a site, a deck, a video and a design system of
// one made-up product, Relay, for the lessons and for trying Sinopia on something real. They are the person's own
// copies: edited and deleted like any project.
//
//go:embed all:demo
var demoProjects embed.FS

// demoKinds are the kinds the folders do not tell by themselves (a deck and a video say so in their meta).
var demoKinds = map[string]wire.DesignKind{"relay-site": wire.DesignSite, "relay-ds": wire.DesignDS}

// Demo copies back the demo projects the person deleted, into their "Demo" folder (the one they have, even renamed;
// a new one when they deleted it).
func (d *v2Designly) Demo(ctx context.Context) (wire.DesignlyDemo, error) {
	ids, err := d.seedDemo(d.api.L, true)
	if err == nil {
		err = d.api.L.PlaceDemo(d.personOf(ctx), ids)
	}
	if err != nil {
		return wire.DesignlyDemo{}, designlyFail(wire.RefuseInternal, locale.Pick("Could not restore the demo projects", "Не удалось вернуть демо-проекты"), "")
	}
	if ids == nil {
		ids = []string{}
	}
	return wire.DesignlyDemo{Restored: ids}, nil
}

// seedDemo gives the library the demo projects once; with force, again the ones the person deleted.
func (d *v2Designly) seedDemo(lib *design.Library, force bool) ([]string, error) {
	sub, err := fs.Sub(demoProjects, "demo")
	if err != nil {
		return nil, err
	}
	ids, err := lib.SeedDemo(sub, force)
	if len(ids) > 0 {
		_ = d.change(func(st *designlyState) {
			for _, id := range ids {
				if k, ok := demoKinds[id]; ok {
					st.Kinds[id] = k
				}
			}
		})
	}
	return ids, err
}
