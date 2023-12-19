package main_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/meln5674/gingk8s"
)

func TestNodeportLoadbalancer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "NodeportLoadbalancer Suite")
}

var _ = BeforeSuite(func() {
	gk8s = gingk8s.ForSuite(GinkgoT())
	gk8s.Options(gk8sOpts)

	nodeportLoadbalancerImageID := gk8s.CustomImage(&nodeportLoadbalancerImage)
	nginxImageID := gk8s.ThirdPartyImage(&nginxImage)

	clusterID = gk8s.Cluster(&cluster, nodeportLoadbalancerImageID, nginxImageID)

	nodeportLoadbalancerID := gk8s.Release(clusterID, &nodeportLoadbalancer, nodeportLoadbalancerImageID)
	gk8s.Release(clusterID, &nginx, nginxImageID, nodeportLoadbalancerID)

	ctx, cancel := context.WithCancel(context.Background())
	DeferCleanup(cancel)
	gk8s.Setup(ctx)
})

var (
	localbin     = os.Getenv("LOCALBIN")
	localKubectl = gingk8s.KubectlCommand{
		Command: []string{filepath.Join(localbin, "kubectl")},
	}
	localHelm = gingk8s.HelmCommand{
		Command: []string{filepath.Join(localbin, "helm")},
	}
	localKind = gingk8s.KindCommand{
		Command: []string{filepath.Join(localbin, "kind")},
	}

	gk8s     gingk8s.Gingk8s
	gk8sOpts = gingk8s.SuiteOpts{
		KLogFlags:      []string{"-v=6"},
		Kubectl:        &localKubectl,
		Helm:           &localHelm,
		Manifests:      &localKubectl,
		NoSuiteCleanup: os.Getenv("NODEPORT_LOADBALANCER_IT_DEV_MODE") != "",
		NoCacheImages:  os.Getenv("IS_CI") != "",
		NoPull:         os.Getenv("IS_CI") != "",
		NoLoadPulled:   os.Getenv("IS_CI") != "",
	}

	cluster = gingk8s.KindCluster{
		Name:        "nodeport-loadbalancer-it",
		KindCommand: &localKind,
		TempDir:     "tmp",
	}
	clusterID gingk8s.ClusterID

	nodeportLoadbalancerImage = gingk8s.CustomImage{
		Registry:   "local.host",
		Repository: "meln5674/nodeport-loadbalancer",
	}
	nodeportLoadbalancer = gingk8s.HelmRelease{
		Name: "nodeport-loadbalancer",
		Chart: &gingk8s.HelmChart{
			LocalChartInfo: gingk8s.LocalChartInfo{
				Path: "deploy/helm/nodeport-loadbalancer",
			},
		},

		Set: gingk8s.Object{
			"image.registry":   nodeportLoadbalancerImage.Registry,
			"image.repository": nodeportLoadbalancerImage.Repository,
			"image.tag":        gingk8s.DefaultExtraCustomImageTags[0],

			"config.controller.include.hostnames":         false,
			"config.controller.include.internalIPs":       true,
			"config.controller.include.controlPlaneNodes": true,
		},
	}

	nginxImage = gingk8s.ThirdPartyImage{
		Name: "docker.io/bitnami/nginx:1.25.3",
	}
	nginx = gingk8s.HelmRelease{
		Name: "nginx",
		Chart: &gingk8s.HelmChart{
			OCIChartInfo: gingk8s.OCIChartInfo{
				Registry: gingk8s.HelmRegistry{
					Hostname: "registry-1.docker.io",
				},
				Repository: "bitnamicharts/nginx",
				Version:    "15.4.5",
			},
		},
	}
)
