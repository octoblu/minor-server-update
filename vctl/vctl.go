package vctl

import (
	"fmt"
	"log"
	"regexp"

	"github.com/octoblu/go-simple-etcd-client/etcdclient"
	Debug "github.com/tj/go-debug"
	"github.com/vulcand/vulcand/api"
	"github.com/vulcand/vulcand/engine"
	"github.com/vulcand/vulcand/plugin/registry"
)

var etcdServerKeyRegexp = regexp.MustCompile("/vulcand/backends/(.*?)/servers/(.*)")
var etcdMinorBackendKeyRegexp = regexp.MustCompile("/vulcand/backends/(.*?)-minor/servers/(.*)-minor-\\d+")
var etcdMinorServerKeyRegexp = regexp.MustCompile("/vulcand/backends/(.*?)/servers/(.*)-minor-\\d+")
var debug = Debug.Debug("vctl.vctl")

// Client allows for interfacing with a vulcan configuration
// over Etcd/Vulcan API
type Client struct {
	etcdURI, vulcandURI string
}

// New constructs a new vctl Client instance
func New(etcdURI, vulcandURI string) *Client {
	fmt.Println("New", etcdURI, vulcandURI)
	debug(`New("%v", "%v")`, etcdURI, vulcandURI)
	return &Client{etcdURI, vulcandURI}
}

// OnServerCallback is called with the backendID and serverID
// of whaver server just changed
type OnServerCallback func(backendID, serverID string)

// ForEachMinorBackendServer calls the onServer callback for each minor server
// already registered to a minor backend
func (client *Client) ForEachMinorBackendServer(onServer OnServerCallback) error {
	etcdClient, err := etcdclient.Dial(client.etcdURI)
	panicOnError("etcdclient.Dial failed", err)

	keys, err := etcdClient.LsRecursive("/vulcand/backends")
	if err != nil {
		return err
	}

	for _, key := range keys {
		if !isMinorBackendKey(key) {
			continue
		}

		backendID := parseBackendID(key)
		serverID := parseServerID(key)
		onServer(backendID, serverID)
	}
	return nil
}

// ForEachMinorServer calls the onServer callback for each minor server
// registered to vulcand
func (client *Client) ForEachMinorServer(onServer OnServerCallback) error {
	etcdClient, err := etcdclient.Dial(client.etcdURI)
	panicOnError("etcdclient.Dial failed", err)

	keys, err := etcdClient.LsRecursive("/vulcand/backends")
	if err != nil {
		return err
	}

	for _, key := range keys {
		if !isValidMinorServerKey(key) {
			continue
		}

		backendID := parseBackendID(key)
		serverID := parseServerID(key)
		onServer(backendID, serverID)
	}
	return nil
}

// OnMinorServerChange calls the function whenever something a server
// is added/removed/modified, but only if that server's cluster name
// is minor. This function only returns on error
func (client *Client) OnMinorServerChange(onServerChange OnServerCallback) error {
	etcdClient, err := etcdclient.Dial(client.etcdURI)
	if err != nil {
		return err
	}

	return etcdClient.WatchRecursive("/vulcand/backends", func(key, value string) {
		debug("something happened")
		if !isValidMinorServerKey(key) {
			debug(`Key doesn't look like a valid minor server. Skipping. "%v"`, key)
			return
		}

		debug(`Key looks legit, calling callback. "%v"`, key)
		backendID := parseBackendID(key)
		serverID := parseServerID(key)
		onServerChange(backendID, serverID)
	})
}

// RegisterServerWithMinor registers servers to a second backend that
// minor appended to it. It uses the vulcand API so that upserts are
// validated before they are saved
// i.e:
//   {backendId: 'octoblu-some-service', serverId: 'octoblu-some-service-minor-1'}
// would be copied to
//   {backendId: 'octoblu-some-service-minor', serverId: 'octoblu-some-service-minor-1'}
func (client *Client) RegisterServerWithMinor(backendID, serverID string) {
	backendMinorID := fmt.Sprintf("%v-minor", backendID)
	// etcdMajorServerKey := fmt.Sprintf("/vulcand/backends/%v/servers/%v", backendID, serverID)

	url := client.getServerURL(backendID, serverID)
	if url == "" {
		err := client.deleteServer(backendMinorID, serverID)
		panicOnError("client.rmServer failed", err)
		return
	}

	err := client.upsertBackend(backendMinorID)
	panicOnError("upsertBackend failed", err)
	err = client.upsertServer(backendMinorID, serverID, url)
	panicOnError("upsertServer failed", err)
}

