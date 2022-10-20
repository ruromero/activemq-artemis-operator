package controllers

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/adler32"
	osruntime "runtime"
	"sort"

	"github.com/RHsyseng/operator-utils/pkg/logs"
	"github.com/RHsyseng/operator-utils/pkg/olm"
	"github.com/RHsyseng/operator-utils/pkg/resource/compare"
	"github.com/RHsyseng/operator-utils/pkg/resource/read"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/resources"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/resources/containers"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/resources/ingresses"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/resources/persistentvolumeclaims"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/resources/pods"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/resources/routes"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/resources/secrets"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/resources/serviceports"
	ss "github.com/artemiscloud/activemq-artemis-operator/pkg/resources/statefulsets"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/utils/channels"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/utils/common"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/utils/cr2jinja2"
	jolokia_client "github.com/artemiscloud/activemq-artemis-operator/pkg/utils/jolokia"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/utils/namer"
	"github.com/artemiscloud/activemq-artemis-operator/version"
	"github.com/go-logr/logr"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"

	rtclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/artemiscloud/activemq-artemis-operator/pkg/resources/environments"
	svc "github.com/artemiscloud/activemq-artemis-operator/pkg/resources/services"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/resources/volumes"

	"reflect"

	routev1 "github.com/openshift/api/route/v1"
	netv1 "k8s.io/api/networking/v1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	brokerv1beta1 "github.com/artemiscloud/activemq-artemis-operator/api/v1beta1"

	"strconv"
	"strings"

	"os"
)

const (
	ImageNamePrefix                  = "RELATED_IMAGE_ActiveMQ_Artemis_Broker_"
	defaultLivenessProbeInitialDelay = 5
	TCPLivenessPort                  = 8161
)

var defaultMessageMigration bool = true
var lastStatusMap map[types.NamespacedName]olm.DeploymentStatus = make(map[types.NamespacedName]olm.DeploymentStatus)

// the helper script looks for "/amq/scripts/post-config.sh"
// and run it if exists.
var initHelperScript = "/opt/amq-broker/script/default.sh"
var brokerConfigRoot = "/amq/init/config"
var configCmd = "/opt/amq/bin/launch.sh"

//default ApplyRule for address-settings
var defApplyRule string = "merge_all"
var yacfgProfileVersion = version.YacfgProfileVersionFromFullVersion[version.LatestVersion]

type ActiveMQArtemisReconcilerImpl struct {
	requestedResources []rtclient.Object
	deployed           map[reflect.Type][]rtclient.Object
}

type ValueInfo struct {
	Value    string
	AutoGen  bool
	Internal bool //if true put this value to the internal secret
}

type ActiveMQArtemisIReconciler interface {
	Process(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, scheme *runtime.Scheme, firstTime bool) uint32
	ProcessStatefulSet(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, log logr.Logger, firstTime bool) (*appsv1.StatefulSet, bool)
	ProcessCredentials(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, scheme *runtime.Scheme, currentStatefulSet *appsv1.StatefulSet) uint32
	ProcessDeploymentPlan(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, scheme *runtime.Scheme, currentStatefulSet *appsv1.StatefulSet, firstTime bool) uint32
	ProcessAcceptorsAndConnectors(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, scheme *runtime.Scheme, currentStatefulSet *appsv1.StatefulSet) uint32
	ProcessConsole(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, scheme *runtime.Scheme, currentStatefulSet *appsv1.StatefulSet)
	ProcessResources(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, scheme *runtime.Scheme, currentStatefulSet *appsv1.StatefulSet) uint8
}

func (reconciler *ActiveMQArtemisReconcilerImpl) Process(customResource *brokerv1beta1.ActiveMQArtemis, namer Namers, client rtclient.Client, scheme *runtime.Scheme) bool {

	logger := logs.GetLogger("controller_v1beta1activemqartemis")
	logger.Info("Reconciler Processing...", "Operator version", version.Version, "ActiveMQArtemis release", customResource.Spec.Version)
	logger.Info("Reconciler Processing...", "CRD.Name", customResource.Name, "CRD ver", customResource.ObjectMeta.ResourceVersion, "CRD Gen", customResource.ObjectMeta.Generation)

	reconciler.CurrentDeployedResources(customResource, client)

	// currentStateful Set is a clone of what exists if already deployed
	// what follows should transform the resources using the crd
	// if the transformation results in some change, process resources will respect that
	// comparisons should not be necessary, leave that to process resources
	currentStatefulSet := reconciler.ProcessStatefulSet(customResource, namer, client, logger)

	reconciler.ProcessDeploymentPlan(customResource, namer, client, scheme, currentStatefulSet)

	reconciler.ProcessCredentials(customResource, namer, client, scheme, currentStatefulSet)

	reconciler.ProcessAcceptorsAndConnectors(customResource, namer, client, scheme, currentStatefulSet)

	reconciler.ProcessConsole(customResource, namer, client, scheme, currentStatefulSet)

	// mods to env var values sourced from secrets are not detected by process resources
	// track updates in trigger env var that has a total checksum
	trackSecretCheckSumInEnvVar(reconciler.requestedResources, currentStatefulSet.Spec.Template.Spec.Containers)

	reconciler.trackDesired(currentStatefulSet)

	// this should apply any deltas/updates
	reconciler.ProcessResources(customResource, client, scheme)

	logger.Info("Reconciler Processing... complete", "CRD ver:", customResource.ObjectMeta.ResourceVersion, "CRD Gen:", customResource.ObjectMeta.Generation)

	// requeue
	return false
}

func trackSecretCheckSumInEnvVar(requestedResources []rtclient.Object, container []corev1.Container) {

	// find desired secrets and checksum their 'sorted' values
	digest := adler32.New()
	for _, obj := range requestedResources {
		if secret, ok := obj.(*corev1.Secret); ok {
			// ignore secret for persistence of cr
			if strings.HasSuffix(secret.Name, "-secret") {
				// note use of StringData to match MakeSecret for the initial create case
				if len(secret.StringData) > 0 {
					for _, k := range sortedKeys(secret.StringData) {
						digest.Write([]byte(secret.StringData[k]))
					}
				} else {
					for _, k := range sortedKeysStringKeyByteValue(secret.Data) {
						digest.Write(secret.Data[k])
					}
				}
			}
		}
	}
	environments.TrackSecretCheckSumInRollCount(hex.EncodeToString(digest.Sum(nil)), container)
}

func (reconciler *ActiveMQArtemisReconcilerImpl) cloneOfDeployed(kind reflect.Type, name string) rtclient.Object {
	for _, obj := range reconciler.deployed[kind] {
		if obj.GetName() == name {
			return obj.DeepCopyObject().(rtclient.Object)
		}
	}
	return nil
}

func (reconciler *ActiveMQArtemisReconcilerImpl) ProcessStatefulSet(customResource *brokerv1beta1.ActiveMQArtemis, namer Namers, client rtclient.Client, log *zap.SugaredLogger) *appsv1.StatefulSet {

	ssNamespacedName := types.NamespacedName{
		Namespace: customResource.Namespace,
		Name:      namer.SsNameBuilder.Name(),
	}

	var currentStatefulSet *appsv1.StatefulSet
	obj := reconciler.cloneOfDeployed(reflect.TypeOf(appsv1.StatefulSet{}), ssNamespacedName.Name)
	if obj != nil {
		currentStatefulSet = obj.(*appsv1.StatefulSet)
	}

	log.Info("Reconciling desired statefulset", "name", ssNamespacedName, "current", currentStatefulSet)
	currentStatefulSet = reconciler.NewStatefulSetForCR(customResource, namer, currentStatefulSet)

	labels := namer.LabelBuilder.Labels()
	headlessServiceDefinition := svc.NewHeadlessServiceForCR2(client, namer.SvcHeadlessNameBuilder.Name(), ssNamespacedName.Namespace, serviceports.GetDefaultPorts(), labels)
	if isClustered(customResource) {
		pingServiceDefinition := svc.NewPingServiceDefinitionForCR2(client, namer.SvcPingNameBuilder.Name(), ssNamespacedName.Namespace, labels, labels)
		reconciler.trackDesired(pingServiceDefinition)
	}
	reconciler.trackDesired(headlessServiceDefinition)

	return currentStatefulSet
}

func isClustered(customResource *brokerv1beta1.ActiveMQArtemis) bool {
	if customResource.Spec.DeploymentPlan.Clustered != nil {
		return *customResource.Spec.DeploymentPlan.Clustered
	}
	return true
}

func (reconciler *ActiveMQArtemisReconcilerImpl) ProcessCredentials(customResource *brokerv1beta1.ActiveMQArtemis, namer Namers, client rtclient.Client, scheme *runtime.Scheme, currentStatefulSet *appsv1.StatefulSet) {

	log := logs.GetLogger("controller_v1beta1activemqartemis")
	log.Info("ProcessCredentials")

	envVars := make(map[string]ValueInfo)

	adminUser := ValueInfo{
		Value:   "",
		AutoGen: false,
	}
	adminPassword := ValueInfo{
		Value:   "",
		AutoGen: false,
	}
	// TODO: Remove singular admin level user and password in favour of at least guest and admin access
	secretName := namer.SecretsCredentialsNameBuilder.Name()
	for {
		adminUser.Value = customResource.Spec.AdminUser
		if adminUser.Value != "" {
			break
		}

		if amqUserEnvVar := environments.Retrieve(currentStatefulSet.Spec.Template.Spec.Containers, "AMQ_USER"); nil != amqUserEnvVar {
			adminUser.Value = amqUserEnvVar.Value
		}
		if adminUser.Value != "" {
			break
		}

		adminUser.Value = environments.Defaults.AMQ_USER
		adminUser.AutoGen = true
		// do once
		break
	}
	envVars["AMQ_USER"] = adminUser

	for {
		adminPassword.Value = customResource.Spec.AdminPassword
		if adminPassword.Value != "" {
			break
		}

		if amqPasswordEnvVar := environments.Retrieve(currentStatefulSet.Spec.Template.Spec.Containers, "AMQ_PASSWORD"); nil != amqPasswordEnvVar {
			adminPassword.Value = amqPasswordEnvVar.Value
		}
		if adminPassword.Value != "" {
			break
		}

		adminPassword.Value = environments.Defaults.AMQ_PASSWORD
		adminPassword.AutoGen = true
		break
	}
	envVars["AMQ_PASSWORD"] = adminPassword

	envVars["AMQ_CLUSTER_USER"] = ValueInfo{
		Value:   environments.GLOBAL_AMQ_CLUSTER_USER,
		AutoGen: true,
	}
	envVars["AMQ_CLUSTER_PASSWORD"] = ValueInfo{
		Value:   environments.GLOBAL_AMQ_CLUSTER_PASSWORD,
		AutoGen: true,
	}

	reconciler.sourceEnvVarFromSecret(customResource, namer, currentStatefulSet, &envVars, secretName, client, scheme)
}

func (reconciler *ActiveMQArtemisReconcilerImpl) ProcessDeploymentPlan(customResource *brokerv1beta1.ActiveMQArtemis, namer Namers, client rtclient.Client, scheme *runtime.Scheme, currentStatefulSet *appsv1.StatefulSet) {

	deploymentPlan := &customResource.Spec.DeploymentPlan

	clog.Info("Processing deployment plan", "plan", deploymentPlan, "broker cr", customResource.Name)
	// Ensure the StatefulSet size is the same as the spec
	currentStatefulSet.Spec.Replicas = &deploymentPlan.Size

	aioSyncCausedUpdateOn(deploymentPlan, currentStatefulSet)

	persistentSyncCausedUpdateOn(namer, deploymentPlan, currentStatefulSet)

	if updatedEnvVar := environments.BoolSyncCausedUpdateOn(currentStatefulSet.Spec.Template.Spec.Containers, "AMQ_REQUIRE_LOGIN", deploymentPlan.RequireLogin); updatedEnvVar != nil {
		environments.Update(currentStatefulSet.Spec.Template.Spec.Containers, updatedEnvVar)
	}

	clog.Info("Now sync Message migration", "for cr", customResource.Name)
	syncMessageMigration(customResource, namer, client, scheme)

}

func (reconciler *ActiveMQArtemisReconcilerImpl) ProcessAcceptorsAndConnectors(customResource *brokerv1beta1.ActiveMQArtemis, namer Namers, client rtclient.Client, scheme *runtime.Scheme, currentStatefulSet *appsv1.StatefulSet) {

	acceptorEntry := generateAcceptorsString(customResource, namer, client)
	connectorEntry := generateConnectorsString(customResource, namer, client)

	reconciler.configureAcceptorsExposure(customResource, namer, client, scheme)
	reconciler.configureConnectorsExposure(customResource, namer, client, scheme)

	envVars := make(map[string]ValueInfo)

	envVars["AMQ_ACCEPTORS"] = ValueInfo{
		Value:   acceptorEntry,
		AutoGen: false,
	}

	envVars["AMQ_CONNECTORS"] = ValueInfo{
		Value:   connectorEntry,
		AutoGen: false,
	}

	secretName := namer.SecretsNettyNameBuilder.Name()
	reconciler.sourceEnvVarFromSecret(customResource, namer, currentStatefulSet, &envVars, secretName, client, scheme)
}

