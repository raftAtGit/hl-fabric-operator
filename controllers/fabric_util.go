package controllers

import (
	"bytes"
	"context"
	"io/ioutil"
	"os/exec"

	"github.com/raftAtGit/hl-fabric-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
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

const (
	freshInstall = true
	reconstruct  = false

	genesisSecret      = "hlf-genesis.block"
	cryptoConfigSecret = "hlf-crypto-config"
)

func (r *FabricNetworkReconciler) prepareChartDirForFabric(ctx context.Context, network *v1alpha1.FabricNetwork, isFreshInstall bool) error {

	networkDir := getNetworkDir(network)

	cryptoConfig := newCryptoConfig(network)
	r.Log.Info("Created cryptoConfig", "cryptoConfig", cryptoConfig)

	file := networkDir + "/crypto-config.yaml"
	if err := writeYamlToFile(cryptoConfig, file); err != nil {
		return err
	}
	r.Log.Info("Wrote cryptoConfig to file", "file", file)

	if !isFreshInstall || network.Spec.CryptoConfig.Secret != "" {
		if isFreshInstall {
			r.Log.Info("CryptoConfig.Secret is provided. Downloading certificates from secret", "CryptoConfig.Secret", network.Spec.CryptoConfig.Secret)
		} else {
			r.Log.Info("This is not fresh installation but reconstruction. Downloading certificates from secret", "CryptoConfig.Secret", cryptoConfigSecret)
		}

		secret := &corev1.Secret{}
		err := r.Get(ctx, types.NamespacedName{Namespace: network.Namespace, Name: cryptoConfigSecret}, secret)
		if err != nil {
			return err
		}

		buf := bytes.NewBuffer(secret.Data["crypto-config"])
		if err := uncompress(buf, networkDir); err != nil {
			return err
		}
		r.Log.Info("Downloaded and uncompressed certificates from secret", "secret", cryptoConfigSecret, "folder", networkDir)

	} else {
		r.Log.Info("Creating certificates", "network", network.Name)
		// cryptogen generate --config ./crypto-config.yaml --output crypto-config
		cmd := exec.CommandContext(ctx, "cryptogen", "generate", "--config", "./crypto-config.yaml", "--output", "crypto-config")
		cmd.Dir = networkDir
		output, err := cmd.CombinedOutput()

		r.Log.Info("cryptogen completed", "err", err, "output", string(output))
		if err != nil {
			return err
		}

		if err = r.storeCryptoConfig(ctx, network); err != nil {
			return err
		}
	}

	if network.Spec.Genesis.Secret != "" {
		r.Log.Info("Genesis.Secret is provided, skipping genesis block creation", "secret", network.Spec.Genesis.Secret)
	} else if !isFreshInstall {
		r.Log.Info("This is not fresh installation but reconstruction. Downloading genesis block from secret", "secret", genesisSecret)

		secret := &corev1.Secret{}
		err := r.Get(ctx, types.NamespacedName{Namespace: network.Namespace, Name: genesisSecret}, secret)
		if err != nil {
			return err
		}

		file := networkDir + "/channel-artifacts/genesis.block"
		if err := ioutil.WriteFile(file, secret.Data["genesis.block"], 0644); err != nil {
			return err
		}
		r.Log.Info("Downloaded genesis block and wrote to file", "secret", genesisSecret, "file", file)

	} else {
		r.Log.Info("Creating genesis block", "network", network.Name)
		// configtxgen -profile $genesisProfile -channelID $systemChannelID -outputBlock ./channel-artifacts/genesis.block
		cmd := exec.CommandContext(ctx, "configtxgen", "-profile", network.Spec.Network.GenesisProfile,
			"-channelID", network.Spec.Network.SystemChannelID, "-outputBlock", "./channel-artifacts/genesis.block")
		cmd.Dir = networkDir
		output, err := cmd.CombinedOutput()

		r.Log.Info("configtxgen completed", "err", err, "output", string(output))
		if err != nil {
			return err
		}
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

func (r *FabricNetworkReconciler) storeCryptoConfig(ctx context.Context, network *v1alpha1.FabricNetwork) error {

	networkDir := getNetworkDir(network)

	var buffer bytes.Buffer
	if err := compress(networkDir, "crypto-config", &buffer); err != nil {
		return err
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "hlf-crypto-config",
			Namespace: network.Namespace,
			Labels: map[string]string{
				"raft.io/fabric-operator-created-for": network.Name,
			},
		},
		Data: map[string][]byte{
			"crypto-config": buffer.Bytes(),
		},
	}
	// set owner to FabricNetwork, so when network is deleted Secret is also deleted
	ctrl.SetControllerReference(network, secret, r.Scheme)

	if err := r.Create(ctx, secret); err != nil {
		return err
	}
	r.Log.Info("Stored crypto-config in secret", "secret", secret.Name)

	return nil
}
