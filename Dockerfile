# Build the manager binary
FROM golang:1.19 as builder

ARG TARGETOS
ARG TARGETARCH

ENV GO_MODULE=github.com/artemiscloud/activemq-artemis-operator

### BEGIN REMOTE SOURCE
# Use the COPY instruction only inside the REMOTE SOURCE block
# Use the COPY instruction only to copy files to the container path $REMOTE_SOURCE_DIR/app
ARG REMOTE_SOURCE_DIR=/tmp/remote_source
RUN mkdir -p $REMOTE_SOURCE_DIR/app
WORKDIR $REMOTE_SOURCE_DIR/app
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
COPY entrypoint/ entrypoint/
COPY pkg/ pkg/
COPY version/ version/
### END REMOTE SOURCE

# Set up the workspace
RUN mkdir -p /workspace
RUN mv $REMOTE_SOURCE_DIR/app /workspace
WORKDIR /workspace/app

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -ldflags="-X '${GO_MODULE}/version.BuildTimestamp=`date '+%Y-%m-%dT%H:%M:%S'`'" -o /workspace/manager main.go

FROM registry.access.redhat.com/ubi8:8.6-855 as base-env

ENV BROKER_NAME=activemq-artemis
ENV USER_UID=1000
ENV USER_NAME=${BROKER_NAME}-operator
ENV USER_HOME=/home/${USER_NAME}
ENV OPERATOR=${USER_HOME}/bin/${BROKER_NAME}-operator

WORKDIR /

# Create operator bin
RUN mkdir -p ${USER_HOME}/bin

# Copy the manager binary
COPY --from=builder /workspace/manager ${OPERATOR}

# Copy the entrypoint script
COPY --from=builder /workspace/app/entrypoint/entrypoint ${USER_HOME}/bin/entrypoint

# Set operator bin owner and permissions
RUN chown -R `id -u`:0 ${USER_HOME}/bin && chmod -R 755 ${USER_HOME}/bin

# Upgrade packages
RUN dnf update -y --setopt=install_weak_deps=0 && rm -rf /var/cache/yum

USER ${USER_UID}
ENTRYPOINT ["${USER_HOME}/bin/entrypoint"]

LABEL name="artemiscloud/activemq-artemis-operator"
LABEL description="ActiveMQ Artemis Broker Operator"
LABEL maintainer="Roddie Kieley <rkieley@redhat.com>"
LABEL version="1.0.11"