func (reconciler *ActiveMQArtemisReconcilerImpl) ProcessConsole(customResource *brokerv1beta1.ActiveMQArtemis, namer Namers, client rtclient.Client, scheme *runtime.Scheme, currentStatefulSet *appsv1.StatefulSet) {

	reconciler.configureConsoleExposure(customResource, namer, client, scheme)
	if !customResource.Spec.Console.SSLEnabled {
		return
	}

	isOpenshift, _ := environments.DetectOpenshift()
	if !isOpenshift && customResource.Spec.Console.Expose {
		//if it is kubernetes the tls termination at ingress point
		//so the console shouldn't be secured.
		return
	}

	sslFlags := ""
	envVarName := "AMQ_CONSOLE_ARGS"
	secretName := namer.SecretsConsoleNameBuilder.Name()
	if customResource.Spec.Console.SSLSecret != "" {
		secretName = customResource.Spec.Console.SSLSecret
	}
	sslFlags = generateConsoleSSLFlags(customResource, namer, client, secretName)
	envVars := make(map[string]ValueInfo)
	envVars[envVarName] = ValueInfo{
		Value:    sslFlags,
		AutoGen:  true,
		Internal: true,
	}

	reconciler.sourceEnvVarFromSecret(customResource, namer, currentStatefulSet, &envVars, secretName, client, scheme)
}

func syncMessageMigration(customResource *brokerv1beta1.ActiveMQArtemis, namer Namers, client rtclient.Client, scheme *runtime.Scheme) {

	var err error = nil
	var retrieveError error = nil

	namespacedName := types.NamespacedName{
		Name:      customResource.Name,
		Namespace: customResource.Namespace,
	}

	ssNames := make(map[string]string)
	ssNames["CRNAMESPACE"] = customResource.Namespace
	ssNames["CRNAME"] = customResource.Name
	ssNames["CLUSTERUSER"] = environments.GLOBAL_AMQ_CLUSTER_USER
	ssNames["CLUSTERPASS"] = environments.GLOBAL_AMQ_CLUSTER_PASSWORD
	ssNames["HEADLESSSVCNAMEVALUE"] = namer.SvcHeadlessNameBuilder.Name()
	ssNames["PINGSVCNAMEVALUE"] = namer.SvcPingNameBuilder.Name()
	ssNames["SERVICE_ACCOUNT"] = os.Getenv("SERVICE_ACCOUNT")
	ssNames["SERVICE_ACCOUNT_NAME"] = os.Getenv("SERVICE_ACCOUNT")
	ssNames["AMQ_CREDENTIALS_SECRET_NAME"] = namer.SecretsCredentialsNameBuilder.Name()

	scaledown := &brokerv1beta1.ActiveMQArtemisScaledown{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ActiveMQArtemisScaledown",
		},
		ObjectMeta: metav1.ObjectMeta{
			Labels:      namer.LabelBuilder.Labels(),
			Name:        customResource.Name,
			Namespace:   customResource.Namespace,
			Annotations: ssNames,
		},
		Spec: brokerv1beta1.ActiveMQArtemisScaledownSpec{
			LocalOnly: isLocalOnly(),
			Resources: customResource.Spec.DeploymentPlan.Resources,
		},
		Status: brokerv1beta1.ActiveMQArtemisScaledownStatus{},
	}

	if nil == customResource.Spec.DeploymentPlan.MessageMigration {
		customResource.Spec.DeploymentPlan.MessageMigration = &defaultMessageMigration
	}

	clustered := isClustered(customResource)

	if *customResource.Spec.DeploymentPlan.MessageMigration && clustered {
		if !customResource.Spec.DeploymentPlan.PersistenceEnabled {
			clog.Info("Won't set up scaledown for non persistent deployment")
			return
		}
		clog.Info("we need scaledown for this cr", "crName", customResource.Name, "scheme", scheme)
		if err = resources.Retrieve(namespacedName, client, scaledown); err != nil {
			// err means not found so create
			clog.Info("Creating builtin drainer CR ", "scaledown", scaledown)
			if retrieveError = resources.Create(customResource, client, scheme, scaledown); retrieveError == nil {
				clog.Info("drainer created successfully", "drainer", scaledown)
			} else {
				clog.Error(retrieveError, "we have error retrieving drainer", "drainer", scaledown, "scheme", scheme)
			}
		}
	} else {
		if err = resources.Retrieve(namespacedName, client, scaledown); err == nil {
			//	ReleaseController(customResource.Name)
			// err means not found so delete
			if retrieveError = resources.Delete(client, scaledown); retrieveError != nil {
				clog.Error(retrieveError, "unable to delete scaledown resource", "drainer", scaledown)
			}
		}
	}
}

func isLocalOnly() bool {
	oprNamespace := os.Getenv("OPERATOR_NAMESPACE")
	watchNamespace := os.Getenv("OPERATOR_WATCH_NAMESPACE")
	return oprNamespace == watchNamespace
}

func (reconciler *ActiveMQArtemisReconcilerImpl) sourceEnvVarFromSecret(fcustomResource *brokerv1beta1.ActiveMQArtemis, namer Namers, currentStatefulSet *appsv1.StatefulSet, envVars *map[string]ValueInfo, secretName string, client rtclient.Client, scheme *runtime.Scheme) {

	log := logs.GetLogger("controller_v1beta1activemqartemis").Named("sourceEnvVarFromSecret")

	namespacedName := types.NamespacedName{
		Name:      secretName,
		Namespace: currentStatefulSet.Namespace,
	}
	// Attempt to retrieve the secret
	stringDataMap := make(map[string]string)
	internalStringDataMap := make(map[string]string)

	for k, v := range *envVars {
		if v.Internal {
			internalStringDataMap[k] = v.Value
		} else {
			stringDataMap[k] = v.Value
		}
	}

	secretDefinition := secrets.NewSecret(namespacedName, secretName, stringDataMap, namer.LabelBuilder.Labels())

	if err := resources.Retrieve(namespacedName, client, secretDefinition); err != nil {
		if errors.IsNotFound(err) {
			log.Info("Did not find secret " + secretName)
		}
	} else {

		log.Info("updating from " + secretName)
		for k := range *envVars {
			elem, exists := secretDefinition.Data[k]
			if strings.Compare(string(elem), (*envVars)[k].Value) != 0 || !exists {
				log.Info("key value not equals or does not exist", "key", k, "exists", exists)
				if !(*envVars)[k].AutoGen || string(elem) == "" {
					secretDefinition.Data[k] = []byte((*envVars)[k].Value)
				}
			}
		}
	}

	// ensure processResources sees it
	reconciler.trackDesired(secretDefinition)

	internalSecretName := secretName + "-internal"
	if len(internalStringDataMap) > 0 {

		internalSecretDefinition := secrets.NewSecret(namespacedName, internalSecretName, internalStringDataMap, namer.LabelBuilder.Labels())
		reconciler.trackDesired(internalSecretDefinition)
	}

	log.Info("Populating env vars references, in order, from secret " + secretName)

	// sort the keys for consistency of reconcilitation as iteration source is a map
	sortedKeys := make([]string, 0, len(*envVars))
	for k := range *envVars {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	for _, envVarName := range sortedKeys {
		envVarInfo := (*envVars)[envVarName]
		secretNameToUse := secretName
		if envVarInfo.Internal {
			secretNameToUse = internalSecretName
		}
		envVarSource := &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: secretNameToUse,
				},
				Key:      envVarName,
				Optional: nil,
			},
		}

		envVarDefinition := &corev1.EnvVar{
			Name:      envVarName,
			Value:     "",
			ValueFrom: envVarSource,
		}
		if retrievedEnvVar := environments.Retrieve(currentStatefulSet.Spec.Template.Spec.Containers, envVarName); nil == retrievedEnvVar {
			log.Info("containers: failed to retrieve " + envVarName + " creating")
			environments.Create(currentStatefulSet.Spec.Template.Spec.Containers, envVarDefinition)
		}

		//custom init container
		if len(currentStatefulSet.Spec.Template.Spec.InitContainers) > 0 {
			if retrievedEnvVar := environments.Retrieve(currentStatefulSet.Spec.Template.Spec.InitContainers, envVarName); nil == retrievedEnvVar {
				log.Info("init_containers: failed to retrieve " + envVarName + " creating")
				environments.Create(currentStatefulSet.Spec.Template.Spec.InitContainers, envVarDefinition)
			}
		}
	}

}

func generateAcceptorsString(customResource *brokerv1beta1.ActiveMQArtemis, namer Namers, client rtclient.Client) string {

	// TODO: Optimize for the single broker configuration
	ensureCOREOn61616Exists := true // as clustered is no longer an option but true by default

	acceptorEntry := ""
	defaultArgs := "tcpSendBufferSize=1048576;tcpReceiveBufferSize=1048576;useEpoll=true;amqpCredits=1000;amqpMinCredits=300"

	var portIncrement int32 = 10
	var currentPortIncrement int32 = 0
	var port61616InUse bool = false
	var i uint32 = 0
	for _, acceptor := range customResource.Spec.Acceptors {
		if acceptor.Port == 0 {
			acceptor.Port = 61626 + currentPortIncrement
			currentPortIncrement += portIncrement
			customResource.Spec.Acceptors[i].Port = acceptor.Port
		}
		if acceptor.Protocols == "" ||
			strings.ToLower(acceptor.Protocols) == "all" {
			acceptor.Protocols = "AMQP,CORE,HORNETQ,MQTT,OPENWIRE,STOMP"
		}
		bindAddress := "ACCEPTOR_IP"
		if acceptor.BindToAllInterfaces != nil && *acceptor.BindToAllInterfaces {
			bindAddress = "0.0.0.0"
		}
		acceptorEntry = acceptorEntry + "<acceptor name=\"" + acceptor.Name + "\">"
		acceptorEntry = acceptorEntry + "tcp:" + "\\/\\/" + bindAddress + ":"
		acceptorEntry = acceptorEntry + fmt.Sprintf("%d", acceptor.Port)
		acceptorEntry = acceptorEntry + "?protocols=" + strings.ToUpper(acceptor.Protocols)
		// TODO: Evaluate more dynamic messageMigration
		if acceptor.Port == 61616 {
			port61616InUse = true
		}
		if ensureCOREOn61616Exists &&
			(acceptor.Port == 61616) &&
			!strings.Contains(strings.ToUpper(acceptor.Protocols), "CORE") {
			acceptorEntry = acceptorEntry + ",CORE"
		}
		if acceptor.SSLEnabled {
			secretName := customResource.Name + "-" + acceptor.Name + "-secret"
			if acceptor.SSLSecret != "" {
				secretName = acceptor.SSLSecret
			}
			acceptorEntry = acceptorEntry + ";" + generateAcceptorConnectorSSLArguments(customResource, namer, client, secretName)
			sslOptionalArguments := generateAcceptorSSLOptionalArguments(acceptor)
			if sslOptionalArguments != "" {
				acceptorEntry = acceptorEntry + ";" + sslOptionalArguments
			}
		}
		if acceptor.AnycastPrefix != "" {
			safeAnycastPrefix := strings.Replace(acceptor.AnycastPrefix, "/", "\\/", -1)
			acceptorEntry = acceptorEntry + ";" + "anycastPrefix=" + safeAnycastPrefix
		}
		if acceptor.MulticastPrefix != "" {
			safeMulticastPrefix := strings.Replace(acceptor.MulticastPrefix, "/", "\\/", -1)
			acceptorEntry = acceptorEntry + ";" + "multicastPrefix=" + safeMulticastPrefix
		}
		if acceptor.ConnectionsAllowed > 0 {
			acceptorEntry = acceptorEntry + ";" + "connectionsAllowed=" + fmt.Sprintf("%d", acceptor.ConnectionsAllowed)
		}
		if acceptor.AMQPMinLargeMessageSize > 0 {
			acceptorEntry = acceptorEntry + ";" + "amqpMinLargeMessageSize=" + fmt.Sprintf("%d", acceptor.AMQPMinLargeMessageSize)
		}
		if acceptor.SupportAdvisory != nil {
			acceptorEntry = acceptorEntry + ";" + "supportAdvisory=" + strconv.FormatBool(*acceptor.SupportAdvisory)
		}
		if acceptor.SuppressInternalManagementObjects != nil {
			acceptorEntry = acceptorEntry + ";" + "suppressInternalManagementObjects=" + strconv.FormatBool(*acceptor.SuppressInternalManagementObjects)
		}
		acceptorEntry = acceptorEntry + ";" + defaultArgs

		acceptorEntry = acceptorEntry + "<\\/acceptor>"

		// Used for indexing the original acceptor port to update it if required
		i = i + 1
	}
	// TODO: Evaluate more dynamic messageMigration
	if ensureCOREOn61616Exists && !port61616InUse {
		acceptorEntry = acceptorEntry + "<acceptor name=\"" + "scaleDown" + "\">"
		acceptorEntry = acceptorEntry + "tcp:" + "\\/\\/" + "ACCEPTOR_IP:"
		acceptorEntry = acceptorEntry + fmt.Sprintf("%d", 61616)
		acceptorEntry = acceptorEntry + "?protocols=" + "CORE"
		acceptorEntry = acceptorEntry + ";" + defaultArgs
		// TODO: SSL
		acceptorEntry = acceptorEntry + "<\\/acceptor>"
	}

	return acceptorEntry
}

