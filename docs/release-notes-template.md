# Install Operator

Following command will install the FabricNetwork CRD and Fabric operator to namespace `fabric-operator`.
```
kubectl apply -f https://github.com/raftAtGit/hl-fabric-operator/releases/download/{VERSION}/install.yaml
```

# Install CLI

## Linux

```
# Download the binary
curl -sLO https://github.com/raftAtGit/hl-fabric-operator/releases/download/{VERSION}/linux-amd64.tar.gz

# Uncompress
tar xf linux-amd64.tar.gz

# Make binary executable
chmod +x rfabric

# Move binary to path
mv rfabric /usr/local/bin/

# Test installation
rfabric --help
```

## Mac

```
# Download the binary
curl -sLO https://github.com/raftAtGit/hl-fabric-operator/releases/download/{VERSION}/darwin-amd64.tar.gz

# Uncompress
tar xf darwin-amd64.tar.gz

# Make binary executable
chmod +x rfabric

# Move binary to path
mv rfabric /usr/local/bin/

# Test installation
rfabric --help
```
