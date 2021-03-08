package client

import (
	"context"
	"log"

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

	client, err := runtimeClient.New(config.GetConfigOrDie(), runtimeClient.Options{Scheme: scheme})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	return context.Background(), client
}