func generateConnectorsString(customResource *brokerv1beta1.ActiveMQArtemis, namer Namers, client rtclient.Client) string {

	connectorEntry := ""
	connectors := customResource.Spec.Connectors
	for _, connector := range connectors {
		if connector.Type == "" {
			connector.Type = "tcp"
		}
		connectorEntry = connectorEntry + "<connector name=\"" + connector.Name + "\">"
		connectorEntry = connectorEntry + strings.ToLower(connector.Type) + ":\\/\\/" + strings.ToLower(connector.Host) + ":"
		connectorEntry = connectorEntry + fmt.Sprintf("%d", connector.Port)

		if connector.SSLEnabled {
			secretName := customResource.Name + "-" + connector.Name + "-secret"
			if connector.SSLSecret != "" {
				secretName = connector.SSLSecret
			}
			connectorEntry = connectorEntry + ";" + generateAcceptorConnectorSSLArguments(customResource, namer, client, secretName)
			sslOptionalArguments := generateConnectorSSLOptionalArguments(connector)
			if sslOptionalArguments != "" {
				connectorEntry = connectorEntry + ";" + sslOptionalArguments
			}
		}
		connectorEntry = connectorEntry + "<\\/connector>"
	}

	return connectorEntry
}

func (reconciler *ActiveMQArtemisReconcilerImpl) configureAcceptorsExposure(customResource *brokerv1beta1.ActiveMQArtemis, namer Namers, client rtclient.Client, scheme *runtime.Scheme) (bool, error) {

	var i int32 = 0
	var err error = nil
	ordinalString := ""
	causedUpdate := false

	originalLabels := namer.LabelBuilder.Labels()
	namespacedName := types.NamespacedName{
		Name:      customResource.Name,
		Namespace: customResource.Namespace,
	}
	for ; i < customResource.Spec.DeploymentPlan.Size; i++ {
		ordinalString = strconv.Itoa(int(i))
		var serviceRoutelabels = make(map[string]string)
		for k, v := range originalLabels {
			serviceRoutelabels[k] = v
		}
		serviceRoutelabels["statefulset.kubernetes.io/pod-name"] = namer.SsNameBuilder.Name() + "-" + ordinalString

		for _, acceptor := range customResource.Spec.Acceptors {
			serviceDefinition := svc.NewServiceDefinitionForCR(client, namespacedName, acceptor.Name+"-"+ordinalString, acceptor.Port, serviceRoutelabels, namer.LabelBuilder.Labels())

			reconciler.trackDesired(serviceDefinition)

			if acceptor.Expose {

				targetPortName := acceptor.Name + "-" + ordinalString
				targetServiceName := customResource.Name + "-" + targetPortName + "-svc"

				exposureDefinition := reconciler.ExposureDefinitionForCR(namespacedName, serviceRoutelabels, targetServiceName, targetPortName, acceptor.SSLEnabled)
				reconciler.trackDesired(exposureDefinition)
			}
		}
	}

	return causedUpdate, err
}

func (reconciler *ActiveMQArtemisReconcilerImpl) ExposureDefinitionForCR(namespacedName types.NamespacedName, labels map[string]string, targetServiceName string, targetPortName string, passthroughTLS bool) rtclient.Object {

	if isOpenshift, err := environments.DetectOpenshift(); isOpenshift && err == nil {
		clog.Info("creating route for " + targetPortName)
		return routes.NewRouteDefinitionForCR(namespacedName, labels, targetServiceName, targetPortName, passthroughTLS)
	} else {
		clog.Info("creating ingress for " + targetPortName)
		return ingresses.NewIngressForCRWithSSL(namespacedName, labels, targetServiceName, targetPortName, passthroughTLS)
	}
}

func (reconciler *ActiveMQArtemisReconcilerImpl) trackDesired(desired rtclient.Object) {
	reconciler.requestedResources = append(reconciler.requestedResources, desired)
}

func (reconciler *ActiveMQArtemisReconcilerImpl) configureConnectorsExposure(customResource *brokerv1beta1.ActiveMQArtemis, namer Namers, client rtclient.Client, scheme *runtime.Scheme) (bool, error) {

	var i int32 = 0
	var err error = nil
	ordinalString := ""
	causedUpdate := false

	originalLabels := namer.LabelBuilder.Labels()
	namespacedName := types.NamespacedName{
		Name:      customResource.Name,
		Namespace: customResource.Namespace,
	}
	for ; i < customResource.Spec.DeploymentPlan.Size; i++ {
		ordinalString = strconv.Itoa(int(i))
		var serviceRoutelabels = make(map[string]string)
		for k, v := range originalLabels {
			serviceRoutelabels[k] = v
		}
		serviceRoutelabels["statefulset.kubernetes.io/pod-name"] = namer.SsNameBuilder.Name() + "-" + ordinalString

		for _, connector := range customResource.Spec.Connectors {
			serviceDefinition := svc.NewServiceDefinitionForCR(client, namespacedName, connector.Name+"-"+ordinalString, connector.Port, serviceRoutelabels, namer.LabelBuilder.Labels())
			reconciler.trackDesired(serviceDefinition)

			if connector.Expose {

				targetPortName := connector.Name + "-" + ordinalString
				targetServiceName := customResource.Name + "-" + targetPortName + "-svc"
				exposureDefinition := reconciler.ExposureDefinitionForCR(namespacedName, serviceRoutelabels, targetServiceName, targetPortName, connector.SSLEnabled)

				reconciler.trackDesired(exposureDefinition)
			}
		}
	}

	return causedUpdate, err
}

func (reconciler *ActiveMQArtemisReconcilerImpl) configureConsoleExposure(customResource *brokerv1beta1.ActiveMQArtemis, namer Namers, client rtclient.Client, scheme *runtime.Scheme) (bool, error) {

	var i int32 = 0
	var err error = nil
	ordinalString := ""
	causedUpdate := false
	console := customResource.Spec.Console

	originalLabels := namer.LabelBuilder.Labels()
	namespacedName := types.NamespacedName{
		Name:      customResource.Name,
		Namespace: customResource.Namespace,
	}
	for ; i < customResource.Spec.DeploymentPlan.Size; i++ {
		ordinalString = strconv.Itoa(int(i))
		var serviceRoutelabels = make(map[string]string)
		for k, v := range originalLabels {
			serviceRoutelabels[k] = v
		}
		serviceRoutelabels["statefulset.kubernetes.io/pod-name"] = namer.SsNameBuilder.Name() + "-" + ordinalString

		portNumber := int32(8161)
		targetPortName := "wconsj" + "-" + ordinalString
		targetServiceName := customResource.Name + "-" + targetPortName + "-svc"

		serviceDefinition := svc.NewServiceDefinitionForCR(client, namespacedName, targetPortName, portNumber, serviceRoutelabels, namer.LabelBuilder.Labels())

		serviceNamespacedName := types.NamespacedName{
			Name:      serviceDefinition.Name,
			Namespace: customResource.Namespace,
		}
		if console.Expose {
			reconciler.trackDesired(serviceDefinition)
			//causedUpdate, err = resources.Enable(customResource, client, scheme, serviceNamespacedName, serviceDefinition)
		} else {
			causedUpdate, err = resources.Disable(customResource, client, scheme, serviceNamespacedName, serviceDefinition)
		}
		var err error = nil
		isOpenshift := false

		if isOpenshift, err = environments.DetectOpenshift(); err != nil {
			clog.Error(err, "Failed to get env, will try kubernetes")
		}
		if isOpenshift {
			clog.Info("Environment is OpenShift")
			clog.Info("Checking routeDefinition for " + targetPortName)
			routeDefinition := routes.NewRouteDefinitionForCR(namespacedName, serviceRoutelabels, targetServiceName, targetPortName, console.SSLEnabled)
			if console.Expose {
				reconciler.trackDesired(routeDefinition)
			}
		} else {
			clog.Info("Environment is not OpenShift, creating ingress")
			ingressDefinition := ingresses.NewIngressForCRWithSSL(namespacedName, serviceRoutelabels, targetServiceName, targetPortName, console.SSLEnabled)
			if console.Expose {
				reconciler.trackDesired(ingressDefinition)
			}
		}
	}

	return causedUpdate, err
}

func generateConsoleSSLFlags(customResource *brokerv1beta1.ActiveMQArtemis, namer Namers, client rtclient.Client, secretName string) string {

	sslFlags := ""
	secretNamespacedName := types.NamespacedName{
		Name:      secretName,
		Namespace: customResource.Namespace,
	}
	namespacedName := types.NamespacedName{
		Name:      customResource.Name,
		Namespace: customResource.Namespace,
	}
	stringDataMap := map[string]string{}
	userPasswordSecret := secrets.NewSecret(namespacedName, secretName, stringDataMap, namer.LabelBuilder.Labels())

	keyStorePassword := "password"
	keyStorePath := "/etc/" + secretName + "-volume/broker.ks"
	trustStorePassword := "password"
	trustStorePath := "/etc/" + secretName + "-volume/client.ts"
	if err := resources.Retrieve(secretNamespacedName, client, userPasswordSecret); err == nil {
		if string(userPasswordSecret.Data["keyStorePassword"]) != "" {
			keyStorePassword = string(userPasswordSecret.Data["keyStorePassword"])
		}
		if string(userPasswordSecret.Data["keyStorePath"]) != "" {
			keyStorePath = string(userPasswordSecret.Data["keyStorePath"])
		}
		if string(userPasswordSecret.Data["trustStorePassword"]) != "" {
			trustStorePassword = string(userPasswordSecret.Data["trustStorePassword"])
		}
		if string(userPasswordSecret.Data["trustStorePath"]) != "" {
			trustStorePath = string(userPasswordSecret.Data["trustStorePath"])
		}
	}

	sslFlags = sslFlags + " " + "--ssl-key" + " " + keyStorePath
	sslFlags = sslFlags + " " + "--ssl-key-password" + " " + keyStorePassword
	sslFlags = sslFlags + " " + "--ssl-trust" + " " + trustStorePath
	sslFlags = sslFlags + " " + "--ssl-trust-password" + " " + trustStorePassword
	if customResource.Spec.Console.UseClientAuth {
		sslFlags = sslFlags + " " + "--use-client-auth"
	}

	return sslFlags
}

func generateAcceptorConnectorSSLArguments(customResource *brokerv1beta1.ActiveMQArtemis, namer Namers, client rtclient.Client, secretName string) string {

	sslArguments := "sslEnabled=true"
	secretNamespacedName := types.NamespacedName{
		Name:      secretName,
		Namespace: customResource.Namespace,
	}
	namespacedName := types.NamespacedName{
		Name:      customResource.Name,
		Namespace: customResource.Namespace,
	}
	stringDataMap := map[string]string{}
	userPasswordSecret := secrets.NewSecret(namespacedName, secretName, stringDataMap, namer.LabelBuilder.Labels())

	keyStorePassword := "password"
	keyStorePath := "\\/etc\\/" + secretName + "-volume\\/broker.ks"
	trustStorePassword := "password"
	trustStorePath := "\\/etc\\/" + secretName + "-volume\\/client.ts"
	if err := resources.Retrieve(secretNamespacedName, client, userPasswordSecret); err == nil {
		if string(userPasswordSecret.Data["keyStorePassword"]) != "" {
			//noinspection GoUnresolvedReference
			keyStorePassword = strings.ReplaceAll(string(userPasswordSecret.Data["keyStorePassword"]), "/", "\\/")
		}
		if string(userPasswordSecret.Data["keyStorePath"]) != "" {
			//noinspection GoUnresolvedReference
			keyStorePath = strings.ReplaceAll(string(userPasswordSecret.Data["keyStorePath"]), "/", "\\/")
		}
		if string(userPasswordSecret.Data["trustStorePassword"]) != "" {
			//noinspection GoUnresolvedReference
			trustStorePassword = strings.ReplaceAll(string(userPasswordSecret.Data["trustStorePassword"]), "/", "\\/")
		}
		if string(userPasswordSecret.Data["trustStorePath"]) != "" {
			//noinspection GoUnresolvedReference
			trustStorePath = strings.ReplaceAll(string(userPasswordSecret.Data["trustStorePath"]), "/", "\\/")
		}
	}
	sslArguments = sslArguments + ";" + "keyStorePath=" + keyStorePath
	sslArguments = sslArguments + ";" + "keyStorePassword=" + keyStorePassword
	sslArguments = sslArguments + ";" + "trustStorePath=" + trustStorePath
	sslArguments = sslArguments + ";" + "trustStorePassword=" + trustStorePassword

	return sslArguments
}

