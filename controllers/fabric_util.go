package controllers

import (
	"context"
	"os/exec"

	"github.com/raftAtGit/hl-fabric-operator/api/v1alpha1"
)

type cryptoConfig struct {
	// Orderer organizations
	OrdererOrgs []ordererOrg `json:"OrdererOrgs"`
	// Peer organizations
	PeerOrgs []peerOrg `json:"PeerOrgs"`
}

// Orderer organization
type ordererOrg struct {
	Name          string `json:"Name"`
	Domain        string `json:"Domain"`
	EnableNodeOUs bool   `json:"EnableNodeOUs"`

	Specs []host `json:"Specs"`
}

type host struct {
	Hostname string `json:"Hostname"`
}

// Peer organization
type peerOrg struct {
	Name          string `json:"Name"`
	Domain        string `json:"Domain"`
	EnableNodeOUs bool   `json:"EnableNodeOUs"`
	Template      count  `json:"Template"`
	Users         count  `json:"Users"`
}

type count struct {
	Count int32 `json:"Count"`
}

func (r *FabricNetworkReconciler) prepareChartDirForFabric(ctx context.Context, network *v1alpha1.FabricNetwork) error {

	networkDir := getNetworkDir(network)

	cryptoConfig := newCryptoConfig(network)
	r.Log.Info("Created cryptoConfig", "cryptoConfig", cryptoConfig)

	file := networkDir + "/crypto-config.yaml"
	if err := writeYamlToFile(cryptoConfig, file); err != nil {
		return err
	}
	r.Log.Info("Wrote cryptoConfig to file", "file", file)

	r.Log.Info("Creating certificates", "network", network.Name)
	// cryptogen generate --config ./crypto-config.yaml --output crypto-config
	cmd := exec.CommandContext(ctx, "cryptogen", "generate", "--config", "./crypto-config.yaml", "--output", "crypto-config")
	cmd.Dir = networkDir
	output, err := cmd.CombinedOutput()

	r.Log.Info("cryptogen completed", "err", err, "output", string(output))
	if err != nil {
		return err
	}

	r.Log.Info("Creating genesis block", "network", network.Name)
	// configtxgen -profile $genesisProfile -channelID $systemChannelID -outputBlock ./channel-artifacts/genesis.block
	cmd = exec.CommandContext(ctx, "configtxgen", "-profile", network.Spec.Network.GenesisProfile,
		"-channelID", network.Spec.Network.SystemChannelID, "-outputBlock", "./channel-artifacts/genesis.block")
	cmd.Dir = networkDir
	output, err = cmd.CombinedOutput()

	r.Log.Info("configtxgen completed", "err", err, "output", string(output))
	if err != nil {
		return err
	}

	return nil
}

func newCryptoConfig(network *v1alpha1.FabricNetwork) cryptoConfig {
	c := cryptoConfig{}

	c.OrdererOrgs = make([]ordererOrg, len(network.Spec.Topology.OrdererOrgs))
	for i, o := range network.Spec.Topology.OrdererOrgs {
		c.OrdererOrgs[i] = ordererOrg{
			Name:          o.Name,
			Domain:        o.Domain,
			EnableNodeOUs: true,
			Specs:         make([]host, len(o.Hosts)),
		}
		for j, h := range o.Hosts {
			c.OrdererOrgs[i].Specs[j] = host{Hostname: h}
		}
	}

	c.PeerOrgs = make([]peerOrg, len(network.Spec.Topology.PeerOrgs))
	for i, p := range network.Spec.Topology.PeerOrgs {
		c.PeerOrgs[i] = peerOrg{
			Name:          p.Name,
			Domain:        p.Domain,
			EnableNodeOUs: true,
			Template:      count{Count: p.PeerCount},
			Users:         count{Count: 1},
		}
	}

	return c
}
