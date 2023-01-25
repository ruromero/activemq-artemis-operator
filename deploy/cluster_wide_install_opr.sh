#!/bin/bash

echo "Deploying cluster-wide operator, dont forget change WATCH_NAMESPACE to empty! and cluser-role-binding subjects namespace"

read -p "Please confirm that you have changed WATCH_NAMESPACE and role-binding to proper values [Y/N]: "
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]
then
  exit 1
fi

DEPLOY_PATH="$( cd -- "$(dirname "$0")" >/dev/null 2>&1 ; pwd -P )"

if oc version; then
    KUBE_CLI=oc
else
    KUBE_CLI=kubectl
fi

$KUBE_CLI create -f $DEPLOY_PATH/crds
$KUBE_CLI create -f $DEPLOY_PATH/service_account.yaml
$KUBE_CLI create -f $DEPLOY_PATH/cluster_role.yaml
$KUBE_CLI create -f $DEPLOY_PATH/cluster_role_binding.yaml
$KUBE_CLI create -f $DEPLOY_PATH/election_role.yaml
$KUBE_CLI create -f $DEPLOY_PATH/election_role_binding.yaml
$KUBE_CLI create -f $DEPLOY_PATH/operator_config.yaml
$KUBE_CLI create -f $DEPLOY_PATH/operator.yaml
