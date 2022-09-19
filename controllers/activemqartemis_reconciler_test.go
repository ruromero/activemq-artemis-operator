package controllers

import (
	"reflect"
	"testing"

	"github.com/RHsyseng/operator-utils/pkg/resource/compare"
	brokerv1beta1 "github.com/artemiscloud/activemq-artemis-operator/api/v1beta1"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHexShaHashOfMap(t *testing.T) {

	nilOne := hexShaHashOfMap(nil)
	nilTwo := hexShaHashOfMap(nil)

	if nilOne != nilTwo {
		t.Errorf("HexShaHashOfMap(nil) = %v, want %v", nilOne, nilTwo)
	}

	props := []string{"a=a", "b=b"}

	propsOriginal := hexShaHashOfMap(props)

	// modify
	props = append(props, "c=c")

	propsModified := hexShaHashOfMap(props)

	if propsOriginal == propsModified {
		t.Errorf("HexShaHashOfMap(props mod) = %v, want %v", propsOriginal, propsModified)
	}

	// revert, drop the last entry b/c they are ordered
	props = append(props[:2])

	if propsOriginal != hexShaHashOfMap(props) {
		t.Errorf("HexShaHashOfMap(props) with revert = %v, want %v", propsOriginal, hexShaHashOfMap(props))
	}

	// modify further, drop first entry
	props = append(props[:1])

	if propsOriginal == hexShaHashOfMap(props) {
		t.Errorf("HexShaHashOfMap(props) with just a = %v, want %v", propsOriginal, hexShaHashOfMap(props))
	}

}

func TestMapComparatorForStatefulSet(t *testing.T) {

	ss := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{Kind: "StatefulSet", APIVersion: "apps/v1beta1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:                       "ss",
			GenerateName:               "",
			Namespace:                  "a",
			SelfLink:                   "",
			UID:                        "",
			ResourceVersion:            "1",
			Generation:                 0,
			CreationTimestamp:          metav1.Time{},
			DeletionTimestamp:          &metav1.Time{},
			DeletionGracePeriodSeconds: new(int64),
			Labels:                     nil,
			Annotations:                nil,
			OwnerReferences:            []metav1.OwnerReference{},
			Finalizers:                 []string{},
			ClusterName:                "",
			ManagedFields:              []metav1.ManagedFieldsEntry{},
		},
		Spec:   appsv1.StatefulSetSpec{},
		Status: appsv1.StatefulSetStatus{},
	}

	ssMod := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{Kind: "StatefulSet", APIVersion: "apps/v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:                       "ss",
			GenerateName:               "",
			Namespace:                  "a",
			SelfLink:                   "",
			UID:                        "",
			ResourceVersion:            "1",
			Generation:                 0,
			CreationTimestamp:          metav1.Time{},
			DeletionTimestamp:          &metav1.Time{},
			DeletionGracePeriodSeconds: new(int64),
			Labels:                     nil,
			Annotations:                nil,
			OwnerReferences:            []metav1.OwnerReference{},
			Finalizers:                 []string{},
			ClusterName:                "",
			ManagedFields:              []metav1.ManagedFieldsEntry{},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:             new(int32),
			Selector:             &metav1.LabelSelector{},
			Template:             v1.PodTemplateSpec{},
			VolumeClaimTemplates: []v1.PersistentVolumeClaim{},
			ServiceName:          "ssMod",
			PodManagementPolicy:  "",
			UpdateStrategy:       appsv1.StatefulSetUpdateStrategy{},
			RevisionHistoryLimit: new(int32),
			MinReadySeconds:      0,
		},
		Status: appsv1.StatefulSetStatus{},
	}

	ss0 := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{Kind: "StatefulSet", APIVersion: "apps/v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:                       "ss0",
			GenerateName:               "",
			Namespace:                  "a",
			SelfLink:                   "",
			UID:                        "",
			ResourceVersion:            "1",
			Generation:                 0,
			CreationTimestamp:          metav1.Time{},
			DeletionTimestamp:          &metav1.Time{},
			DeletionGracePeriodSeconds: new(int64),
			Labels:                     nil,
			Annotations:                nil,
			OwnerReferences:            []metav1.OwnerReference{},
			Finalizers:                 []string{},
			ClusterName:                "",
			ManagedFields:              []metav1.ManagedFieldsEntry{},
		},
		Spec:   appsv1.StatefulSetSpec{},
		Status: appsv1.StatefulSetStatus{},
	}

	var requestedResources []client.Object

	requestedResources = append(requestedResources, ss0)

	requestedResources = append(requestedResources, ssMod)

	deployed := make(map[reflect.Type][]client.Object)
	var deployedSets []client.Object
	deployedSets = append(deployedSets, ss)

	ssType := reflect.ValueOf(ss).Elem().Type()
	deployed[ssType] = deployedSets

	requested := compare.NewMapBuilder().Add(requestedResources...).ResourceMap()
	comparator := compare.NewMapComparator()

	comparator.Comparator.SetDefaultComparator(semanticEquals)
	deltas := comparator.Compare(deployed, requested)

	for resourceType, delta := range deltas {
		t.Log("", "instances of ", resourceType, "Will create ", len(delta.Added), "update ", len(delta.Updated), "and delete", len(delta.Removed))

		for index := range delta.Added {
			resourceToAdd := delta.Added[index]
			t.Log("", "instances of ", resourceType, "Will add", resourceToAdd)
		}

		for index := range delta.Updated {
			resourceToUpdate := delta.Updated[index]
			t.Log("", "instances of ", resourceType, "Will update", resourceToUpdate)

		}

		for index := range delta.Removed {
			resourceToRemove := delta.Removed[index]
			t.Log("", "instances of ", resourceType, "Will remove", resourceToRemove)

		}
	}
	if len(deltas[ssType].Added) != 1 {
		t.Errorf("expect new addition to appear!")

	}
	if (len(deltas[ssType].Updated)) != 1 {
		t.Errorf("not good!, expect difference on ss to be respected as an update")
	}
}

