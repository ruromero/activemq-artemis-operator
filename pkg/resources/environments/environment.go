package environments

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/artemiscloud/activemq-artemis-operator/pkg/utils/common"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/utils/random"
	corev1 "k8s.io/api/core/v1"

	logf "sigs.k8s.io/controller-runtime"
)

var log = logf.Log.WithName("package environments")

//TODO: Remove this blatant hack
var GLOBAL_AMQ_CLUSTER_USER string = ""
var GLOBAL_AMQ_CLUSTER_PASSWORD string = ""

type defaults struct {
	AMQ_USER             string
	AMQ_PASSWORD         string
	AMQ_CLUSTER_USER     string
	AMQ_CLUSTER_PASSWORD string
}

var Defaults defaults

func init() {
	if "" == Defaults.AMQ_USER {
		Defaults.AMQ_USER = random.GenerateRandomString(8)
	}
	if "" == Defaults.AMQ_PASSWORD {
		Defaults.AMQ_PASSWORD = random.GenerateRandomString(8)
	}
	if "" == Defaults.AMQ_CLUSTER_USER {
		Defaults.AMQ_CLUSTER_USER = random.GenerateRandomString(8)
		// TODO: remove this hack
		GLOBAL_AMQ_CLUSTER_USER = Defaults.AMQ_CLUSTER_USER
	}
	if "" == Defaults.AMQ_CLUSTER_PASSWORD {
		Defaults.AMQ_CLUSTER_PASSWORD = random.GenerateRandomString(8)
		// TODO: remove this hack
		GLOBAL_AMQ_CLUSTER_PASSWORD = Defaults.AMQ_CLUSTER_PASSWORD
	}
}

func DetectOpenshift() (bool, error) {

	value, ok := os.LookupEnv("OPERATOR_OPENSHIFT")
	if ok {
		log.Info("Set by env-var 'OPERATOR_OPENSHIFT': " + value)
		return strings.ToLower(value) == "true", nil
	}

	// Find out if we're on OpenShift or Kubernetes
	stateManager := common.GetStateManager()
	isOpenshift, keyExists := stateManager.GetState(common.OpenShiftAPIServerKind).(bool)

	if keyExists {
		if isOpenshift {
			log.Info("environment is openshift")
		} else {
			log.Info("environment is not openshift")
		}

		return isOpenshift, nil
	}
	return false, errors.New("environment not yet determined")
}

func AddEnvVarForBasic(requireLogin string, journalType string, svcPingName string) []corev1.EnvVar {

	envVarArray := []corev1.EnvVar{
		{
			Name:      "AMQ_ROLE",
			Value:     "admin",
			ValueFrom: nil,
		},
		{
			Name:      "AMQ_NAME",
			Value:     "amq-broker",
			ValueFrom: nil,
		},
		{
			Name:      "AMQ_TRANSPORTS",
			Value:     "",
			ValueFrom: nil,
		},
		{
			Name:      "AMQ_QUEUES",
			Value:     "",
			ValueFrom: nil,
		},
		{
			Name:      "AMQ_ADDRESSES",
			Value:     "",
			ValueFrom: nil,
		},
		{
			Name:      "AMQ_GLOBAL_MAX_SIZE",
			Value:     "100 mb",
			ValueFrom: nil,
		},
		{
			Name:      "AMQ_REQUIRE_LOGIN",
			Value:     requireLogin,
			ValueFrom: nil,
		},
		{
			Name:      "AMQ_EXTRA_ARGS",
			Value:     "--no-autotune",
			ValueFrom: nil,
		},
		{
			Name:      "AMQ_ANYCAST_PREFIX",
			Value:     "",
			ValueFrom: nil,
		},
		{
			Name:      "AMQ_MULTICAST_PREFIX",
			Value:     "",
			ValueFrom: nil,
		},
		{
			Name:      "POD_NAMESPACE",
			Value:     "",
			ValueFrom: nil,
		},
		{
			Name:      "AMQ_JOURNAL_TYPE",
			Value:     journalType,
			ValueFrom: nil,
		},
		{
			Name:      "TRIGGERED_ROLL_COUNT",
			Value:     "0",
			ValueFrom: nil,
		},
		{
			Name:      "PING_SVC_NAME",
			Value:     svcPingName,
			ValueFrom: nil,
		},
		{
			Name:      "OPENSHIFT_DNS_PING_SERVICE_PORT",
			Value:     "7800",
			ValueFrom: nil,
		},
	}

	return envVarArray
}

func AddEnvVarForPersistent(customResourceName string) []corev1.EnvVar {

	envVarArray := []corev1.EnvVar{
		{
			Name:      "AMQ_DATA_DIR",
			Value:     "/opt/" + customResourceName + "/data",
			ValueFrom: nil,
		},
		{
			Name:      "AMQ_DATA_DIR_LOGGING",
			Value:     "true",
			ValueFrom: nil,
		},
	}

	return envVarArray
}

func AddEnvVarForCluster(isClustered bool) []corev1.EnvVar {

	envVarArray := []corev1.EnvVar{
		{
			Name:      "AMQ_CLUSTERED",
			Value:     strconv.FormatBool(isClustered),
			ValueFrom: nil,
		},
	}

	return envVarArray
}

