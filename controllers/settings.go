package controllers

import "os"

var settings = operatorSettings{
	PivtDir:    envOr("FBOP_PIVT_DIR", "/opt/fabric-operator/PIVT"),
	NetworkDir: envOr("FBOP_NETWORK_DIR", "/var/fabric-operator/network"),
}

type operatorSettings struct {
	// directory PIVT repository resides
	PivtDir string

	// parent directory fabric-operator will create fabric-network files.
	// NetworkDir/<namespace>/<fabric-network-name>/
	NetworkDir string
}

func envOr(name, def string) string {
	if v, ok := os.LookupEnv(name); ok {
		return v
	}
	return def
}
