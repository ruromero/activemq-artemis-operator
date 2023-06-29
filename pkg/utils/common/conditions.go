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

package common

import (
	brokerv1beta1 "github.com/artemiscloud/activemq-artemis-operator/api/v1beta1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	DeployedConditionZeroSizeMessage       = "Pods not scheduled. Deployment size is 0"
	ConfigAppliedConditionOutOfSyncMessage = "Waiting for the Broker to acknowledge changes"
	NotReadyConditionMessage               = "Some conditions are not met"
	ImageVersionConflictMessage            = "Version and Images cannot be specified at the same time"
	ImageDependentPairMessage              = "Init image and broker image must both be configured as an interdependent pair"
	PDBNonNilSelectorMessage               = "PodDisruptionBudget's selector should not be specified"
)

func SetReadyCondition(conditions *[]metav1.Condition) {
	condition := newReadyCondition()
	ready := true
	for _, c := range *conditions {
		if c.Type != brokerv1beta1.ReadyConditionType && c.Status == metav1.ConditionFalse {
			ready = false
		}
	}
	if !ready {
		condition.Status = metav1.ConditionFalse
		condition.Reason = brokerv1beta1.NotReadyConditionReason
		condition.Message = NotReadyConditionMessage
	}
	meta.SetStatusCondition(conditions, condition)
}

func IsConditionPresentAndEqual(conditions []metav1.Condition, condition metav1.Condition) bool {
	for _, c := range conditions {
		if condition.Type == c.Type &&
			condition.Message == c.Message &&
			condition.Reason == c.Reason &&
			condition.Status == c.Status {
			return true
		}
	}
	return false
}

func newReadyCondition() metav1.Condition {
	return metav1.Condition{
		Type:   brokerv1beta1.ReadyConditionType,
		Reason: brokerv1beta1.ReadyConditionReason,
		Status: metav1.ConditionTrue,
	}
}
