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
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestConditions(t *testing.T) {
	RegisterFailHandler(Fail)
}

var _ = Describe("Common Conditions", func() {
	Describe("SetReadyCondition", func() {
		It("is Ready when there are no other conditions", func() {
			conditions := []metav1.Condition{}
			SetReadyCondition(&conditions)
			Expect(meta.IsStatusConditionTrue(conditions, ReadyConditionType)).To(BeTrue())
		})
		It("changes back to Ready when there are no other conditions and Ready was false", func() {
			conditions := []metav1.Condition{
				{
					Type:    ReadyConditionType,
					Status:  metav1.ConditionFalse,
					Reason:  "ReplaceMe",
					Message: "replace this message",
				},
			}
			SetReadyCondition(&conditions)

			Expect(meta.IsStatusConditionTrue(conditions, ReadyConditionType)).To(BeTrue())
			ready := meta.FindStatusCondition(conditions, ReadyConditionType)
			Expect(ready.Reason).To(Equal(ReadyConditionReason))
			Expect(ready.Message).To(BeEmpty())
		})
		It("is Ready when all conditions are True", func() {
			conditions := []metav1.Condition{
				{
					Type:   "FooCondition",
					Status: metav1.ConditionTrue,
					Reason: "FooIsOK",
				},
				{
					Type:   "BarCondition",
					Status: metav1.ConditionTrue,
					Reason: "BarIsOK",
				},
			}

			SetReadyCondition(&conditions)

			Expect(meta.IsStatusConditionTrue(conditions, ReadyConditionType)).To(BeTrue())
			Expect(conditions).To(HaveLen(3))
			ready := meta.FindStatusCondition(conditions, ReadyConditionType)
			Expect(ready.Reason).To(Equal(ReadyConditionReason))
			Expect(ready.Message).To(BeEmpty())
		})
		It("is not Ready when one condition is False", func() {
			conditions := []metav1.Condition{
				{
					Type:    "FooCondition",
					Status:  metav1.ConditionFalse,
					Reason:  "FooHasErrors",
					Message: "Test",
				},
				{
					Type:   "BarCondition",
					Status: metav1.ConditionTrue,
					Reason: "BarIsOK",
				},
				{
					Type:   "BazCondition",
					Status: metav1.ConditionTrue,
					Reason: "BazIsOK",
				},
			}

			SetReadyCondition(&conditions)

			Expect(meta.IsStatusConditionFalse(conditions, ReadyConditionType)).To(BeTrue())
			Expect(conditions).To(HaveLen(4))
			ready := meta.FindStatusCondition(conditions, ReadyConditionType)
			Expect(ready.Reason).To(Equal(NotReadyConditionReason))
			Expect(ready.Message).To(Equal(NotReadyConditionMessage))
		})
		It("changes to not Ready when one condition is False and was Ready", func() {
			conditions := []metav1.Condition{
				{
					Type:    "FooCondition",
					Status:  metav1.ConditionFalse,
					Reason:  "FooHasErrors",
					Message: "Test",
				},
				{
					Type:   "BarCondition",
					Status: metav1.ConditionTrue,
					Reason: "BarIsOK",
				},
				{
					Type:   "BazCondition",
					Status: metav1.ConditionTrue,
					Reason: "BazIsOK",
				},
				newReadyCondition(),
			}

			SetReadyCondition(&conditions)

			Expect(meta.IsStatusConditionFalse(conditions, ReadyConditionType)).To(BeTrue())
			Expect(conditions).To(HaveLen(4))
			ready := meta.FindStatusCondition(conditions, ReadyConditionType)
			Expect(ready.Reason).To(Equal(NotReadyConditionReason))
			Expect(ready.Message).To(Equal(NotReadyConditionMessage))
		})
		It("ignores condition in Unknown state", func() {
			conditions := []metav1.Condition{
				{
					Type:    "FooCondition",
					Status:  metav1.ConditionUnknown,
					Reason:  "Unknown condition",
					Message: "Test",
				},
				{
					Type:   "BarCondition",
					Status: metav1.ConditionTrue,
					Reason: "BarIsOK",
				},
				{
					Type:   "BazCondition",
					Status: metav1.ConditionTrue,
					Reason: "BazIsOK",
				},
			}

			SetReadyCondition(&conditions)

			Expect(meta.IsStatusConditionTrue(conditions, ReadyConditionType)).To(BeTrue())
			Expect(conditions).To(HaveLen(4))
		})
	})

	Describe("IsConditionPresentAndEquals", func() {
		It("returns false when there are no conditions to compare with", func() {
			Expect(IsConditionPresentAndEqual([]metav1.Condition{}, metav1.Condition{})).To(BeFalse())
		})
		It("returns false when one field is different", func() {
			current := metav1.Condition{
				Type:               "TestCondition",
				Status:             metav1.ConditionFalse,
				Reason:             "TestHasErrors",
				Message:            "Test",
				LastTransitionTime: metav1.Now(),
			}
			other := *current.DeepCopy()
			other.Message = "OtherMessage"
			conditions := []metav1.Condition{
				{
					Type:   "BarCondition",
					Status: metav1.ConditionTrue,
					Reason: "BarIsOK",
				},
				{
					Type:   "BazCondition",
					Status: metav1.ConditionTrue,
					Reason: "BazIsOK",
				},
				newReadyCondition(),
				current,
			}

			Expect(IsConditionPresentAndEqual(conditions, other)).To(BeFalse())
		})
		It("returns True when only time is different", func() {
			current := metav1.Condition{
				Type:               "TestCondition",
				Status:             metav1.ConditionFalse,
				Reason:             "TestHasErrors",
				Message:            "Test",
				LastTransitionTime: metav1.Now(),
			}
			other := *current.DeepCopy()
			other.LastTransitionTime = metav1.NewTime(current.LastTransitionTime.Add(5 * time.Hour))
			conditions := []metav1.Condition{
				current,
				{
					Type:   "BarCondition",
					Status: metav1.ConditionTrue,
					Reason: "BarIsOK",
				},
				{
					Type:   "BazCondition",
					Status: metav1.ConditionTrue,
					Reason: "BazIsOK",
				},
				newReadyCondition(),
			}

			Expect(IsConditionPresentAndEqual(conditions, other)).To(BeTrue())
		})
	})
})
