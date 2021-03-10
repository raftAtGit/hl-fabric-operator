package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/raftAtGit/hl-fabric-operator/api/v1alpha1"
	apiClient "github.com/raftAtGit/hl-fabric-operator/cli/cmd/client"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// createCmd represents the submit command
var createCmd = &cobra.Command{
	Use:   "create FABRIC_NETWORK_FILE",
	Args:  cobra.ExactArgs(1),
	Short: "Create a new FabricNetwork",
	Long: `Create a new FabricNetwork in K8S cluster:

Usage details and samples will be here`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, client := apiClient.NewClient()

		if err := submitNetwork(ctx, client, args); err != nil {
			fail("%v", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(createCmd)

	createCmd.Flags().BoolVar(&overwrite, "overwrite", false, "overwrite existing Secrets and ConfigMaps")
}

func submitNetwork(ctx context.Context, cl client.Client, args []string) error {
	if err := checkOtherFabricNetworksExist(ctx, cl, namespace); err != nil {
		return err
	}

	networkFile := args[0]
	network, err := loadFabricNetwork(networkFile)
	if err != nil {
		return err
	}

	if err = validateNewNetwork(ctx, cl, network); err != nil {
		return err
	}

	if network.Spec.Configtx.File != "" {
		if err := createOrUpdateConfigtxSecret(ctx, cl, network, networkFile); err != nil {
			return nil
		}
		network.Spec.Configtx.File = ""
		network.Spec.Configtx.Secret = "hlf-configtx.yaml"
	}

	if network.Spec.Chaincode.Folder != "" {
		if err := createOrUpdateChaincodeConfigMaps(ctx, cl, network, networkFile); err != nil {
			return nil
		}
		network.Spec.Chaincode.Folder = ""
	}

	network.Namespace = namespace
	if err := cl.Create(ctx, network); err != nil {
		return err
	}
	info("created new FabricNetwork %v in namespace %v", network.Name, network.Namespace)

	return nil
}

func validateNewNetwork(ctx context.Context, cl client.Client, network *v1alpha1.FabricNetwork) error {
	if network.Spec.Configtx.Secret == "" && network.Spec.Configtx.File == "" {
		return errors.New("Either Configtx.Secret or Configtx.File is required")
	}
	if network.Spec.Configtx.Secret != "" && network.Spec.Configtx.File != "" {
		return errors.New("Both Configtx.Secret and Configtx.File are provided, only either one is required")
	}
	if network.Spec.Configtx.Secret != "" && network.Spec.Configtx.Secret != "hlf-configtx.yaml" {
		return errors.New("Configtx.Secret should be named 'hlf-configtx.yaml' (for now)")
	}
	if network.Spec.Configtx.Secret == "" && !overwrite {
		exists, err := secretExists(ctx, cl, namespace, "hlf-configtx.yaml")
		if err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("A K8S Secret with name hlf-configtx.yaml already exists in namespace %v. Provide --overwrite flag to force overwrite", namespace)
		}
	}

	if network.Spec.Chaincode.Folder == "" {
		for _, chaincode := range network.Spec.Network.Chaincode {
			name := "hlf-chaincode--" + chaincode.Name
			exists, err := configMapExists(ctx, cl, namespace, name)
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("Chaincode ConfigMap %v does not exist in namespace %v", name, namespace)
			}
		}
	}

	//TODO other checks
	return nil
}

func checkOtherFabricNetworksExist(ctx context.Context, cl client.Client, namespace string) error {
	networkList := &v1alpha1.FabricNetworkList{}
	opts := []client.ListOption{
		client.InNamespace(namespace),
	}

	if err := cl.List(ctx, networkList, opts...); err != nil {
		return fmt.Errorf("failed to get FabricNetworkList: %v", err)
	}
	debug("got FabricNetworkList, size: %v", len(networkList.Items))

	if len(networkList.Items) != 0 {
		return fmt.Errorf("There is already %v FabricNetwork(s) in namespace %v. Only one is allowed", len(networkList.Items), namespace)
	}
	return nil
}

func createOrUpdateConfigtxSecret(ctx context.Context, cl client.Client, network *v1alpha1.FabricNetwork, networkFile string) error {
	var configtxFile = network.Spec.Configtx.File

	if filepath.IsAbs(configtxFile) {
		debug("configtx.file is absolute: %v", configtxFile)
	} else {
		configtxFile = filepath.Join(filepath.Dir(networkFile), configtxFile)
		debug("configtx.file is not absolute, merged path: %v", configtxFile)
	}

	bytes, err := ioutil.ReadFile(configtxFile)
	if err != nil {
		return err
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "hlf-configtx.yaml",
			Namespace: namespace,
			Labels: map[string]string{
				"raft.io/fabric-operator-cli-created-for": network.Name,
			},
		},
		Data: map[string][]byte{
			"configtx.yaml": bytes,
		},
	}

	exists, err := secretExists(ctx, cl, namespace, "hlf-configtx.yaml")
	if err != nil {
		return err
	}

	if exists {
		if err := cl.Update(ctx, secret); err != nil {
			fmt.Printf("configtx secret update failed: %v \n", err)
			return err
		}
		info("updated configtx secret")
	} else {
		if err := cl.Create(ctx, secret); err != nil {
			fmt.Printf("configtx secret creation failed: %v \n", err)
			return err
		}
		info("created configtx secret")
	}
	return nil
}

func createOrUpdateChaincodeConfigMaps(ctx context.Context, cl client.Client, network *v1alpha1.FabricNetwork, networkFile string) error {
	var chaincodeFolder = network.Spec.Chaincode.Folder

	if filepath.IsAbs(chaincodeFolder) {
		debug("chaincode.folder is absolute: %v", chaincodeFolder)
	} else {
		chaincodeFolder = filepath.Join(filepath.Dir(networkFile), chaincodeFolder)
		debug("chaincode.folder is not absolute, merged path: %v", chaincodeFolder)
	}

	for _, chaincode := range network.Spec.Network.Chaincode {
		name := "hlf-chaincode--" + strings.ToLower(chaincode.Name)
		exists, err := configMapExists(ctx, cl, namespace, name)
		if err != nil {
			return err
		}
		if exists && !overwrite {
			return fmt.Errorf("A K8S ConfigMap with name %v already exists in namespace %v. Provide --overwrite flag to force overwrite", name, namespace)
		}

		var buffer bytes.Buffer
		if err = tarArchive(chaincodeFolder, chaincode.Name, &buffer); err != nil {
			return err
		}

		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels: map[string]string{
					"chaincodeName": name,
					"type":          "chaincode",
					"raft.io/fabric-operator-cli-created-for": network.Name,
				},
			},
			BinaryData: map[string][]byte{
				chaincode.Name + ".tar": buffer.Bytes(),
			},
		}

		if exists {
			if err := cl.Update(ctx, configMap); err != nil {
				fmt.Printf("chaincode configMap %v update failed: %v \n", name, err)
				return err
			}
			info("updated chaincode configMap %v", name)
		} else {
			if err := cl.Create(ctx, configMap); err != nil {
				fmt.Printf("chaincode configMap %v creation failed: %v \n", name, err)
				return err
			}
			info("created chaincode configMap %v", name)
		}
	}

	return nil
}
