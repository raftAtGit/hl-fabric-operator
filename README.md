# Kubernetes Operator for Hyperledger Fabric
![Fabric Meets K8S](https://raft-fabric-kube.s3-eu-west-1.amazonaws.com/images-operator/fabric_operator.png)

* [What is this?](#what-is-this)
* [Who made this?](#who-made-this)
* [License](#License)
* [Requirements](#requirements)
* [Quick start](#quick-start)
* [Overview](#overview)
  * [Strawman](#strawman)
  * [CRD](#crd)
  * [CLI](#cli)
* [State machine](#state-machine)
* [Network architecture](#network-architecture)
* [Go over the samples](#go-over-samples)
  * [Simple](#simple)
  * [Simple Raft-TLS](#simple-raft-tls)
  * [Scaled Raft-TLS](#scaled-raft-tls)
  * [Scaled Kafka](#scaled-kafka)
  * [Updating chaincodes](#updating-chaincodes)
  * [Updating channels](#updating-channels)
  * [Adding new peer organizations](#adding-new-peer-organizations)
  * [Adding new peers to organizations](#adding-new-peers-to-organizations)
* [Trouble shooting](#trouble-shooting)
  * [Important remarks](#important-remarks)
* [Known issues](#known-issues)
* [Conclusion](#conclusion)

## [What is this?](#what-is-this)
This repository contains a [Kubernetes Operator](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/) to:
* Configure and launch the whole HL Fabric network or part of it, either:
  * A simple one, one peer per organization and Solo orderer
  * Or scaled up one, multiple peers per organization and Kafka or Raft orderer
* Populate the network declaratively:
  * Create the channels, join peers to channels, update channels for Anchor peers
  * Install/Instantiate all chaincodes, or some of them, or upgrade them to newer version
* Add new peer organizations to an already running network declaratively

HL Fabric operator is a wrapper around our previous work [PIVT Helm charts](https://github.com/hyfen-nl/PIVT)
and makes running and operating Hyperledger Fabric in Kubernetes even more easier.

Not all functionality provided by PIVT Helm charts are covered, but HL Fabric operator is completely compatible with 
PIVT Helm charts. This means any uncovered functionality can still be utilized by directly using PIVT Helm charts.

## [Who made this?](#who-made-this)
This is made by the original author of [PIVT Helm charts](https://github.com/hyfen-nl/PIVT); Hakan Eryargi *(a.k.a. r a f t)*.

This work started as an experimental/PoC hobby project but turned out to be quite complete and functional.

## [License](#License)
This work is licensed under the same license with HL Fabric; [Apache License 2.0](LICENSE).

## [Requirements](#requirements)
* A running Kubernetes cluster, Minikube should also work, but not tested
* [Argo](https://github.com/argoproj/argo) Controller 2.4.0+ (Argo CLI is not needed but can be handy for debugging)
* [Minio](https://github.com/argoproj/argo/blob/master/docs/configure-artifact-repository.md), only required for adding new peer organizations
* AWS EKS users please also apply this [fix](https://github.com/APGGroeiFabriek/PIVT/issues/1)

## [Quick start](#quick-start)
Please refer to [release notes](https://github.com/raftAtGit/hl-fabric-operator/releases) for installing the operator and the CLI.

## [Overview](#overview)

### [Strawman](#strawman)
Below is the strawman diagram for HL Fabric Operator:

![strawman](https://raft-fabric-kube.s3-eu-west-1.amazonaws.com/images-operator/overview.png)

### [CRD](#crd)
FabricNetwork CRD (Custom Resource Definition) spec consists of four sections:

#### Top level
Here the sources of `configtx`, `chaincode`, `genesis block` and `crypto material` are defined. They can be either Kubernetes `Secrets` and/or `ConfigMaps` 
or references to local file system. References to local file system is only possible when using CLI tool.

`hostAliases` is provided for communication with external peers/orderers. 
If `useActualDomains` is true, Fabric Operator will still create internal hostAliases and append to this one.

`forceState` forces the Fabric Operator to set the the state of FabricNetwork to given state and continue. 
See [Trouble shooting](#trouble-shooting) section for how to use.

```yaml
  # source of the configtx.yaml file. either a Kubernetes Secret or a file.
  configtx:
    file: configtx.yaml # see CLI for usage
    # secret: hlf-configtx.yaml

  chaincode:
    version: "1.0"
    language: node
    folder: ../chaincode # see CLI for usage
    # configMaps: implied list

  # source of the genesis block. either a Kubernetes Secret or a file.
  # if none provided Fabric Operator will create the genesis block
  genesis: {}
    # file: # see CLI for usage
    # secret: hlf-genesis.block

  # source of the crypto materials. either a Kubernetes Secret or a folder.
  # if none provided Fabric Operator will create the crypto materials via cryptogen tool.
  # the secret contains TAR archived crypto material
  crypto-config: {}
    # folder: ./crypto-config
    # secret: hlf-crypto-config

  # adds additional DNS entries to /etc/hosts files of pods
  # this is provided for communication with external peers/orderers
  # if useActualDomains is true, Fabric Operator will still create internal hostAliases and append to this one
  hostAliases: 
  
  # forces Fabric Operator to set the the state of FabricNetwork to given state and continue. 
  # use with caution. see troubleshooting section for how to use.
  forceState: 
```

#### Topology

Topology of the Fabric network managed by Fabric Operator. This part also contains some top level properties which is applied to whole network. 
`crypto-config.yaml` is derived from this part.

```yaml
  # topology of the Fabric network managed by Fabric Operator
  # also contains some top level properties which is applied to whole network
  topology:
    # Hyperledger Fabric Version
    version: 1.4.9  
    # TLS enabled?
    tlsEnabled: true
    # use actual domain names like peer0.atlantis.com instead of internal service names
    useActualDomains: true

    # Orderer and Peer organizations topology
    # crypto-config.yaml will be derived from this part
    ordererOrgs:
      - name: Pivt
        domain: pivt.nl
        hosts:
          - orderer0
    peerOrgs:
      - name: Karga
        domain: aptalkarga.tr
        peerCount: 1
      - name: Nevergreen
        domain: nevergreen.nl
        peerCount: 1
      - name: Atlantis
        domain: atlantis.com
        peerCount: 1
```
#### Network
This part defines how network is populated regarding channels and chaincodes. This part is identical to 
[network.yaml](https://github.com/hyfen-nl/PIVT#networkyaml) in PIVT Helm charts.

```yaml
  network:
    # used to create genesis block and by peer-org-flow to parse consortiums
    genesisProfile: OrdererGenesis
    # used to create genesis block 
    systemChannelID: testchainid

    # defines which organizations will join to which channels
    channels:
      - name: common
        # all peers in these organizations will join the channel
        orgs: [Karga, Nevergreen, Atlantis]
      - name: private-karga-atlantis
        # all peers in these organizations will join the channel
        orgs: [Karga, Atlantis]

    # defines which chaincodes will be installed to which organizations
    chaincodes:
      - name: very-simple
        # if defined, this will override the global chaincode.version value
        # version: # "2.0" 
        # chaincode will be installed to all peers in these organizations
        orgs: [Karga, Nevergreen, Atlantis]
        # at which channels are we instantiating/upgrading chaincode?
        channels:
        - name: common
          # chaincode will be instantiated/upgraded using the first peer in the first organization
          # chaincode will be invoked on all peers in these organizations
          orgs: [Karga, Nevergreen, Atlantis]
          policy: OR('KargaMSP.member','NevergreenMSP.member','AtlantisMSP.member')
          
      - name: even-simpler
        # if defined, this will override the global chaincode.language value
        language: golang
        orgs: [Karga, Atlantis]
        channels:
        - name: private-karga-atlantis
          orgs: [Karga, Atlantis]
          policy: OR('KargaMSP.member','AtlantisMSP.member')
```
#### Additional settings
This part contains additional settings passed to relevant PIVT Helm charts. See each chart's `values.yaml` file for details.
```yaml
  hlf-kube: 
    peer:
      docker:
        dind: 
          # use a side car docker in docker container? required for Kubernetes versions 1.19+ 
          enabled: true
  channel-flow: {}
  chaincode-flow: {}
  peer-org-flow: {}
```

### [CLI](#cli)
Fabric Operator CLI is a supplementary tool for interacting with Fabric Operator.

It’s not required for normal operation of Fabric Operator but provided as a tool for convenience.

It performs client-side validation and creates necessary resources in Kubernetes on the fly. 

By using CLI, it's possible to specify supplementary inputs as references to the local file system. 
For example if `chaincode.folder` is provided in the FabricNetwork CRD like below, CLI will create chaincode `ConfigMaps` before submitting the FabricNetwork to Kubernetes.

```yaml
chaincode:
  folder: ../chaincode
```

CLI uses local `kubectl` settings: it uses `KUBECONFIG` environment variable if set, otherwise it uses `~/.kube/config`.

Many CLI commands use the same semantics with `kubectl`. For example:
```
-n, --namespace string
-A, --all-namespaces
```

## [State machine](#state-machine)
Below diagram shows the state machine of HL Fabric Operator:

![State Machine](https://raft-fabric-kube.s3-eu-west-1.amazonaws.com/images-operator/state-machine.png)

## [Network architecture](#network-architecture)
Please refer to [Network Architecture](https://github.com/hyfen-nl/PIVT#network-architecture) section in the PIVT repo.

## [Go over samples](#go-over-samples)

Samples provided assumes CLI tool is used. All samples are based on the samples of [PIVT Helm charts](https://github.com/hyfen-nl/PIVT/tree/master/fabric-kube/samples).

### [Simple](#simple)
__A solo orderer network with one peer per organization.__

Launch the network:
```
rfabric create samples/simple/fabric-network.yaml
```
Output:
```
created configtx Secret hlf-configtx.yaml
created chaincode ConfigMap hlf-chaincode--very-simple
created chaincode ConfigMap hlf-chaincode--even-simpler
created new FabricNetwork simple in namespace default
```

So what happened? CLI noticed the references to configtx and chaincodes are local file system references, it created the relevant `Secret` and `ConfigMaps`, 
updated the `FabricNetwork` to use the created Secret/ConfigMaps and submitted to Kubernetes.

Fabric operator will now create a Helm release for [hlf-kube](https://github.com/hyfen-nl/PIVT/tree/master/fabric-kube/hlf-kube), wait until it's ready, 
then will start [channel Argo flow](https://github.com/hyfen-nl/PIVT/tree/master/fabric-kube/channel-flow), wait until it's completed,
then will start [chaincode Argo flow](https://github.com/hyfen-nl/PIVT/tree/master/fabric-kube/chaincode-flow), wait until it's completed,
then FabricNetwork will be ready.

Watch the process by either:
```
kubectl get FabricNetwork simple -o yaml --watch
```
or 
```
watch rfabric list
```

You will see the FabricNetwork will go over a couple of states:
```
New
HelmChartInstalled
HelmChartReady
ChannelFlowSubmitted
ChannelFlowCompleted
ChaincodeFlowSubmitted
ChaincodeFlowCompleted
Ready
```
Congratulations! You now have a running HL Fabric network in Kubernetes! Channels created, peer orgs joined to channels and chaincodes are installed and instantiated.

Delete the FabricNetwork and all resources:
```
rfabric delete simple
```

### [Simple Raft-TLS](#simple-raft-tls)
__A one organization Raft orderer network with one peer per organization.__

```
rfabric create samples/simple-raft-tls/fabric-network.yaml
```

Watch the process by either:
```
kubectl get FabricNetwork simple-raft-tls -o yaml --watch
```
or 
```
watch rfabric list
```

This time FabricNetwork will go over slightly different states:
```
New
HelmChartInstalled
HelmChartNeedsUpdate -> New step
HelmChartReady
ChannelFlowSubmitted
ChannelFlowCompleted
ChaincodeFlowSubmitted
ChaincodeFlowCompleted
Ready
```
This time Fabric operator noticed `useActualDomains` is set to true, so it first created the Helm release for 
[hlf-kube](https://github.com/hyfen-nl/PIVT/tree/master/fabric-kube/hlf-kube), then collected `hostAliases` and then updated the 
Helm release with the `hostAliases`. The next steps remains the same. 
It will start [channel Argo flow](https://github.com/hyfen-nl/PIVT/tree/master/fabric-kube/channel-flow), wait until it's completed,
then will start [chaincode Argo flow](https://github.com/hyfen-nl/PIVT/tree/master/fabric-kube/chaincode-flow), wait until it's completed,
then FabricNetwork will be ready.

Congratulations! You now have a running HL Fabric network in Kubernetes with Raft orderer and TLS is enabled! Channels created, peer orgs joined to channels and chaincodes are installed and instantiated.

Delete the FabricNetwork and all resources:
```
rfabric delete simple-raft-tls
```

### [Scaled Raft-TLS](#scaled-raft-tls)
__Scaled up network based on three Raft orderer nodes spanning two Orderer organizations and two peers per organization.__

```
rfabric create samples/scaled-raft-tls/fabric-network.yaml
```

Same as [Simple Raft-TLS](#simple-raft-tls-network) sample. It will go over the same steps.


Delete the FabricNetwork and all resources:
```
rfabric delete scaled-raft-tls
```

### [Scaled Kafka](#scaled-kafka)
__Scaled up network based on three Kafka orderer nodes and two peers per organization.__

```
rfabric create samples/scaled-kafka/fabric-network.yaml
```

This will also launch Kafka and Zookeeper pods because of the setting passed to hlf-kube Helm chart:
```yaml
  hlf-kube: 
    hlf-kafka:
      enabled: true
```
See hlf-kube Helm chart's [values.yaml](https://github.com/hyfen-nl/PIVT/blob/master/fabric-kube/hlf-kube/values.yaml#L174)
for further configuration options of Kafka and Zookeper pods.

Delete the FabricNetwork and all resources:
```
rfabric delete scaled-kafka
```

## [Updating chaincodes](#updating-chaincodes)

Launch any of the samples above and wait until they are ready.

Let's assume you have updated the chaincode source codes. You can actually update them anyway.

Update the top level chaincode version to `2.0`.
```yaml
  chaincode:
    version: "2.0"
```
Then update the FabricNetwork with CLI:
```
rfabric update samples/simple/fabric-network.yaml
```
Fabric Operator will start the chaincode Argo flow and both chaincodes will be updated to version `2.0`.

You can also update individuals chaincodes. For example, below change will only update chaincode `very-simple` to version `3.0`.
```yaml
    chaincodes:
      - name: very-simple
        version: "3.0" 
```

## [Updating channels](#updating-channels)

Let's create another channel called `common-2`.

Add the below profile to `configtx.yaml`:
```yaml
    common-2:
        Consortium: TheConsortium
        <<: *ChannelDefaults
        Application:
            <<: *ApplicationDefaults
            Organizations:
                - *Karga
                - *Nevergreen
                - *Atlantis
```
And below to `channels` part of FabricNetwork CRD:
```yaml
    channels:
      - name: common-2
        # all peers in these organizations will join the channel
        orgs: [Karga, Nevergreen, Atlantis]
```
Then update the FabricNetwork with CLI:
```
rfabric update samples/simple/fabric-network.yaml
```
Fabric Operator will start the channel Argo flow which will create the channel `common-2` and add all peers in the mentioned organizations to channel `common-2`.

## [Adding new peer organizations](#adding-new-peer-organizations)

To add new peer organizations, you need to configure Argo to use some artifactory. Minio is the simplest way. 
Please make sure you can run an Argo provided [artifact sample](https://argoproj.github.io/argo-workflows/examples/#artifacts) before proceeding.

As of 6 April 2021, adding new peer organizations is not possible in Kubernetes versions 19+. See [known issues](#known-issues) section for details.

First launch the simple network and wait until it's ready:
```
rfabric create samples/simple/fabric-network.yaml
```

Then update the FabricNetwork with the provided extended definition.
```
rfabric update samples/simple/extended/fabric-network.yaml
```
Fabric Operator will start the peer-org Argo flow which will add missing organizations to consortiums
and add missing organizations to existing channels as defined in `network` section. 
Then channel Argo flow will be started and create missing channels. And finally chaincode Argo flow will be run.

See the [adding new peer organizations](https://github.com/hyfen-nl/PIVT#adding-new-peer-organizations) section in PIVT Helm charts repo for details.

__Note,__ if you are not running the majority of organizations and policy requires majority, adding new peer organizations will fail. 
However, you can still use Fabric Operator to do the heavy lifting:
* You can make Fabric Operator prepare and sign config updates for both user and orderer channels but prevent sending the update to orderer nodes 
(So, you can send config updates to other parties and they can proceed)
* You can make Fabric Operator start from provided signed config updates
* Or you can do both

See the extending [cross cluster raft orderer network](https://github.com/hyfen-nl/PIVT#cross-cluster-raft-orderer-network) 
sub section in PIVT Helm charts repo for details.

You can extend the Raft and Kafka orderer networks in the same way. But **in Raft orderer networks, (more precisely if `useActualDomains` is true) `persistence` should be enabled**. 
The pods will restart due to update of `hostAliases` and they will lose all data if `persistence` is not enabled. 

## [Adding new peers to organizations](#adding-new-peers-to-organizations)

Just increase the `peerCount` in the `topology` section and make an update. 
```yaml
    peerOrgs:
      - name: Karga
        domain: aptalkarga.tr
        peerCount: 2 # --> Increase this one
```

## [Trouble shooting](#trouble-shooting)

When something goes wrong, logs are your best friend. 

If it's Argo workflow failing, you can check details with `argo logs <workflow-name> [pod-name]` command. 

Fabric Operator __does not re-submit__ Argo workflows if they fail, since:
* The retry mechanism is baked into Argo workflows, guarding the flows against temporary failures: [example](https://github.com/hyfen-nl/PIVT/blob/master/fabric-kube/chaincode-flow/values.yaml#L7)
* Otherwise, if the underlying issue is not resolved, re-submitting the Argo workflow will just consume cluster resources for nothing

Fix the issue, and force the Fabric Operator to continue by setting the `forceState` field in the `FabricNetwork`. Use with caution this option. See the [state-machine](#state-machine) for how to use this feature.

For example, you can force `channel-flow` run again by setting `forceState` to `HelmChartReady`:
```
forceState: HelmChartReady
```

Or force `chaincode-flow` run again by setting `forceState` to `ChannelFlowCompleted`:
```
forceState: ChannelFlowCompleted
```

__Remember,__ if you are stuck, any time you can use Fabric tools or PIVT Helm charts directly to fix the issue and set `forceState` to `Ready`:
```
forceState: Ready
```

### [Important remarks](#important-remarks)

When `persistance` is enabled for any component, deleting the FabricNetwork with CLI or deleting the Helm chart directly, will __NOT__ delete `PersistentVolumeClaims`. 
This can result in unexpected behaviour, like orderer nodes cannot initiliaze correctly or cannot elect a leader.

So, make sure you delete the relevant `PersistentVolumeClaims` after deleting a FabricNetwork. 

## [Known issues](#known-issues)

**Adding new peer organizations is not possible with Kubernetes versions 1.19+.**

Kubernetes deprecated Docker in version 1.19, so Argo's default 
[Docker workflow executor](https://github.com/argoproj/argo-workflows/blob/master/docs/workflow-executors.md#docker-docker)
is not an option with Kubernetes versions 1.19+. The other executors cannot distinguish between `stdout` and `stderr` 
which breaks the functionality of `peer-org-flow`. 
See [this issue](https://github.com/argoproj/argo-workflows/issues/5408) for details.

Hopefully this will be fixed with the [emissary executor](https://github.com/argoproj/argo-workflows/blob/master/docs/workflow-executors.md#emissary-emissary) 
when Argo version `3.1` is released.

## [Conclusion](#conclusion)

So happy BlockChaining in Kubernetes :)

And don't forget the first rule of BlockChain club:

**"Do not use BlockChain unless absolutely necessary!"**

*Hakan Eryargi (r a f t)*
