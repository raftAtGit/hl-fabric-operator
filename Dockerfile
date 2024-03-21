# download HL Fabric binaries and Helm
FROM curlimages/curl as curl

USER root
RUN apk update && apk add bash

WORKDIR /fabric
RUN curl -sSL http://bit.ly/2ysbOFE | bash -s -- 1.4.9 -d -s

WORKDIR /helm
RUN curl https://get.helm.sh/helm-v3.5.2-linux-386.tar.gz --output helm.tar.gz \
    && tar xf helm.tar.gz

# clone PIVT repository
FROM alpine/git as git

WORKDIR /workspace
RUN git clone https://github.com/raftAtGit/PIVT.git \
    && cd PIVT \
    && git checkout a10c7bd4e628ef81d50508a84b5deaaf103d8e29

# Install hlf-kube Helm chart dependencies (Kafka)
COPY --from=curl /helm/linux-386/helm /usr/local/bin/
RUN cd /workspace/PIVT/fabric-kube/ \
    && helm dependency update ./hlf-kube/

# Build the manager binary
FROM golang:1.15 as builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY api/ api/
COPY controllers/ controllers/

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -a -o manager main.go

# Actual runtime image
FROM alpine

WORKDIR /
COPY --from=builder /workspace/manager .
COPY --from=git /workspace/PIVT /opt/fabric-operator/PIVT/
COPY --from=curl /fabric/bin/configtxgen /fabric/bin/cryptogen /fabric/bin/configtxlator /opt/hlf/

ENV PATH "$PATH:/opt/hlf"

# one way to run Fabric binaries in Alpine container
# see https://stackoverflow.com/a/59367690/3134813
RUN apk add --no-cache libc6-compat

RUN mkdir -p /var/fabric-operator \
    && chmod 777 /var/fabric-operator
# USER 65532:65532
# USER root

ENTRYPOINT ["/manager"]
