package apps

import (
	"os"
	"sync/atomic"
)

// The apps' permissions can be switched off: an installed app works, with no consent screen and no grant,
// no tool rules of the apps for the agent (app-guard) and no wall a deployment requires (RequireWall), and the pages
// show no rights. The rights a manifest lists still hold: an app's view reaches only what its manifest asks for
// (decided 09.11). The code of the permissions stays: on brings back everything as it was.
//
// It is off by default while the concept is being redone: the owner accepted the risk on 09.11.2026 (few people use
// apps, and the permissions kept installed apps from working). RUNTIME_APPS_PERMISSIONS=on turns them on.

// PermissionsEnv is the deployment's switch: "on" turns the apps' permissions on; anything else leaves them off.
const PermissionsEnv = "RUNTIME_APPS_PERMISSIONS"

var permissionsOn atomic.Bool

// SetPermissions turns the apps' permissions on or off for this process (the runtime reads PermissionsEnv at start;
// tests of the refusals turn them on).
func SetPermissions(on bool) { permissionsOn.Store(on) }

// PermissionsOn says whether the apps' permissions are on.
func PermissionsOn() bool { return permissionsOn.Load() }

// PermissionsFromEnv sets the switch from PermissionsEnv.
func PermissionsFromEnv() { SetPermissions(os.Getenv(PermissionsEnv) == "on") }