func generateAcceptorSSLOptionalArguments(acceptor brokerv1beta1.AcceptorType) string {

	sslOptionalArguments := ""

	if acceptor.EnabledCipherSuites != "" {
		sslOptionalArguments = sslOptionalArguments + "enabledCipherSuites=" + acceptor.EnabledCipherSuites
	}
	if acceptor.EnabledProtocols != "" {
		sslOptionalArguments = sslOptionalArguments + ";" + "enabledProtocols=" + acceptor.EnabledProtocols
	}
	if acceptor.NeedClientAuth {
		sslOptionalArguments = sslOptionalArguments + ";" + "needClientAuth=true"
	}
	if acceptor.WantClientAuth {
		sslOptionalArguments = sslOptionalArguments + ";" + "wantClientAuth=true"
	}
	if acceptor.VerifyHost {
		sslOptionalArguments = sslOptionalArguments + ";" + "verifyHost=true"
	}
	if acceptor.SSLProvider != "" {
		sslOptionalArguments = sslOptionalArguments + ";" + "sslProvider=" + acceptor.SSLProvider
	}
	if acceptor.SNIHost != "" {
		sslOptionalArguments = sslOptionalArguments + ";" + "sniHost=" + acceptor.SNIHost
	}

	if acceptor.KeyStoreProvider != "" {
		sslOptionalArguments = sslOptionalArguments + ";" + "keyStoreProvider=" + acceptor.KeyStoreProvider
	}

	if acceptor.TrustStoreType != "" {
		sslOptionalArguments = sslOptionalArguments + ";" + "trustStoreType=" + acceptor.TrustStoreType
	}

	if acceptor.TrustStoreProvider != "" {
		sslOptionalArguments = sslOptionalArguments + ";" + "trustStoreProvider=" + acceptor.TrustStoreProvider
	}

	return sslOptionalArguments
}

func generateConnectorSSLOptionalArguments(connector brokerv1beta1.ConnectorType) string {

	sslOptionalArguments := ""

	if connector.EnabledCipherSuites != "" {
		sslOptionalArguments = sslOptionalArguments + "enabledCipherSuites=" + connector.EnabledCipherSuites
	}
	if connector.EnabledProtocols != "" {
		sslOptionalArguments = sslOptionalArguments + ";" + "enabledProtocols=" + connector.EnabledProtocols
	}
	if connector.NeedClientAuth {
		sslOptionalArguments = sslOptionalArguments + ";" + "needClientAuth=true"
	}
	if connector.WantClientAuth {
		sslOptionalArguments = sslOptionalArguments + ";" + "wantClientAuth=true"
	}
	if connector.VerifyHost {
		sslOptionalArguments = sslOptionalArguments + ";" + "verifyHost=true"
	}
	if connector.SSLProvider != "" {
		sslOptionalArguments = sslOptionalArguments + ";" + "sslProvider=" + connector.SSLProvider
	}
	if connector.SNIHost != "" {
		sslOptionalArguments = sslOptionalArguments + ";" + "sniHost=" + connector.SNIHost
	}

	if connector.KeyStoreProvider != "" {
		sslOptionalArguments = sslOptionalArguments + ";" + "keyStoreProvider=" + connector.KeyStoreProvider
	}

	if connector.TrustStoreType != "" {
		sslOptionalArguments = sslOptionalArguments + ";" + "trustStoreType=" + connector.TrustStoreType
	}

	if connector.TrustStoreProvider != "" {
		sslOptionalArguments = sslOptionalArguments + ";" + "trustStoreProvider=" + connector.TrustStoreProvider
	}

	return sslOptionalArguments
}

// https://stackoverflow.com/questions/37334119/how-to-delete-an-element-from-a-slice-in-golang
func remove(s []corev1.EnvVar, i int) []corev1.EnvVar {
	s[i] = s[len(s)-1]
	// We do not need to put s[i] at the end, as it will be discarded anyway
	return s[:len(s)-1]
}

func aioSyncCausedUpdateOn(deploymentPlan *brokerv1beta1.DeploymentPlanType, currentStatefulSet *appsv1.StatefulSet) bool {

	foundAio := false
	foundNio := false
	var extraArgs string = ""
	extraArgsNeedsUpdate := false

	// Find the existing values
	for _, v := range currentStatefulSet.Spec.Template.Spec.Containers[0].Env {
		if v.Name == "AMQ_JOURNAL_TYPE" {
			if strings.Contains(v.Value, "aio") {
				foundAio = true
			}
			if strings.Contains(v.Value, "nio") {
				foundNio = true
			}
			extraArgs = v.Value
			break
		}
	}

	if strings.ToLower(deploymentPlan.JournalType) == "aio" && foundNio {
		extraArgs = strings.Replace(extraArgs, "nio", "aio", 1)
		extraArgsNeedsUpdate = true
	}

	if !(strings.ToLower(deploymentPlan.JournalType) == "aio") && foundAio {
		extraArgs = strings.Replace(extraArgs, "aio", "nio", 1)
		extraArgsNeedsUpdate = true
	}

	if !foundAio && !foundNio {
		extraArgs = "--" + strings.ToLower(deploymentPlan.JournalType)
		extraArgsNeedsUpdate = true
	}

	if extraArgsNeedsUpdate {
		newExtraArgsValue := corev1.EnvVar{
			Name:      "AMQ_JOURNAL_TYPE",
			Value:     extraArgs,
			ValueFrom: nil,
		}
		environments.Update(currentStatefulSet.Spec.Template.Spec.Containers, &newExtraArgsValue)
	}

	return extraArgsNeedsUpdate
}

func persistentSyncCausedUpdateOn(namer Namers, deploymentPlan *brokerv1beta1.DeploymentPlanType, currentStatefulSet *appsv1.StatefulSet) bool {

	foundDataDir := false
	foundDataDirLogging := false

	dataDirNeedsUpdate := false
	dataDirLoggingNeedsUpdate := false

	statefulSetUpdated := false

	// TODO: Remove yuck
	// ensure password and username are valid if can't via openapi validation?
	if deploymentPlan.PersistenceEnabled {

		envVarArray := []corev1.EnvVar{}
		// Find the existing values
		for _, v := range currentStatefulSet.Spec.Template.Spec.Containers[0].Env {
			if v.Name == "AMQ_DATA_DIR" {
				foundDataDir = true
				if v.Value != namer.GLOBAL_DATA_PATH {
					dataDirNeedsUpdate = true
				}
			}
			if v.Name == "AMQ_DATA_DIR_LOGGING" {
				foundDataDirLogging = true
				if v.Value != "true" {
					dataDirLoggingNeedsUpdate = true
				}
			}
		}

		if !foundDataDir || dataDirNeedsUpdate {
			newDataDirValue := corev1.EnvVar{
				Name:      "AMQ_DATA_DIR",
				Value:     namer.GLOBAL_DATA_PATH,
				ValueFrom: nil,
			}
			envVarArray = append(envVarArray, newDataDirValue)
			statefulSetUpdated = true
		}

		if !foundDataDirLogging || dataDirLoggingNeedsUpdate {
			newDataDirLoggingValue := corev1.EnvVar{
				Name:      "AMQ_DATA_DIR_LOGGING",
				Value:     "true",
				ValueFrom: nil,
			}
			envVarArray = append(envVarArray, newDataDirLoggingValue)
			statefulSetUpdated = true
		}

		if statefulSetUpdated {
			envVarArrayLen := len(envVarArray)
			if envVarArrayLen > 0 {
				for i := 0; i < len(currentStatefulSet.Spec.Template.Spec.Containers); i++ {
					for j := len(currentStatefulSet.Spec.Template.Spec.Containers[i].Env) - 1; j >= 0; j-- {
						if (currentStatefulSet.Spec.Template.Spec.Containers[i].Env[j].Name == "AMQ_DATA_DIR" && dataDirNeedsUpdate) ||
							(currentStatefulSet.Spec.Template.Spec.Containers[i].Env[j].Name == "AMQ_DATA_DIR_LOGGING" && dataDirLoggingNeedsUpdate) {
							currentStatefulSet.Spec.Template.Spec.Containers[i].Env = remove(currentStatefulSet.Spec.Template.Spec.Containers[i].Env, j)
						}
					}
				}

				containerArrayLen := len(currentStatefulSet.Spec.Template.Spec.Containers)
				for i := 0; i < containerArrayLen; i++ {
					for j := 0; j < envVarArrayLen; j++ {
						currentStatefulSet.Spec.Template.Spec.Containers[i].Env = append(currentStatefulSet.Spec.Template.Spec.Containers[i].Env, envVarArray[j])
					}
				}
			}
		}
	} else {

		for i := 0; i < len(currentStatefulSet.Spec.Template.Spec.Containers); i++ {
			for j := len(currentStatefulSet.Spec.Template.Spec.Containers[i].Env) - 1; j >= 0; j-- {
				if currentStatefulSet.Spec.Template.Spec.Containers[i].Env[j].Name == "AMQ_DATA_DIR" ||
					currentStatefulSet.Spec.Template.Spec.Containers[i].Env[j].Name == "AMQ_DATA_DIR_LOGGING" {
					currentStatefulSet.Spec.Template.Spec.Containers[i].Env = remove(currentStatefulSet.Spec.Template.Spec.Containers[i].Env, j)
					statefulSetUpdated = true
				}
			}
		}
	}

	return statefulSetUpdated
}

func (reconciler *ActiveMQArtemisReconcilerImpl) CurrentDeployedResources(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client) {
	reqLogger := clog.With("ActiveMQArtemis Name", customResource.Name)
	reqLogger.Info("currentDeployedResources")

	var err error
	if customResource.Spec.DeploymentPlan.PersistenceEnabled {
		checkExistingPersistentVolumes(customResource, client)
	}
	reconciler.deployed, err = getDeployedResources(customResource, client)
	if err != nil {
		reqLogger.Error(err, "error getting deployed resources")
		return
	}

	// track persisted cr secret
	for _, secret := range reconciler.deployed[reflect.TypeOf(corev1.Secret{})] {
		if strings.HasPrefix(secret.GetName(), "secret-broker-") {
			// track this as it is managed by the controller state machine, not by reconcile
			reconciler.trackDesired(secret)
		}
	}

	for t, objs := range reconciler.deployed {
		for _, obj := range objs {
			reqLogger.Info("Deployed ", "Type", t, "Name", obj.GetName())
		}
	}
}

func (reconciler *ActiveMQArtemisReconcilerImpl) ProcessResources(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, scheme *runtime.Scheme) error {

	reqLogger := clog.With("ActiveMQArtemis Name", customResource.Name)

	for index := range reconciler.requestedResources {
		reconciler.requestedResources[index].SetNamespace(customResource.Namespace)
	}

	var currenCount int
	for index := range reconciler.deployed {
		currenCount += len(reconciler.deployed[index])
	}
	reqLogger.Info("Processing resources", "num requested", len(reconciler.requestedResources), "num current", currenCount)

	requested := compare.NewMapBuilder().Add(reconciler.requestedResources...).ResourceMap()
	comparator := compare.NewMapComparator()

	comparator.Comparator.SetComparator(reflect.TypeOf(appsv1.StatefulSet{}), func(deployed, requested rtclient.Object) bool {
		ss1 := deployed.(*appsv1.StatefulSet)
		ss2 := requested.(*appsv1.StatefulSet)
		isEqual := equality.Semantic.DeepEqual(ss1.Spec, ss2.Spec)

		if !isEqual {
			reqLogger.Info("Unequal", "depoyed", ss1.Spec, "requested", ss2.Spec)
		}
		return isEqual
	})

	deltas := comparator.Compare(reconciler.deployed, requested)
	for _, resourceType := range getOrderedTypeList() {
		delta, ok := deltas[resourceType]
		if !ok {
			// not all types will have deltas
			continue
		}
		reqLogger.Info("", "instances of ", resourceType, "Will create ", len(delta.Added), "update ", len(delta.Updated), "and delete", len(delta.Removed))

		for index := range delta.Added {
			resourceToAdd := delta.Added[index]
			reconciler.createResource(customResource, client, scheme, resourceToAdd, resourceType, reqLogger)
		}
		for index := range delta.Updated {
			resourceToUpdate := delta.Updated[index]
			reconciler.updateResource(customResource, client, scheme, resourceToUpdate, resourceType, reqLogger)
		}
		for index := range delta.Removed {
			resourceToRemove := delta.Removed[index]
			reconciler.deleteResource(customResource, client, scheme, resourceToRemove, resourceType, reqLogger)
		}
	}

	//empty the collected objects
	reconciler.requestedResources = nil

	return nil
}

