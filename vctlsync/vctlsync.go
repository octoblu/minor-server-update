package vctlsync

import "github.com/octoblu/minor-server-update/vctl"

// Synchronizer syncs minor servers to an
// alternate minor backend
type Synchronizer struct {
	vctlClient *vctl.Client
}

// New constructs a new Synchronizer
func New(etcdURI, vulcandURI string) *Synchronizer {
	vctlClient := vctl.New(etcdURI, vulcandURI)
	return &Synchronizer{vctlClient}
}

// Run runs the syncing process until
// Stop is called
func (synchronizer *Synchronizer) Run() error {
	vctlClient := synchronizer.vctlClient

	vctlClient.ForEachMinorServer(vctlClient.RegisterServerWithMinor)
	return vctlClient.OnMinorServerChange(vctlClient.RegisterServerWithMinor)
}
