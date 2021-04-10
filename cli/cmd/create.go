package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
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
	Long: `Create a new FabricNetwork in Kubernetes cluster:

Create command first makes a client side validation. 

If there are references to local file system in the FabricNetwork CRD, 
create command creates the relevant Secrets and ConfigMaps and replaces 
local file system references with created Secrets and ConfigMaps 
and submits the FabricNetwork to Kubernetes.
`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, client := apiClient.NewClient()

		if err := submitNewNetwork(ctx, client, args); err != nil {
			fail("%v", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(createCmd)

	createCmd.Flags().BoolVar(&overwrite, "overwrite", false, "overwrite existing Secrets and ConfigMaps")
}

func submitNewNetwork(ctx context.Context, cl client.Client, args []string) error {
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
	if err := cl.Create(ctx, network); err != nil {
		return err
	}
	info("created new FabricNetwork %v in namespace %v", network.Name, network.Namespace)

	return nil
}

func validateNewNetwork(ctx context.Context, cl client.Client, network *v1alpha1.FabricNetwork) error {
	if network.Spec.Topology.TLSEnabled && !network.Spec.Topology.UseActualDomains {
		return errors.New("tlsEnabled is true but useActualDomains is false")
	}
	if network.Spec.Configtx.Secret == "" && network.Spec.Configtx.File == "" {
		return errors.New("Either configtx.secret or configtx.file is required")
	}
	if network.Spec.Configtx.Secret != "" && network.Spec.Configtx.File != "" {
		return errors.New("Both configtx.secret and configtx.file are provided, only either one is required")
	}
	if network.Spec.Configtx.Secret != "" && network.Spec.Configtx.Secret != "hlf-configtx.yaml" {
		return errors.New("configtx.secret should be named 'hlf-configtx.yaml'")
	}
	if network.Spec.Configtx.Secret == "" && !overwrite {
		exists, err := secretExists(ctx, cl, namespace, "hlf-configtx.yaml")
		if err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("A Secret with name hlf-configtx.yaml already exists in namespace %v. Provide --overwrite flag to force overwrite", namespace)
		}
	}

	if network.Spec.Configtx.Secret != "" {
		exists, err := secretExists(ctx, cl, namespace, network.Spec.Configtx.Secret)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("configtx.secret %v does not exist in namespace %v", network.Spec.Configtx.Secret, namespace)
		}
	}

	if network.Spec.Genesis.IsProvided() && !network.Spec.CryptoConfig.IsProvided() {
		return errors.New("Genesis block is provided but CryptoConfig is not provided. Genesis block will not match generated certificates")
	}
	if network.Spec.Genesis.Secret != "" && network.Spec.Genesis.File != "" {
		return errors.New("Both genesis.secret and genesis.file are provided, at most one is allowed")
	}
	if network.Spec.Genesis.Secret != "" {
		if network.Spec.Genesis.Secret != "hlf-genesis.block" {
			return errors.New("genesis.secret should be named 'hlf-genesis.block'")
		}
		exists, err := secretExists(ctx, cl, namespace, network.Spec.Genesis.Secret)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("genesis.secret %v does not exist in namespace %v", network.Spec.Genesis.Secret, namespace)
		}
	}

	for _, chaincode := range network.Spec.Network.Chaincodes {

		if network.Spec.Chaincode.Folder == "" {
			name := "hlf-chaincode--" + chaincode.Name
			exists, err := configMapExists(ctx, cl, namespace, name)
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("Chaincode ConfigMap %v does not exist in namespace %v", name, namespace)
			}
		}

		if network.Spec.Chaincode.Language == "" && chaincode.Language == "" {
			return fmt.Errorf("Global chaincode language is not specified. Language for chaincode %v is required", chaincode.Name)
		}

		if network.Spec.Chaincode.Version == "" && chaincode.Version == "" {
			return fmt.Errorf("Global chaincode version is not specified. Version for chaincode %v is required", chaincode.Name)
		}
	}

	if network.Spec.CryptoConfig.Secret != "" && network.Spec.CryptoConfig.Folder != "" {
		return errors.New("Both crypto-config.secret and crypto-config.folder are provided, at most one is allowed")
	}
	if network.Spec.CryptoConfig.Secret != "" {
		if network.Spec.CryptoConfig.Secret != "hlf-crypto-config" {
			return errors.New("crypto-config.secret should be named 'hlf-crypto-config'")
		}
		exists, err := secretExists(ctx, cl, namespace, network.Spec.CryptoConfig.Secret)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("crypto-config.secret %v does not exist in namespace %v", network.Spec.CryptoConfig.Secret, namespace)
		}
	}
	if network.Spec.CryptoConfig.Folder != "" && !overwrite {
		exists, err := secretExists(ctx, cl, namespace, "hlf-crypto-config")
		if err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("A Secret with name hlf-crypto-config already exists in namespace %v. Provide --overwrite flag to force overwrite", namespace)
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
		info("updated configtx Secret hlf-configtx.yaml")
	} else {
		if err := cl.Create(ctx, secret); err != nil {
			fmt.Printf("configtx secret creation failed: %v \n", err)
			return err
		}
		info("created configtx Secret hlf-configtx.yaml")
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

	for _, chaincode := range network.Spec.Network.Chaincodes {
		debug("creating %v", strings.ToLower(chaincode.Name))
		name := "hlf-chaincode--" + strings.ToLower(chaincode.Name)
		exists, err := configMapExists(ctx, cl, namespace, name)
		if err != nil {
			return err
		}
		if exists && !overwrite {
			return fmt.Errorf("A ConfigMap with name %v already exists in namespace %v. Provide --overwrite flag to force overwrite", name, namespace)
		}

		var buffer bytes.Buffer
		if err = compress(chaincodeFolder, chaincode.Name, &buffer); err != nil {
			info("couldnt TAR archive chaincode.folder %v", chaincodeFolder)
			return err
		}

		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels: map[string]string{
					"chaincodeName": chaincode.Name,
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
				fmt.Printf("chaincode ConfigMap %v update failed: %v \n", name, err)
				return err
			}
			info("updated chaincode ConfigMap %v", name)
		} else {
			if err := cl.Create(ctx, configMap); err != nil {
				fmt.Printf("chaincode ConfigMap %v creation failed: %v \n", name, err)
				return err
			}
			info("created chaincode ConfigMap %v", name)
		}
	}

	return nil
}

