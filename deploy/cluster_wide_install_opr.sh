#!/bin/bash
createSecret() {
  echo "creating secret $1 with $2 and $3 in namespace $TARGET_NAMESPACE"
  OPENSSL_CFG=$(cat <<-END
[req]
distinguished_name = req_distinguished_name
x509_extensions = v3_req
prompt = no
[req_distinguished_name]
CN = artemiscloud.io
[v3_req]
keyUsage = keyEncipherment, dataEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names
[alt_names]
DNS.1 = $2
DNS.2 = $3

END
)

  echo "$OPENSSL_CFG" | openssl req -x509 -newkey rsa:4096 -nodes -days 3650 -keyout tls.key -out tls.crt -config -
  CERT_B64=$(base64 -w 0 tls.crt)
  $KUBE_CLI create secret tls $1 --key="tls.key" --cert="tls.crt" -n $TARGET_NAMESPACE
}

echo "Deploying cluster-wide operator"
USE_CERT_MGR="false"
while getopts 'cn:h' opt; do
  case "$opt" in
    c)
      echo "Using Cert-Manager..."
      USE_CERT_MGR="true"
      ;;
    n)
      TARGET_NAMESPACE="$OPTARG"
      ;;
    h)
      echo "Usage: $(basename $0) [-c] [-n target_namespace]"
      echo "where -c means to use cert-manager to config webhooks"
      echo "and -n to pass the target namespace to install the operator"
      exit 0
      ;;

    ?)
      echo "Invalid command option.\nUsage: $(basename $0) [-c] [-n target_namespace]"
      echo "where -c means to use cert-manager to config webhooks"
      echo "and -n to pass the target namespace to install the operator"
      exit 1
      ;;
  esac
done
shift "$(($OPTIND -1))"

read -p "Enter namespaces to watch (empty for all namespaces): " WATCH_NAMESPACE
if [ -z ${WATCH_NAMESPACE} ]; then
  WATCH_NAMESPACE="*"
fi

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

if [ $USE_CERT_MGR == "true" ]; then

  for CRD in $DEPLOY_PATH/crds/*.yaml; do
    sed -e "s|inject-ca-from: activemq-artemis-operator/|inject-ca-from: $TARGET_NAMESPACE/|g" -e "s|namespace: activemq-artemis-operator|namespace: $TARGET_NAMESPACE|g" -e "/caBundle:/d" $CRD | $KUBE_CLI apply -f -
  done

  $KUBE_CLI apply -f $DEPLOY_PATH/service_account.yaml -n $TARGET_NAMESPACE
  $KUBE_CLI apply -f $DEPLOY_PATH/cluster_role.yaml
  sed "s/namespace:.*/namespace: ${TARGET_NAMESPACE}/" $DEPLOY_PATH/cluster_role_binding.yaml | $KUBE_CLI apply -f -
  $KUBE_CLI apply -f $DEPLOY_PATH/election_role.yaml -n $TARGET_NAMESPACE
  $KUBE_CLI apply -f $DEPLOY_PATH/election_role_binding.yaml -n $TARGET_NAMESPACE
  $KUBE_CLI apply -f $DEPLOY_PATH/operator_config.yaml -n $TARGET_NAMESPACE
  $KUBE_CLI apply -f $DEPLOY_PATH/Issuer_activemq-artemis-selfsigned-issuer.yaml -n $TARGET_NAMESPACE
  sed "s|activemq-artemis-operator.svc|$TARGET_NAMESPACE.svc|g" $DEPLOY_PATH/Certificate_activemq-artemis-serving-cert.yaml | $KUBE_CLI apply -n $TARGET_NAMESPACE -f -
  $KUBE_CLI apply -f $DEPLOY_PATH/Service_activemq-artemis-webhook-service.yaml -n $TARGET_NAMESPACE

  # need replace namespace in ca-injector annotations
  sed -e "s|inject-ca-from: activemq-artemis-operator/|inject-ca-from: $TARGET_NAMESPACE/|g" -e "s|namespace: activemq-artemis-operator|namespace: $TARGET_NAMESPACE|g" -e "/caBundle:/d" $DEPLOY_PATH/MutatingWebhookConfiguration_activemq-artemis-mutating-webhook-configuration.yaml | $KUBE_CLI apply -f -
  sed -e "s|inject-ca-from: activemq-artemis-operator/|inject-ca-from: $TARGET_NAMESPACE/|g" -e "s|namespace: activemq-artemis-operator|namespace: $TARGET_NAMESPACE|g" -e "/caBundle:/d" $DEPLOY_PATH/ValidatingWebhookConfiguration_activemq-artemis-validating-webhook-configuration.yaml | $KUBE_CLI apply -f -

  sed -e "/- name: WATCH_NAMESPACE/, /fieldPath: metadata.namespace/ {s/valueFrom:/value: '${WATCH_NAMESPACE}'/; t; /WATCH/!d }" $DEPLOY_PATH/operator.yaml | $KUBE_CLI apply -n $TARGET_NAMESPACE -f -
