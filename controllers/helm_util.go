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

// Struct to write the values passed to Helm chart to a file
type helmValues struct {
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
	if network.Spec.Topology.UseActualDomains {
		extraValues = []string{
			"peer.launchPods=false",
			"orderer.launchPods=false",
		}
	}
	values, err := r.getChartValues(network, settings, "hlf-kube-values.yaml", extraValues...)
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

	values, err := r.getChartValues(network, settings, "hlf-kube-values.yaml")
	if err != nil {
		r.Log.Error(err, "Couldnt get chart values")
		return err
	}

	client := action.NewUpgrade(actionConfig)
	client.Namespace = network.Namespace

	r.Log.Info("updating release")
	release, err := client.Run("hlf-kube", chart, values)
	if err != nil {
		return err
	}
	r.Log.Info("updated release", "name", release.Name, "version", release.Version, "namespace", network.Namespace)

	return nil

}
func (r *FabricNetworkReconciler) renderChannelFlow(ctx context.Context, network *v1alpha1.FabricNetwork) (string, error) {
	// TODO
	chartDir := "/home/raft/c/raft_code/PIVT/fabric-kube/channel-flow/"
	return r.renderHelmChart(ctx, network, chartDir, "channel-flow-values.yaml")
}

func (r *FabricNetworkReconciler) renderChaincodeFlow(ctx context.Context, network *v1alpha1.FabricNetwork) (string, error) {
	// TODO
	chartDir := "/home/raft/c/raft_code/PIVT/fabric-kube/chaincode-flow/"
	return r.renderHelmChart(ctx, network, chartDir, "chaincode-flow-values.yaml")
}

func (r *FabricNetworkReconciler) renderHelmChart(ctx context.Context, network *v1alpha1.FabricNetwork, chartDir string, valuesFile string) (string, error) {
	settings := cli.New()
	actionConfig := new(action.Configuration)

	chart, err := loader.Load(chartDir)
	if err != nil {
		return "", err
	}

	if err := r.createValuesFiles(ctx, network); err != nil {
		r.Log.Error(err, "Couldnt create values files")
		return "", err
	}

	extraValues := []string{}
	values, err := r.getChartValues(network, settings, valuesFile, extraValues...)
	if err != nil {
		return "", err
	}

	client := action.NewInstall(actionConfig)
	client.DryRun = true
	client.ReleaseName = "doesnt-matter"
	client.Namespace = network.Namespace
	client.Replace = true // Skip the name check
	client.ClientOnly = true
	// client.APIVersions = chartutil.VersionSet(extraAPIs)
	client.IncludeCRDs = false

	r.Log.Info("Rendering Helm chart", "path", chartDir)
	release, err := client.Run(chart, values)
	if err != nil {
		return "", err
	}
	r.Log.Info("Rendered Helm chart", "path", chartDir)

	return release.Manifest, nil
}

func (r *FabricNetworkReconciler) isHelmChartReady(ctx context.Context, network *v1alpha1.FabricNetwork) (bool, error) {
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

func (r *FabricNetworkReconciler) getChartValues(network *v1alpha1.FabricNetwork, settings *cli.EnvSettings, valuesFile string, extraValues ...string) (map[string]interface{}, error) {
	valueOpts := &values.Options{}
	valueOpts.ValueFiles = []string{
		// TODO
		"/home/raft/c/raft_code/PIVT/fabric-kube/samples/scaled-raft-tls/network.yaml",
		"/home/raft/c/raft_code/PIVT/fabric-kube/samples/scaled-raft-tls/crypto-config.yaml",

		// "/home/raft/c/raft_code/PIVT/fabric-kube/samples/scaled-kafka/network.yaml",
		// "/home/raft/c/raft_code/PIVT/fabric-kube/samples/scaled-kafka/crypto-config.yaml",

		getChartDir(network) + valuesFile,
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
	chartDir := getChartDir(network)

	if err := createValuesFile(network.Spec.HlfKube.Raw, chartDir+"hlf-kube-values.yaml"); err != nil {
		return err
	}
	if err := createValuesFile(network.Spec.ChannelFlow.Raw, chartDir+"channel-flow-values.yaml"); err != nil {
		return err
	}
	if err := createValuesFile(network.Spec.ChaincodeFlow.Raw, chartDir+"chaincode-flow-values.yaml"); err != nil {
		return err
	}

	hostAliases, err := r.getHostAliases(ctx, network)
	if err != nil {
		return err
	}

	values := helmValues{
		HostAliases: hostAliases,
	}

	yml, err := yaml.Marshal(values)
	if err != nil {
		return err
	}

	hostAliasesFile := chartDir + "operator-values.yaml"
	if err := ioutil.WriteFile(hostAliasesFile, yml, 0644); err != nil {
		return err
	}

	return nil
}

func createValuesFile(contents []byte, fileName string) error {
	yml, err := yaml.JSONToYAML(contents)
	if err != nil {
		return err
	}

	if err := ioutil.WriteFile(fileName, yml, 0644); err != nil {
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
		r.Log.Info("Got ServiceList", "size", len(svcList.Items))

		hostAliases := make([]corev1.HostAlias, len(svcList.Items))
		for i, svc := range svcList.Items {
			hostAliases[i] = corev1.HostAlias{
				IP:        svc.Spec.ClusterIP,
				Hostnames: []string{svc.Labels["fqdn"]},
			}
		}
		r.Log.Info("Created hostAliases", "items", hostAliases)

		allHostAliases = append(allHostAliases, hostAliases...)
	}
	return allHostAliases, nil
}
