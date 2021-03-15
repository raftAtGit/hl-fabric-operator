package controllers

import (
	"context"
	"reflect"
	"time"

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

// struct to keep trackof change in FabricNetwork
type change struct {
	Topology          bool
	Channel           bool
	Chaincode         bool
	Chaincodes        []string
	OrdererOrgs       bool
	PeerOrgs          bool
	PeerCountIncrease bool
	PeerCountDecrease bool
	Version           bool
}

func (c change) areThereAnyChanges() bool {
	return c.Topology || c.Channel || c.Chaincode
}

func (c change) needsCertificateUpdate() bool {
	return c.OrdererOrgs || c.PeerOrgs || c.PeerCountIncrease
}

// +kubebuilder:rbac:groups=hyperledger.org,resources=fabricnetworks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hyperledger.org,resources=fabricnetworks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hyperledger.org,resources=fabricnetworks/finalizers,verbs=update

// for Helm
// +kubebuilder:rbac:groups="",resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments;statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch;create;update;patch;delete

// for Argo
// +kubebuilder:rbac:groups=argoproj.io,resources=workflows,verbs=get;list;watch;create;update;patch;delete

// SetupWithManager sets up the controller with the Manager.
func (r *FabricNetworkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Log.Info("SetupWithManager", "settings", settings)

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.FabricNetwork{}).
		Complete(r)
}

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
			r.Log.Info("FabricNetwork resource not found, deleting resources")

			if err = r.maybeUninstallHelmChart(ctx, request.NamespacedName.Namespace, request.NamespacedName.Name); err != nil {
				r.Log.Error(err, "Failed to uninstall Helm chart")
			}
			if err = r.deleteWorkflows(ctx, request.NamespacedName.Namespace, request.NamespacedName.Name); err != nil {
				r.Log.Error(err, "Failed to delete workflows")
			}

			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		r.Log.Error(err, "Failed to get FabricNetwork")
		return ctrl.Result{}, err
	}

	changes := getChanges(network)
	r.Log.Info("Got the FabricNetwork", "network", network.Name, "state", network.Status.State, "changes", changes)

	if network.Spec.ForceState != "" {
		r.Log.Info("Setting the state to forced state", "ForceState", network.Spec.ForceState)

		if err := r.saveStatus(ctx, network, v1alpha1.FabricNetworkStatus{
			State:   network.Spec.ForceState,
			Message: "State is forced",
		}); err != nil {
			return ctrl.Result{}, err
		}

		network.Spec.ForceState = ""
		if err := r.Update(ctx, network); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{Requeue: true}, nil
	}

	if err := r.maybeReconstructHelmChart(ctx, network); err != nil {
		r.Log.Error(err, "Reconstructing Helm chart failed")
		return ctrl.Result{}, err
	}

	switch network.Status.State {

	case v1alpha1.StateRejected:
		return ctrl.Result{Requeue: false}, err
	case v1alpha1.StateFailed:
		return ctrl.Result{Requeue: false}, err
	case v1alpha1.StateInvalid:
		if err := r.validate(ctx, network); err != nil {
			r.Log.Error(err, "Validation failed")
			return ctrl.Result{Requeue: false}, err
		}
		r.saveStatus(ctx, network, v1alpha1.FabricNetworkStatus{State: ""})
		return ctrl.Result{}, nil

	case "":
		if err := r.validate(ctx, network); err != nil {
			r.Log.Error(err, "Validation failed")
			return ctrl.Result{Requeue: false}, err
		}
		rejected, err := r.checkOthersInNamespace(ctx, network)
		if err != nil {
			return ctrl.Result{}, err
		}
		if rejected {
			return ctrl.Result{}, nil
		}
		network.Status.Topology = network.Spec.Topology
		network.Status.Channels = network.Spec.Network.Channels
		network.Status.Chaincode = network.Spec.Chaincode
		network.Status.Chaincodes = network.Spec.Network.Chaincodes
		r.saveStatus(ctx, network, v1alpha1.FabricNetworkStatus{State: v1alpha1.StateNew})

	case v1alpha1.StateNew:
		if err = r.maybeUninstallHelmChart(ctx, request.NamespacedName.Namespace, request.NamespacedName.Name); err != nil {
			r.Log.Error(err, "Failed to uninstall Helm chart")
			return ctrl.Result{}, err
		}
		if err = r.deleteWorkflows(ctx, request.NamespacedName.Namespace, request.NamespacedName.Name); err != nil {
			r.Log.Error(err, "Failed to delete workflows")
		}
		if err := r.prepareHelmChart(ctx, network); err != nil {
			r.Log.Error(err, "Preparing Helm chart failed")
			return ctrl.Result{}, err
		}
		if err := r.installHelmChart(ctx, network); err != nil {
			r.Log.Error(err, "Installing Helm chart failed")
			return ctrl.Result{}, err
		}
		if network.Spec.Topology.UseActualDomains {
			r.saveStatus(ctx, network, v1alpha1.FabricNetworkStatus{State: v1alpha1.StateHelmChartNeedsUpdate})
		} else {
			r.saveStatus(ctx, network, v1alpha1.FabricNetworkStatus{State: v1alpha1.StateHelmChartInstalled})
		}

	case v1alpha1.StateHelmChartNeedsUpdate:
		if err := r.updateHelmChart(ctx, network); err != nil {
			r.Log.Error(err, "Updating Helm chart failed")
			return ctrl.Result{}, err
		}
		r.saveStatus(ctx, network, v1alpha1.FabricNetworkStatus{State: v1alpha1.StateHelmChartInstalled})

	case v1alpha1.StateHelmChartNeedsDoubleUpdate:
		if err := r.updateHelmChart(ctx, network); err != nil {
			r.Log.Error(err, "Updating Helm chart failed")
			return ctrl.Result{}, err
		}
		if network.Spec.Topology.UseActualDomains {
			if err := r.updateHelmChart(ctx, network); err != nil {
				r.Log.Error(err, "Updating Helm chart failed")
				return ctrl.Result{}, err
			}
		}
		r.saveStatus(ctx, network, v1alpha1.FabricNetworkStatus{State: v1alpha1.StateHelmChartInstalled})

	case v1alpha1.StateHelmChartInstalled:
		ready, err := r.isHelmChartReady(ctx, network)
		if err != nil {
			// TODO if error is not found, maybe re-install helm chart?
			r.Log.Error(err, "Get Helm chart status failed")
			return ctrl.Result{}, err
		}
		if ready {
			r.saveStatus(ctx, network, v1alpha1.FabricNetworkStatus{State: v1alpha1.StateHelmChartReady})
		} else {
			// reconcile until ready
			return ctrl.Result{RequeueAfter: time.Second * 10}, nil
		}

	case v1alpha1.StateHelmChartReady:
		if changes.Topology {
			r.Log.Info("Topology changed, starting from scratch", "name", request.NamespacedName)

			network.Status.Topology = network.Spec.Topology
			network.Status.Channels = network.Spec.Network.Channels
			network.Status.Chaincode = network.Spec.Chaincode
			network.Status.Chaincodes = network.Spec.Network.Chaincodes
			if err := r.saveStatus(ctx, network, v1alpha1.FabricNetworkStatus{State: v1alpha1.StateNew}); err != nil {
				return ctrl.Result{}, err
			}
		}
		switch network.Status.NextFlow {
		case "":
			wfName, err := r.startChannelFlow(ctx, network)
			if err != nil {
				r.Log.Error(err, "Starting channel-flow failed")
				return ctrl.Result{}, err
			}
			r.Log.Info("Started channel-flow", "name", wfName)
			r.saveStatus(ctx, network, v1alpha1.FabricNetworkStatus{
				State:    v1alpha1.StateChannelFlowSubmitted,
				Workflow: wfName,
			})

		case v1alpha1.NextFlowNone:
			r.Log.Info("Status.NextFlow is None. Setting state to ready")
			network.Status.NextFlow = ""
			r.saveStatus(ctx, network, v1alpha1.FabricNetworkStatus{State: v1alpha1.StateReady, Message: "HL Fabric Network is ready"})
			return ctrl.Result{Requeue: false}, nil

		case v1alpha1.NextFlowPeerOrgFlow:
			wfName, err := r.startPeerOrgFlow(ctx, network)
			if err != nil {
				r.Log.Error(err, "Starting peer-org-flow failed")
				return ctrl.Result{}, err
			}
			r.Log.Info("Started peer-org-flow", "name", wfName)
			r.saveStatus(ctx, network, v1alpha1.FabricNetworkStatus{
				State:    v1alpha1.StatePeerOrgFlowSubmitted,
				Workflow: wfName,
			})
		}

	case v1alpha1.StateChannelFlowSubmitted:
		status, err := r.getWorkflowStatus(ctx, network, network.Status.Workflow)
		if err != nil {
			r.Log.Error(err, "Failed to get workflow status")
			return ctrl.Result{}, err
		}
		r.Log.Info("Got workflow status", "status", status)
		switch status {
		case wfCompleted:
			r.saveStatus(ctx, network, v1alpha1.FabricNetworkStatus{State: v1alpha1.StateChannelFlowCompleted})
		case wfFailed:
			r.saveStatus(ctx, network, v1alpha1.FabricNetworkStatus{State: v1alpha1.StateFailed, Message: "channel-flow failed"})
			return ctrl.Result{Requeue: false}, nil
		case wfSubmitted:
			// reconcile until completed or failed
			return ctrl.Result{RequeueAfter: time.Second * 10}, nil
		}

	case v1alpha1.StateChannelFlowCompleted:
		wfName, err := r.startChaincodeFlow(ctx, network, []string{})
		if err != nil {
			r.Log.Error(err, "Starting chaincode-flow failed")
			return ctrl.Result{}, err
		}
		r.Log.Info("Started chaincode-flow", "name", wfName)

		r.saveStatus(ctx, network, v1alpha1.FabricNetworkStatus{
			State:    v1alpha1.StateChaincodeFlowSubmitted,
			Workflow: wfName,
		})

	case v1alpha1.StateChaincodeFlowSubmitted:
		status, err := r.getWorkflowStatus(ctx, network, network.Status.Workflow)
		if err != nil {
			r.Log.Error(err, "Failed to get workflow status")
			return ctrl.Result{}, err
		}
		r.Log.Info("Got workflow status", "status", status)
		switch status {
		case wfCompleted:
			r.saveStatus(ctx, network, v1alpha1.FabricNetworkStatus{State: v1alpha1.StateChaincodeFlowCompleted})
		case wfFailed:
			r.saveStatus(ctx, network, v1alpha1.FabricNetworkStatus{State: v1alpha1.StateFailed, Message: "chaincode-flow failed"})
			return ctrl.Result{Requeue: false}, nil
		case wfSubmitted:
			// reconcile until completed or failed
			return ctrl.Result{RequeueAfter: time.Second * 10}, nil
		}

	case v1alpha1.StateChaincodeFlowCompleted:
		r.saveStatus(ctx, network, v1alpha1.FabricNetworkStatus{State: v1alpha1.StateReady, Message: "HL Fabric Network is ready"})
		return ctrl.Result{Requeue: false}, nil

	case v1alpha1.StatePeerOrgFlowSubmitted:
		status, err := r.getWorkflowStatus(ctx, network, network.Status.Workflow)
		if err != nil {
			r.Log.Error(err, "Failed to get workflow status")
			return ctrl.Result{}, err
		}
		r.Log.Info("Got workflow status", "status", status)
		switch status {
		case wfCompleted:
			r.saveStatus(ctx, network, v1alpha1.FabricNetworkStatus{State: v1alpha1.StatePeerOrgFlowCompleted})
		case wfFailed:
			r.saveStatus(ctx, network, v1alpha1.FabricNetworkStatus{State: v1alpha1.StateFailed, Message: "peer-org-flow failed"})
			return ctrl.Result{Requeue: false}, nil
		case wfSubmitted:
			// reconcile until completed or failed
			return ctrl.Result{RequeueAfter: time.Second * 10}, nil
		}

	case v1alpha1.StatePeerOrgFlowCompleted:
		wfName, err := r.startChannelFlow(ctx, network)
		if err != nil {
			r.Log.Error(err, "Starting channel-flow failed")
			return ctrl.Result{}, err
		}
		r.Log.Info("Started channel-flow", "name", wfName)

		r.saveStatus(ctx, network, v1alpha1.FabricNetworkStatus{
			State:    v1alpha1.StateChannelFlowSubmitted,
			Workflow: wfName,
		})

	case v1alpha1.StateReady:
		if changes.areThereAnyChanges() {
			r.Log.Info("There are changes in FabricNetwork, will recreate values files", "changes", changes)
			if err := r.createValuesFiles(ctx, network); err != nil {
				return ctrl.Result{}, err
			}
			network.Status.Topology = network.Spec.Topology
			network.Status.Channels = network.Spec.Network.Channels
			network.Status.Chaincode = network.Spec.Chaincode
			network.Status.Chaincodes = network.Spec.Network.Chaincodes
		}
		if changes.Topology {
			if changes.needsCertificateUpdate() {
				r.Log.Info("Will download or extend certificates")
				if err := r.extendOrDownloadCertificates(ctx, network); err != nil {
					return ctrl.Result{}, err
				}
			}
			if changes.OrdererOrgs {
				r.Log.Error(nil, "Orderer organizations changed in topology. Will update Helm chart. But new Orderers cannot be functional automatically")
				if !changes.PeerOrgs {
					r.Log.Info("Peer organizations didnt change. Setting NextFlow to None")
					network.Status.NextFlow = v1alpha1.NextFlowNone
					if err := r.saveStatus(ctx, network, v1alpha1.FabricNetworkStatus{State: v1alpha1.StateHelmChartNeedsDoubleUpdate}); err != nil {
						return ctrl.Result{}, err
					}
					return ctrl.Result{}, nil
				}
			}
			if changes.PeerOrgs {
				r.Log.Info("Peer organizations changed. Will update Helm chart. Setting NextFlow to PeerOrgFlow")
				network.Status.NextFlow = v1alpha1.NextFlowPeerOrgFlow
				if err := r.saveStatus(ctx, network, v1alpha1.FabricNetworkStatus{State: v1alpha1.StateHelmChartNeedsDoubleUpdate}); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{}, nil
			}
			if changes.PeerCountIncrease {
				r.Log.Info("Peer counts increased. Will update Helm chart")
				if err := r.saveStatus(ctx, network, v1alpha1.FabricNetworkStatus{State: v1alpha1.StateHelmChartNeedsUpdate}); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{}, nil
			}
			if changes.PeerCountDecrease || changes.Version {
				r.Log.Info("Peer counts decreased and/or FabricVersion changed. Will update Helm chart. Setting NextFlow to None")
				network.Status.NextFlow = v1alpha1.NextFlowNone
				if err := r.saveStatus(ctx, network, v1alpha1.FabricNetworkStatus{State: v1alpha1.StateHelmChartNeedsUpdate}); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{}, nil
			}
			// still here
			r.Log.Error(nil, "Unexpected change in topology", "changes", changes, "spec.topology", network.Spec.Topology, "status.topology", network.Status.Topology)
		}
		if changes.Channel {
			r.Log.Info("Channels changed, will run channel-flow", "include", changes.Chaincodes)

			if err := r.createNetworkValuesFile(ctx, network); err != nil {
				return ctrl.Result{}, err
			}
			wfName, err := r.startChannelFlow(ctx, network)
			if err != nil {
				r.Log.Error(err, "Starting channel-flow failed")
				return ctrl.Result{}, err
			}
			r.Log.Info("Started channel-flow", "name", wfName)

			r.saveStatus(ctx, network, v1alpha1.FabricNetworkStatus{
				State:    v1alpha1.StateChannelFlowSubmitted,
				Workflow: wfName,
			})
			return ctrl.Result{}, nil
		}

		if changes.Chaincode {
			r.Log.Info("Chaincodes changed, will run chaincode-flow", "include", changes.Chaincodes)

			if err := r.createNetworkValuesFile(ctx, network); err != nil {
				return ctrl.Result{}, err
			}
			wfName, err := r.startChaincodeFlow(ctx, network, changes.Chaincodes)
			if err != nil {
				r.Log.Error(err, "Starting chaincode-flow failed")
				return ctrl.Result{}, err
			}
			r.Log.Info("Started chaincode-flow", "name", wfName)

			r.saveStatus(ctx, network, v1alpha1.FabricNetworkStatus{
				State:    v1alpha1.StateChaincodeFlowSubmitted,
				Workflow: wfName,
			})
			return ctrl.Result{}, nil
		}
	default:
		r.Log.Error(nil, "Unknown state", "state", network.Status.State)
	}

	return ctrl.Result{Requeue: false}, nil
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
	if err := r.saveStatus(ctx, network, v1alpha1.FabricNetworkStatus{
		State:   v1alpha1.StateRejected,
		Message: "More than one FabricNetwork per namespace is not allowed",
	}); err != nil {

		return false, err
	}

	// TODO write to events
	return true, nil
}

