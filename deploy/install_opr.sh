#!/bin/bash

echo "Deploying single watch namespace Operator"

DEPLOY_PATH="$( cd -- "$(dirname "$0")" >/dev/null 2>&1 ; pwd -P )/resources"

if oc version; then
    KUBE_CLI=oc
else
    KUBE_CLI=kubectl
fi

$KUBE_CLI create -f $DEPLOY_PATH/crd_artemis.yaml
$KUBE_CLI create -f $DEPLOY_PATH/crd_artemis_security.yaml
$KUBE_CLI create -f $DEPLOY_PATH/crd_artemis_address.yaml
$KUBE_CLI create -f $DEPLOY_PATH/crd_artemis_scaledown.yaml
$KUBE_CLI create -f $DEPLOY_PATH/service_account.yaml
$KUBE_CLI create -f $DEPLOY_PATH/namespace_role.yaml
$KUBE_CLI create -f $DEPLOY_PATH/namespace_role_binding.yaml
$KUBE_CLI create -f $DEPLOY_PATH/election_role.yaml
$KUBE_CLI create -f $DEPLOY_PATH/election_role_binding.yaml
$KUBE_CLI create -f $DEPLOY_PATH/operator_config.yaml
$KUBE_CLI create -f $DEPLOY_PATH/operator.yaml