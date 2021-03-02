package controllers

import (
	wfv1 "github.com/argoproj/argo/v3/pkg/apis/workflow/v1alpha1"

	argoCommon "github.com/argoproj/argo/v3/workflow/common"
	argoJson "github.com/argoproj/pkg/json"
)

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
