package main_test

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/meln5674/gosh"
	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NodePort LoadBalancer", func() {
	It("should set the service ingress to a reachable address and port", func(ctx context.Context) {
		var svc corev1.Service
		Expect(
			gk8s.
				Kubectl(ctx, &cluster, "get", "service", "nginx", "-o", "json").
				WithStreams(gosh.FuncOut(gosh.SaveJSON(&svc))).
				Run(),
		).To(Succeed())

		ingresses := svc.Status.LoadBalancer.Ingress
		Expect(ingresses).To(HaveLen(1))
		ingress := ingresses[0]
		Expect(ingress.IP).ToNot(BeEmpty())
		ports := ingress.Ports
		Expect(ports).To(HaveLen(1))
		port := ports[0]
		Expect(port.Error).To(BeNil())
		Expect(port.Port).ToNot(BeZero())

		var resp *http.Response
		var err error
		Eventually(func() error {
			resp, err = http.Get(fmt.Sprintf("http://%s:%d", ingress.IP, port.Port))
			return err
		}, "5s", "500ms").Should(Succeed())
		defer resp.Body.Close()
		_, err = io.Copy(GinkgoWriter, resp.Body)
		Expect(err).To(Succeed())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	})
})
