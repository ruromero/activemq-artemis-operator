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

	brokerv1beta1 "github.com/artemiscloud/activemq-artemis-operator/api/v1beta1"
	ss "github.com/artemiscloud/activemq-artemis-operator/pkg/resources/statefulsets"
	mgmt "github.com/artemiscloud/activemq-artemis-operator/pkg/utils/artemis"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/utils/channels"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/utils/common"
	jc "github.com/artemiscloud/activemq-artemis-operator/pkg/utils/jolokia_client"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/utils/lsrcrs"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/utils/namer"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/utils/selectors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var glog = ctrl.Log.WithName("controller_v1beta1activemqartemisaddress")

type AddressDeployment struct {
	AddressResource brokerv1beta1.ActiveMQArtemisAddress
	//a 0-len array means all statefulsets
	SsTargetNameBuilders []SSInfoData
}

var namespacedNameToAddressName = make(map[types.NamespacedName]AddressDeployment)

// ActiveMQArtemisAddressReconciler reconciles a ActiveMQArtemisAddress object
type ActiveMQArtemisAddressReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=broker.amq.io,namespace=activemq-artemis-operator,resources=activemqartemisaddresses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=broker.amq.io,namespace=activemq-artemis-operator,resources=activemqartemisaddresses/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=broker.amq.io,namespace=activemq-artemis-operator,resources=activemqartemisaddresses/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ActiveMQArtemisAddress object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *ActiveMQArtemisAddressReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	reqLogger := log.FromContext(ctx).WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name, "Reconciling", "ActiveMQArtemisAddress")

	addressInstance, lookupSucceeded := namespacedNameToAddressName[request.NamespacedName]
	// Fetch the ActiveMQArtemisAddress instance
	instance := &brokerv1beta1.ActiveMQArtemisAddress{}
	err := r.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Delete action
			if lookupSucceeded {
				if addressInstance.AddressResource.Spec.RemoveFromBrokerOnDelete {
					err = deleteQueue(&addressInstance, request, r.Client, r.Scheme)
					return ctrl.Result{}, err
				} else {
					reqLogger.Info("Not to delete address", "address", addressInstance)
				}
				delete(namespacedNameToAddressName, request.NamespacedName)
				lsrcrs.DeleteLastSuccessfulReconciledCR(request.NamespacedName, "address", getAddressLabels(&addressInstance.AddressResource), r.Client)
			}
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return ctrl.Result{RequeueAfter: common.GetReconcileResyncPeriod()}, nil
		}
		reqLogger.Error(err, "Requeue the request for error")
		return ctrl.Result{}, err
	}

	addressDeployment := AddressDeployment{
		AddressResource:      *instance,
		SsTargetNameBuilders: createNameBuilders(instance),
	}

	if !lookupSucceeded {
		//check stored cr
		if existingCr := lsrcrs.RetrieveLastSuccessfulReconciledCR(request.NamespacedName, "address", r.Client, getAddressLabels(instance)); existingCr != nil {
			//compare resource version
			if existingCr.Checksum == instance.ResourceVersion {
				reqLogger.V(1).Info("The incoming address CR is identical to stored CR, don't do reconcile")
				//the namespacedNameToAddressName is empty after a restart
				namespacedNameToAddressName[request.NamespacedName] = addressDeployment
				return ctrl.Result{RequeueAfter: common.GetReconcileResyncPeriod()}, nil
			}
		}
	}

	err = createQueue(&addressDeployment, request, r.Client, r.Scheme)
	if nil == err {
		namespacedNameToAddressName[request.NamespacedName] = addressDeployment
		crstr, merr := common.ToJson(instance)
		if merr != nil {
			reqLogger.Error(merr, "failed to marshal cr")
		}
		lsrcrs.StoreLastSuccessfulReconciledCR(instance, instance.Name, instance.Namespace, "address", crstr, "", instance.ResourceVersion, getAddressLabels(instance), r.Client, r.Scheme)
	} else {
		reqLogger.Error(err, "failed to create address resource, request will be requeued")
	}
	if err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: common.GetReconcileResyncPeriod()}, nil
}

