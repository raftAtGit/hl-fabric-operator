package controllers

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/raftAtGit/hl-fabric-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func (r *FabricNetworkReconciler) prepareChartDirForFabric(ctx context.Context, network *v1alpha1.FabricNetwork) error {

	networkDir := getNetworkDir(network)

	cryptoConfig := newCryptoConfig(network)
	r.Log.Info("Created cryptoConfig", "cryptoConfig", cryptoConfig)

	file := networkDir + "/crypto-config.yaml"
	if err := writeYamlToFile(cryptoConfig, file); err != nil {
		return err
	}
	r.Log.Info("Wrote cryptoConfig to file", "file", file)

	if network.Spec.CryptoConfig.Secret != "" {
		r.Log.Info("TODO: implement me. download certificates from secret")
	}

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

func (r *FabricNetworkReconciler) storeCryptoConfig(ctx context.Context, network *v1alpha1.FabricNetwork) error {

	networkDir := getNetworkDir(network)

	var buffer bytes.Buffer
	if err := tarArchive(networkDir, "crypto-config", &buffer); err != nil {
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

// TAR archives given file or folder
// modified from: https://gist.github.com/mimoo/25fc9716e0f1353791f5908f94d6e726
func tarArchive(parentFolder string, childFolder string, buf io.Writer) error {
	// tar > gzip > buf
	zr := gzip.NewWriter(buf)
	defer zr.Close()

	tw := tar.NewWriter(zr)
	defer tw.Close()

	src := parentFolder + "/" + childFolder

	// walk through every file in the folder
	return filepath.Walk(src, func(file string, fi os.FileInfo, err error) error {
		// generate tar header
		header, err := tar.FileInfoHeader(fi, file)
		if err != nil {
			return err
		}

		// must provide real name
		// (see https://golang.org/src/archive/tar/common.go?#L626)
		// header.Name = filepath.ToSlash(file)
		header.Name = strings.TrimPrefix(filepath.ToSlash(file), parentFolder+"/")
		// hj, _ := yaml.Marshal(header)
		// debug("header: %v", string(hj))

		// write header
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		// if not a dir, write file content
		if !fi.IsDir() {
			f, err := os.Open(file)
			if err != nil {
				return err
			}
			if _, err := io.Copy(tw, f); err != nil {
				return err
			}
			f.Close()
		}
		return nil
	})
}