func (reconciler *ActiveMQArtemisReconcilerImpl) createResource(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, scheme *runtime.Scheme, requested rtclient.Object, kind reflect.Type, reqLogger *zap.SugaredLogger) error {
	reqLogger.Info("Adding delta resources, i.e. creating ", "name ", requested.GetName(), "of kind ", kind)
	return reconciler.createRequestedResource(customResource, client, scheme, requested, reqLogger, kind)
}

func (reconciler *ActiveMQArtemisReconcilerImpl) updateResource(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, scheme *runtime.Scheme, requested rtclient.Object, kind reflect.Type, reqLogger *zap.SugaredLogger) error {
	reqLogger.Info("Updating delta resources, i.e. updating ", "name ", requested.GetName(), "of kind ", kind)
	return reconciler.updateRequestedResource(customResource, client, scheme, requested, reqLogger, kind)

}

func (reconciler *ActiveMQArtemisReconcilerImpl) deleteResource(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, scheme *runtime.Scheme, requested rtclient.Object, kind reflect.Type, reqLogger *zap.SugaredLogger) error {
	reqLogger.Info("Deleting delta resources, i.e. removing ", "name ", requested.GetName(), "of kind ", kind)
	return reconciler.deleteRequestedResource(customResource, client, scheme, requested, reqLogger, kind)
}

func (reconciler *ActiveMQArtemisReconcilerImpl) createRequestedResource(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, scheme *runtime.Scheme, requested rtclient.Object, reqLogger *zap.SugaredLogger, kind reflect.Type) error {
	reqLogger.Info("Createing ", "kind ", kind, "named ", requested.GetName())

	return resources.Create(customResource, client, scheme, requested)
}

func (reconciler *ActiveMQArtemisReconcilerImpl) updateRequestedResource(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, scheme *runtime.Scheme, requested rtclient.Object, reqLogger *zap.SugaredLogger, kind reflect.Type) error {

	var updateError error
	if updateError = resources.Update(client, requested); updateError == nil {
		reqLogger.Info("updated", "kind ", kind, "named ", requested.GetName())
	} else {
		reqLogger.Error(updateError, "updated Failed", "kind ", kind, "named ", requested.GetName())
	}
	return updateError
}

func (reconciler *ActiveMQArtemisReconcilerImpl) deleteRequestedResource(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, scheme *runtime.Scheme, requested rtclient.Object, reqLogger *zap.SugaredLogger, kind reflect.Type) error {

	var deleteError error
	if deleteError := resources.Delete(client, requested); deleteError == nil {
		reqLogger.Info("deleted", "kind", kind, " named ", requested.GetName())
	} else {
		reqLogger.Error(deleteError, "delete Failed", "kind", kind, " named ", requested.GetName())
	}
	return deleteError
}

func checkExistingPersistentVolumes(instance *brokerv1beta1.ActiveMQArtemis, client rtclient.Client) {
	var log = ctrl.Log.WithName("controller_v1beta1activemqartemis")

	var i int32
	for i = 0; i < instance.Spec.DeploymentPlan.Size; i++ {
		ordinalString := strconv.Itoa(int(i))
		pvcKey := types.NamespacedName{Namespace: instance.Namespace, Name: instance.Name + "-" + namer.CrToSS(instance.Name) + "-" + ordinalString}
		pvc := &corev1.PersistentVolumeClaim{}
		err := client.Get(context.TODO(), pvcKey, pvc)

		if err == nil {
			if len(pvc.OwnerReferences) > 0 {
				found := false
				newOwnerReferences := make([]metav1.OwnerReference, 0)
				for _, oref := range pvc.OwnerReferences {
					if oref.UID == instance.UID {
						found = true
					} else {
						newOwnerReferences = append(newOwnerReferences, oref)
					}
				}
				if found {
					log.Info("removing owner ref from pvc to avoid potential data loss")
					pvc.OwnerReferences = newOwnerReferences
					if er := client.Update(context.TODO(), pvc); er != nil {
						log.Error(er, "failed to remove ownerReference from pvc", "pvc", *pvc)
					}
				}
			}
		} else {
			if !errors.IsNotFound(err) {
				log.Error(err, "got error in getting pvc")
			}
		}
	}
}

var orderedTypes *([]reflect.Type)

func getOrderedTypeList() []reflect.Type {
	return genOrderedTypesLists()
}

func genOrderedTypesLists() []reflect.Type {

	if orderedTypes == nil {
		isOpenshift, _ := environments.DetectOpenshift()
		types := make([]reflect.Type, 5)

		// we want to create/update in this order
		types[0] = reflect.TypeOf(corev1.Secret{})
		types[1] = reflect.TypeOf(corev1.ConfigMap{})
		types[2] = reflect.TypeOf(appsv1.StatefulSet{})
		types[3] = reflect.TypeOf(corev1.Service{})

		if isOpenshift {
			types[4] = reflect.TypeOf(routev1.Route{})
		} else {
			types[4] = reflect.TypeOf(netv1.Ingress{})
		}
		orderedTypes = &types
	}
	return *orderedTypes
}

func getDeployedResources(instance *brokerv1beta1.ActiveMQArtemis, client rtclient.Client) (map[reflect.Type][]rtclient.Object, error) {

	var log = ctrl.Log.WithName("controller_v1beta1activemqartemis")

	reader := read.New(client).WithNamespace(instance.Namespace).WithOwnerObject(instance)
	var resourceMap map[reflect.Type][]rtclient.Object
	var err error
	if isOpenshift, _ := environments.DetectOpenshift(); isOpenshift {
		resourceMap, err = reader.ListAll(
			&corev1.ServiceList{},
			&appsv1.StatefulSetList{},
			&routev1.RouteList{},
			&corev1.SecretList{},
			&corev1.ConfigMapList{},
		)
	} else {
		resourceMap, err = reader.ListAll(
			&corev1.ServiceList{},
			&appsv1.StatefulSetList{},
			&netv1.IngressList{},
			&corev1.SecretList{},
			&corev1.ConfigMapList{},
		)
	}
	if err != nil {
		log.Error(err, "Failed to list deployed objects.")
		return nil, err
	}

	return resourceMap, nil
}

func MakeVolumes(customResource *brokerv1beta1.ActiveMQArtemis, namer Namers) []corev1.Volume {

	volumeDefinitions := []corev1.Volume{}
	if customResource.Spec.DeploymentPlan.PersistenceEnabled {
		basicCRVolume := volumes.MakePersistentVolume(customResource.Name)
		volumeDefinitions = append(volumeDefinitions, basicCRVolume...)
	}

	// Scan acceptors for any with sslEnabled
	for _, acceptor := range customResource.Spec.Acceptors {
		if !acceptor.SSLEnabled {
			continue
		}
		secretName := customResource.Name + "-" + acceptor.Name + "-secret"
		if acceptor.SSLSecret != "" {
			secretName = acceptor.SSLSecret
		}
		volume := volumes.MakeVolume(secretName)
		volumeDefinitions = append(volumeDefinitions, volume)
	}

	// Scan connectors for any with sslEnabled
	for _, connector := range customResource.Spec.Connectors {
		if !connector.SSLEnabled {
			continue
		}
		secretName := customResource.Name + "-" + connector.Name + "-secret"
		if connector.SSLSecret != "" {
			secretName = connector.SSLSecret
		}
		volume := volumes.MakeVolume(secretName)
		volumeDefinitions = append(volumeDefinitions, volume)
	}

	if customResource.Spec.Console.SSLEnabled {
		secretName := namer.SecretsConsoleNameBuilder.Name()
		if customResource.Spec.Console.SSLSecret != "" {
			secretName = customResource.Spec.Console.SSLSecret
		}
		volume := volumes.MakeVolume(secretName)
		volumeDefinitions = append(volumeDefinitions, volume)
	}

	return volumeDefinitions
}

func MakeVolumeMounts(customResource *brokerv1beta1.ActiveMQArtemis, namer Namers) []corev1.VolumeMount {

	volumeMounts := []corev1.VolumeMount{}
	if customResource.Spec.DeploymentPlan.PersistenceEnabled {
		persistentCRVlMnt := volumes.MakePersistentVolumeMount(customResource.Name, namer.GLOBAL_DATA_PATH)
		volumeMounts = append(volumeMounts, persistentCRVlMnt...)
	}

	// Scan acceptors for any with sslEnabled
	for _, acceptor := range customResource.Spec.Acceptors {
		if !acceptor.SSLEnabled {
			continue
		}
		volumeMountName := customResource.Name + "-" + acceptor.Name + "-secret-volume"
		if acceptor.SSLSecret != "" {
			volumeMountName = acceptor.SSLSecret + "-volume"
		}
		volumeMount := volumes.MakeVolumeMount(volumeMountName)
		volumeMounts = append(volumeMounts, volumeMount)
	}

	// Scan connectors for any with sslEnabled
	for _, connector := range customResource.Spec.Connectors {
		if !connector.SSLEnabled {
			continue
		}
		volumeMountName := customResource.Name + "-" + connector.Name + "-secret-volume"
		if connector.SSLSecret != "" {
			volumeMountName = connector.SSLSecret + "-volume"
		}
		volumeMount := volumes.MakeVolumeMount(volumeMountName)
		volumeMounts = append(volumeMounts, volumeMount)
	}

	if customResource.Spec.Console.SSLEnabled {
		volumeMountName := namer.SecretsConsoleNameBuilder.Name() + "-volume"
		if customResource.Spec.Console.SSLSecret != "" {
			volumeMountName = customResource.Spec.Console.SSLSecret + "-volume"
		}
		volumeMount := volumes.MakeVolumeMount(volumeMountName)
		volumeMounts = append(volumeMounts, volumeMount)
	}

	return volumeMounts
}

func MakeContainerPorts(cr *brokerv1beta1.ActiveMQArtemis) []corev1.ContainerPort {

	containerPorts := []corev1.ContainerPort{}
	if cr.Spec.DeploymentPlan.JolokiaAgentEnabled {
		jolokiaContainerPort := corev1.ContainerPort{

			Name:          "jolokia",
			ContainerPort: 8778,
			Protocol:      "TCP",
		}
		containerPorts = append(containerPorts, jolokiaContainerPort)
	}

	return containerPorts
}

