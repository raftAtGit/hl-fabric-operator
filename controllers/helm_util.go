package controllers

import (
	"context"
	"io/ioutil"
	"log"
	"os"
	"strconv"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/getter"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/raftAtGit/hl-fabric-operator/api/v1alpha1"
)

type HelmValues struct {
	HostAliases []corev1.HostAlias `json:"hostAliases,omitempty"`
}

func (r *FabricNetworkReconciler) installHelmChart(ctx context.Context, network *v1alpha1.FabricNetwork) error {
	// TODO mutex this.
	os.Setenv("HELM_NAMESPACE", network.Namespace)
	settings := cli.New()
	actionConfig := new(action.Configuration)

	if err := actionConfig.Init(settings.RESTClientGetter(), network.Namespace, "secret", log.Printf); err != nil {
		r.Log.Error(err, "Couldnt init")
		return err
	}

	chart, err := loader.Load(getChartDir(network))
	if err != nil {
		return err
	}

	if err := r.createValuesFiles(ctx, network); err != nil {
		r.Log.Error(err, "Couldnt create values files")
		return err
	}

	extraValues := []string{}
	if network.Spec.Topology.TlsEnabled {
		extraValues = []string{
			"peer.launchPods=false",
			"orderer.launchPods=false",
		}
	}
	values, err := r.getChartValues(network, settings, extraValues...)
	if err != nil {
		return err
	}

	client := action.NewInstall(actionConfig)
	client.ReleaseName = "hlf-kube"
	client.Namespace = network.Namespace

	r.Log.Info("Creating release", "namespace", network.Namespace)
	// TODO for Kafka orderer, wait is not reliable. how to handle this?
	release, err := client.Run(chart, values)
	if err != nil {
		return err
	}
	r.Log.Info("created release", "name", release.Name, "version", release.Version, "namespace", network.Namespace)

	return nil
}

func (r *FabricNetworkReconciler) updateHelmChart(ctx context.Context, network *v1alpha1.FabricNetwork) error {
	// TODO mutex this.
	os.Setenv("HELM_NAMESPACE", network.Namespace)
	settings := cli.New()
	actionConfig := new(action.Configuration)

	if err := actionConfig.Init(settings.RESTClientGetter(), network.Namespace, "secret", log.Printf); err != nil {
		r.Log.Error(err, "Couldnt init")
		return err
	}

	chart, err := loader.Load(getChartDir(network))
	if err != nil {
		return err
	}

	if err := r.createValuesFiles(ctx, network); err != nil {
		r.Log.Error(err, "Couldnt create values files")
		return err
	}

	values, err := r.getChartValues(network, settings)
	if err != nil {
		r.Log.Error(err, "Couldnt get chart values")
		return err
	}

	client := action.NewUpgrade(actionConfig)
	client.Namespace = network.Namespace

	r.Log.Info("updating release")
	// TODO for Kafka orderer, wait is not reliable. how to handle this?
	release, err := client.Run("hlf-kube", chart, values)
	if err != nil {
		return err
	}
	r.Log.Info("updated release", "name", release.Name, "version", release.Version, "namespace", network.Namespace)

	return nil

}

func (r *FabricNetworkReconciler) isHelmChartReady(ctx context.Context, network *v1alpha1.FabricNetwork) (bool, error) {
	// TODO mutex this.
	// os.Setenv("HELM_NAMESPACE", network.Namespace)
	// settings := cli.New()
	// actionConfig := new(action.Configuration)

	// if err := actionConfig.Init(settings.RESTClientGetter(), network.Namespace, "secret", log.Printf); err != nil {
	// 	r.Log.Error(err, "Couldnt init")
	// 	return false, err
	// }

	// client := action.NewStatus(actionConfig)

	// r.Log.Info("getting status of release")
	// release, err := client.Run("hlf-kube")
	// if err != nil {
	// 	return false, err
	// }
	// r.Log.Info("got status of release", "name", release.Name, "version", release.Version, "namespace", network.Namespace, "status", release.Info.Status)

	stsList := &appsv1.StatefulSetList{}
	listOpts := []client.ListOption{
		client.InNamespace(network.Namespace),
		client.MatchingLabels(map[string]string{"app.kubernetes.io/managed-by": "Helm"}),
	}

	if err := r.List(ctx, stsList, listOpts...); err != nil {
		r.Log.Error(err, "Failed to get StatefulSetList")
		return false, err
	}
	r.Log.Info("got StatefulSetList", "size", len(stsList.Items))

	for _, sts := range stsList.Items {
		if sts.Annotations["meta.helm.sh/release-name"] != "hlf-kube" {
			continue
		}
		if *sts.Spec.Replicas != sts.Status.ReadyReplicas {
			r.Log.Info("StatefulSet is not ready", "name", sts.Name, "replicas", *sts.Spec.Replicas, "readyReplicas", sts.Status.ReadyReplicas)
			return false, nil
		}
	}
	r.Log.Info("All StatefulSets are ready", "count", len(stsList.Items))

	deployList := &appsv1.DeploymentList{}

	if err := r.List(ctx, deployList, listOpts...); err != nil {
		r.Log.Error(err, "Failed to get DeploymentList")
		return false, err
	}
	r.Log.Info("got DeploymentList", "size", len(deployList.Items))

	for _, deploy := range deployList.Items {
		if deploy.Annotations["meta.helm.sh/release-name"] != "hlf-kube" {
			continue
		}
		if *deploy.Spec.Replicas != deploy.Status.ReadyReplicas {
			r.Log.Info("Deployment is not ready", "name", deploy.Name, "replicas", *deploy.Spec.Replicas, "readyReplicas", deploy.Status.ReadyReplicas)
			return false, nil
		}
	}
	r.Log.Info("All Deployments are ready", "count", len(stsList.Items))

	return true, nil
}

