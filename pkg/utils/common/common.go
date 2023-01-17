package common

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// extra kinds
const (
	RouteKind              = "Route"
	OpenShiftAPIServerKind = "OpenShiftAPIServer"
	DEFAULT_RESYNC_PERIOD  = 30 * time.Second
)

var theManager manager.Manager

var resyncPeriod time.Duration = DEFAULT_RESYNC_PERIOD

func init() {
	if period, defined := os.LookupEnv("RECONCILE_RESYNC_PERIOD"); defined {
		var err error
		if resyncPeriod, err = time.ParseDuration(period); err != nil {
			resyncPeriod = DEFAULT_RESYNC_PERIOD
		}
	} else {
		resyncPeriod = DEFAULT_RESYNC_PERIOD
	}
}

func GetReconcileResyncPeriod() time.Duration {
	return resyncPeriod
}

type ActiveMQArtemisConfigHandler interface {
	IsApplicableFor(brokerNamespacedName types.NamespacedName) bool
	Config(initContainers []corev1.Container, outputDirRoot string, yacfgProfileVersion string, yacfgProfileName string) (value []string)
}

func compareQuantities(resList1 corev1.ResourceList, resList2 corev1.ResourceList, keys []corev1.ResourceName) bool {

	for _, key := range keys {
		if q1, ok1 := resList1[key]; ok1 {
			if q2, ok2 := resList2[key]; ok2 {
				if q1.Cmp(q2) != 0 {
					return false
				}
			} else {
				return false
			}
		} else {
			if _, ok2 := resList2[key]; ok2 {
				return false
			}
		}
	}
	return true
}

func CompareRequiredResources(res1 *corev1.ResourceRequirements, res2 *corev1.ResourceRequirements) bool {

	resNames := []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory, corev1.ResourceStorage, corev1.ResourceEphemeralStorage}
	if !compareQuantities(res1.Limits, res2.Limits, resNames) {
		return false
	}

	if !compareQuantities(res1.Requests, res2.Requests, resNames) {
		return false
	}
	return true
}

func ToJson(obj interface{}) (string, error) {
	bytes, err := json.Marshal(obj)
	if err == nil {
		return string(bytes), nil
	}
	return "", err
}

func FromJson(jsonStr *string, obj interface{}) error {
	return json.Unmarshal([]byte(*jsonStr), obj)
}

func SetManager(mgr manager.Manager) {
	theManager = mgr
}

func GetManager() manager.Manager {
	return theManager
}

func NewTrue() *bool {
	b := true
	return &b
}

// Given the operator's namespace and a string representation of
// it's WATCH_NAMESPACE value, this method returns whether
// the operator is watching its own(single) namespace, or watching multiple
// namespaces, or all namespace
// For watching single: it returns (true, nil)
// For watching multiple: it returns (false, [n]string) where n > 0
// For watching all: it returns (false, nil)
func ResolveWatchNamespaceForManager(oprNamespace string, watchNamespace string) (bool, []string) {

	if oprNamespace == watchNamespace {
		return true, nil
	}
	if watchNamespace == "*" || watchNamespace == "" {
		return false, nil
	}
	return false, strings.Split(watchNamespace, ",")
}

type BrokerVersion struct {
	Version *semver.Version
}

func NewBrokerVersion(ver *semver.Version) *BrokerVersion {
	return &BrokerVersion{
		Version: ver,
	}
}

func (bver *BrokerVersion) MajorOnly() bool {
	fields := strings.Split(bver.Version.Original(), ".")
	return len(fields) == 1
}

func (bver *BrokerVersion) MinorOnly() bool {
	fields := strings.Split(bver.Version.Original(), ".")
	return len(fields) == 2
}

func (bver *BrokerVersion) IsMatch(ver *semver.Version) bool {
	if bver.MajorOnly() {
		return bver.Version.Major() == ver.Major()
	}
	if bver.MinorOnly() {
		return bver.Version.Major() == ver.Major() && bver.Version.Minor() == ver.Minor()
	}
	return bver.Version.Equal(ver)
}

func ResolveBrokerVersion(existingVersions map[string]*semver.Version, expected *semver.Version) *semver.Version {

	versions := []*semver.Version{}
	for _, v := range existingVersions {
		versions = append(versions, v)
	}

	sort.Sort(semver.Collection(versions))

	brokerVersion := NewBrokerVersion(expected)

	for i := len(versions) - 1; i >= 0; i-- {
		if brokerVersion.IsMatch(versions[i]) {
			return versions[i]
		}
	}
	return nil
}

func Int32ToPtr(v int32) *int32 {
	return &v
}