func (reconciler *ActiveMQArtemisReconcilerImpl) NewPodTemplateSpecForCR(customResource *brokerv1beta1.ActiveMQArtemis, namer Namers, current *corev1.PodTemplateSpec) *corev1.PodTemplateSpec {

	reqLogger := ctrl.Log.WithName(customResource.Name)

	namespacedName := types.NamespacedName{
		Name:      customResource.Name,
		Namespace: customResource.Namespace,
	}

	terminationGracePeriodSeconds := int64(60)

	// custom labels provided in CR applied only to the pod template spec
	// note: work with a clone of the default labels to not modify defaults
	labels := make(map[string]string)
	for key, value := range namer.LabelBuilder.Labels() {
		labels[key] = value
	}
	if customResource.Spec.DeploymentPlan.Labels != nil {
		for key, value := range customResource.Spec.DeploymentPlan.Labels {
			labels[key] = value
			reqLogger.Info("Adding CR Label", "key", key, "value", value)
		}
	}

	pts := pods.MakePodTemplateSpec(current, namespacedName, labels)
	podSpec := &pts.Spec

	// REVISIT: don't know when this is nil
	if podSpec == nil {
		podSpec = &corev1.PodSpec{}
	}

	imageName := ""
	if customResource.Spec.DeploymentPlan.Image == "placeholder" ||
		len(customResource.Spec.DeploymentPlan.Image) == 0 {
		reqLogger.Info("Determining the kubernetes image to use due to placeholder setting")
		imageName = determineImageToUse(customResource, "Kubernetes")
	} else {
		reqLogger.Info("Using the user provided kubernetes image " + customResource.Spec.DeploymentPlan.Image)
		imageName = customResource.Spec.DeploymentPlan.Image
	}
	reqLogger.Info("NewPodTemplateSpecForCR determined image to use " + imageName)
	container := containers.MakeContainer(podSpec, customResource.Name, imageName, MakeEnvVarArrayForCR(customResource, namer))

	container.Resources = customResource.Spec.DeploymentPlan.Resources

	containerPorts := MakeContainerPorts(customResource)
	if len(containerPorts) > 0 {
		reqLogger.Info("Adding new ports to main", "len", len(containerPorts))
		container.Ports = containerPorts
	}

	reqLogger.Info("Checking out extraMounts", "extra config", customResource.Spec.DeploymentPlan.ExtraMounts)
	brokerPropertiesConfigMapName := reconciler.addConfigMapForBrokerProperties(customResource)
	configMapsToCreate := []string{brokerPropertiesConfigMapName}
	configMapsToCreate = append(configMapsToCreate, customResource.Spec.DeploymentPlan.ExtraMounts.ConfigMaps...)

	extraVolumes, extraVolumeMounts := createExtraConfigmapsAndSecrets(container, configMapsToCreate, customResource.Spec.DeploymentPlan.ExtraMounts.Secrets)

	reqLogger.Info("Extra volumes", "volumes", extraVolumes)
	reqLogger.Info("Extra mounts", "mounts", extraVolumeMounts)
	container.VolumeMounts = MakeVolumeMounts(customResource, namer)
	if len(extraVolumeMounts) > 0 {
		container.VolumeMounts = append(container.VolumeMounts, extraVolumeMounts...)
	}

	container.LivenessProbe = configureLivenessProbe(container, customResource.Spec.DeploymentPlan.LivenessProbe)
	container.ReadinessProbe = configureReadinessProbe(container, customResource.Spec.DeploymentPlan.ReadinessProbe)

	if len(customResource.Spec.DeploymentPlan.NodeSelector) > 0 {
		reqLogger.Info("Adding Node Selectors", "len", len(customResource.Spec.DeploymentPlan.NodeSelector))
		podSpec.NodeSelector = customResource.Spec.DeploymentPlan.NodeSelector
	}

	configureAffinity(podSpec, &customResource.Spec.DeploymentPlan.Affinity)

	if len(customResource.Spec.DeploymentPlan.Tolerations) > 0 {
		reqLogger.Info("Adding Tolerations", "len", len(customResource.Spec.DeploymentPlan.Tolerations))
		podSpec.Tolerations = customResource.Spec.DeploymentPlan.Tolerations
	}

	newContainersArray := []corev1.Container{}
	podSpec.Containers = append(newContainersArray, *container)
	brokerVolumes := MakeVolumes(customResource, namer)
	if len(extraVolumes) > 0 {
		brokerVolumes = append(brokerVolumes, extraVolumes...)
	}
	if len(brokerVolumes) > 0 {
		podSpec.Volumes = brokerVolumes
	}
	podSpec.TerminationGracePeriodSeconds = &terminationGracePeriodSeconds

	var cfgVolumeName string = "amq-cfg-dir"

	//tell container don't config
	envConfigBroker := corev1.EnvVar{
		Name:  "CONFIG_BROKER",
		Value: "false",
	}
	environments.Create(podSpec.Containers, &envConfigBroker)

	envBrokerCustomInstanceDir := corev1.EnvVar{
		Name:  "CONFIG_INSTANCE_DIR",
		Value: brokerConfigRoot,
	}
	environments.Create(podSpec.Containers, &envBrokerCustomInstanceDir)

	//add empty-dir volume and volumeMounts to main container
	volumeForCfg := volumes.MakeVolumeForCfg(cfgVolumeName)
	podSpec.Volumes = append(podSpec.Volumes, volumeForCfg)

	volumeMountForCfg := volumes.MakeRwVolumeMountForCfg(cfgVolumeName, brokerConfigRoot)
	podSpec.Containers[0].VolumeMounts = append(podSpec.Containers[0].VolumeMounts, volumeMountForCfg)

	clog.Info("Creating init container for broker configuration")
	initImageName := ""
	if customResource.Spec.DeploymentPlan.InitImage == "placeholder" ||
		len(customResource.Spec.DeploymentPlan.InitImage) == 0 {
		reqLogger.Info("Determining the init image to use due to placeholder setting")
		initImageName = determineImageToUse(customResource, "Init")
	} else {
		reqLogger.Info("Using the user provided init image " + customResource.Spec.DeploymentPlan.InitImage)
		initImageName = customResource.Spec.DeploymentPlan.InitImage
	}
	reqLogger.Info("NewPodTemplateSpecForCR initImage to use " + initImageName)

	initContainer := containers.MakeInitContainer(podSpec, customResource.Name, initImageName, MakeEnvVarArrayForCR(customResource, namer))
	initContainer.Resources = customResource.Spec.DeploymentPlan.Resources

	var initCmds []string
	var initCfgRootDir = "/init_cfg_root"

	compactVersionToUse := determineCompactVersionToUse(customResource)
	yacfgProfileVersion = version.YacfgProfileVersionFromFullVersion[version.FullVersionFromCompactVersion[compactVersionToUse]]
	yacfgProfileName := version.YacfgProfileName

	//address settings
	addressSettings := customResource.Spec.AddressSettings.AddressSetting
	if len(addressSettings) > 0 {
		reqLogger.Info("processing address-settings")

		var configYaml strings.Builder
		var configSpecials map[string]string = make(map[string]string)

		brokerYaml, specials := cr2jinja2.MakeBrokerCfgOverrides(customResource, nil, nil)

		configYaml.WriteString(brokerYaml)

		for k, v := range specials {
			configSpecials[k] = v
		}

		byteArray, err := json.Marshal(configSpecials)
		if err != nil {
			clog.Error(err, "failed to marshal specials")
		}
		jsonSpecials := string(byteArray)

		envVarTuneFilePath := "TUNE_PATH"
		outputDir := initCfgRootDir + "/yacfg_etc"

		initCmd := "mkdir -p " + outputDir + "; echo \"" + configYaml.String() + "\" > " + outputDir +
			"/broker.yaml; cat " + outputDir + "/broker.yaml; yacfg --profile " + yacfgProfileName + "/" +
			yacfgProfileVersion + "/default_with_user_address_settings.yaml.jinja2  --tune " +
			outputDir + "/broker.yaml --extra-properties '" + jsonSpecials + "' --output " + outputDir

		clog.Info("==debug==, initCmd: " + initCmd)
		initCmds = append(initCmds, initCmd)

		//populate args of init container

		podSpec.InitContainers = []corev1.Container{
			*initContainer,
		}

		//expose env for address-settings
		envVarApplyRule := "APPLY_RULE"
		envVarApplyRuleValue := customResource.Spec.AddressSettings.ApplyRule

		if envVarApplyRuleValue == nil {
			envVarApplyRuleValue = &defApplyRule
		}
		reqLogger.Info("Process addresssetting", "ApplyRule", *envVarApplyRuleValue)

		applyRule := corev1.EnvVar{
			Name:  envVarApplyRule,
			Value: *envVarApplyRuleValue,
		}
		environments.Create(podSpec.InitContainers, &applyRule)

		mergeBrokerAs := corev1.EnvVar{
			Name:  "MERGE_BROKER_AS",
			Value: "true",
		}
		environments.Create(podSpec.InitContainers, &mergeBrokerAs)

		//pass cfg file location and apply rule to init container via env vars
		tuneFile := corev1.EnvVar{
			Name:  envVarTuneFilePath,
			Value: outputDir,
		}
		environments.Create(podSpec.InitContainers, &tuneFile)

	} else {
		clog.Info("No addressetings")

		podSpec.InitContainers = []corev1.Container{
			*initContainer,
		}
	}
	//now make volumes mount available to init image
	clog.Info("making volume mounts")

	//setup volumeMounts from scratch
	podSpec.InitContainers[0].VolumeMounts = []corev1.VolumeMount{}
	volumeMountForCfgRoot := volumes.MakeRwVolumeMountForCfg(cfgVolumeName, brokerConfigRoot)
	podSpec.InitContainers[0].VolumeMounts = append(podSpec.InitContainers[0].VolumeMounts, volumeMountForCfgRoot)

	volumeMountForCfg = volumes.MakeRwVolumeMountForCfg("tool-dir", initCfgRootDir)
	podSpec.InitContainers[0].VolumeMounts = append(podSpec.InitContainers[0].VolumeMounts, volumeMountForCfg)

	//add empty-dir volume
	volumeForCfg = volumes.MakeVolumeForCfg("tool-dir")
	podSpec.Volumes = append(podSpec.Volumes, volumeForCfg)

	clog.Info("Total volumes ", "volumes", podSpec.Volumes)

	// this depends on init container passing --java-opts to artemis create via launch.sh *and* it
	// not getting munged on the way. We CreateOrAppend to any value from spec.Env
	javaOpts := corev1.EnvVar{
		Name:  "JAVA_OPTS",
		Value: "-Dbroker.properties=/amq/extra/configmaps/" + brokerPropertiesConfigMapName + "/",
	}
	environments.CreateOrAppend(podSpec.InitContainers, &javaOpts)

	var initArgs []string = []string{"-c"}

	//provide a way to configuration after launch.sh
	var brokerHandlerCmds []string = []string{}
	clog.Info("Checking if there are any config handlers", "main cr", namespacedName)
	brokerConfigHandler := GetBrokerConfigHandler(namespacedName)
	if brokerConfigHandler != nil {
		clog.Info("there is a config handler")
		handlerCmds := brokerConfigHandler.Config(podSpec.InitContainers, initCfgRootDir+"/security", yacfgProfileVersion, yacfgProfileName)
		clog.Info("Getting back some init commands", "handlerCmds", handlerCmds)
		if len(handlerCmds) > 0 {
			clog.Info("appending to initCmd array...")
			brokerHandlerCmds = append(brokerHandlerCmds, handlerCmds...)
		}
	}

	var strBuilder strings.Builder

	isFirst := true
	initCmds = append(initCmds, configCmd)
	initCmds = append(initCmds, brokerHandlerCmds...)
	initCmds = append(initCmds, initHelperScript)

	for _, icmd := range initCmds {
		if isFirst {
			isFirst = false
		} else {
			strBuilder.WriteString(" && ")
		}
		strBuilder.WriteString(icmd)
	}
	initArgs = append(initArgs, strBuilder.String())

	clog.Info("The final init cmds to init ", "the cmd array", initArgs)

	podSpec.InitContainers[0].Args = initArgs

	if len(extraVolumeMounts) > 0 {
		podSpec.InitContainers[0].VolumeMounts = append(podSpec.InitContainers[0].VolumeMounts, extraVolumeMounts...)
		clog.Info("Added some extra mounts to init", "total mounts: ", podSpec.InitContainers[0].VolumeMounts)
	}

	dontRun := corev1.EnvVar{
		Name:  "RUN_BROKER",
		Value: "false",
	}
	environments.Create(podSpec.InitContainers, &dontRun)

	envBrokerCustomInstanceDir = corev1.EnvVar{
		Name:  "CONFIG_INSTANCE_DIR",
		Value: brokerConfigRoot,
	}
	environments.Create(podSpec.InitContainers, &envBrokerCustomInstanceDir)

	// NOTE: PodSecurity contains a RunAsUser that will be overridden by that in the provided PodSecurityContext if any
	configPodSecurity(podSpec, &customResource.Spec.DeploymentPlan.PodSecurity)
	configurePodSecurityContext(podSpec, customResource.Spec.DeploymentPlan.PodSecurityContext)

	clog.Info("Final Init spec", "Detail", podSpec.InitContainers)

	pts.Spec = *podSpec

	return pts
}

func configureLivenessProbe(container *corev1.Container, probeFromCR *corev1.Probe) *corev1.Probe {
	var livenessProbe *corev1.Probe = container.LivenessProbe
	clog.Info("Configuring Liveness Probe", "existing", livenessProbe)

	if livenessProbe == nil {
		livenessProbe = &corev1.Probe{}
	}

	if probeFromCR != nil {
		//copy the probe
		clog.Info("Liveness Probe provided by user, must provide all optional values")
		livenessProbe.InitialDelaySeconds = probeFromCR.InitialDelaySeconds
		livenessProbe.TimeoutSeconds = probeFromCR.TimeoutSeconds
		livenessProbe.PeriodSeconds = probeFromCR.PeriodSeconds
		livenessProbe.TerminationGracePeriodSeconds = probeFromCR.TerminationGracePeriodSeconds
		livenessProbe.SuccessThreshold = probeFromCR.SuccessThreshold
		livenessProbe.FailureThreshold = probeFromCR.FailureThreshold

		// not complete in this case!
		if probeFromCR.Exec == nil && probeFromCR.HTTPGet == nil && probeFromCR.TCPSocket == nil {
			clog.Info("Adding default TCP check")
			livenessProbe.ProbeHandler = corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt(TCPLivenessPort),
				},
			}
		} else if probeFromCR.TCPSocket != nil {
			clog.Info("Using user specified TCPSocket")
			livenessProbe.ProbeHandler = corev1.ProbeHandler{
				TCPSocket: probeFromCR.TCPSocket,
			}
		} else {
			clog.Info("Using user provided Liveness Probe Exec " + probeFromCR.Exec.String())
			livenessProbe.Exec = probeFromCR.Exec
		}
	} else {
		clog.Info("Creating Default Liveness Probe")

		livenessProbe.InitialDelaySeconds = defaultLivenessProbeInitialDelay
		livenessProbe.TimeoutSeconds = 5
		livenessProbe.ProbeHandler = corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.FromInt(TCPLivenessPort),
			},
		}
	}

	return livenessProbe
}

