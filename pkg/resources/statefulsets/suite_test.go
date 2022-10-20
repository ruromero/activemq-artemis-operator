package statefulsets_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestStatefulsets(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Statefulsets Suite")
}