else
  # the cert's san need to have *.<namespace>.svc and *.<namespace>.svc.cluster.local
  createSecret "webhook-server-cert" "*.$TARGET_NAMESPACE.svc" "*.$TARGET_NAMESPACE.svc.cluster.local"
  
  $KUBE_CLI apply -f $DEPLOY_PATH/crds
  # need to change webhook service namespace and caBundle using the generated secret
  sed -e "s|namespace: activemq-artemis-operator|namespace: $TARGET_NAMESPACE|g" -e "s|caBundle: .*|caBundle: $CERT_B64|g" $DEPLOY_PATH/crds/broker_activemqartemis_crd.yaml | $KUBE_CLI apply -f -
  $KUBE_CLI apply -f $DEPLOY_PATH/service_account.yaml -n $TARGET_NAMESPACE
  $KUBE_CLI apply -f $DEPLOY_PATH/cluster_role.yaml
  sed "s/namespace:.*/namespace: ${TARGET_NAMESPACE}/" $DEPLOY_PATH/cluster_role_binding.yaml | $KUBE_CLI apply -f -
  $KUBE_CLI apply -f $DEPLOY_PATH/election_role.yaml -n $TARGET_NAMESPACE
  $KUBE_CLI apply -f $DEPLOY_PATH/election_role_binding.yaml -n $TARGET_NAMESPACE
  $KUBE_CLI apply -f $DEPLOY_PATH/operator_config.yaml -n $TARGET_NAMESPACE
  $KUBE_CLI apply -f $DEPLOY_PATH/Service_activemq-artemis-webhook-service.yaml -n $TARGET_NAMESPACE
  # need to change webhook service namespace and caBundle using values generated in secret
  sed -e "s|namespace: activemq-artemis-operator|namespace: $TARGET_NAMESPACE|g" -e "/inject-ca-from:/d" -e "s|caBundle: .*|caBundle: $CERT_B64|g" $DEPLOY_PATH/MutatingWebhookConfiguration_activemq-artemis-mutating-webhook-configuration.yaml | $KUBE_CLI apply -f -
  sed -e "s|namespace: activemq-artemis-operator|namespace: $TARGET_NAMESPACE|g" -e "/inject-ca-from:/d" -e "s|caBundle: .*|caBundle: $CERT_B64|g" $DEPLOY_PATH/ValidatingWebhookConfiguration_activemq-artemis-validating-webhook-configuration.yaml | $KUBE_CLI apply -f -

  sed -e "/- name: WATCH_NAMESPACE/, /fieldPath: metadata.namespace/ {s/valueFrom:/value: '${WATCH_NAMESPACE}'/; t; /WATCH/!d }" $DEPLOY_PATH/operator.yaml | $KUBE_CLI apply -n $TARGET_NAMESPACE -f -
fi