func configureReadinessProbe(container *corev1.Container, probe *corev1.Probe) *corev1.Probe {

	var readinessProbe *corev1.Probe = container.ReadinessProbe
	if readinessProbe == nil {
		readinessProbe = &corev1.Probe{}
	}

	if probe != nil {
		//copy the probe
		readinessProbe.InitialDelaySeconds = probe.InitialDelaySeconds
		readinessProbe.TimeoutSeconds = probe.TimeoutSeconds
		readinessProbe.PeriodSeconds = probe.PeriodSeconds
		readinessProbe.TerminationGracePeriodSeconds = probe.TerminationGracePeriodSeconds
		readinessProbe.SuccessThreshold = probe.SuccessThreshold
		readinessProbe.FailureThreshold = probe.FailureThreshold
		if probe.Exec == nil && probe.HTTPGet == nil && probe.TCPSocket == nil {
			//add the default readiness check if none
			clog.Info("Using user provided readiness Probe")
			readinessProbe.ProbeHandler = corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{
						"/bin/bash",
						"-c",
						"/opt/amq/bin/readinessProbe.sh",
					},
				},
			}
		} else {
			readinessProbe.ProbeHandler = probe.ProbeHandler
		}
	} else {
		clog.Info("Creating Default readiness Probe")
		readinessProbe.InitialDelaySeconds = defaultLivenessProbeInitialDelay
		readinessProbe.TimeoutSeconds = 5
		readinessProbe.ProbeHandler = corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{
					"/bin/bash",
					"-c",
					"/opt/amq/bin/readinessProbe.sh",
				},
			},
		}
	}

	return readinessProbe
}

func getConfigAppliedConfigMapName(artemis *brokerv1beta1.ActiveMQArtemis) types.NamespacedName {
	return types.NamespacedName{
		Namespace: artemis.Namespace,
		Name:      artemis.Name + "-props",
	}
}

func (reconciler *ActiveMQArtemisReconcilerImpl) addConfigMapForBrokerProperties(customResource *brokerv1beta1.ActiveMQArtemis) string {

	// fetch and do idempotent transform based on CR

	// deal with upgrade to immutable, only upgrade to mutable on not found
	immmutable := false
	shaOfMap := hexShaHashOfMap(customResource.Spec.BrokerProperties)
	configMapName := types.NamespacedName{
		Namespace: customResource.Namespace,
		Name:      customResource.Name + "-props-" + shaOfMap,
	}

	var desired *corev1.ConfigMap
	obj := reconciler.cloneOfDeployed(reflect.TypeOf(corev1.ConfigMap{}), configMapName.Name)
	if obj != nil {
		desired = obj.(*corev1.ConfigMap)
		// found existing (immuable) map with sha in the name
		immmutable = true
	}

	if !immmutable {
		configMapName = getConfigAppliedConfigMapName(customResource)

		obj := reconciler.cloneOfDeployed(reflect.TypeOf(corev1.ConfigMap{}), configMapName.Name)
		if obj != nil {
			desired = obj.(*corev1.ConfigMap)
		}
	}

	if desired == nil {

		desired = &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "k8s.io.api.core.v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:         configMapName.Name,
				GenerateName: "",
				Namespace:    configMapName.Namespace,
			},
			Immutable: &immmutable,
		}
	}

	desired.Data = brokerPropertiesData(customResource.Spec.BrokerProperties)

	if !immmutable {
		desired.Data = appendStatus(desired.Data, shaOfMap)
	}

	clog.Info("Requesting configMap for broker properties", "name", configMapName.Name)
	reconciler.trackDesired(desired)

	clog.Info("Requesting mount for broker properties config map")
	return configMapName.Name
}

const statusPropertiesName = "a_status.properties"

func appendStatus(data map[string]string, shaOfMap string) map[string]string {
	buf := &bytes.Buffer{}
	fmt.Fprintln(buf, "# generated by crd")
	fmt.Fprintln(buf, "#")

	// populating the sha key of the config::properties broker status object with JSON
	fmt.Fprintf(buf, "status={\"properties\":{\"%s\": { \"cr:alder32\": \"%s\"}}}\n", statusPropertiesName, shaOfMap)

	// a is alphanumericlaly first to be applied
	data[statusPropertiesName] = buf.String()
	return data
}

func hexShaHashOfMap(props []string) string {

	digest := adler32.New()
	for _, k := range props {
		digest.Write([]byte(k))
	}

	return hex.EncodeToString(digest.Sum(nil))
}

func brokerPropertiesData(props []string) map[string]string {
	buf := &bytes.Buffer{}
	fmt.Fprintln(buf, "# generated by crd")
	fmt.Fprintln(buf, "#")

	for _, k := range props {
		fmt.Fprintf(buf, "%s\n", k)
	}

	return map[string]string{"broker.properties": buf.String()}
}

func configureAffinity(podSpec *corev1.PodSpec, affinity *corev1.Affinity) {
	if affinity != nil {
		podSpec.Affinity = &corev1.Affinity{}
		if affinity.PodAffinity != nil {
			clog.Info("Adding Pod Affinity")
			podSpec.Affinity.PodAffinity = affinity.PodAffinity
		}
		if affinity.PodAntiAffinity != nil {
			clog.Info("Adding Pod AntiAffinity")
			podSpec.Affinity.PodAntiAffinity = affinity.PodAntiAffinity
		}
		if affinity.NodeAffinity != nil {
			clog.Info("Adding Node Affinity")
			podSpec.Affinity.NodeAffinity = affinity.NodeAffinity
		}
	}
}

func configurePodSecurityContext(podSpec *corev1.PodSpec, podSecurityContext *corev1.PodSecurityContext) {
	clog.Info("Configuring PodSecurityContext")

	if nil != podSecurityContext {
		clog.Info("Incoming podSecurityContext is NOT nil, assigning")
		podSpec.SecurityContext = podSecurityContext
	} else {
		clog.Info("Incoming podSecurityContext is nil, creating with default values")
		podSpec.SecurityContext = &corev1.PodSecurityContext{}
	}
}

func sortedKeys(props map[string]string) []string {
	sortedKeys := make([]string, 0, len(props))
	for k := range props {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)
	return sortedKeys
}

// generic version?
func sortedKeysStringKeyByteValue(props map[string][]byte) []string {
	sortedKeys := make([]string, 0, len(props))
	for k := range props {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)
	return sortedKeys
}

func configPodSecurity(podSpec *corev1.PodSpec, podSecurity *brokerv1beta1.PodSecurityType) {
	if podSecurity.ServiceAccountName != nil {
		clog.Info("Pod serviceAccountName specified", "existing", podSpec.ServiceAccountName, "new", *podSecurity.ServiceAccountName)
		podSpec.ServiceAccountName = *podSecurity.ServiceAccountName
	}
	if podSecurity.RunAsUser != nil {
		clog.Info("Pod runAsUser specified", "runAsUser", *podSecurity.RunAsUser)
		if podSpec.SecurityContext == nil {
			secCtxt := corev1.PodSecurityContext{
				RunAsUser: podSecurity.RunAsUser,
			}
			podSpec.SecurityContext = &secCtxt
		} else {
			podSpec.SecurityContext.RunAsUser = podSecurity.RunAsUser
		}
	}
}

func determineImageToUse(customResource *brokerv1beta1.ActiveMQArtemis, imageTypeName string) string {

	imageName := ""
	compactVersionToUse := determineCompactVersionToUse(customResource)

	genericRelatedImageEnvVarName := ImageNamePrefix + imageTypeName + "_" + compactVersionToUse
	// Default case of x86_64/amd64 covered here
	archSpecificRelatedImageEnvVarName := genericRelatedImageEnvVarName
	if osruntime.GOARCH == "s390x" || osruntime.GOARCH == "ppc64le" {
		archSpecificRelatedImageEnvVarName = genericRelatedImageEnvVarName + "_" + osruntime.GOARCH
	}
	imageName, found := os.LookupEnv(archSpecificRelatedImageEnvVarName)
	clog.Info("DetermineImageToUse", "env", archSpecificRelatedImageEnvVarName, "imageName", imageName)
	if !found {
		imageName = version.DefaultImageName(archSpecificRelatedImageEnvVarName)
		clog.Info("DetermineImageToUse - from default", "env", archSpecificRelatedImageEnvVarName, "imageName", imageName)
	}

	return imageName
}

func determineCompactVersionToUse(customResource *brokerv1beta1.ActiveMQArtemis) string {

	specifiedVersion := customResource.Spec.Version
	compactVersionToUse := version.CompactLatestVersion
	//yacfgProfileVersion

	// See if we need to lookup what version to use
	for {
		// If there's no version specified just use the default above
		if len(specifiedVersion) == 0 {
			clog.Info("DetermineImageToUse specifiedVersion was empty")
			break
		}
		clog.Info("DetermineImageToUse specifiedVersion was " + specifiedVersion)

		// There is a version specified by the user...
		// Are upgrades enabled?
		if !customResource.Spec.Upgrades.Enabled {
			clog.Info("DetermineImageToUse upgrades are disabled")
			break
		}
		clog.Info("DetermineImageToUse upgrades are enabled")

		// We have a specified version and upgrades are enabled in general
		// Is the version specified on "the list"
		compactSpecifiedVersion := version.CompactVersionFromVersion[specifiedVersion]
		if len(compactSpecifiedVersion) == 0 {
			clog.Info("DetermineImageToUse failed to find the compact form of the specified version " + specifiedVersion)
			break
		}
		clog.Info("DetermineImageToUse found the compact form " + compactSpecifiedVersion + " of specifiedVersion")

		// We found the compact form in our list, is it a minor bump?
		if version.LastMinorVersion == specifiedVersion &&
			!customResource.Spec.Upgrades.Minor {
			clog.Info("DetermineImageToUse requested minor version upgrade but minor upgrades NOT enabled")
			break
		}

		clog.Info("DetermineImageToUse all checks ok using user specified version " + specifiedVersion)
		compactVersionToUse = compactSpecifiedVersion
		break
	}

	return compactVersionToUse
}

func createExtraConfigmapsAndSecrets(brokerContainer *corev1.Container, configMaps []string, secrets []string) ([]corev1.Volume, []corev1.VolumeMount) {

	var extraVolumes []corev1.Volume
	var extraVolumeMounts []corev1.VolumeMount

	cfgMapPathBase := "/amq/extra/configmaps/"
	secretPathBase := "/amq/extra/secrets/"

	if len(configMaps) > 0 {
		for _, cfgmap := range configMaps {
			if cfgmap == "" {
				clog.Info("No ConfigMap name specified, ignore", "configMap", cfgmap)
				continue
			}
			cfgmapPath := cfgMapPathBase + cfgmap
			clog.Info("Resolved configMap path", "path", cfgmapPath)
			//now we have a config map. First create a volume
			cfgmapVol := volumes.MakeVolumeForConfigMap(cfgmap)
			cfgmapVolumeMount := volumes.MakeVolumeMountForCfg(cfgmapVol.Name, cfgmapPath, true)
			extraVolumes = append(extraVolumes, cfgmapVol)
			extraVolumeMounts = append(extraVolumeMounts, cfgmapVolumeMount)
		}
	}

	if len(secrets) > 0 {
		for _, secret := range secrets {
			if secret == "" {
				clog.Info("No Secret name specified, ignore", "Secret", secret)
				continue
			}
			secretPath := secretPathBase + secret
			//now we have a secret. First create a volume
			secretVol := volumes.MakeVolumeForSecret(secret)
			secretVolumeMount := volumes.MakeVolumeMountForCfg(secretVol.Name, secretPath, true)
			extraVolumes = append(extraVolumes, secretVol)
			extraVolumeMounts = append(extraVolumeMounts, secretVolumeMount)
		}
	}

	return extraVolumes, extraVolumeMounts
}

