// Package upgradecheck holds the scenario of HRN-735: a machine adapter that runs as its own process keeps working
// when the hub is replaced by the next release build. The scenario runs real binaries and is driven by
// tools/ci/adapter-upgrade.sh; without HUB_BIN_N it is skipped.
package upgradecheck
