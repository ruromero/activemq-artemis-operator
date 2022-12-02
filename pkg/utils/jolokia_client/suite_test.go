package jolokia_client_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestJolokia(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Jolokia Suite")
}
