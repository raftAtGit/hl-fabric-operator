package controllers

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	"helm.sh/helm/v3/pkg/action"
	hchart "helm.sh/helm/v3/pkg/chart"
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

// Struct to write the Network to a file
type networkContainer struct {
	Network v1alpha1.Network `json:"network,omitempty"`
}

func (r *FabricNetworkReconciler) prepareHelmChart(ctx context.Context, network *v1alpha1.FabricNetwork) error {
	networkDir := getNetworkDir(network)

	if err := os.RemoveAll(networkDir); err != nil {
		r.Log.Error(err, "Network dir alredy exists and couldnt delete", "networkDir", networkDir)
		return err
	}

	if err := r.createHelmChartDir(ctx, network); err != nil {
		r.Log.Error(err, "Create chart dir failed")
		return err
	}

	if err := r.prepareChartDirForFabric(ctx, network, freshInstall); err != nil {
		r.Log.Error(err, "Prepare chart dir failed")
		return err
	}

	return nil
}

func (r *FabricNetworkReconciler) maybeReconstructHelmChart(ctx context.Context, network *v1alpha1.FabricNetwork) error {
	networkDir := getNetworkDir(network)
	if _, err := os.Stat(networkDir); !os.IsNotExist(err) {
		// networkDir exists
		return nil
	}

	switch network.Status.State {
	case "":
	case v1alpha1.StateNew:
	case v1alpha1.StateRejected:
	case v1alpha1.StateInvalid:
	case v1alpha1.StateFailed:
		return nil
	}

	r.Log.Info("networkDir does not exist, will reconstruct", "networkDir", networkDir, "state", network.Status.State)

	if err := r.createHelmChartDir(ctx, network); err != nil {
		r.Log.Error(err, "Create chart dir failed")
		return err
	}

	if err := r.prepareChartDirForFabric(ctx, network, reconstruct); err != nil {
		r.Log.Error(err, "Prepare chart dir failed")
		return err
	}

	return nil
}

func (r *FabricNetworkReconciler) createHelmChartDir(ctx context.Context, network *v1alpha1.FabricNetwork) error {
	networkDir := getNetworkDir(network)

	if err := copyDir(settings.PivtDir+"/fabric-kube/hlf-kube", networkDir); err != nil {
		r.Log.Error(err, "Couldnt copy hlf-kube folder to network dir", "networkDir", networkDir)
		return err
	}

	if err := r.createValuesFiles(ctx, network); err != nil {
		r.Log.Error(err, "Couldnt create values files")
		return err
	}

	if err := os.MkdirAll(networkDir+"/channel-artifacts", 0755); err != nil {
		return err
	}

	return nil
}

