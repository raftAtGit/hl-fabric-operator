package cmd

import (
	"context"
	"fmt"

	apiClient "github.com/raftAtGit/hl-fabric-operator/cli/cmd/client"
	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// updateCmd represents the update command
var updateCmd = &cobra.Command{
	Use:   "update FABRIC_NETWORK_FILE",
	Args:  cobra.ExactArgs(1),
	Short: "Update a FabricNetwork",
	Long:  `Update an existing FabricNetwork in Kubernetes cluster`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, client := apiClient.NewClient()

		if err := updateNetwork(ctx, client, args); err != nil {
			fail("%v", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(updateCmd)
}

func updateNetwork(ctx context.Context, cl client.Client, args []string) error {
	networkFile := args[0]
	network, err := loadFabricNetwork(networkFile)
	if err != nil {
		return err
	}

	exists, old, err := fabricNetworkExists(ctx, cl, namespace, network.Name)
	if err != nil {
		return err
	}

	if !exists {
		return fmt.Errorf("FabricNetwork %v does not exist in namespace %v", network.Name, namespace)
	}

	// TODO  cleanup!!!
	overwrite = true
	if err = validateNewNetwork(ctx, cl, network); err != nil {
		return err
	}

	if network.Spec.Configtx.File != "" {
		if err := createOrUpdateConfigtxSecret(ctx, cl, network, networkFile); err != nil {
			return err
		}
		network.Spec.Configtx.File = ""
		network.Spec.Configtx.Secret = "hlf-configtx.yaml"
	}

	if network.Spec.CryptoConfig.Folder != "" {
		if err := createOrUpdateCryptoConfigSecret(ctx, cl, network, networkFile); err != nil {
			return err
		}
		network.Spec.CryptoConfig.Folder = ""
		network.Spec.CryptoConfig.Secret = "hlf-crypto-config"
	}

	if network.Spec.Chaincode.Folder != "" {
		if err := createOrUpdateChaincodeConfigMaps(ctx, cl, network, networkFile); err != nil {
			return err
		}
		network.Spec.Chaincode.Folder = ""
	}

	network.Namespace = namespace
	network.ResourceVersion = old.ResourceVersion
	if err := cl.Update(ctx, network); err != nil {
		return err
	}
	info("updated FabricNetwork %v in namespace %v", network.Name, network.Namespace)

	return nil
}
