package jolokia_client_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	ss "github.com/artemiscloud/activemq-artemis-operator/pkg/resources/statefulsets"
	jolokia_client "github.com/artemiscloud/activemq-artemis-operator/pkg/utils/jolokia"
)

var _ = Describe("JolokiaClient", func() {

	Describe("GetBrokers", func() {
		Context("without any target", func() {
			It("should not return any brokers", func() {
				objs := []client.Object{}
				client := fake.NewClientBuilder().WithObjects(objs...).Build()
				brokerRef := types.NamespacedName{
					Name:      "broker",
					Namespace: "some-ns",
				}
				ssInfos := ss.GetDeployedStatefulSetNames(client, []types.NamespacedName{brokerRef})
				infos := jolokia_client.GetBrokers(brokerRef, ssInfos, client)
				Expect(infos).Should(BeEmpty())
			})
		})
		Context("without a valid deployment", func() {
			It("should return an empty array if no StatefulSets exist with the given name", func() {
				replicas := int32(2)
				objs := []client.Object{
					&appsv1.StatefulSet{
						ObjectMeta: v1.ObjectMeta{
							Name:      "other-broker",
							Namespace: "some-ns",
						},
						Spec: appsv1.StatefulSetSpec{
							Replicas: &replicas,
						},
					},
				}
				client := fake.NewClientBuilder().WithObjects(objs...).Build()
				brokerRef := types.NamespacedName{
					Name:      "broker",
					Namespace: "some-ns",
				}
				ssInfos := ss.GetDeployedStatefulSetNames(client, []types.NamespacedName{brokerRef})
				infos := jolokia_client.GetBrokers(brokerRef, ssInfos, client)
				Expect(infos).Should(BeEmpty())
			})
			It("should return an empty array if no Pods are available", func() {
				replicas := int32(2)
				objs := []client.Object{
					&appsv1.StatefulSet{
						ObjectMeta: v1.ObjectMeta{
							Name:      "broker",
							Namespace: "some-ns",
							Labels: map[string]string{
								"label1": "value1",
							},
						},
						Spec: appsv1.StatefulSetSpec{
							Replicas: &replicas,
						},
					},
					&corev1.Pod{
						ObjectMeta: v1.ObjectMeta{
							Name:      "other-broker-0",
							Namespace: "some-ns",
						},
					},
				}
				client := fake.NewClientBuilder().WithObjects(objs...).Build()
				brokerRef := types.NamespacedName{
					Name:      "broker",
					Namespace: "some-ns",
				}
				ssInfos := ss.GetDeployedStatefulSetNames(client, []types.NamespacedName{brokerRef})
				infos := jolokia_client.GetBrokers(brokerRef, ssInfos, client)
				Expect(infos).Should(BeEmpty())

			})
		})
		Context("with a valid target", func() {
			It("should return a Jolokia client", func() {
				replicas := int32(2)
				objs := []client.Object{
					&appsv1.StatefulSet{
						ObjectMeta: v1.ObjectMeta{
							Name:      "broker",
							Namespace: "some-ns",
							Labels: map[string]string{
								"label1": "value1",
							},
						},
						Spec: appsv1.StatefulSetSpec{
							Replicas: &replicas,
						},
					},
					&corev1.Pod{
						ObjectMeta: v1.ObjectMeta{
							Name:      "broker-0",
							Namespace: "some-ns",
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{},
							},
						},
						Status: corev1.PodStatus{
							PodIP: "1.2.3.4",
						},
					},
				}
				client := fake.NewClientBuilder().WithObjects(objs...).Build()
				brokerRef := types.NamespacedName{
					Name:      "broker",
					Namespace: "some-ns",
				}
				ssInfos := ss.GetDeployedStatefulSetNames(client, []types.NamespacedName{brokerRef})
				infos := jolokia_client.GetBrokers(brokerRef, ssInfos, client)
				Expect(infos).Should(HaveLen(1))
				Expect(infos[0].IP).To(Equal("1.2.3.4"))
				Expect(infos[0].Artemis).NotTo(BeNil())
			})
		})
	})

})
