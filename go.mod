module github.com/artemiscloud/activemq-artemis-operator

go 1.16

require (
	github.com/Azure/go-amqp v0.17.4
	github.com/Masterminds/semver v1.5.0
	//this module is problematic
	github.com/RHsyseng/operator-utils v1.4.10
	github.com/go-logr/logr v1.2.0
	github.com/golang/mock v1.6.0
	github.com/onsi/ginkgo/v2 v2.1.4
	github.com/onsi/gomega v1.19.0
	github.com/openshift/api v3.9.0+incompatible
	github.com/pkg/errors v0.9.1
	github.com/stretchr/testify v1.8.0
	go.uber.org/zap v1.19.1
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.23.0
	k8s.io/apimachinery v0.23.0
	k8s.io/client-go v0.23.0
	sigs.k8s.io/controller-runtime v0.11.1
)
