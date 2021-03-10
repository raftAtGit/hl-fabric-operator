package cmd

import (
	"context"

	"github.com/raftAtGit/hl-fabric-operator/api/v1alpha1"
	apiClient "github.com/raftAtGit/hl-fabric-operator/cli/cmd/client"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// deleteCmd represents the delete command
var deleteCmd = &cobra.Command{
	Use:   "delete FABRIC_NETWORK_NAME",
	Args:  cobra.ExactArgs(1),
	Short: "Delete a FabricNetwork",
	Long:  `Delete a FabricNetwork from K8S cluster`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, client := apiClient.NewClient()

		if err := deleteNetwork(ctx, client, args); err != nil {
			fail("%v", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(deleteCmd)

	deleteCmd.Flags().BoolVarP(&keepResources, "keep-resources", "k", false, "do not delete Secrets and ConfigMaps create by CLI")
}

func deleteNetwork(ctx context.Context, cl client.Client, args []string) error {
	name := args[0]

	network := &v1alpha1.FabricNetwork{}
	if err := cl.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, network); err != nil {
		return err
	}
	debug("Got FabricNetwork: %v", network.Name)

	if err := cl.Delete(ctx, network); err != nil {
		return err
	}
	info("deleted FabricNetwork %v from namespace %v", network.Name, network.Namespace)

	if keepResources {
		return nil
	}

	secretList := &corev1.SecretList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels(map[string]string{"raft.io/fabric-operator-cli-created-for": name}),
	}

	if err := cl.List(ctx, secretList, listOpts...); err != nil {
		return err
	}
	debug("got SecretList size: %v", len(secretList.Items))

	for _, secret := range secretList.Items {
		if err := cl.Delete(ctx, &secret); err != nil {
			return err
		}
		info("deleted Secret %v", secret.Name)
	}

	configMapList := &corev1.ConfigMapList{}

	if err := cl.List(ctx, configMapList, listOpts...); err != nil {
		return err
	}
	debug("got ConfigMapList size: %v", len(configMapList.Items))

	for _, configMap := range configMapList.Items {
		if err := cl.Delete(ctx, &configMap); err != nil {
			return err
		}
		info("deleted ConfigMap %v", configMap.Name)
	}

	return nil
}
