#!/bin/bash

echo "Undeploy everything..."

while getopts 'n:h' opt; do
  case "$opt" in
    n)
      TARGET_NAMESPACE="$OPTARG"
      ;;
    h)
      echo "Usage: $(basename $0) [-n target_namespace]"
      echo "where -n is the target namespace to uninstall the operator"
      exit 0
      ;;

    ?)
      echo "Invalid command option."
      echo "Usage: $(basename $0) [-n target_namespace]"
      echo "where -n is the target namespace to uninstall the operator"
      exit 1
      ;;
  esac
done
shift "$(($OPTIND -1))"

DEPLOY_PATH="$( cd -- "$(dirname "$0")" >/dev/null 2>&1 ; pwd -P )"

if oc version; then
    KUBE_CLI=oc
    CURRENT_NAMESPACE=$(oc project | cut -d '"' -f2)
else
    KUBE_CLI=kubectl
    CURRENT_NAMESPACE=$(kubectl config view --minify -o jsonpath='{..namespace}')
fi

if [[ $TARGET_NAMESPACE == "" ]]; then
  echo using current namespace $CURRENT_NAMESPACE
  TARGET_NAMESPACE=$CURRENT_NAMESPACE
fi
echo "Target namespace: $TARGET_NAMESPACE"

$KUBE_CLI delete -f $DEPLOY_PATH/crds
$KUBE_CLI delete -f $DEPLOY_PATH/service_account.yaml -n $TARGET_NAMESPACE
$KUBE_CLI delete -f $DEPLOY_PATH/role.yaml -n $TARGET_NAMESPACE
$KUBE_CLI delete -f $DEPLOY_PATH/role_binding.yaml -n $TARGET_NAMESPACE
$KUBE_CLI delete -f $DEPLOY_PATH/cluster_role.yaml
$KUBE_CLI delete -f $DEPLOY_PATH/cluster_role_binding.yaml
$KUBE_CLI delete -f $DEPLOY_PATH/election_role.yaml -n $TARGET_NAMESPACE
$KUBE_CLI delete -f $DEPLOY_PATH/election_role_binding.yaml -n $TARGET_NAMESPACE
$KUBE_CLI delete -f $DEPLOY_PATH/operator_config.yaml -n $TARGET_NAMESPACE
$KUBE_CLI delete -f $DEPLOY_PATH/operator.yaml -n $TARGET_NAMESPACE

$KUBE_CLI delete -f $DEPLOY_PATH/Issuer_activemq-artemis-selfsigned-issuer.yaml -n $TARGET_NAMESPACE
$KUBE_CLI delete -f $DEPLOY_PATH/Certificate_activemq-artemis-serving-cert.yaml -n $TARGET_NAMESPACE
$KUBE_CLI delete -f $DEPLOY_PATH/MutatingWebhookConfiguration_activemq-artemis-mutating-webhook-configuration.yaml
$KUBE_CLI delete -f $DEPLOY_PATH/ValidatingWebhookConfiguration_activemq-artemis-validating-webhook-configuration.yaml
$KUBE_CLI delete -f $DEPLOY_PATH/Service_activemq-artemis-webhook-service.yaml -n $TARGET_NAMESPACE
$KUBE_CLI delete secret webhook-server-cert -n $TARGET_NAMESPACE