func AddEnvVarForJolokia(jolokiaAgentEnabled string) []corev1.EnvVar {

	envVarArray := []corev1.EnvVar{
		{
			Name:      "AMQ_ENABLE_JOLOKIA_AGENT",
			Value:     jolokiaAgentEnabled,
			ValueFrom: nil,
		},
	}

	return envVarArray
}

func AddEnvVarForManagement(managementRBACEnabled string) []corev1.EnvVar {

	envVarArray := []corev1.EnvVar{
		{
			Name:      "AMQ_ENABLE_MANAGEMENT_RBAC",
			Value:     managementRBACEnabled,
			ValueFrom: nil,
		},
	}

	return envVarArray
}

func AddEnvVarForMetricsPlugin(metricsPluginEnabled string) []corev1.EnvVar {

	envVarArray := []corev1.EnvVar{
		{
			Name:      "AMQ_ENABLE_METRICS_PLUGIN",
			Value:     metricsPluginEnabled,
			ValueFrom: nil,
		},
	}

	return envVarArray
}

// https://stackoverflow.com/questions/37334119/how-to-delete-an-element-from-a-slice-in-golang
func remove(s []corev1.EnvVar, i int) []corev1.EnvVar {
	s[i] = s[len(s)-1]
	// We do not need to put s[i] at the end, as it will be discarded anyway
	return s[:len(s)-1]
}

func BoolSyncCausedUpdateOn(containers []corev1.Container, envVarName string, updatedValue bool) *corev1.EnvVar {

	var retEnvVar *corev1.EnvVar = nil

	found := false
	needsUpdate := false

	// Find the existing values
	for _, v := range containers[0].Env {
		if v.Name == envVarName {
			found = true
			currentValue, _ := strconv.ParseBool(v.Value)
			if currentValue != updatedValue {
				needsUpdate = true
			}
		}
	}

	if !found || needsUpdate {
		retEnvVar = &corev1.EnvVar{
			Name:      envVarName,
			Value:     strconv.FormatBool(updatedValue),
			ValueFrom: nil,
		}
	}

	return retEnvVar
}

func StringSyncCausedUpdateOn(containers []corev1.Container, envVarName string, updatedValue string) *corev1.EnvVar {

	var retEnvVar *corev1.EnvVar = nil

	found := false
	needsUpdate := false

	// Find the existing values
	for _, v := range containers[0].Env {
		if v.Name == envVarName {
			found = true
			currentValue := v.Value
			if currentValue != updatedValue {
				needsUpdate = true
			}
		}
	}

	if !found || needsUpdate {
		retEnvVar = &corev1.EnvVar{
			Name:      envVarName,
			Value:     updatedValue,
			ValueFrom: nil,
		}
	}

	return retEnvVar
}

func TrackSecretCheckSumInRollCount(checkSum string, containers []corev1.Container) {

	newTriggeredRollCountEnvVar := corev1.EnvVar{
		Name:      "TRIGGERED_ROLL_COUNT",
		Value:     checkSum,
		ValueFrom: nil,
	}
	Update(containers, &newTriggeredRollCountEnvVar)
}

func Create(containers []corev1.Container, envVar *corev1.EnvVar) {

	for i := 0; i < len(containers); i++ {
		containers[i].Env = append(containers[i].Env, *envVar)
	}
}

func CreateOrAppend(containers []corev1.Container, envVar *corev1.EnvVar) {

	for i, container := range containers {
		existing := RetrieveFrom(containers[i], envVar.Name)
		if existing == nil {
			containers[i].Env = append(container.Env, *envVar)
		} else {
			existing.Value += " " + envVar.Value
		}
	}
}

func Retrieve(containers []corev1.Container, envVarName string) *corev1.EnvVar {

	var retEnvVar *corev1.EnvVar = nil
	for i := 0; i < len(containers) && nil == retEnvVar; i++ {
		for j := len(containers[i].Env) - 1; j >= 0; j-- {
			if envVarName == containers[i].Env[j].Name {
				retEnvVar = &containers[i].Env[j]
				break
			}
		}
	}

	return retEnvVar
}

func RetrieveFrom(container corev1.Container, envVarName string) *corev1.EnvVar {

	var retEnvVar *corev1.EnvVar = nil
	for i, envVar := range container.Env {
		if envVarName == envVar.Name {
			retEnvVar = &container.Env[i]
			break
		}
	}
	return retEnvVar
}

func Update(containers []corev1.Container, envVar *corev1.EnvVar) {

	for i := 0; i < len(containers); i++ {
		for j := len(containers[i].Env) - 1; j >= 0; j-- {
			if envVar.Name == containers[i].Env[j].Name {
				containers[i].Env[j] = *envVar
			}
		}
	}
}

func Delete(containers []corev1.Container, envVarName string) {

	for i := 0; i < len(containers); i++ {
		for j := len(containers[i].Env) - 1; j >= 0; j-- {
			if envVarName == containers[i].Env[j].Name {
				containers[i].Env = remove(containers[i].Env, j)
			}
		}
	}
}
