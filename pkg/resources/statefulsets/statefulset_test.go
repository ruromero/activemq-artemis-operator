package statefulsets_test

import (
	"github.com/artemiscloud/activemq-artemis-operator/pkg/resources/statefulsets"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("Statefulset", func() {

	Describe("GetDeployedStatefulSetNames", func() {
		Context("without existing stafulsets", func() {
			objs := []client.Object{&appsv1.StatefulSet{}}
			client := fake.NewClientBuilder().WithObjects(objs...).Build()
			It("returns an empty collection if the filter is not empty", func() {
				infos := statefulsets.GetDeployedStatefulSetNames(client, []types.NamespacedName{
					{
						Namespace: "foo",
						Name:      "bar",
					},
				})
				Expect(infos).Should(BeEmpty())
			})
		})
		Context("with deployed statefulsets", func() {
			objs := []client.Object{
				&appsv1.StatefulSet{
					ObjectMeta: v1.ObjectMeta{
						Name:      "sts-0",
						Namespace: "some-ns",
						Labels: map[string]string{
							"label1": "value1",
						},
					},
				},
				&appsv1.StatefulSet{
					ObjectMeta: v1.ObjectMeta{
						Name:      "sts-1",
						Namespace: "some-ns",
						Labels: map[string]string{
							"label1": "value1",
							"label2": "value2",
						},
					},
				},
				&appsv1.StatefulSet{
					ObjectMeta: v1.ObjectMeta{
						Name:      "sts-0",
						Namespace: "other-ns",
						Labels: map[string]string{
							"label2": "value2",
						},
					},
				},
			}
			client := fake.NewClientBuilder().WithObjects(objs...).Build()
			It("returns the same collection when the filter is empty", func() {
				infos := statefulsets.GetDeployedStatefulSetNames(client, []types.NamespacedName{})
				Expect(infos).Should(HaveLen(3))
				Expect(infos).Should(ContainElements(statefulsets.StatefulSetInfo{
					NamespacedName: types.NamespacedName{
						Name:      "sts-0",
						Namespace: "some-ns",
					},
					Labels: map[string]string{
						"label1": "value1",
					},
				}, statefulsets.StatefulSetInfo{
					NamespacedName: types.NamespacedName{
						Name:      "sts-1",
						Namespace: "some-ns",
					},
					Labels: map[string]string{
						"label1": "value1",
						"label2": "value2",
					},
				}, statefulsets.StatefulSetInfo{
					NamespacedName: types.NamespacedName{
						Name:      "sts-0",
						Namespace: "other-ns",
					},
					Labels: map[string]string{
						"label2": "value2",
					},
				}))
			})
			It("returns the statefulsets that match the namespace and name", func() {
				infos := statefulsets.GetDeployedStatefulSetNames(client, []types.NamespacedName{
					{
						Namespace: "some-ns",
						Name:      "sts-0",
					},
					{
						Namespace: "other-ns",
						Name:      "sts-0",
					},
				})
				Expect(infos).Should(HaveLen(2))
				Expect(infos).Should(ContainElements(statefulsets.StatefulSetInfo{
					NamespacedName: types.NamespacedName{
						Name:      "sts-0",
						Namespace: "some-ns",
					},
					Labels: map[string]string{
						"label1": "value1",
					},
				}, statefulsets.StatefulSetInfo{
					NamespacedName: types.NamespacedName{
						Name:      "sts-0",
						Namespace: "other-ns",
					},
					Labels: map[string]string{
						"label2": "value2",
					},
				}))
			})
			It("returns an empty collection if the filter doesn't match any Statefulset", func() {
				infos := statefulsets.GetDeployedStatefulSetNames(client, []types.NamespacedName{
					{
						Namespace: "foo",
						Name:      "bar",
					},
				})
				Expect(infos).Should(BeEmpty())
			})
		})
	})

})
