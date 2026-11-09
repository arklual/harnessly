package main

import (
	"os"

	"github.com/ly-solutions/harnessly/internal/apps"
)

// readHlapp reads a .hlapp and its signature (sigPath, or <archive>.sig when that file exists).
func readHlapp(archive, sigPath string) ([]byte, *apps.Signature, error) {
	raw, err := os.ReadFile(archive)
	if err != nil {
		return nil, nil, err
	}
	if sigPath == "" {
		sigPath = archive + apps.SigSuffix
	}
	var sig *apps.Signature
	if sraw, err := os.ReadFile(sigPath); err == nil {
		if sig, err = apps.ParseSignature(sraw); err != nil {
			return nil, nil, err
		}
	}
	return raw, sig, nil
}

// publishHlapp publishes a .hlapp on a channel; the registry checks its signature under the deployment's policy.
func publishHlapp(reg *apps.Registry, archive, sigPath string, ch apps.Channel) (apps.PublishedVersion, apps.Problems, error) {
	raw, sig, err := readHlapp(archive, sigPath)
	if err != nil {
		return apps.PublishedVersion{}, nil, err
	}
	pv, ps, _, err := reg.PublishArchive(raw, sig, ch, apps.PublishOptions{})
	return pv, ps, err
}

// installHlapp reads a .hlapp and its signature (sigPath, or <archive>.sig when that file exists) and installs it
// through the registry, which checks the signature under the deployment's policy.
func installHlapp(reg *apps.Registry, archive, sigPath string, opt apps.InstallOptions) (apps.Installed, apps.Problems, apps.Verified, error) {
	raw, sig, err := readHlapp(archive, sigPath)
	if err != nil {
		return apps.Installed{}, nil, apps.Verified{}, err
	}
	return reg.InstallArchive(raw, sig, opt)
}
