#!/bin/bash

echo "Undeploy everything..."

DEPLOY_PATH="$( cd -- "$(dirname "$0")" >/dev/null 2>&1 ; pwd -P )/install"

if oc version; then
    KUBE_CLI=oc
else
    KUBE_CLI=kubectl
fi

$KUBE_CLI delete -f $DEPLOY_PATH/010_crd_artemis.yaml
$KUBE_CLI delete -f $DEPLOY_PATH/020_crd_artemis_security.yaml
$KUBE_CLI delete -f $DEPLOY_PATH/030_crd_artemis_address.yaml
$KUBE_CLI delete -f $DEPLOY_PATH/040_crd_artemis_scaledown.yaml
$KUBE_CLI delete -f $DEPLOY_PATH/050_service_account.yaml
$KUBE_CLI delete -f $DEPLOY_PATH/060_cluster_role.yaml
$KUBE_CLI delete -f $DEPLOY_PATH/060_namespace_role.yaml
$KUBE_CLI delete -f $DEPLOY_PATH/060_common_cluster_role.yaml
$KUBE_CLI delete -f $DEPLOY_PATH/070_cluster_role_binding.yaml
$KUBE_CLI delete -f $DEPLOY_PATH/070_namespace_role_binding.yaml
$KUBE_CLI delete -f $DEPLOY_PATH/070_common_cluster_role_binding.yaml
$KUBE_CLI delete -f $DEPLOY_PATH/080_election_role.yaml
$KUBE_CLI delete -f $DEPLOY_PATH/090_election_role_binding.yaml
$KUBE_CLI delete -f $DEPLOY_PATH/100_operator_config.yaml
$KUBE_CLI delete -f $DEPLOY_PATH/110_operator.yaml
