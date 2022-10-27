#!/bin/bash

echo "Deploying cluster-wide Operator"

read -p "Enter namespaces to watch (empty for all namespaces): " WATCH_NAMESPACE
if [ -z ${WATCH_NAMESPACE} ]; then
  WATCH_NAMESPACE="*"
fi

DEPLOY_PATH="$( cd -- "$(dirname "$0")" >/dev/null 2>&1 ; pwd -P )/install"

if oc version; then
    KUBE_CLI=oc
else
    KUBE_CLI=kubectl
fi

$KUBE_CLI create -f $DEPLOY_PATH/010_crd_artemis.yaml
$KUBE_CLI create -f $DEPLOY_PATH/020_crd_artemis_security.yaml
$KUBE_CLI create -f $DEPLOY_PATH/030_crd_artemis_address.yaml
$KUBE_CLI create -f $DEPLOY_PATH/040_crd_artemis_scaledown.yaml
$KUBE_CLI create -f $DEPLOY_PATH/050_service_account.yaml
$KUBE_CLI create -f $DEPLOY_PATH/060_cluster_role.yaml
SERVICE_ACCOUNT_NS="$(kubectl get -f $DEPLOY_PATH/050_service_account.yaml -o jsonpath='{.metadata.namespace}')"
sed "s/namespace:.*/namespace: ${SERVICE_ACCOUNT_NS}/" $DEPLOY_PATH/070_cluster_role_binding.yaml | kubectl apply -f -
$KUBE_CLI create -f $DEPLOY_PATH/080_election_role.yaml
$KUBE_CLI create -f $DEPLOY_PATH/090_election_role_binding.yaml
$KUBE_CLI create -f $DEPLOY_PATH/100_operator_config.yaml
sed -e "/WATCH_NAMESPACE/,/- name/ { /WATCH_NAMESPACE/b; /valueFrom:/bx; /- name/b; d; :x s/valueFrom:/value: '${WATCH_NAMESPACE}'/}" $DEPLOY_PATH/110_operator.yaml | kubectl apply -f -