func (r *FabricNetworkReconciler) validate(ctx context.Context, network *v1alpha1.FabricNetwork) error {
	// TODO
	return nil
}

func (r *FabricNetworkReconciler) saveStatus(ctx context.Context, network *v1alpha1.FabricNetwork, status v1alpha1.FabricNetworkStatus) error {
	network.Status.State = status.State
	network.Status.Message = status.Message
	network.Status.Workflow = status.Workflow

	if err := r.Status().Update(ctx, network); err != nil {
		r.Log.Error(err, "Unable to update FabricNetwork status")
		return err
	}
	return nil
}

func getChanges(network *v1alpha1.FabricNetwork) change {

	ccSpecChanged := !reflect.DeepEqual(network.Spec.Chaincode, network.Status.Chaincode)

	ch := change{
		Topology: !reflect.DeepEqual(network.Spec.Topology, network.Status.Topology),
		Channel:  !reflect.DeepEqual(network.Spec.Network.Channels, network.Status.Channels),
		// TODO we also need to check if any peer count is increased
		Chaincode: ccSpecChanged || !reflect.DeepEqual(network.Spec.Network.Chaincodes, network.Status.Chaincodes),
	}

	// if global chaincode spec changed or number of chaincoded changed, we will run chaincode-flow for all of them
	// othewise we will run chaincode flow for only changed chaincodes
	// TODO this can be further optimized
	if ch.Chaincode {
		if !ccSpecChanged && len(network.Spec.Network.Chaincodes) == len(network.Status.Chaincodes) {
			for i, cc1 := range network.Spec.Network.Chaincodes {
				cc2 := network.Status.Chaincodes[i]
				if cc1.Name != cc2.Name {
					// chaincode name at same index changed, run chaincode-flow for all
					ch.Chaincodes = []string{}
					break
				}
				if !reflect.DeepEqual(cc1, cc2) {
					ch.Chaincodes = append(ch.Chaincodes, cc1.Name)
				}
			}
		}
	}

	if ch.Topology {
		ch.Version = network.Spec.Topology.Version != network.Status.Topology.Version

		ch.OrdererOrgs = !reflect.DeepEqual(network.Spec.Topology.OrdererOrgNames(), network.Status.Topology.OrdererOrgNames())
		ch.PeerOrgs = !reflect.DeepEqual(network.Spec.Topology.PeerOrgNames(), network.Status.Topology.PeerOrgNames())

		for _, p := range network.Spec.Topology.PeerOrgs {
			p2 := network.Status.Topology.PeerOrgByName(p.Name)
			if p2 != nil {
				if p.PeerCount > p2.PeerCount {
					ch.PeerCountIncrease = true
				}
				if p.PeerCount < p2.PeerCount {
					ch.PeerCountDecrease = true
				}
			}
		}
	}

	return ch
}
