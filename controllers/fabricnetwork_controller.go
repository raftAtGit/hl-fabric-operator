/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/raftAtGit/hl-fabric-operator/api/v1alpha1"
)

// FabricNetworkReconciler reconciles a FabricNetwork object
type FabricNetworkReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=hyperledger.org,resources=fabricnetworks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hyperledger.org,resources=fabricnetworks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hyperledger.org,resources=fabricnetworks/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.0/pkg/reconcile
func (r *FabricNetworkReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	// log := r.Log.WithValues("fabricnetwork", request.NamespacedName)
	// var err error = nil

	r.Log.Info("Reconcile", "request", request)

	// Fetch the FabricNetwork instance
	network := &v1alpha1.FabricNetwork{}
	err := r.Get(ctx, request.NamespacedName, network)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("FabricNetwork resource not found")
			r.Log.Info("TODO delete resources")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		r.Log.Error(err, "Failed to get FabricNetwork")
		return ctrl.Result{}, err
	}

	r.Log.Info("Got the FabricNetwork", "network", network.Spec, "state", network.Status.State)
	// fmt.Printf("hlf-kube %T, %v \n", network.Spec.HlfKube, network.Spec.HlfKube.Object)

	switch network.Status.State {
	case "":
		rejected, err := r.checkOthersInNamespace(ctx, network)
		if err != nil {
			return ctrl.Result{}, err
		}
		if rejected {
			return ctrl.Result{}, nil
		}
		r.setState(ctx, network, v1alpha1.StateNew, "", "")
	case v1alpha1.StateNew:
		if err := r.validate(ctx, network); err != nil {
			r.Log.Error(err, "Validation failed")
			return ctrl.Result{}, err
		}
		if err := r.installHelmChart(ctx, network); err != nil {
			r.Log.Error(err, "Installing Helm chart failed")
			return ctrl.Result{}, err
		}
		if network.Spec.Topology.UseActualDomains {
			r.setState(ctx, network, v1alpha1.StateHelmChartNeedsUpdate, "", "")
		} else {
			r.setState(ctx, network, v1alpha1.StateHelmChartInstalled, "", "")
		}
	case v1alpha1.StateHelmChartNeedsUpdate:
		if err := r.updateHelmChart(ctx, network); err != nil {
			r.Log.Error(err, "Updating Helm chart failed")
			return ctrl.Result{}, err
		}
		r.setState(ctx, network, v1alpha1.StateHelmChartInstalled, "", "")
	case v1alpha1.StateHelmChartInstalled:
		ready, err := r.isHelmChartReady(ctx, network)
		if err != nil {
			// TODO if error is not found, maybe re-install helm chart?
			r.Log.Error(err, "Get Helm chart status failed")
			return ctrl.Result{}, err
		}
		if ready {
			r.setState(ctx, network, v1alpha1.StateHelmChartReady, "", "")
		} else {
			// reconcile until ready
			return ctrl.Result{Requeue: true}, nil
		}
	case v1alpha1.StateHelmChartReady:
		wfName, err := r.startChannelFlow(ctx, network)
		if err != nil {
			r.Log.Error(err, "Starting channel-flow failed")
			return ctrl.Result{}, err
		}
		r.Log.Info("Started channel-flow", "name", wfName)
		network.Status.Workflow = wfName
		r.setState(ctx, network, v1alpha1.StateChannelFlowSubmitted, "", "")
	case v1alpha1.StateChannelFlowSubmitted:
		status, err := r.getWorkflowStatus(ctx, network, network.Status.Workflow)
		if err != nil {
			r.Log.Error(err, "Failed to get workflow status")
			return ctrl.Result{}, err
		}
		r.Log.Info("Got workflow status", "status", status)
		switch status {
		case wfCompleted:
			r.setState(ctx, network, v1alpha1.StateChannelFlowCompleted, "", "")
		case wfFailed:
			r.setState(ctx, network, v1alpha1.StateFailed, "ChannelFlowFailed", "Channel flow failed")
			return ctrl.Result{Requeue: false}, nil
		case wfSubmitted:
			// reconcile until completed or failed
			return ctrl.Result{Requeue: true}, nil
		}
	}

	return ctrl.Result{Requeue: false}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FabricNetworkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.FabricNetwork{}).
		Complete(r)
}

func (r *FabricNetworkReconciler) checkOthersInNamespace(ctx context.Context, network *v1alpha1.FabricNetwork) (bool, error) {
	networkList := &v1alpha1.FabricNetworkList{}
	opts := []client.ListOption{
		client.InNamespace(network.Namespace),
	}

	if err := r.List(ctx, networkList, opts...); err != nil {
		r.Log.Error(err, "Failed to get FabricNetworkList")
		return false, err
	}
	r.Log.Info("Got FabricNetworkList", "size", len(networkList.Items))

	if len(networkList.Items) == 1 {
		return false, nil
	}

	allNew := true
	for _, n := range networkList.Items {
		if n.Status.State != "" {
			allNew = false
			break
		}
	}
	if allNew {
		r.Log.Info("All FabricNetworks are new, not rejecting this one")
		return false, nil
	}

	r.Log.Info("Rejecting FabricNetwork since there is more than one in the namespace")
	if err := r.setState(ctx, network, v1alpha1.StateRejected, "MoreThanOneInNamespace",
		"More than one FabricNetwork per namespace is not allowed"); err != nil {

		return false, err
	}

	// TODO write to events
	return true, nil
}

func (r *FabricNetworkReconciler) validate(ctx context.Context, network *v1alpha1.FabricNetwork) error {
	// TODO
	return nil
}

func (r *FabricNetworkReconciler) setState(ctx context.Context, network *v1alpha1.FabricNetwork, state v1alpha1.State, reason string, message string) error {
	network.Status.State = state
	network.Status.Reason = reason
	network.Status.Message = message

	if err := r.Status().Update(ctx, network); err != nil {
		r.Log.Error(err, "Unable to update FabricNetwork status")
		return err
	}
	return nil
}
