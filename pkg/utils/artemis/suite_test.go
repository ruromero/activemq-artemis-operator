package artemis

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestArtemis(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Artemis Suite")
}
