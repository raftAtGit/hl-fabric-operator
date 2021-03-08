package cmd

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/gosuri/uitable"
	"github.com/raftAtGit/hl-fabric-operator/api/v1alpha1"
	apiClient "github.com/raftAtGit/hl-fabric-operator/cli/cmd/client"
	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// listCmd represents the list command
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List FabricNetworks",
	Long:  `List FabricNetworks in K8S cluster`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, client := apiClient.NewClient()
		executeList(ctx, client, args)
	},
}

func init() {
	rootCmd.AddCommand(listCmd)

	listCmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "list FabricNetworks across all namespaces")
}

func executeList(ctx context.Context, cl client.Client, args []string) {
	ns := namespace
	if allNamespaces {
		ns = ""
	}

	opts := []client.ListOption{
		client.InNamespace(ns),
	}

	networkList := &v1alpha1.FabricNetworkList{}
	if err := cl.List(ctx, networkList, opts...); err != nil {
		log.Fatalf("Failed to get FabricNetworkList: %v", err)
	}
	debug("Got FabricNetworkList, size: %v", len(networkList.Items))

	if len(networkList.Items) == 0 {
		if allNamespaces {
			fmt.Println("No FabricNetwork found.")
		} else {
			fmt.Printf("No FabricNetwork found in %v namespace.\n", namespace)
		}
		return
	}

	table := uitable.New()
	if allNamespaces {
		table.AddRow("NAMESPACE", "NAME", "STATUS")
		for _, n := range networkList.Items {
			table.AddRow(n.Namespace, n.Name, n.Status.State)
		}
	} else {
		table.AddRow("NAME", "STATUS")
		for _, n := range networkList.Items {
			table.AddRow(n.Name, n.Status.State)
		}
	}
	if err := encodeTable(os.Stdout, table); err != nil {
		log.Fatalf("Unable to write table: %v", err)
	}

}