func getChartDir(network *v1alpha1.FabricNetwork) string {
	// TODO
	return "/home/raft/c/raft_code/PIVT/fabric-kube/hlf-kube/"
}

func (r *FabricNetworkReconciler) getChartValues(network *v1alpha1.FabricNetwork, settings *cli.EnvSettings, extraValues ...string) (map[string]interface{}, error) {
	valueOpts := &values.Options{}
	valueOpts.ValueFiles = []string{
		// TODO
		// "/home/raft/c/raft_code/PIVT/fabric-kube/samples/scaled-raft-tls/network.yaml",
		// "/home/raft/c/raft_code/PIVT/fabric-kube/samples/scaled-raft-tls/crypto-config.yaml",

		"/home/raft/c/raft_code/PIVT/fabric-kube/samples/scaled-kafka/network.yaml",
		"/home/raft/c/raft_code/PIVT/fabric-kube/samples/scaled-kafka/crypto-config.yaml",

		getChartDir(network) + "user-values.yaml",
		getChartDir(network) + "operator-values.yaml",
	}
	valueOpts.Values = append([]string{
		// TODO
		"hyperledgerVersion=" + network.Spec.Topology.Version,
		"tlsEnabled=" + strconv.FormatBool(network.Spec.Topology.TlsEnabled),
		"useActualDomains=" + strconv.FormatBool(network.Spec.Topology.UseActualDomains),
	}, extraValues...)
	r.Log.Info("Values", "valueOpts", valueOpts)

	providers := getter.All(settings)
	values, err := valueOpts.MergeValues(providers)
	r.Log.Info("Final values", "values", values)

	return values, err
}

func (r *FabricNetworkReconciler) createValuesFiles(ctx context.Context, network *v1alpha1.FabricNetwork) error {
	yml, err := yaml.JSONToYAML(network.Spec.HlfKube.Raw)
	if err != nil {
		return err
	}

	userValuesFile := getChartDir(network) + "user-values.yaml"
	if err := ioutil.WriteFile(userValuesFile, yml, 0644); err != nil {
		return err
	}

	values := HelmValues{}

	values.HostAliases, err = r.getHostAliases(ctx, network)
	if err != nil {
		return err
	}

	yml, err = yaml.Marshal(values)
	if err != nil {
		return err
	}

	hostAliasesFile := getChartDir(network) + "operator-values.yaml"
	if err := ioutil.WriteFile(hostAliasesFile, yml, 0644); err != nil {
		return err
	}

	return nil
}

func (r *FabricNetworkReconciler) getHostAliases(ctx context.Context, network *v1alpha1.FabricNetwork) ([]corev1.HostAlias, error) {
	allHostAliases := network.Spec.HostAliases
	r.Log.Info("user provided hostAliases", "items", allHostAliases)

	if network.Spec.Topology.UseActualDomains {

		svcList := &corev1.ServiceList{}
		listOpts := []client.ListOption{
			client.InNamespace(network.Namespace),
			client.MatchingLabels(map[string]string{"addToHostAliases": "true"}),
		}

		if err := r.List(ctx, svcList, listOpts...); err != nil {
			r.Log.Error(err, "Failed to get ServiceList")
			return nil, err
		}
		r.Log.Info("got ServiceList", "size", len(svcList.Items))

		hostAliases := make([]corev1.HostAlias, len(svcList.Items))
		for i, svc := range svcList.Items {
			hostAliases[i] = corev1.HostAlias{
				IP:        svc.Spec.ClusterIP,
				Hostnames: []string{svc.Labels["fqdn"]},
			}
		}
		r.Log.Info("created hostAliases", "items", hostAliases)

		allHostAliases = append(allHostAliases, hostAliases...)
	}
	return allHostAliases, nil
}