func getAddressLabels(cr *brokerv1beta1.ActiveMQArtemisAddress) map[string]string {
	labelBuilder := selectors.LabelerData{}
	labelBuilder.Base(cr.Name).Suffix("addr").Generate()
	return labelBuilder.Labels()
}

type SSInfoData struct {
	NameBuilder namer.NamerData
	Labels      map[string]string
}

func createStatefulSetNameBuilder(crName string) SSInfoData {
	ssNameBuilder := namer.NamerData{}
	ssNameBuilder.Base(crName).Suffix("ss").Generate()
	ssLabelData := selectors.LabelerData{}
	ssLabelData.Base(crName).Suffix("app").Generate()

	return SSInfoData{
		NameBuilder: ssNameBuilder,
		Labels:      ssLabelData.Labels(),
	}
}

func createNameBuilders(instance *brokerv1beta1.ActiveMQArtemisAddress) []SSInfoData {
	var nameBuilders []SSInfoData = nil
	for _, crName := range instance.Spec.ApplyToCrNames {
		if crName != "*" {
			builder := createStatefulSetNameBuilder(crName)
			glog.Info("created a new name builder", "builder", builder, "buldername", builder.NameBuilder.Name())
			nameBuilders = append(nameBuilders, builder)
			glog.Info("added one builder for "+crName, "builders", nameBuilders, "len", len(nameBuilders))
		} else {
			return nil
		}
	}
	glog.Info("Created ss name builder for addr", "instance", instance, "builders", nameBuilders)
	return nameBuilders
}