func semanticEquals(a client.Object, b client.Object) bool {
	return equality.Semantic.DeepEqual(a, b)
}

func TestGetSingleStatefulSetStatus(t *testing.T) {

	var expected int32 = int32(1)
	ss := &appsv1.StatefulSet{}
	ss.ObjectMeta.Name = "joe"
	ss.Spec.Replicas = &expected
	ss.Status.Replicas = 1
	ss.Status.ReadyReplicas = 1

	statusRunning := getSingleStatefulSetStatus(ss)
	if statusRunning.Ready[0] != "joe-0" {
		t.Errorf("not good!, expect correct 0 ordinal" + statusRunning.Ready[0])
	}

	ss.Status.Replicas = 0
	ss.Status.ReadyReplicas = 0

	statusRunning = getSingleStatefulSetStatus(ss)
	if statusRunning.Stopped[0] != "joe" {
		t.Errorf("not good!, expect ss name in stopped" + statusRunning.Stopped[0])
	}

	var expectedTwo int32 = int32(2)
	ss.Spec.Replicas = &expectedTwo
	ss.Status.Replicas = 2
	ss.Status.ReadyReplicas = 1

	statusRunning = getSingleStatefulSetStatus(ss)
	if statusRunning.Ready[0] != "joe-0" {
		t.Errorf("not good!, expect correct 0 ordinal ready" + statusRunning.Ready[0])
	}

	if statusRunning.Starting[0] != "joe-1" {
		t.Errorf("not good!, expect ss name in starting" + statusRunning.Stopped[0])
	}

}

func TestGetConfigAppliedConfigMapName(t *testing.T) {
	cr := brokerv1beta1.ActiveMQArtemis{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-ns",
			Name:      "test",
		},
	}
	name := getConfigAppliedConfigMapName(&cr)
	assert.Equal(t, "test-ns", name.Namespace)
	assert.Equal(t, "test-props", name.Name)
}

func TestExtractSha(t *testing.T) {
	json := `{"properties": {"a_status.properties": {"cr:alder32": "123456"}}}`
	sha, err := extractSha(json)
	assert.Equal(t, "123456", sha)
	assert.NoError(t, err)

	json = `{"properties": {"a_status.properties": {}}}`
	sha, err = extractSha(json)
	assert.Empty(t, sha)
	assert.NoError(t, err)

	json = `you shall fail`
	sha, err = extractSha(json)
	assert.Empty(t, sha)
	assert.Error(t, err)
}