func (reconciler *ActiveMQArtemisReconcilerImpl) NewStatefulSetForCR(customResource *brokerv1beta1.ActiveMQArtemis, namer Namers, currentStatefulSet *appsv1.StatefulSet) *appsv1.StatefulSet {

	namespacedName := types.NamespacedName{
		Name:      customResource.Name,
		Namespace: customResource.Namespace,
	}
	currentStatefulSet = ss.MakeStatefulSet2(currentStatefulSet, namer.SsNameBuilder.Name(), namer.SvcHeadlessNameBuilder.Name(), namespacedName, customResource.Annotations, namer.LabelBuilder.Labels(), customResource.Spec.DeploymentPlan.Size)

	podTemplateSpec := *reconciler.NewPodTemplateSpecForCR(customResource, namer, &currentStatefulSet.Spec.Template)
	if customResource.Spec.DeploymentPlan.PersistenceEnabled {
		currentStatefulSet.Spec.VolumeClaimTemplates = *NewPersistentVolumeClaimArrayForCR(customResource, namer, 1)
	}
	currentStatefulSet.Spec.Template = podTemplateSpec

	return currentStatefulSet
}

func NewPersistentVolumeClaimArrayForCR(customResource *brokerv1beta1.ActiveMQArtemis, namer Namers, arrayLength int) *[]corev1.PersistentVolumeClaim {

	var pvc *corev1.PersistentVolumeClaim = nil
	capacity := "2Gi"
	pvcArray := make([]corev1.PersistentVolumeClaim, 0, arrayLength)
	storageClassName := ""

	namespacedName := types.NamespacedName{
		Name:      customResource.Name,
		Namespace: customResource.Namespace,
	}

	if customResource.Spec.DeploymentPlan.Storage.Size != "" {
		capacity = customResource.Spec.DeploymentPlan.Storage.Size
	}

	if customResource.Spec.DeploymentPlan.Storage.StorageClassName != "" {
		storageClassName = customResource.Spec.DeploymentPlan.Storage.StorageClassName
	}

	for i := 0; i < arrayLength; i++ {
		pvc = persistentvolumeclaims.NewPersistentVolumeClaimWithCapacityAndStorageClassName(namespacedName, capacity, namer.LabelBuilder.Labels(), storageClassName)
		pvcArray = append(pvcArray, *pvc)
	}

	return &pvcArray
}

func UpdatePodStatus(cr *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, namespacedName types.NamespacedName) error {

	reqLogger := ctrl.Log.WithValues("ActiveMQArtemis Name", cr.Name)
	reqLogger.Info("Updating status for pods")

	podStatus := getPodStatus(cr, client, namespacedName)

	reqLogger.Info("PodStatus current..................", "info:", podStatus)
	reqLogger.Info("Ready Count........................", "info:", len(podStatus.Ready))
	reqLogger.Info("Stopped Count......................", "info:", len(podStatus.Stopped))
	reqLogger.Info("Starting Count.....................", "info:", len(podStatus.Starting))

	meta.SetStatusCondition(&cr.Status.Conditions, getDeploymentCondition(cr, podStatus))

	var err error
	if !reflect.DeepEqual(podStatus, cr.Status.PodStatus) {
		reqLogger.Info("Pods status updated")
		cr.Status.PodStatus = podStatus
	} else {
		// could leave this to kube, it will do a []byte comparison
		reqLogger.Info("Pods status unchanged")
	}
	return err
}

func getDeploymentCondition(cr *brokerv1beta1.ActiveMQArtemis, podStatus olm.DeploymentStatus) metav1.Condition {
	if cr.Spec.DeploymentPlan.Size == 0 {
		return metav1.Condition{
			Type:    brokerv1beta1.DeployedConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  brokerv1beta1.DeployedConditionZeroSizeReason,
			Message: brokerv1beta1.DeployedConditionZeroSizeMessage,
		}
	}
	if len(podStatus.Ready) != int(cr.Spec.DeploymentPlan.Size) {
		return metav1.Condition{
			Type:    brokerv1beta1.DeployedConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  brokerv1beta1.DeployedConditionNotReadyReason,
			Message: fmt.Sprintf("%d/%d pods ready", len(podStatus.Ready), cr.Spec.DeploymentPlan.Size),
		}
	}
	return metav1.Condition{
		Type:   brokerv1beta1.DeployedConditionType,
		Reason: brokerv1beta1.DeployedConditionReadyReason,
		Status: metav1.ConditionTrue,
	}
}

func getPodStatus(cr *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, namespacedName types.NamespacedName) olm.DeploymentStatus {

	reqLogger := ctrl.Log.WithValues("ActiveMQArtemis Name", namespacedName.Name)
	reqLogger.Info("Getting status for pods")

	var status olm.DeploymentStatus
	var lastStatus olm.DeploymentStatus

	if _, lastStatusExist := lastStatusMap[namespacedName]; !lastStatusExist {
		ctrl.Log.Info("Creating lastStatus for new CR", "name", namespacedName)
		lastStatus = olm.DeploymentStatus{}
		lastStatusMap[namespacedName] = lastStatus
	}

	ssNamespacedName := types.NamespacedName{Name: namer.CrToSS(namespacedName.Name), Namespace: namespacedName.Namespace}
	sfsFound := &appsv1.StatefulSet{}
	err := client.Get(context.TODO(), ssNamespacedName, sfsFound)
	if err == nil {
		status = getSingleStatefulSetStatus(sfsFound)
	}

	// TODO: Remove global usage
	reqLogger.Info("lastStatus.Ready len is " + fmt.Sprint(len(lastStatus.Ready)))
	reqLogger.Info("status.Ready len is " + fmt.Sprint(len(status.Ready)))
	if len(status.Ready) > len(lastStatus.Ready) {
		// More pods ready, let the address controller know
		for i := len(lastStatus.Ready); i < len(status.Ready); i++ {
			reqLogger.Info("Notifying address controller", "new ready", i)
			channels.AddressListeningCh <- types.NamespacedName{Namespace: namespacedName.Namespace, Name: status.Ready[i]}
		}
	}
	lastStatusMap[namespacedName] = status

	return status
}

func getSingleStatefulSetStatus(ss *appsv1.StatefulSet) olm.DeploymentStatus {
	var ready, starting, stopped []string
	var requestedCount = int32(0)
	if ss.Spec.Replicas != nil {
		requestedCount = *ss.Spec.Replicas
	}

	targetCount := ss.Status.Replicas
	readyCount := ss.Status.ReadyReplicas

	if requestedCount == 0 || targetCount == 0 {
		stopped = append(stopped, ss.Name)
	} else {
		for i := int32(0); i < targetCount; i++ {
			instanceName := fmt.Sprintf("%s-%d", ss.Name, i)
			if i < readyCount {
				ready = append(ready, instanceName)
			} else {
				starting = append(starting, instanceName)
			}
		}
	}
	return olm.DeploymentStatus{
		Stopped:  stopped,
		Starting: starting,
		Ready:    ready,
	}
}

func MakeEnvVarArrayForCR(customResource *brokerv1beta1.ActiveMQArtemis, namer Namers) []corev1.EnvVar {

	requireLogin := "false"
	if customResource.Spec.DeploymentPlan.RequireLogin {
		requireLogin = "true"
	} else {
		requireLogin = "false"
	}

	journalType := "aio"
	if strings.ToLower(customResource.Spec.DeploymentPlan.JournalType) == "aio" {
		journalType = "aio"
	} else {
		journalType = "nio"
	}

	jolokiaAgentEnabled := "false"
	if customResource.Spec.DeploymentPlan.JolokiaAgentEnabled {
		jolokiaAgentEnabled = "true"
	} else {
		jolokiaAgentEnabled = "false"
	}

	managementRBACEnabled := "false"
	if customResource.Spec.DeploymentPlan.ManagementRBACEnabled {
		managementRBACEnabled = "true"
	} else {
		managementRBACEnabled = "false"
	}

	metricsPluginEnabled := "false"
	if customResource.Spec.DeploymentPlan.EnableMetricsPlugin != nil {
		metricsPluginEnabled = strconv.FormatBool(*customResource.Spec.DeploymentPlan.EnableMetricsPlugin)
	}

	envVar := []corev1.EnvVar{}
	envVarArrayForBasic := environments.AddEnvVarForBasic(requireLogin, journalType, namer.SvcPingNameBuilder.Name())
	envVar = append(envVar, envVarArrayForBasic...)
	if customResource.Spec.DeploymentPlan.PersistenceEnabled {
		envVarArrayForPresistent := environments.AddEnvVarForPersistent(customResource.Name)
		envVar = append(envVar, envVarArrayForPresistent...)
	}

	envVarArrayForCluster := environments.AddEnvVarForCluster(isClustered(customResource))
	envVar = append(envVar, envVarArrayForCluster...)

	envVarArrayForJolokia := environments.AddEnvVarForJolokia(jolokiaAgentEnabled)
	envVar = append(envVar, envVarArrayForJolokia...)

	envVarArrayForManagement := environments.AddEnvVarForManagement(managementRBACEnabled)
	envVar = append(envVar, envVarArrayForManagement...)

	envVarArrayForMetricsPlugin := environments.AddEnvVarForMetricsPlugin(metricsPluginEnabled)
	envVar = append(envVar, envVarArrayForMetricsPlugin...)

	// appending any Env from CR, to allow potential override
	envVar = append(envVar, customResource.Spec.Env...)

	return envVar
}

type brokerStatus struct {
	Properties brokerProps `json:"properties"`
}

type brokerProps struct {
	StatusProperties statusProps `json:"a_status.properties"`
}

type statusProps struct {
	Sha string `json:"cr:alder32"`
}

func UpdateBrokerPropertiesStatus(cr *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, scheme *runtime.Scheme) (ctrl.Result, error) {
	reqLogger := ctrl.Log.WithValues("ActiveMQArtemis Name", cr.Name)

	cmName := getConfigAppliedConfigMapName(cr)
	configMap := corev1.ConfigMap{}

	err := client.Get(context.TODO(), cmName, &configMap)
	if err != nil {
		return ctrl.Result{RequeueAfter: common.GetReconcileResyncPeriod()}, err
	}

	cmData := configMap.Data["a_status.properties"]
	cmData = strings.Replace(cmData, "# generated by crd\n#\nstatus=", "", 1)
	cmData = strings.Replace(cmData, "\n", "", 1)

	expected, err := extractSha(cmData)
	if err != nil {
		return ctrl.Result{}, err
	}

	resource := types.NamespacedName{
		Name:      cr.Name,
		Namespace: cr.Namespace,
	}

	ssInfos := ss.GetDeployedStatefulSetNames(client, []types.NamespacedName{resource})

	jks := jolokia_client.GetBrokers(resource, ssInfos, client)

	if len(jks) == 0 {
		reqLogger.Info("not found Jolokia Clients available. requeing")
		condition := metav1.Condition{
			Type:    brokerv1beta1.ConfigAppliedConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  brokerv1beta1.ConfigAppliedConditionNoJolokiaClientsAvailableReason,
			Message: brokerv1beta1.ConfigAppliedConditionNoJolokiaClientsAvailableMessage,
		}
		meta.SetStatusCondition(&cr.Status.Conditions, condition)
		return ctrl.Result{RequeueAfter: common.GetReconcileResyncPeriod()}, err
	}

	for _, jk := range jks {
		currentJson, err := jk.Artemis.GetStatus()
		if err != nil {
			reqLogger.Info("different status reported from Jolokia. requeing", "IP", jk.IP)
			condition := metav1.Condition{
				Type:    brokerv1beta1.ConfigAppliedConditionType,
				Status:  metav1.ConditionUnknown,
				Reason:  brokerv1beta1.ConfigAppliedConditionUnknownReason,
				Message: err.Error(),
			}
			meta.SetStatusCondition(&cr.Status.Conditions, condition)
			return ctrl.Result{}, err
		}
		current, err := extractSha(currentJson)
		if err != nil {
			return ctrl.Result{}, err
		}
		if expected != current {
			reqLogger.Info("different status reported from Jolokia. requeing", "IP", jk.IP)
			condition := metav1.Condition{
				Type:    brokerv1beta1.ConfigAppliedConditionType,
				Status:  metav1.ConditionFalse,
				Reason:  brokerv1beta1.ConfigAppliedConditionOutOfSyncReason,
				Message: brokerv1beta1.ConfigAppliedConditionOutOfSyncMessage,
			}
			meta.SetStatusCondition(&cr.Status.Conditions, condition)
			return ctrl.Result{RequeueAfter: common.GetReconcileResyncPeriod()}, err
		}
	}

	condition := metav1.Condition{
		Type:   brokerv1beta1.ConfigAppliedConditionType,
		Status: metav1.ConditionTrue,
		Reason: brokerv1beta1.ConfigAppliedConditionSynchedReason,
	}
	reqLogger.Info("successfully synced status in all Jolokia clients")
	meta.SetStatusCondition(&cr.Status.Conditions, condition)

	return ctrl.Result{}, nil
}

func extractSha(jsonStatus string) (string, error) {
	cmStatus := brokerStatus{}
	err := json.Unmarshal([]byte(jsonStatus), &cmStatus)
	if err != nil {
		return "", err
	}
	return cmStatus.Properties.StatusProperties.Sha, nil
}