func createOrUpdateCryptoConfigSecret(ctx context.Context, cl client.Client, network *v1alpha1.FabricNetwork, networkFile string) error {
	var folder = network.Spec.CryptoConfig.Folder

	if filepath.IsAbs(folder) {
		debug("crypto-config.folder is absolute: %v", folder)
	} else {
		folder = filepath.Join(filepath.Dir(networkFile), folder)
		debug("crypto-config is not absolute, merged path: %v", folder)
	}

	if _, err := os.Stat(folder); os.IsNotExist(err) {
		return fmt.Errorf("crypto-config.folder %v does not exist", folder)
	}

	var buffer bytes.Buffer
	if err := compress(folder, "", &buffer); err != nil {
		info("couldnt TAR archive crypto-config.folder %v", folder)
		return err
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "hlf-crypto-config",
			Namespace: namespace,
			Labels: map[string]string{
				"raft.io/fabric-operator-cli-created-for": network.Name,
			},
		},
		Data: map[string][]byte{
			"crypto-config": buffer.Bytes(),
		},
	}

	exists, err := secretExists(ctx, cl, namespace, "hlf-crypto-config")
	if err != nil {
		return err
	}

	if exists {
		if err := cl.Update(ctx, secret); err != nil {
			fmt.Printf("crypto-config secret update failed: %v \n", err)
			return err
		}
		info("updated crypto-config Secret hlf-crypto-config")
	} else {
		if err := cl.Create(ctx, secret); err != nil {
			fmt.Printf("crypto-config secret creation failed: %v \n", err)
			return err
		}
		info("created crypto-config Secret hlf-crypto-config")
	}
	return nil
}