func TestGetJaasConfigExtraMountPath(t *testing.T) {
	cr := &brokerv1beta1.ActiveMQArtemis{
		Spec: brokerv1beta1.ActiveMQArtemisSpec{
			DeploymentPlan: brokerv1beta1.DeploymentPlanType{
				ExtraMounts: brokerv1beta1.ExtraMountsType{
					ConfigMaps: []string{
						"test-config-jaas-config",
						"some-cm",
					},
					Secrets: []string{
						"other",
					},
				},
			},
		},
	}
	path, found := getJaasConfigExtraMountPath(cr)
	assert.Equal(t, path, "/amq/extra/configmaps/test-config-jaas-config/login.config")
	assert.True(t, found)

	cr = &brokerv1beta1.ActiveMQArtemis{
		Spec: brokerv1beta1.ActiveMQArtemisSpec{
			DeploymentPlan: brokerv1beta1.DeploymentPlanType{
				ExtraMounts: brokerv1beta1.ExtraMountsType{
					ConfigMaps: []string{
						"test-config",
						"some-cm",
					},
					Secrets: []string{
						"test-config-jaas-config",
						"other-secret",
					},
				},
			},
		},
	}
	path, found = getJaasConfigExtraMountPath(cr)
	assert.Equal(t, path, "/amq/extra/secrets/test-config-jaas-config/login.config")
	assert.True(t, found)
}

func TestGetJaasConfigExtraMountPathNotPresent(t *testing.T) {
	cr := &brokerv1beta1.ActiveMQArtemis{
		Spec: brokerv1beta1.ActiveMQArtemisSpec{
			DeploymentPlan: brokerv1beta1.DeploymentPlanType{
				ExtraMounts: brokerv1beta1.ExtraMountsType{
					ConfigMaps: []string{
						"test-config",
					},
				},
			},
		},
	}
	path, found := getJaasConfigExtraMountPath(cr)
	assert.Empty(t, path)
	assert.False(t, found)
}

func TestNewPodTemplateSpecForCR_IncludesDebugArgs(t *testing.T) {
	// client := fake.NewClientBuilder().Build()
	reconciler := &ActiveMQArtemisReconcilerImpl{}

	cr := &brokerv1beta1.ActiveMQArtemis{
		Spec: brokerv1beta1.ActiveMQArtemisSpec{
			DeploymentPlan: brokerv1beta1.DeploymentPlanType{
				ExtraMounts: brokerv1beta1.ExtraMountsType{
					ConfigMaps: []string{
						"test-config-jaas-config",
						"some-cm",
					},
					Secrets: []string{
						"other",
					},
				},
			},
		},
	}

	newSpec, err := reconciler.NewPodTemplateSpecForCR(cr, Namers{}, &v1.PodTemplateSpec{}, k8sClient)

	assert.NoError(t, err)
	assert.NotNil(t, newSpec)
	expectedEnv := v1.EnvVar{
		Name:  "DEBUG_ARGS",
		Value: "-Djava.security.auth.login.config=/amq/extra/configmaps/test-config-jaas-config/login.config",
	}
	assert.Contains(t, newSpec.Spec.Containers[0].Env, expectedEnv)
}

func TestNewPodTemplateSpecForCR_AppendsDebugArgs(t *testing.T) {
	// client := fake.NewClientBuilder().Build()
	reconciler := &ActiveMQArtemisReconcilerImpl{}

	cr := &brokerv1beta1.ActiveMQArtemis{
		Spec: brokerv1beta1.ActiveMQArtemisSpec{
			Env: []v1.EnvVar{
				{
					Name:  "DEBUG_ARGS",
					Value: "-Dtest.arg=foo",
				},
			},
			DeploymentPlan: brokerv1beta1.DeploymentPlanType{
				ExtraMounts: brokerv1beta1.ExtraMountsType{
					ConfigMaps: []string{
						"test-config-jaas-config",
						"some-cm",
					},
					Secrets: []string{
						"other",
					},
				},
			},
		},
	}

	newSpec, err := reconciler.NewPodTemplateSpecForCR(cr, Namers{}, &v1.PodTemplateSpec{}, k8sClient)

	assert.NoError(t, err)
	assert.NotNil(t, newSpec)
	expectedEnv := v1.EnvVar{
		Name:  "DEBUG_ARGS",
		Value: "-Dtest.arg=foo -Djava.security.auth.login.config=/amq/extra/configmaps/test-config-jaas-config/login.config",
	}
	assert.Contains(t, newSpec.Spec.Containers[0].Env, expectedEnv)
}
