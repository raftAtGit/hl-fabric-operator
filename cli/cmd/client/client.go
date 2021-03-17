package client

import (
	"context"
	"fmt"
	"os"

	"github.com/raftAtGit/hl-fabric-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	utilRuntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoScheme "k8s.io/client-go/kubernetes/scheme"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

// NewClient creates and configures a new runtime client and returns it
func NewClient() (context.Context, runtimeClient.Client) {
	scheme := runtime.NewScheme()

	utilRuntime.Must(clientgoScheme.AddToScheme(scheme))
	utilRuntime.Must(v1alpha1.AddToScheme(scheme))

	config, err := config.GetConfig()
	if err != nil {
		fmt.Printf("Unable to get kubeconfig: %v \n", err)
		os.Exit(1)
	}

	client, err := runtimeClient.New(config, runtimeClient.Options{Scheme: scheme})
	if err != nil {
		fmt.Printf("Failed to create client: %v \n", err)
		os.Exit(1)
	}

	return context.Background(), client
}