// SetupWithManager sets up the controller with the Manager.
func (r *ActiveMQArtemisAddressReconciler) SetupWithManager(mgr ctrl.Manager, ctx context.Context) error {
	go setupAddressObserver(mgr, channels.AddressListeningCh, ctx)
	return ctrl.NewControllerManagedBy(mgr).
		For(&brokerv1beta1.ActiveMQArtemisAddress{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}

// This method deals with creating queues and addresses.
func createQueue(instance *AddressDeployment, request ctrl.Request, client client.Client, scheme *runtime.Scheme) error {

	reqLogger := ctrl.Log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Creating ActiveMQArtemisAddress")

	var err error = nil
	artemisArray := getPodBrokers(instance, request, client, scheme)
	if nil != artemisArray {
		for _, a := range artemisArray {
			if nil == a {
				reqLogger.Info("Creating ActiveMQArtemisAddress artemisArray had a nil!")
				continue
			}
			err = createAddressResource(a, &instance.AddressResource)
			if err != nil {
				reqLogger.V(1).Info("Failed to create address resource", "failed broker", a)
				continue
			}
		}
	}

	if err == nil {
		reqLogger.V(1).Info("Successfully created resources on all brokers", "size", len(artemisArray))
	}

	return err
}

func createAddressResource(a *jc.JkInfo, addressRes *brokerv1beta1.ActiveMQArtemisAddress) error {
	//Now checking if create queue or address
	if addressRes.Spec.QueueName == nil || *addressRes.Spec.QueueName == "" {
		//create address
		response, err := a.Artemis.CreateAddress(addressRes.Spec.AddressName, *addressRes.Spec.RoutingType)
		if nil != err {
			if mgmt.GetCreationError(response) == mgmt.ADDRESS_ALREADY_EXISTS {
				glog.Info("Address already exists, no retry", "address", addressRes.Spec.AddressName)
				return nil
			} else {
				glog.Error(err, "Error creating ActiveMQArtemisAddress", "address", addressRes.Spec.AddressName)
				return err
			}
		} else {
			glog.Info("Created ActiveMQArtemisAddress for address " + addressRes.Spec.AddressName)
		}
	} else {
		glog.Info("Queue name is not empty so create queue", "name", *addressRes.Spec.QueueName, "broker", a.IP)
		//first make sure address exists
		response, err := a.Artemis.CreateAddress(addressRes.Spec.AddressName, *addressRes.Spec.RoutingType)
		if nil != err && mgmt.GetCreationError(response) != mgmt.ADDRESS_ALREADY_EXISTS {
			glog.Error(err, "Error creating ActiveMQArtemisAddress", "address", addressRes.Spec.AddressName)
			return err
		}

		defaultConfigurationManaged := true
		if addressRes.Spec.QueueConfiguration == nil {
			routingType := "MULTICAST"
			if addressRes.Spec.RoutingType != nil {
				routingType = *addressRes.Spec.RoutingType
			}

			addressRes.Spec.QueueConfiguration = &brokerv1beta1.QueueConfigurationType{
				RoutingType:          &routingType,
				ConfigurationManaged: &defaultConfigurationManaged,
			}
		} else if addressRes.Spec.QueueConfiguration.ConfigurationManaged == nil {
			addressRes.Spec.QueueConfiguration.ConfigurationManaged = &defaultConfigurationManaged
		}
		//create queue using queueconfig
		queueCfg, ignoreIfExists, err := GetQueueConfig(addressRes)
		if err != nil {
			glog.Error(err, "Failed to get queue config json string")
			//here we return nil as no point to requeue reconcile again
			return nil
		}
		respData, err := a.Artemis.CreateQueueFromConfig(queueCfg, ignoreIfExists)
		if nil != err {
			if mgmt.GetCreationError(respData) == mgmt.QUEUE_ALREADY_EXISTS {
				glog.Info("The queue already exists, updating", "queue", queueCfg)
				respData, err := a.Artemis.UpdateQueue(queueCfg)
				if err != nil {
					glog.Error(err, "Failed to update queue", "details", respData)
				}
				return err
			}
			glog.Error(err, "Creating ActiveMQArtemisAddress error for "+*addressRes.Spec.QueueName)
			return err
		} else {
			glog.Info("Created ActiveMQArtemisAddress for " + *addressRes.Spec.QueueName)
		}

	}
	return nil
}

type AddressRetry struct {
	address string
	artemis []*mgmt.Artemis
}

func (ar *AddressRetry) addToDelete(a *mgmt.Artemis) {
	ar.artemis = append(ar.artemis, a)
}

func (ar *AddressRetry) safeDelete() {
	for _, a := range ar.artemis {
		glog.Info("Checking parent address for bindings " + ar.address)
		bindingsData, err := a.ListBindingsForAddress(ar.address)
		if nil == err {
			if bindingsData.Value == "" {
				glog.Info("No bindings found, removing " + ar.address)
				a.DeleteAddress(ar.address)
			} else {
				glog.Info("Bindings found, not removing", "address", ar.address, "bindings", bindingsData.Value)
			}
		} else {
			glog.Error(err, "failed to list bindings", "address", ar.address)
		}
	}
}

// This method deals with deleting queues and addresses.
func deleteQueue(instance *AddressDeployment, request ctrl.Request, client client.Client, scheme *runtime.Scheme) error {

	reqLogger := ctrl.Log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)

	addressName := instance.AddressResource.Spec.AddressName

	queueName := ""
	if instance.AddressResource.Spec.QueueName != nil {
		queueName = *instance.AddressResource.Spec.QueueName
	}

	reqLogger.Info("Deleting ActiveMQArtemisAddress for queue " + addressName + "/" + queueName)

	var err error = nil
	artemisArray := getPodBrokers(instance, request, client, scheme)
	if nil != artemisArray {
		addressRetry := &AddressRetry{
			address: addressName,
			artemis: make([]*mgmt.Artemis, 0),
		}
		for _, a := range artemisArray {
			if queueName == "" {
				//delete address
				_, err = a.Artemis.DeleteAddress(addressName)
				if nil != err {
					reqLogger.Error(err, "Deleting ActiveMQArtemisAddress error", "address", addressName)
					break
				}
				reqLogger.Info("Deleted ActiveMQArtemisAddress for address " + addressName)
			} else {
				//delete queues
				_, err = a.Artemis.DeleteQueue(queueName)
				if nil != err {
					reqLogger.Error(err, "Deleting ActiveMQArtemisAddress error for queue "+queueName)
					break
				} else {
					addressRetry.addToDelete(a.Artemis)
				}
			}
		}
		// we delete address after all queues are deleted
		addressRetry.safeDelete()
		reqLogger.Info("Deleted ActiveMQArtemisAddress for queue " + addressName + "/" + queueName)
	}

	return err
}

func getPodBrokers(instance *AddressDeployment, request ctrl.Request, client client.Client, scheme *runtime.Scheme) []*jc.JkInfo {
	reqLogger := ctrl.Log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Getting Pod Brokers", "instance", instance)
	targetCrNamespacedNames := createTargetCrNamespacedNames(request.Namespace, instance.AddressResource.Spec.ApplyToCrNames)
	reqLogger.Info("target Cr names", "result", targetCrNamespacedNames)
	ssInfos := ss.GetDeployedStatefulSetNames(client, targetCrNamespacedNames)

	return jc.GetBrokers(request.NamespacedName, ssInfos, client)
}

func createTargetCrNamespacedNames(namespace string, targetCrNames []string) []types.NamespacedName {
	var result []types.NamespacedName = nil
	for _, crName := range targetCrNames {
		result = append(result, types.NamespacedName{
			Namespace: namespace,
			Name:      crName,
		})
		if crName == "" || crName == "*" {
			glog.Info("Found empty or * in target crName, return nil for all")
			return nil
		}
	}
	return result
}

func GetStatefulSetNameForPod(client client.Client, pod *types.NamespacedName) (string, int, map[string]string) {
	glog.Info("Trying to find SS name for pod", "pod name", pod.Name, "pod ns", pod.Namespace)
	for crName, addressDeployment := range namespacedNameToAddressName {
		glog.Info("checking address cr in stock", "cr", crName)
		if crName.Namespace != pod.Namespace {
			glog.Info("this cr doesn't match pod's namespace", "cr's ns", crName.Namespace)
			continue
		}
		if len(addressDeployment.SsTargetNameBuilders) == 0 {
			glog.Info("this cr doesn't have target specified, it will be applied to all")
			//deploy to all sts, need get from broker controller
			ssInfos := ss.GetDeployedStatefulSetNames(client, nil)
			if len(ssInfos) == 0 {
				glog.Info("No statefulset found")
				continue
			}
			for _, info := range ssInfos {
				glog.Info("checking if this ss belong", "ss", info.NamespacedName.Name)
				if _, ok, podSerial := namer.PodBelongsToStatefulset(pod, &info.NamespacedName); ok {
					glog.Info("got a match", "ss", info.NamespacedName.Name, "podSerial", podSerial)
					return info.NamespacedName.Name, podSerial, info.Labels
				}
			}
			glog.Info("no match at all")
			continue
		}
		//iterate and check the ss name
		glog.Info("Now processing cr with applyToCrNames")
		for _, ssNameBuilder := range addressDeployment.SsTargetNameBuilders {
			ssName := ssNameBuilder.NameBuilder.Name()
			glog.Info("checking one applyTo", "ss", ssName)
			//at this point the ss name space is sure the same
			ssNameSpace := types.NamespacedName{
				Name:      ssName,
				Namespace: pod.Namespace,
			}
			if _, ok, podSerial := namer.PodBelongsToStatefulset(pod, &ssNameSpace); ok {
				glog.Info("yes this ssName match, returning results", "ssName", ssName, "podSerial", podSerial)
				return ssName, podSerial, ssNameBuilder.Labels
			}
		}
	}
	glog.Info("all through, but none")
	return "", -1, nil
}

func setupAddressObserver(mgr manager.Manager, c chan types.NamespacedName, ctx context.Context) {
	glog.Info("Setting up address observer")

	kubeClient, err := kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		glog.Error(err, "Error building kubernetes clientset")
	}

	observer := NewAddressObserver(kubeClient, mgr.GetClient(), mgr.GetScheme())

	if err = observer.Run(channels.AddressListeningCh, ctx); err != nil {
		glog.Error(err, "Error running controller")
	}

	glog.Info("Finish address observer")
}
