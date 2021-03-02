module github.com/raftAtGit/hl-fabric-operator

go 1.15

require (
	github.com/argoproj/argo/v3 v3.0.0-rc1
	github.com/argoproj/pkg v0.3.0
	github.com/go-logr/logr v0.3.0
	github.com/onsi/ginkgo v1.14.1
	github.com/onsi/gomega v1.10.2
	helm.sh/helm/v3 v3.4.2
	k8s.io/api v0.19.6
	k8s.io/apimachinery v0.19.6
	k8s.io/client-go v0.19.6
	sigs.k8s.io/controller-runtime v0.7.0
	sigs.k8s.io/yaml v1.2.0
)

replace (
	github.com/docker/distribution => github.com/docker/distribution v0.0.0-20191216044856-a8371794149d
	github.com/docker/docker => github.com/moby/moby v17.12.0-ce-rc1.0.20200618181300-9dc6525e6118+incompatible
	sigs.k8s.io/kustomize => ../gofix/sigs.k8s.io/kustomize
)
