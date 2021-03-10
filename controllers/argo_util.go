package controllers

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	wf "github.com/argoproj/argo/v3/pkg/apiclient/workflow"
	wfv1 "github.com/argoproj/argo/v3/pkg/apis/workflow/v1alpha1"

	"github.com/argoproj/argo/v3/cmd/argo/commands/client"
	argoCommon "github.com/argoproj/argo/v3/workflow/common"
	argoJson "github.com/argoproj/pkg/json"

	"github.com/raftAtGit/hl-fabric-operator/api/v1alpha1"
)

type wfStatus string

const (
	wfSubmitted wfStatus = "Submitted"
	wfCompleted wfStatus = "Completed"
	wfFailed    wfStatus = "Failed"
)

func (r *FabricNetworkReconciler) startChannelFlow(ctx context.Context, network *v1alpha1.FabricNetwork) (string, error) {
	wfManifest, err := r.renderChannelFlow(ctx, network)
	if err != nil {
		r.Log.Error(err, "Rendering channel-flow failed")
		return "", err
	}

	return r.submitWorkflow(network, wfManifest)
}

func (r *FabricNetworkReconciler) startChaincodeFlow(ctx context.Context, network *v1alpha1.FabricNetwork) (string, error) {
	wfManifest, err := r.renderChaincodeFlow(ctx, network)
	if err != nil {
		r.Log.Error(err, "Rendering chaincode-flow failed")
		return "", err
	}

	return r.submitWorkflow(network, wfManifest)
}

func (r *FabricNetworkReconciler) submitWorkflow(network *v1alpha1.FabricNetwork, wfManifest string) (string, error) {

	wfs, err := r.unmarshalWorkflows([]byte(wfManifest), true)
	if err != nil {
		r.Log.Error(err, "Unmarshaling workflow failed")
		return "", err
	}
	if len(wfs) != 1 {
		return "", fmt.Errorf("Rendered template has %d workflows, expected exactly one", len(wfs))
	}

	if wfs[0].Labels == nil {
		wfs[0].Labels = make(map[string]string)
	}
	wfs[0].Labels["raft.io/fabric-operator-created-for"] = network.Name

	ctx, apiClient := client.NewAPIClient()
	serviceClient := apiClient.NewWorkflowServiceClient()

	options := &metav1.CreateOptions{}

	created, err := serviceClient.CreateWorkflow(ctx, &wf.WorkflowCreateRequest{
		Namespace:     network.Namespace,
		Workflow:      &wfs[0],
		ServerDryRun:  false,
		CreateOptions: options,
	})

	if err != nil {
		r.Log.Error(err, "Failed to submit workflow")
		return "", err
	}
	r.Log.Info("Submitted workflow", "name", created.ObjectMeta.Name)
	return created.ObjectMeta.Name, nil
}

func (r *FabricNetworkReconciler) getWorkflowStatus(ctx context.Context, network *v1alpha1.FabricNetwork, wfName string) (wfStatus, error) {
	ctx, apiClient := client.NewAPIClient()
	serviceClient := apiClient.NewWorkflowServiceClient()

	workflow, err := serviceClient.GetWorkflow(ctx, &wf.WorkflowGetRequest{
		Namespace: network.Namespace,
		Name:      wfName,
	})

	if err != nil {
		r.Log.Error(err, "Failed to get workflow")
		return "", err
	}
	r.Log.Info("Got workflow", "name", wfName, "phase", workflow.Status.Phase)

	switch workflow.Status.Phase {
	case wfv1.WorkflowSucceeded:
		return wfCompleted, nil
	case wfv1.WorkflowFailed:
		return wfFailed, nil
	default:
		return wfSubmitted, nil
	}
}

// unmarshalWorkflows unmarshals the input bytes as either json or yaml
func (r *FabricNetworkReconciler) unmarshalWorkflows(wfBytes []byte, strict bool) ([]wfv1.Workflow, error) {
	var wf wfv1.Workflow
	var jsonOpts []argoJson.JSONOpt
	if strict {
		jsonOpts = append(jsonOpts, argoJson.DisallowUnknownFields)
	}
	err := argoJson.Unmarshal(wfBytes, &wf, jsonOpts...)
	if err == nil {
		return []wfv1.Workflow{wf}, nil
	}
	yamlWfs, err := argoCommon.SplitWorkflowYAMLFile(wfBytes, strict)
	if err != nil {
		r.Log.Error(err, "Failed to parse workflow")
		return nil, err
	}
	return yamlWfs, nil
}

func (r *FabricNetworkReconciler) deleteWorkflows(ctx context.Context, namespace string, name string) error {
	wfList := &wfv1.WorkflowList{}
	listOpts := []runtimeClient.ListOption{
		runtimeClient.InNamespace(namespace),
		runtimeClient.MatchingLabels(map[string]string{"raft.io/fabric-operator-created-for": name}),
	}

	if err := r.List(ctx, wfList, listOpts...); err != nil {
		r.Log.Error(err, "Failed to get WorkflowList")
		return err
	}

	for _, wf := range wfList.Items {
		if err := r.Delete(ctx, &wf); err != nil {
			return err
		}
		r.Log.Info("deleted workflow", "workflow", wf.Name)
	}

	return nil
}
