/*
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
// +kubebuilder:docs-gen:collapse=Apache License

/*
As usual, we start with the necessary imports. We also define some utility variables.
*/
package controllers

import (
	"os"

	brokerv1beta1 "github.com/artemiscloud/activemq-artemis-operator/api/v1beta1"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/utils/common"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("artemis controller", Label("do"), func() {

	BeforeEach(func() {
		BeforeEachSpec()
	})

	Context("tls jolokia access", Label("do-secure-console-with-sni"), func() {
		It("check the util works in test env", func() {
			domainName := common.GetClusterDomain()
			Expect(domainName).To(Equal("cluster.local"))
		})
		It("get status from broker", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") == "true" && os.Getenv("DEPLOY_OPERATOR") == "true" {

				commonSecretName := "common-amq-tls-sni-secret"
				dnsNames := []string{"*.artemis-broker-hdls-svc.default.svc.cluster.local"}
				commonSecret, err := CreateTlsSecret(commonSecretName, defaultNamespace, defaultPassword, dnsNames)
				Expect(err).To(BeNil())

				Expect(k8sClient.Create(ctx, commonSecret)).Should(Succeed())

				createdSecret := corev1.Secret{}
				secretKey := types.NamespacedName{
					Name:      commonSecretName,
					Namespace: defaultNamespace,
				}

				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, secretKey, &createdSecret)).To(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				brokerName := "artemis-broker"
				By("Deploying the broker cr")
				brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {

					candidate.Name = brokerName
					candidate.Spec.DeploymentPlan.Size = common.Int32ToPtr(2)
					candidate.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
						InitialDelaySeconds: 1,
						PeriodSeconds:       1,
						TimeoutSeconds:      5,
					}
					candidate.Spec.Console.Expose = true
					candidate.Spec.Console.SSLEnabled = true
					candidate.Spec.Console.SSLSecret = commonSecretName
				})

				By("Check ready status")
				Eventually(func(g Gomega) {
					oprLog, rrr := GetOperatorLog(defaultNamespace)
					g.Expect(rrr).To(BeNil())
					getPersistedVersionedCrd(brokerCr.ObjectMeta.Name, defaultNamespace, createdBrokerCr)
					g.Expect(len(createdBrokerCr.Status.PodStatus.Ready)).Should(BeEquivalentTo(2))
					g.Expect(meta.IsStatusConditionTrue(createdBrokerCr.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)).Should(BeTrue(), *oprLog)
				}, existingClusterTimeout, interval).Should(Succeed())

				CleanResource(createdBrokerCr, createdBrokerCr.Name, defaultNamespace)
				CleanResource(commonSecret, commonSecret.Name, defaultNamespace)
			}
		})
	})

	Context("operator logging config test", Label("do-operator-log"), func() {
		It("test operator with env var", func() {
			if os.Getenv("DEPLOY_OPERATOR") == "true" {
				// re-install a new operator to have a fresh log
				uninstallOperator(false)
				installOperator(nil)
				By("checking default operator should have INFO logs")
				Eventually(func(g Gomega) {
					oprLog, err := GetOperatorLog(defaultNamespace)
					g.Expect(err).To(BeNil())
					g.Expect(*oprLog).To(ContainSubstring("INFO"))
					g.Expect(*oprLog).NotTo(ContainSubstring("DEBUG"))
					g.Expect(*oprLog).NotTo(ContainSubstring("ERROR"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Uninstall existing operator")
				uninstallOperator(false)

				By("install the operator again with logging env var")
				envMap := make(map[string]string)
				envMap["ARGS"] = "--zap-log-level=error"
				installOperator(envMap)
				By("delploy a basic broker to produce some more log")
				brokerCr, createdCr := DeployCustomBroker(defaultNamespace, nil)

				By("wait for pod so enough log is generated")
				Eventually(func(g Gomega) {
					getPersistedVersionedCrd(brokerCr.Name, defaultNamespace, createdCr)
					g.Expect(len(createdCr.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("check no INFO/DEBUG in the log")
				oprLog, err := GetOperatorLog(defaultNamespace)
				Expect(err).To(BeNil())
				Expect(*oprLog).NotTo(ContainSubstring("INFO"))
				Expect(*oprLog).NotTo(ContainSubstring("DEBUG"))

				//clean up all resources
				Expect(k8sClient.Delete(ctx, createdCr)).Should(Succeed())
			}
		})
	})
})