// RemoveServerIfNescessary checks to see if a server is present on
// the non-minor backend and removes it if it is not
func (client *Client) RemoveServerIfNescessary(backendID, serverID string) {
	// octoblu-sms-service-minor -> octoblu-sms-service
	nonMinorBackendID := regexp.MustCompile("-minor$").ReplaceAllString(backendID, "")

	nonMinorURL := client.getServerURL(nonMinorBackendID, serverID)
	if nonMinorURL != "" {
		return
	}

	client.deleteServer(backendID, serverID)
}

func (client *Client) getServerURL(backendID, serverID string) string {
	backendKey := engine.BackendKey{Id: backendID}
	serverKey := engine.ServerKey{Id: serverID, BackendKey: backendKey}

	etcdClient := api.NewClient(client.vulcandURI, registry.GetRegistry())
	server, err := etcdClient.GetServer(serverKey)
	if err != nil {
		return ""
	}

	return server.URL
}

func (client *Client) deleteServer(backendID, serverID string) error {
	debug(`deleteServer {backendID: "%v", serverID: "%v"}`, backendID, serverID)

	backendKey := engine.BackendKey{Id: backendID}
	serverKey := engine.ServerKey{Id: serverID, BackendKey: backendKey}

	etcdClient := api.NewClient(client.vulcandURI, registry.GetRegistry())
	_, err := etcdClient.GetServer(serverKey)
	if err != nil {
		debug("Server doesn't seem to exist, doing nothing")
		return nil
	}
	return etcdClient.DeleteServer(serverKey)
}

func (client *Client) upsertBackend(backendID string) error {
	debug(`upsertBackend {backendID: "%v"}`, backendID)

	backendKey := engine.BackendKey{Id: backendID}
	backendSettings := engine.HTTPBackendSettings{}
	backend, err := engine.NewHTTPBackend(backendID, backendSettings)
	if err != nil {
		return err
	}

	etcdClient := api.NewClient(client.vulcandURI, registry.GetRegistry())
	_, err = etcdClient.GetBackend(backendKey)
	if err != nil {
		debug("Looks like the minor backend is missing, upserting")
		return etcdClient.UpsertBackend(*backend)
	}
	debug("Backend is already there, doing nothing.")
	return nil
}

func (client *Client) upsertServer(backendID, serverID, url string) error {
	debug(`upsertServer {backendID: "%v", serverID: "%v", url: "%v"}`, backendID, serverID, url)

	server := engine.Server{Id: serverID, URL: url}
	backendKey := engine.BackendKey{Id: backendID}
	serverKey := engine.ServerKey{Id: serverID, BackendKey: backendKey}

	etcdClient := api.NewClient(client.vulcandURI, registry.GetRegistry())

	oldServer, err := etcdClient.GetServer(serverKey)
	if err != nil {
		debug("Looks like the minor server is missing, upserting")
		return etcdClient.UpsertServer(backendKey, server, 0)
	}

	if oldServer.URL != server.URL {
		debug("Looks like the URL is out of date, upserting")
		return etcdClient.UpsertServer(backendKey, server, 0)
	}

	debug("Everything looks cool already. Gonna take a nap instead.")
	return nil
}

func panicOnError(message string, err error) {
	if err != nil {
		log.Panicln(message, err.Error())
	}
}

func isEtcdServerKey(key string) bool {
	return etcdServerKeyRegexp.MatchString(key)
}

func isMinorBackendKey(key string) bool {
	return etcdMinorBackendKeyRegexp.MatchString(key)
}

func isMinorServerKey(key string) bool {
	return etcdMinorServerKeyRegexp.MatchString(key)
}

func isValidMinorServerKey(key string) bool {
	if !isEtcdServerKey(key) {
		debug(`Key doesn't look like an etcd server. "%v"`, key)
		return false
	}

	if !isMinorServerKey(key) {
		debug(`Key doesn't look like a minor server. "%v"`, key)
		return false
	}

	if isMinorBackendKey(key) {
		debug(`Key looks like it belongs to a minor backend. "%v"`, key)
		return false
	}

	debug(`key is a valid minor server key. "%v"`, key)
	return true
}

// parseBackendID expects key to match the correct format as
// defined by etcdServerKeyRegexp above. isEtcdServerKey
// can be used to verify the format before this method is called
func parseBackendID(key string) string {
	matches := etcdServerKeyRegexp.FindStringSubmatch(key)
	return matches[1]
}

// parseServerID expects key to match the correct format as
// defined by etcdServerKeyRegexp above. isEtcdServerKey
// can be used to verify the format before this method is called
func parseServerID(key string) string {
	matches := etcdServerKeyRegexp.FindStringSubmatch(key)
	return matches[2]
}