func (r *FabricNetworkReconciler) installHelmChart(ctx context.Context, network *v1alpha1.FabricNetwork) error {
	settings, actionConfig, err := r.initHelmClient(network.Namespace)
	if err != nil {
		return err
	}

	chart, err := loadHelmChart(network)
	if err != nil {
		return err
	}

	extraValues := []string{}
	if network.Spec.Topology.UseActualDomains {
		extraValues = []string{
			"peer.launchPods=false",
			"orderer.launchPods=false",
		}
	}
	values, err := r.getChartValues(ctx, network, settings, []string{"hlf-kube-values.yaml"}, extraValues)
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
	settings, actionConfig, err := r.initHelmClient(network.Namespace)
	if err != nil {
		return err
	}

	chart, err := loadHelmChart(network)
	if err != nil {
		return err
	}

	values, err := r.getChartValues(ctx, network, settings, []string{"hlf-kube-values.yaml"}, nil)
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

// uninstalls the hlf-kube Helm chart if its found and Chart.Metadata.Name is hlf-kube and annotated for specified FabricNetwork
func (r *FabricNetworkReconciler) maybeUninstallHelmChart(ctx context.Context, namespace string, name string) error {
	_, actionConfig, err := r.initHelmClient(namespace)
	if err != nil {
		return err
	}

	getClient := action.NewGet(actionConfig)

	release, err := getClient.Run("hlf-kube")
	if err != nil {
		if strings.Contains(err.Error(), "release: not found") {
			r.Log.Info("Helm release is not found, skipping uninstall")
			return nil
		}
		return err
	}
	r.Log.Info("got Helm release", "release", release.Chart.Metadata.Name, "version", release.Chart.Metadata.Version,
		"annotations", release.Chart.Metadata.Annotations)

	if release.Chart.Metadata.Name != "hlf-kube" {
		r.Log.Info("Helm release is not hlf-kube, skipping uninstall")
		return nil
	}

	if release.Chart.Metadata.Annotations["raft.io/fabric-operator-created-for"] != name {
		r.Log.Info("Helm release is created for another FabricNetwork, skipping uninstall")
		return nil
	}

	client := action.NewUninstall(actionConfig)
	client.KeepHistory = false

	r.Log.Info("uninstalling release")
	_, err = client.Run("hlf-kube")
	if err != nil {
		return err
	}
	r.Log.Info("uninstalled release hlf-kube")

	return nil
}

func loadHelmChart(network *v1alpha1.FabricNetwork) (*hchart.Chart, error) {
	chart, err := loader.Load(getNetworkDir(network))
	if err != nil {
		return nil, err
	}
	if chart.Metadata.Annotations == nil {
		chart.Metadata.Annotations = make(map[string]string)
	}
	chart.Metadata.Annotations["raft.io/fabric-operator-created-for"] = network.Name

	return chart, nil
}

func (r *FabricNetworkReconciler) renderChannelFlow(ctx context.Context, network *v1alpha1.FabricNetwork) (string, error) {
	chartDir := settings.PivtDir + "/fabric-kube/channel-flow/"
	return r.renderHelmChart(ctx, network, chartDir, []string{"channel-flow-values.yaml"}, nil)
}

func (r *FabricNetworkReconciler) renderChaincodeFlow(ctx context.Context, network *v1alpha1.FabricNetwork, includeChaincodes []string) (string, error) {
	chartDir := settings.PivtDir + "/fabric-kube/chaincode-flow/"

	extraValues := []string{
		"chaincode.version=" + network.Spec.Chaincode.Version,
		"chaincode.language=" + network.Spec.Chaincode.Language,
	}
	if len(includeChaincodes) != 0 {
		extraValues = append(extraValues, "flow.chaincode.include={"+strings.Join(includeChaincodes, ",")+"}")
	}

	return r.renderHelmChart(ctx, network, chartDir, []string{"chaincode-flow-values.yaml"}, extraValues)
}

func (r *FabricNetworkReconciler) renderPeerOrgFlow(ctx context.Context, network *v1alpha1.FabricNetwork) (string, error) {
	chartDir := settings.PivtDir + "/fabric-kube/peer-org-flow/"
	return r.renderHelmChart(ctx, network, chartDir, []string{"peer-org-flow-values.yaml", "configtx.yaml"}, nil)
}

func (r *FabricNetworkReconciler) renderHelmChart(ctx context.Context, network *v1alpha1.FabricNetwork,
	chartDir string, valuesFiles []string, extraValues []string) (string, error) {

	settings := cli.New()
	actionConfig := new(action.Configuration)

	chart, err := loader.Load(chartDir)
	if err != nil {
		return "", err
	}

	values, err := r.getChartValues(ctx, network, settings, valuesFiles, extraValues)
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

	r.Log.Info("Rendering Helm chart", "path", chartDir, "extraValues", extraValues, "values", values)
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

func getNetworkDir(network *v1alpha1.FabricNetwork) string {
	return settings.NetworkDir + "/" + network.Namespace + "/" + network.Name
}

func (r *FabricNetworkReconciler) getChartValues(ctx context.Context, network *v1alpha1.FabricNetwork, settings *cli.EnvSettings, valuesFiles []string, extraValues []string) (map[string]interface{}, error) {
	if err := r.createOperatorValuesFile(ctx, network); err != nil {
		r.Log.Error(err, "Couldnt create operator values")
		return nil, err
	}

	valueOpts := &values.Options{}
	valueOpts.ValueFiles = []string{
		getNetworkDir(network) + "/network.yaml",
		getNetworkDir(network) + "/crypto-config.yaml",
		getNetworkDir(network) + "/operator-values.yaml",
	}
	for _, vf := range valuesFiles {
		valueOpts.ValueFiles = append(valueOpts.ValueFiles, getNetworkDir(network)+"/"+vf)
	}
	genesisProvided := false
	if network.Spec.Genesis.Secret != "" {
		genesisProvided = true
	}
	valueOpts.Values = append([]string{
		// TODO
		"hyperledgerVersion=" + network.Spec.Topology.Version,
		"tlsEnabled=" + strconv.FormatBool(network.Spec.Topology.TLSEnabled),
		"useActualDomains=" + strconv.FormatBool(network.Spec.Topology.UseActualDomains),
		"configMap.chaincode=false",
		"secret.configtx=false",
		"secret.genesis=" + strconv.FormatBool(!genesisProvided),
	}, extraValues...)
	// if extraValues != nil {
	// 	valueOpts.Values = append(valueOpts.Values, extraValues...)
	// }
	r.Log.Info("Values", "valueOpts", valueOpts)

	providers := getter.All(settings)
	values, err := valueOpts.MergeValues(providers)
	r.Log.Info("Final values", "values", values)

	return values, err
}

func (r *FabricNetworkReconciler) createValuesFiles(ctx context.Context, network *v1alpha1.FabricNetwork) error {
	networkDir := getNetworkDir(network)

	if err := r.createConfigtxFile(ctx, network); err != nil {
		return err
	}
	if err := r.createCryptoConfigFile(ctx, network); err != nil {
		return err
	}
	if err := r.createNetworkValuesFile(ctx, network); err != nil {
		return err
	}
	if err := r.createValuesFile(network.Spec.HlfKube.Raw, networkDir+"/hlf-kube-values.yaml"); err != nil {
		return err
	}
	if err := r.createValuesFile(network.Spec.ChannelFlow.Raw, networkDir+"/channel-flow-values.yaml"); err != nil {
		return err
	}
	if err := r.createValuesFile(network.Spec.ChaincodeFlow.Raw, networkDir+"/chaincode-flow-values.yaml"); err != nil {
		return err
	}
	if err := r.createValuesFile(network.Spec.PeerOrgFlow.Raw, networkDir+"/peer-org-flow-values.yaml"); err != nil {
		return err
	}
	if err := r.createOperatorValuesFile(ctx, network); err != nil {
		return err
	}
	return nil
}

func (r *FabricNetworkReconciler) createNetworkValuesFile(ctx context.Context, network *v1alpha1.FabricNetwork) error {
	networkDir := getNetworkDir(network)

	netContainer := networkContainer{Network: network.Spec.Network}
	file := networkDir + "/network.yaml"
	if err := writeYamlToFile(netContainer, file); err != nil {
		return err
	}
	r.Log.Info("Wrote network to file", "file", file, "network", netContainer)

	return nil
}

func (r *FabricNetworkReconciler) createOperatorValuesFile(ctx context.Context, network *v1alpha1.FabricNetwork) error {
	networkDir := getNetworkDir(network)

	hostAliases, err := r.getHostAliases(ctx, network)
	if err != nil {
		return err
	}

	values := helmValues{
		HostAliases: hostAliases,
	}

	file := networkDir + "/operator-values.yaml"
	if err := writeYamlToFile(values, file); err != nil {
		return err
	}
	r.Log.Info("Wrote values to file", "values", values, "file", file)

	return nil
}

func (r *FabricNetworkReconciler) createValuesFile(contents []byte, file string) error {
	yml, err := yaml.JSONToYAML(contents)
	if err != nil {
		return err
	}

	if err := ioutil.WriteFile(file, yml, 0644); err != nil {
		return err
	}
	r.Log.Info("Wrote values to file", "values", string(contents), "file", file)

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

func (r *FabricNetworkReconciler) helmLog(format string, v ...interface{}) {
	r.Log.Info("Helm log", "message", fmt.Sprintf(format, v...))
}

func (r *FabricNetworkReconciler) initHelmClient(namespace string) (*cli.EnvSettings, *action.Configuration, error) {
	// TODO mutex this.
	os.Setenv("HELM_NAMESPACE", namespace)
	settings := cli.New()
	actionConfig := new(action.Configuration)

	if err := actionConfig.Init(settings.RESTClientGetter(), namespace, "secret", r.helmLog); err != nil {
		r.Log.Error(err, "Couldnt init Helm client")
		return nil, nil, err
	}

	return settings, actionConfig, nil
}
