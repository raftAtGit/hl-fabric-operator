package cmd

import (
	"bytes"
	"context"
	"io/ioutil"
	"path"

	"github.com/raftAtGit/hl-fabric-operator/api/v1alpha1"
	apiClient "github.com/raftAtGit/hl-fabric-operator/cli/cmd/client"
	"github.com/spf13/cobra"

	// corev1 "k8s.io/api/core/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// downloadCmd represents the download command
var downloadCmd = &cobra.Command{
	Use:   "download FABRIC_NETWORK_NAME",
	Args:  cobra.ExactArgs(1),
	Short: "Download resources of a FabricNetwork",
	Long:  `Download resources of a FabricNetwork from Kubernetes cluster`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, client := apiClient.NewClient()

		if err := downloadResources(ctx, client, args); err != nil {
			fail("%v", err)
		}

	},
}

const (
	configTxSecret     = "hlf-configtx.yaml"
	genesisSecret      = "hlf-genesis.block"
	cryptoConfigSecret = "hlf-crypto-config"
)

func init() {
	rootCmd.AddCommand(downloadCmd)

	downloadCmd.Flags().StringVarP(&outputDir, "output", "o", ".", "output directory")
}

func downloadResources(ctx context.Context, cl client.Client, args []string) error {
	name := args[0]

	network := &v1alpha1.FabricNetwork{}
	if err := cl.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, network); err != nil {
		return err
	}
	debug("Got FabricNetwork: %v", network.Name)

	createDirIfNotExists(outputDir)

	secret := &corev1.Secret{}
	if err := cl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: configTxSecret}, secret); err != nil {
		return err
	}
	file := path.Join(outputDir, "configtx.yaml")
	if err := ioutil.WriteFile(file, secret.Data["configtx.yaml"], 0644); err != nil {
		return err
	}
	info("downloaded configtx to %v", file)

	secret = &corev1.Secret{}
	if err := cl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: genesisSecret}, secret); err != nil {
		return err
	}
	file = path.Join(outputDir, "genesis.block")
	if err := ioutil.WriteFile(file, secret.Data["genesis.block"], 0644); err != nil {
		return err
	}
	info("downloaded genesis block to %v", file)

	secret = &corev1.Secret{}
	if err := cl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cryptoConfigSecret}, secret); err != nil {
		return err
	}

	folder := path.Join(outputDir, "crypto-config")
	buf := bytes.NewBuffer(secret.Data["crypto-config"])
	if err := uncompress(buf, folder); err != nil {
		return err
	}
	info("downloaded certificates to %v", folder)

	return nil
}
