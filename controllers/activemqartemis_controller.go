/*
Copyright 2021.

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

package controllers

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	rtclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/artemiscloud/activemq-artemis-operator/pkg/resources"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/utils/namer"
	"github.com/pkg/errors"

	brokerv1beta1 "github.com/artemiscloud/activemq-artemis-operator/api/v1beta1"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/utils/common"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/utils/selectors"
)

var clog = ctrl.Log.WithName("controller_v1beta1activemqartemis")

var namespaceToConfigHandler = make(map[types.NamespacedName]common.ActiveMQArtemisConfigHandler)

func GetBrokerConfigHandler(brokerNamespacedName types.NamespacedName) (handler common.ActiveMQArtemisConfigHandler) {
	for _, handler := range namespaceToConfigHandler {
		if handler.IsApplicableFor(brokerNamespacedName) {
			return handler
		}
	}
	return nil
}

func (r *ActiveMQArtemisReconciler) UpdatePodForSecurity(securityHandlerNamespacedName types.NamespacedName, handler common.ActiveMQArtemisConfigHandler) error {

	existingCrs := &brokerv1beta1.ActiveMQArtemisList{}
	var err error
	opts := &rtclient.ListOptions{}
	if err = r.Client.List(context.TODO(), existingCrs, opts); err == nil {
		var candidate types.NamespacedName
		for index, artemis := range existingCrs.Items {
			candidate.Name = artemis.Name
			candidate.Namespace = artemis.Namespace
			if handler.IsApplicableFor(candidate) {
				clog.V(1).Info("force reconcile for security", "handler", securityHandlerNamespacedName, "CR", candidate)
				r.events <- event.GenericEvent{Object: &existingCrs.Items[index]}
			}
		}
	}
	return err
}

func (r *ActiveMQArtemisReconciler) RemoveBrokerConfigHandler(namespacedName types.NamespacedName) {
	clog.V(1).Info("Removing config handler", "name", namespacedName)
	oldHandler, ok := namespaceToConfigHandler[namespacedName]
	if ok {
		delete(namespaceToConfigHandler, namespacedName)
		clog.V(2).Info("Handler removed", "name", namespacedName)
		r.UpdatePodForSecurity(namespacedName, oldHandler)
	}
}

func (r *ActiveMQArtemisReconciler) AddBrokerConfigHandler(namespacedName types.NamespacedName, handler common.ActiveMQArtemisConfigHandler, toReconcile bool) error {
	if _, ok := namespaceToConfigHandler[namespacedName]; ok {
		clog.V(2).Info("There is an old config handler, it'll be replaced")
	}
	namespaceToConfigHandler[namespacedName] = handler
	clog.V(2).Info("A new config handler has been added", "handler", handler)
	if toReconcile {
		clog.V(1).Info("Updating broker security")
		return r.UpdatePodForSecurity(namespacedName, handler)
	}
	return nil
}

// ActiveMQArtemisReconciler reconciles a ActiveMQArtemis object
type ActiveMQArtemisReconciler struct {
	rtclient.Client
	Scheme *runtime.Scheme
	events chan event.GenericEvent
}

//run 'make manifests' after changing the following rbac markers

//+kubebuilder:rbac:groups=broker.amq.io,namespace=activemq-artemis-operator,resources=activemqartemises,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=broker.amq.io,namespace=activemq-artemis-operator,resources=activemqartemises/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=broker.amq.io,namespace=activemq-artemis-operator,resources=activemqartemises/finalizers,verbs=update
//+kubebuilder:rbac:groups=broker.amq.io,namespace=activemq-artemis-operator,resources=pods,verbs=get;list
//+kubebuilder:rbac:groups="",namespace=activemq-artemis-operator,resources=pods;services;endpoints;persistentvolumeclaims;events;configmaps;secrets;routes;serviceaccounts,verbs=*
//+kubebuilder:rbac:groups="",namespace=activemq-artemis-operator,resources=namespaces,verbs=get
//+kubebuilder:rbac:groups=apps,namespace=activemq-artemis-operator,resources=deployments;daemonsets;replicasets;statefulsets,verbs=*
//+kubebuilder:rbac:groups=networking.k8s.io,namespace=activemq-artemis-operator,resources=ingresses,verbs=get;list;watch;create;delete
//+kubebuilder:rbac:groups=route.openshift.io,namespace=activemq-artemis-operator,resources=routes;routes/custom-host;routes/status,verbs=get;list;watch;create;delete;update
//+kubebuilder:rbac:groups=monitoring.coreos.com,namespace=activemq-artemis-operator,resources=servicemonitors,verbs=get;create
//+kubebuilder:rbac:groups=apps,namespace=activemq-artemis-operator,resources=deployments/finalizers,verbs=update
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,namespace=activemq-artemis-operator,resources=roles;rolebindings,verbs=create;get;delete
//+kubebuilder:rbac:groups=policy,namespace=activemq-artemis-operator,resources=poddisruptionbudgets,verbs=create;get;delete;list;update;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ActiveMQArtemis object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *ActiveMQArtemisReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	reqLogger := ctrl.Log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name, "Reconciling", "ActiveMQArtemis")

	customResource := &brokerv1beta1.ActiveMQArtemis{}

	// Fetch the ActiveMQArtemis instance
	// When first creating this will have err == nil
	// When deleting after creation this will have err NotFound
	// When deleting before creation reconcile won't be called
	err := r.Get(context.TODO(), request.NamespacedName, customResource)

	if err != nil {
		if apierrors.IsNotFound(err) {
			reqLogger.V(1).Info("ActiveMQArtemis Controller Reconcile encountered a IsNotFound, for request NamespacedName " + request.NamespacedName.String())
			return ctrl.Result{}, nil
		}
		reqLogger.Error(err, "unable to retrieve the ActiveMQArtemis", "request", request)
		return ctrl.Result{}, err
	}

	namer := MakeNamers(customResource)
	reconciler := ActiveMQArtemisReconcilerImpl{}

	result := ctrl.Result{}
	var valid = true

	if valid, result = validate(customResource, r.Client, r.Scheme, *namer); valid {

		reconciler.Process(customResource, *namer, r.Client, r.Scheme)

		result = UpdateBrokerPropertiesStatus(customResource, r.Client, r.Scheme)
	}

	UpdateStatus(customResource, r.Client, request.NamespacedName, *namer)

	err = UpdateCRStatus(customResource, r.Client, request.NamespacedName)

	if err != nil {
		if apierrors.IsConflict(err) {
			reqLogger.V(1).Info("unable to update ActiveMQArtemis status", "Request Namespace", request.Namespace, "Request Name", request.Name, "error", err)
			err = nil // we don't want the controller event loop reporting this as an error
		} else {
			reqLogger.V(1).Error(err, "unable to update ActiveMQArtemis status", "Request Namespace", request.Namespace, "Request Name", request.Name)
		}
		return ctrl.Result{RequeueAfter: common.GetReconcileResyncPeriod()}, err
	}

	if result.IsZero() {
		reqLogger.Info("resource successfully reconciled")
		if hasExtraMounts(customResource) {
			reqLogger.V(1).Info("resource has extraMounts, requeuing for periodic sync")
			result = ctrl.Result{RequeueAfter: common.GetReconcileResyncPeriod()}
		}
	} else {
		reqLogger.V(1).Info("requeue resource")
	}
	return result, err
}

func validate(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, scheme *runtime.Scheme, namer Namers) (bool, ctrl.Result) {
	// Do additional validation here
	validationCondition := metav1.Condition{
		Type:   brokerv1beta1.ValidConditionType,
		Status: metav1.ConditionTrue,
		Reason: brokerv1beta1.ValidConditionSuccessReason,
	}

	condition, retry := validateExtraMounts(customResource, client, scheme)
	if condition != nil {
		validationCondition = *condition
	}

	if validationCondition.Status == metav1.ConditionTrue && customResource.Spec.DeploymentPlan.PodDisruptionBudget != nil {
		condition := validatePodDisruption(customResource)
		if condition != nil {
			validationCondition = *condition
		}
	}

	if validationCondition.Status == metav1.ConditionTrue {
		condition, retry = validateSSLEnabledSecrets(customResource, client, scheme, namer)
		if condition != nil {
			validationCondition = *condition
		}
	}

	if validationCondition.Status == metav1.ConditionTrue {
		condition := validateBrokerVersion(customResource)
		if condition != nil {
			validationCondition = *condition
		}
	}

	validationCondition.ObservedGeneration = customResource.Generation
	meta.SetStatusCondition(&customResource.Status.Conditions, validationCondition)

	if retry {
		return validationCondition.Status != metav1.ConditionFalse, ctrl.Result{Requeue: retry, RequeueAfter: common.GetReconcileResyncPeriod()}
	} else {
		return validationCondition.Status != metav1.ConditionFalse, ctrl.Result{}
	}
}

func validateSSLEnabledSecrets(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, scheme *runtime.Scheme, namer Namers) (*metav1.Condition, bool) {

	var retry = true
	if customResource.Spec.Console.SSLEnabled {

		secretName := namer.SecretsConsoleNameBuilder.Name()

		secret := corev1.Secret{}
		found := retrieveResource(secretName, customResource.Namespace, &secret, client, scheme)
		if !found {
			return &metav1.Condition{
				Type:    brokerv1beta1.ValidConditionType,
				Status:  metav1.ConditionFalse,
				Reason:  brokerv1beta1.ValidConditionMissingResourcesReason,
				Message: fmt.Sprintf(".Spec.Console.SSLEnabled is true but required secret %v is not found", secretName),
			}, retry
		}

		contextMessage := ".Spec.Console.SSLEnabled is true but required"
		for _, key := range []string{
			"keyStorePassword",
			"trustStorePassword",
		} {
			Condition := AssertSecretContainsKey(secret, key, contextMessage)
			if Condition != nil {
				return Condition, retry
			}
		}

		Condition := AssertSecretContainsOneOf(secret, []string{
			"keyStorePath",
			"broker.ks"}, contextMessage)
		if Condition != nil {
			return Condition, retry
		}

		Condition = AssertSecretContainsOneOf(secret, []string{
			"trustStorePath",
			"client.ts"}, contextMessage)
		if Condition != nil {
			return Condition, retry
		}
	}
	return nil, false
}

func validatePodDisruption(customResource *brokerv1beta1.ActiveMQArtemis) *metav1.Condition {
	pdb := customResource.Spec.DeploymentPlan.PodDisruptionBudget
	if pdb.Selector != nil {
		return &metav1.Condition{
			Type:    brokerv1beta1.ValidConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  brokerv1beta1.ValidConditionPDBNonNilSelectorReason,
			Message: common.PDBNonNilSelectorMessage,
		}
	}
	return nil
}

func validateBrokerVersion(customResource *brokerv1beta1.ActiveMQArtemis) *metav1.Condition {

	var result *metav1.Condition = nil
	if customResource.Spec.Version != "" {
		if isLockedDown(customResource.Spec.DeploymentPlan.Image) || isLockedDown(customResource.Spec.DeploymentPlan.InitImage) {
			result = &metav1.Condition{
				Type:    brokerv1beta1.ValidConditionType,
				Status:  metav1.ConditionUnknown,
				Reason:  brokerv1beta1.ValidConditionUnknownReason,
				Message: common.ImageVersionConflictMessage,
			}
		}

		_, err := resolveBrokerVersion(customResource)
		if err != nil {
			result = &metav1.Condition{
				Type:    brokerv1beta1.ValidConditionType,
				Status:  metav1.ConditionFalse,
				Reason:  brokerv1beta1.ValidConditionInvalidVersionReason,
				Message: fmt.Sprintf(".Spec.Version does not resolve to a supported broker version, reason %v", err),
			}
		}

	} else if (isLockedDown(customResource.Spec.DeploymentPlan.Image) && !isLockedDown(customResource.Spec.DeploymentPlan.InitImage)) || (isLockedDown(customResource.Spec.DeploymentPlan.InitImage) && !isLockedDown(customResource.Spec.DeploymentPlan.Image)) {
		result = &metav1.Condition{
			Type:    brokerv1beta1.ValidConditionType,
			Status:  metav1.ConditionUnknown,
			Reason:  brokerv1beta1.ValidConditionUnknownReason,
			Message: common.ImageDependentPairMessage,
		}
	}

	return result
}

func validateExtraMounts(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, scheme *runtime.Scheme) (*metav1.Condition, bool) {

	instanceCounts := map[string]int{}
	var Condition *metav1.Condition
	var retry bool = true
	var ContextMessage = ".Spec.DeploymentPlan.ExtraMounts.ConfigMaps,"
	for _, cm := range customResource.Spec.DeploymentPlan.ExtraMounts.ConfigMaps {
		configMap := corev1.ConfigMap{}
		found := retrieveResource(cm, customResource.Namespace, &configMap, client, scheme)
		if !found {
			return &metav1.Condition{
				Type:    brokerv1beta1.ValidConditionType,
				Status:  metav1.ConditionFalse,
				Reason:  brokerv1beta1.ValidConditionMissingResourcesReason,
				Message: fmt.Sprintf("%v missing required configMap %v", ContextMessage, cm),
			}, retry
		}
		if strings.HasSuffix(cm, loggingConfigSuffix) {
			Condition = AssertConfigMapContainsKey(configMap, LoggingConfigKey, ContextMessage)
			instanceCounts[loggingConfigSuffix]++
		} else if strings.HasSuffix(cm, jaasConfigSuffix) {
			Condition = &metav1.Condition{
				Type:    brokerv1beta1.ValidConditionType,
				Status:  metav1.ConditionFalse,
				Reason:  brokerv1beta1.ValidConditionFailedExtraMountReason,
				Message: fmt.Sprintf("%v entry %v with suffix %v must be a secret", ContextMessage, cm, jaasConfigSuffix),
			}
			retry = false // Cr needs an update
		}
		if Condition != nil {
			return Condition, retry
		}
	}

	ContextMessage = ".Spec.DeploymentPlan.ExtraMounts.Secrets,"
	for _, s := range customResource.Spec.DeploymentPlan.ExtraMounts.Secrets {
		secret := corev1.Secret{}
		found := retrieveResource(s, customResource.Namespace, &secret, client, scheme)
		if !found {
			return &metav1.Condition{
				Type:    brokerv1beta1.ValidConditionType,
				Status:  metav1.ConditionFalse,
				Reason:  brokerv1beta1.ValidConditionMissingResourcesReason,
				Message: fmt.Sprintf("%v missing required secret %v", ContextMessage, s),
			}, retry
		}
		if strings.HasSuffix(s, loggingConfigSuffix) {
			Condition = AssertSecretContainsKey(secret, LoggingConfigKey, ContextMessage)
			instanceCounts[loggingConfigSuffix]++
		} else if strings.HasSuffix(s, jaasConfigSuffix) {
			Condition = AssertSecretContainsKey(secret, JaasConfigKey, ContextMessage)
			if Condition == nil {
				Condition = AssertSyntaxOkOnLoginConfigData(secret.Data[JaasConfigKey], s, ContextMessage)
			}
			instanceCounts[jaasConfigSuffix]++
		}
		if Condition != nil {
			return Condition, retry
		}
	}
	Condition = AssertInstanceCounts(instanceCounts)
	if Condition != nil {
		return Condition, false // CR needs update
	}

	return nil, false
}

func AssertSyntaxOkOnLoginConfigData(SecretContentForLoginConfigKey []byte, name string, contextMessage string) *metav1.Condition {

	if !MatchBytesAgainsLoginConfigRegexp(SecretContentForLoginConfigKey) {

		return &metav1.Condition{
			Type:    brokerv1beta1.ValidConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  brokerv1beta1.ValidConditionFailedExtraMountReason,
			Message: fmt.Sprintf("%s content of login.config key in secret %v does not match supported jaas config file syntax", contextMessage, name),
		}
	}

	return nil
}

var loginConfigSyntaxMatcher *regexp.Regexp

func MatchBytesAgainsLoginConfigRegexp(buffer []byte) bool {
	syntaxMatchRegEx := common.GetJaasConfigSyntaxMatchRegEx()
	if syntaxMatchRegEx == "" {
		// disabled
		return true
	}

	if loginConfigSyntaxMatcher == nil {
		loginConfigSyntaxMatcher, _ = regexp.Compile(syntaxMatchRegEx)
	}
	return loginConfigSyntaxMatcher.Match(buffer)
}

func AssertInstanceCounts(instanceCounts map[string]int) *metav1.Condition {
	for key, v := range instanceCounts {
		if v > 1 {
			return &metav1.Condition{
				Type:    brokerv1beta1.ValidConditionType,
				Status:  metav1.ConditionFalse,
				Reason:  brokerv1beta1.ValidConditionFailedExtraMountReason,
				Message: fmt.Sprintf("Spec.DeploymentPlan.ExtraMounts, entry with suffix %v can only be supplied once", key),
			}
		}
	}
	return nil
}

func AssertConfigMapContainsKey(configMap corev1.ConfigMap, key string, contextMessage string) *metav1.Condition {
	if _, present := configMap.Data[key]; !present {
		return &metav1.Condition{
			Type:    brokerv1beta1.ValidConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  brokerv1beta1.ValidConditionFailedExtraMountReason,
			Message: fmt.Sprintf("%s configmap %v must have key %v", contextMessage, configMap.Name, key),
		}
	}
	return nil
}

func AssertSecretContainsKey(secret corev1.Secret, key string, contextMessage string) *metav1.Condition {
	if _, present := secret.Data[key]; !present {
		return &metav1.Condition{
			Type:    brokerv1beta1.ValidConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  brokerv1beta1.ValidConditionFailedExtraMountReason,
			Message: fmt.Sprintf("%s secret %v must have key %v", contextMessage, secret.Name, key),
		}
	}
	return nil
}

func AssertSecretContainsOneOf(secret corev1.Secret, keys []string, contextMessage string) *metav1.Condition {
	for _, key := range keys {
		_, present := secret.Data[key]
		if present {
			return nil
		}
	}
	return &metav1.Condition{
		Type:    brokerv1beta1.ValidConditionType,
		Status:  metav1.ConditionFalse,
		Reason:  brokerv1beta1.ValidConditionFailedExtraMountReason,
		Message: fmt.Sprintf("%s secret %v must contain one of following keys %v", contextMessage, secret.Name, keys),
	}
}

func retrieveResource(name, namespace string, obj rtclient.Object, client rtclient.Client, scheme *runtime.Scheme) bool {
	err := client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, obj)
	return err == nil
}

func hasExtraMounts(cr *brokerv1beta1.ActiveMQArtemis) bool {
	if cr == nil {
		return false
	}
	if len(cr.Spec.DeploymentPlan.ExtraMounts.ConfigMaps) > 0 {
		return true
	}
	return len(cr.Spec.DeploymentPlan.ExtraMounts.Secrets) > 0
}

type Namers struct {
	SsGlobalName                  string
	SsNameBuilder                 namer.NamerData
	SvcHeadlessNameBuilder        namer.NamerData
	SvcPingNameBuilder            namer.NamerData
	PodsNameBuilder               namer.NamerData
	SecretsCredentialsNameBuilder namer.NamerData
	SecretsConsoleNameBuilder     namer.NamerData
	SecretsNettyNameBuilder       namer.NamerData
	LabelBuilder                  selectors.LabelerData
	GLOBAL_DATA_PATH              string
}

func MakeNamers(customResource *brokerv1beta1.ActiveMQArtemis) *Namers {
	newNamers := Namers{
		SsGlobalName:                  "",
		SsNameBuilder:                 namer.NamerData{},
		SvcHeadlessNameBuilder:        namer.NamerData{},
		SvcPingNameBuilder:            namer.NamerData{},
		PodsNameBuilder:               namer.NamerData{},
		SecretsCredentialsNameBuilder: namer.NamerData{},
		SecretsConsoleNameBuilder:     namer.NamerData{},
		SecretsNettyNameBuilder:       namer.NamerData{},
		LabelBuilder:                  selectors.LabelerData{},
		GLOBAL_DATA_PATH:              "/opt/" + customResource.Name + "/data",
	}
	newNamers.SsNameBuilder.Base(customResource.Name).Suffix("ss").Generate()
	newNamers.SsGlobalName = customResource.Name
	newNamers.SvcHeadlessNameBuilder.Prefix(customResource.Name).Base("hdls").Suffix("svc").Generate()
	newNamers.SvcPingNameBuilder.Prefix(customResource.Name).Base("ping").Suffix("svc").Generate()
	newNamers.PodsNameBuilder.Base(customResource.Name).Suffix("container").Generate()
	newNamers.SecretsCredentialsNameBuilder.Prefix(customResource.Name).Base("credentials").Suffix("secret").Generate()
	if customResource.Spec.Console.SSLSecret != "" {
		newNamers.SecretsConsoleNameBuilder.SetName(customResource.Spec.Console.SSLSecret)
	} else {
		newNamers.SecretsConsoleNameBuilder.Prefix(customResource.Name).Base("console").Suffix("secret").Generate()
	}
	newNamers.SecretsNettyNameBuilder.Prefix(customResource.Name).Base("netty").Suffix("secret").Generate()
	newNamers.LabelBuilder.Base(customResource.Name).Suffix("app").Generate()

	return &newNamers
}

func GetDefaultLabels(cr *brokerv1beta1.ActiveMQArtemis) map[string]string {
	defaultLabelData := selectors.LabelerData{}
	defaultLabelData.Base(cr.Name).Suffix("app").Generate()
	return defaultLabelData.Labels()
}

// only test uses this
func NewReconcileActiveMQArtemis(c rtclient.Client, s *runtime.Scheme) ActiveMQArtemisReconciler {
	return ActiveMQArtemisReconciler{
		Client: c,
		Scheme: s,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ActiveMQArtemisReconciler) SetupWithManager(mgr ctrl.Manager) error {
	builder := ctrl.NewControllerManagedBy(mgr).
		For(&brokerv1beta1.ActiveMQArtemis{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Pod{})
	var err error
	controller, err := builder.Build(r)
	if err == nil {
		r.events = make(chan event.GenericEvent)
		err = controller.Watch(
			&source.Channel{Source: r.events},
			&handler.EnqueueRequestForObject{},
		)
	}
	return err
}

func UpdateCRStatus(desired *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, namespacedName types.NamespacedName) error {

	common.SetReadyCondition(&desired.Status.Conditions)

	current := &brokerv1beta1.ActiveMQArtemis{}

	err := client.Get(context.TODO(), namespacedName, current)
	if err != nil {
		clog.V(1).Error(err, "unable to retrieve current resource", "ActiveMQArtemis", namespacedName)
		return err
	}

	if current.Status.DeploymentPlanSize != desired.Status.DeploymentPlanSize ||
		current.Status.ScaleLabelSelector != desired.Status.ScaleLabelSelector ||
		!reflect.DeepEqual(current.Status.Version, desired.Status.Version) ||
		len(desired.Status.ExternalConfigs) != len(current.Status.ExternalConfigs) ||
		externalConfigsModified(desired, current) ||
		!reflect.DeepEqual(current.Status.PodStatus, desired.Status.PodStatus) ||
		len(current.Status.Conditions) != len(desired.Status.Conditions) ||
		conditionsModified(desired, current) {

		clog.Info("CR.status update", "Namespace", desired.Namespace, "Name", desired.Name, "Observed status", desired.Status)
		return resources.UpdateStatus(client, desired)
	}

	return nil
}

func conditionsModified(desired *brokerv1beta1.ActiveMQArtemis, current *brokerv1beta1.ActiveMQArtemis) bool {
	for _, c := range desired.Status.Conditions {
		if !common.IsConditionPresentAndEqual(current.Status.Conditions, c) {
			return true
		}
	}
	return false
}

func externalConfigsModified(desired *brokerv1beta1.ActiveMQArtemis, current *brokerv1beta1.ActiveMQArtemis) bool {
	if len(desired.Status.ExternalConfigs) >= 0 {
		for _, cfg := range desired.Status.ExternalConfigs {
			for _, curCfg := range current.Status.ExternalConfigs {
				if curCfg.Name == cfg.Name && curCfg.ResourceVersion != cfg.ResourceVersion {
					return true
				}
			}
		}
	}
	return false
}

// Controller Errors

type ArtemisError interface {
	Error() string
	Requeue() bool
}

type unknownJolokiaError struct {
	cause error
}
type jolokiaClientNotFoundError struct {
	cause error
}

type statusOutOfSyncError struct {
	cause string
}

type statusOutOfSyncMissingKeyError struct {
	cause string
}

func NewUnknownJolokiaError(err error) unknownJolokiaError {
	return unknownJolokiaError{
		err,
	}
}

func (e unknownJolokiaError) Error() string {
	return e.cause.Error()
}

func (e unknownJolokiaError) Requeue() bool {
	return false
}

func NewJolokiaClientsNotFoundError(err error) jolokiaClientNotFoundError {
	return jolokiaClientNotFoundError{
		err,
	}
}

func (e jolokiaClientNotFoundError) Error() string {
	return errors.Wrap(e.cause, "no available Jolokia Clients found").Error()
}

func (e jolokiaClientNotFoundError) Requeue() bool {
	return true
}

func NewStatusOutOfSyncErrorWith(brokerPropertiesName, expected, current string) statusOutOfSyncError {
	return statusOutOfSyncError{
		fmt.Sprintf("%s status out of sync, expected: %s, current: %s", brokerPropertiesName, expected, current),
	}
}

func NewStatusOutOfSyncError(err error) statusOutOfSyncError {
	return statusOutOfSyncError{err.Error()}
}

func (e statusOutOfSyncError) Error() string {
	return e.cause
}

func (e statusOutOfSyncError) Requeue() bool {
	return true
}

func NewStatusOutOfSyncMissingKeyError(err error) statusOutOfSyncMissingKeyError {
	return statusOutOfSyncMissingKeyError{err.Error()}
}

func (e statusOutOfSyncMissingKeyError) Error() string {
	return e.cause
}

func (e statusOutOfSyncMissingKeyError) Requeue() bool {
	return true
}

type inSyncApplyError struct {
	cause  error
	detail map[string]string
}

const inSyncWithErrorCause = "some properties resulted in error on ordinal %s"

func NewInSyncWithError(ordinal string) *inSyncApplyError {
	return &inSyncApplyError{
		cause:  errors.Errorf(inSyncWithErrorCause, ordinal),
		detail: map[string]string{},
	}
}

func (e inSyncApplyError) Requeue() bool {
	return false
}

func (e inSyncApplyError) Error() string {
	return fmt.Sprintf("%s : reasons: %v", e.cause.Error(), e.detail)
}

func (e *inSyncApplyError) ErrorApplyDetail(container string, reason string) {
	existing, present := e.detail[container]
	if present {
		e.detail[container] = fmt.Sprintf("%s, %s", existing, reason)
	} else {
		e.detail[container] = reason
	}
}
