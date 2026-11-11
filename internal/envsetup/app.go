package envsetup

import (
	"strings"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// app installs the item's app from the folder of a repository, as the environment's: the person cannot remove it,
// and cannot turn it off when the item is pinned.
func (ex *execution) app(i int) {
	it, src := ex.items[i], ex.srcs[i]
	if ex.r.Apps == nil {
		ex.fail(i, "эта машина не ставит приложения")
		return
	}
	if src.Path == "" {
		ex.fail(i, "не сказано, какая папка репозитория — приложение (path)")
		return
	}
	if ex.sandbox {
		ex.done(i, hubproto.EnvSkipped, "в песочнице приложения не ставятся")
		return
	}
	if !ex.modifies {
		if ex.r.Apps.Has(it.ItemID) {
			ex.done(i, hubproto.EnvSkipped, "уже стоит")
		} else {
			ex.pend(i, "не поставлено — поставится при обновлении")
		}
		return
	}
	w, err := ex.locate(i, true)
	if err != nil {
		ex.fail(i, err.Error())
		return
	}
	defer w.cleanup()
	id, changed, missing, err := ex.r.Apps.Install(ex.hostPath(w.file), it.ItemID, it.Pinned, src.Install)
	switch {
	case err != nil:
		ex.fail(i, "приложение не поставлено: "+err.Error())
	case len(missing) > 0:
		// Not an error: the installation has no such service, and the app stays out of sight until it does.
		ex.done(i, hubproto.EnvSkipped, "не настроено: у хаба нет адреса сервиса ("+strings.Join(missing, ", ")+")")
	case changed:
		ex.done(i, hubproto.EnvOK, "поставлено: "+id)
	default:
		ex.done(i, hubproto.EnvSkipped, "уже стоит")
	}
}

// pruneApps takes off the environment's apps whose items left the list, after a full run on the machine itself.
func (ex *execution) pruneApps() {
	if ex.r.Apps == nil {
		return
	}
	keep := []string{}
	for _, it := range ex.items {
		if it.Kind == hubproto.EnvKindApp {
			keep = append(keep, it.ItemID)
		}
	}
	gone, err := ex.r.Apps.Prune(keep)
	if err != nil {
		ex.r.logf("warn", "envsetup: the environment's apps not pruned", "error", err.Error())
		return
	}
	for _, id := range gone {
		ex.r.logf("info", "envsetup: the environment's app taken off", "id", id)
	}
}
