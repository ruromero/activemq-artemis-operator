package pods

import (
	"github.com/artemiscloud/activemq-artemis-operator/pkg/utils/namer"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime"
)

var log = logf.Log.WithName("package pods")
var NameBuilder namer.NamerData

func MakePodTemplateSpec(current *corev1.PodTemplateSpec, namespacedName types.NamespacedName, labels map[string]string, annotations map[string]string) *corev1.PodTemplateSpec {

	var desired *corev1.PodTemplateSpec = current
	if desired == nil {
		desired = &corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Name:      namespacedName.Name,
				Namespace: namespacedName.Namespace,
			},
			Spec: corev1.PodSpec{},
		}
	}
	desired.ObjectMeta.Labels = labels
	desired.ObjectMeta.Annotations = annotations

	return desired
}
