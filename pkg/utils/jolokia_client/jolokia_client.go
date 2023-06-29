/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package jolokia_client

import (
	"context"
	"fmt"
	"strconv"

	"github.com/artemiscloud/activemq-artemis-operator/pkg/resources"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/resources/secrets"
	ss "github.com/artemiscloud/activemq-artemis-operator/pkg/resources/statefulsets"
	mgmt "github.com/artemiscloud/activemq-artemis-operator/pkg/utils/artemis"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/utils/common"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/utils/namer"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	rtclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type JkInfo struct {
	Artemis *mgmt.Artemis
	IP      string
	Ordinal string
}

// Get all matching broker pod infos for a give resource
// parameters:
// resource: the address CR for which the broker pods are gathered
// ssInfos: target statefulsets to look for pods in
// client: the client to access the api server
func GetBrokers(resource types.NamespacedName, ssInfos []ss.StatefulSetInfo, client rtclient.Client) []*JkInfo {
	reqLogger := ctrl.Log.WithValues("Request.Namespace", resource.Namespace, "Request.Name", resource.Name)

	var artemisArray []*JkInfo = nil

	for _, info := range ssInfos {
		jkInfos := GetBrokersFromDNS(namer.SSToCr(info.NamespacedName.Name), info.NamespacedName.Namespace, info.Replicas, client)
		artemisArray = append(artemisArray, jkInfos...)
	}

	reqLogger.Info("Gathered some mgmt array", "size", len(artemisArray))
	return artemisArray
}

// Get brokers Using DNS names in the namespace
func GetBrokersFromDNS(crName string, namespace string, size int32, client rtclient.Client) []*JkInfo {
	reqLogger := ctrl.Log.WithValues("Request.Namespace", namespace, "Request.Name", crName)

	var artemisArray []*JkInfo = nil
	var i int32 = 0

	clusterDomain := common.GetClusterDomain()
	for i = 0; i < size; i++ {
		// from NewHeadlessServiceForCR2 and
		// https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/#a-aaaa-records
		ordinalFqdn := fmt.Sprintf("%s-ss-%d.%s-hdls-svc.%s.svc.%s", crName, i, crName, namespace, clusterDomain)

		pod := &corev1.Pod{}
		podNamespacedName := types.NamespacedName{
			Name:      namer.CrToSS(crName) + "-" + strconv.Itoa(int(i)),
			Namespace: namespace,
		}

		reqLogger.V(2).Info("Trying finding pod " + pod.Name)
		if err := client.Get(context.TODO(), podNamespacedName, pod); err != nil {
			if errors.IsNotFound(err) {
				reqLogger.Error(err, "Pod IsNotFound", "Namespace", namespace, "Name", pod.Name)
			} else {
				reqLogger.Error(err, "Pod lookup error", "Namespace", namespace, "Name", pod.Name)
			}
		} else {
			reqLogger.V(2).Info("Pod found", "Namespace", namespace, "Name", crName)
			containers := pod.Spec.Containers //get env from this
			jolokiaSecretName := crName + "-jolokia-secret"

			jolokiaUser, jolokiaPassword, jolokiaProtocol := resolveJolokiaRequestParams(namespace, client, client.Scheme(), jolokiaSecretName, &containers, podNamespacedName)

			reqLogger.V(2).Info("hostname to use for jolokia ", "hostname", ordinalFqdn)

			artemis := mgmt.GetArtemis(ordinalFqdn, "8161", "amq-broker", jolokiaUser, jolokiaPassword, jolokiaProtocol)

			jkInfo := JkInfo{
				Artemis: artemis,
				IP:      ordinalFqdn,
				Ordinal: strconv.FormatInt(int64(i), 10),
			}
			artemisArray = append(artemisArray, &jkInfo)
		}
	}

	reqLogger.V(2).Info("got mgmt array", "size", len(artemisArray))
	return artemisArray
}

func resolveJolokiaRequestParams(namespace string,
	client rtclient.Client,
	scheme *runtime.Scheme,
	jolokiaSecretName string,
	containers *[]corev1.Container,
	podNamespacedName types.NamespacedName) (string, string, string) {

	var jolokiaUser string
	var jolokiaPassword string
	var jolokiaProtocol string

	userDefined := false
	jolokiaUserFromSecret := secrets.GetValueFromSecret(namespace, jolokiaSecretName, "jolokiaUser", nil, client, scheme, nil)
	if jolokiaUserFromSecret != nil {
		userDefined = true
		jolokiaUser = *jolokiaUserFromSecret
	}
	if userDefined {
		jolokiaPasswordFromSecret := secrets.GetValueFromSecret(namespace, jolokiaSecretName, "jolokiaPassword", nil, client, scheme, nil)
		if jolokiaPasswordFromSecret != nil {
			jolokiaPassword = *jolokiaPasswordFromSecret
		}
	}
	if len(*containers) == 1 {
		envVars := (*containers)[0].Env
		for _, oneVar := range envVars {
			if !userDefined && oneVar.Name == "AMQ_USER" {
				jolokiaUser = getEnvVarValue(&oneVar, &podNamespacedName, client, nil)
			}
			if !userDefined && oneVar.Name == "AMQ_PASSWORD" {
				jolokiaPassword = getEnvVarValue(&oneVar, &podNamespacedName, client, nil)
			}
			if oneVar.Name == "AMQ_CONSOLE_ARGS" {
				jolokiaProtocol = getEnvVarValue(&oneVar, &podNamespacedName, client, nil)
			}
			if jolokiaUser != "" && jolokiaPassword != "" && jolokiaProtocol != "" {
				break
			}
		}
	}

	if jolokiaProtocol == "" {
		jolokiaProtocol = "http"
	} else {
		jolokiaProtocol = "https"
	}

	return jolokiaUser, jolokiaPassword, jolokiaProtocol
}

func getEnvVarValue(envVar *corev1.EnvVar, namespace *types.NamespacedName, client rtclient.Client, labels map[string]string) string {
	var result string
	if envVar.Value == "" {
		result = getEnvVarValueFromSecret(envVar.Name, envVar.ValueFrom, namespace, client, labels)
	} else {
		result = envVar.Value
	}
	return result
}

func getEnvVarValueFromSecret(envName string, varSource *corev1.EnvVarSource, namespace *types.NamespacedName, client rtclient.Client, labels map[string]string) string {

	reqLogger := ctrl.Log.WithValues("Namespace", namespace.Namespace, "Name", namespace.Name)

	var result string = ""

	secretName := varSource.SecretKeyRef.LocalObjectReference.Name
	secretKey := varSource.SecretKeyRef.Key

	namespacedName := types.NamespacedName{
		Name:      secretName,
		Namespace: namespace.Namespace,
	}
	// Attempt to retrieve the secret
	stringDataMap := map[string]string{
		envName: "",
	}
	theSecret := secrets.NewSecret(namespacedName, secretName, stringDataMap, labels)
	var err error = nil
	if err = resources.Retrieve(namespacedName, client, theSecret); err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Secret IsNotFound.", "Secret Name", secretName, "Key", secretKey)
		}
	} else {
		elem, ok := theSecret.Data[envName]
		if ok {
			result = string(elem)
		}
	}
	return result
}
