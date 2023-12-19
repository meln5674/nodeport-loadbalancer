package main

import (
	"context"
	goflag "flag"
	"fmt"
	"os"
	"time"

	"github.com/pkg/errors"

	"github.com/meln5674/rflag"
	flag "github.com/spf13/pflag"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

type leaderElectionArgs struct {
	Enabled bool          `rflag:"usage=Enable leader election"`
	ID      string        `rflag:"usage=Identity for leader election"`
	Lease   time.Duration `rflag:"usage=Lease duration for leader election"`
	Renew   time.Duration `rflag:"usage=Renewal deadline for leader election"`
	Retry   time.Duration `rflag:"usage=Retry period for leader election"`
}

func (leaderElectionArgs) Defaults() leaderElectionArgs {
	return leaderElectionArgs{
		Lease: 15 * time.Second,
		Renew: 10 * time.Second,
		Retry: 2 * time.Second,
	}
}

type managerArgs struct {
	LeaderElection      leaderElectionArgs `rflag:"prefix=leader-election-"`
	MetricsAddr         string             `rflag:"name=metrics-bind-address,usage=The address the metric endpoint binds to."`
	ProbeAddr           string             `rflag:"name=health-probe-bind-address,usage=The address the probe endpoint binds to."`
	Kubeconfig          string             `rflag:"usage=Path to kubeconfig file"`
	kubeconfigOverrides clientcmd.ConfigOverrides
}

func (managerArgs) Defaults() managerArgs {
	return managerArgs{
		MetricsAddr: ":8080",
		ProbeAddr:   ":8081",
		Kubeconfig:  os.Getenv("KUBECONFIG"),
	}
}

type controllerArgs struct {
	Controller controllerConfig `rflag:""`
	Manager    managerArgs      `rflag:""`
	zap        zap.Options
}

func (controllerArgs) Defaults() controllerArgs {
	return controllerArgs{
		Controller: controllerConfig{}.Defaults(),
		Manager:    managerArgs{}.Defaults(),
	}
}

type controllerConfig struct {
	IncludeControlPlaneNodes bool `rflag:"usage=Include control plane nodes in the list of ingresses"`
	IncludeInternalIPs       bool `rflag:"name=include-internal-ips,usage=Include node internal IPs in the list of ingresses"`
	IncludeExternalIPs       bool `rflag:"name=include-external-ips,usage=Include node external IPs in the list of ingresses"`
	IncludeHostnames         bool `rflag:"usage=Include hostnames in the list of ingresses"`
}

func (controllerConfig) Defaults() controllerConfig {
	return controllerConfig{
		IncludeExternalIPs: true,
		IncludeHostnames:   true,
	}
}

type controller struct {
	k8s client.Client
	controllerConfig
}

func (c *controller) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	key := client.ObjectKey(req.NamespacedName)
	var svc corev1.Service
	err = c.k8s.Get(ctx, key, &svc)
	if kerrors.IsNotFound(err) {
		err = nil
		return
	}
	if err != nil {
		return
	}
	var nodes corev1.NodeList
	err = c.k8s.List(ctx, &nodes)
	if err != nil {
		return
	}
	ingressCount := 0
	for _, node := range nodes.Items {
		_, isControlPlane := node.Labels["node-role.kubernetes.io/control-plane"]
		_, isMaster := node.Labels["node-role.kubernetes.io/master"]
		_, isEtcd := node.Labels["node-role.kubernetes.io/etcd"]
		if !c.IncludeControlPlaneNodes && (isControlPlane || isMaster || isEtcd) {
			continue
		}
		for _, address := range node.Status.Addresses {
			switch address.Type {
			case corev1.NodeHostName:
				if c.IncludeHostnames {
					ingressCount++
				}
			case corev1.NodeExternalIP:
				if c.IncludeExternalIPs {
					ingressCount++
				}
			case corev1.NodeInternalIP:
				if c.IncludeInternalIPs {
					ingressCount++
				}
			}
		}
	}

	svc.Status.LoadBalancer.Ingress = make([]corev1.LoadBalancerIngress, 0, ingressCount)

	for _, node := range nodes.Items {
		_, isControlPlane := node.Labels["node-role.kubernetes.io/control-plane"]
		_, isMaster := node.Labels["node-role.kubernetes.io/master"]
		_, isEtcd := node.Labels["node-role.kubernetes.io/etcd"]
		if !c.IncludeControlPlaneNodes && (isControlPlane || isMaster || isEtcd) {
			continue
		}
		for _, address := range node.Status.Addresses {
			ports := make([]corev1.PortStatus, len(svc.Spec.Ports))
			for ix, port := range svc.Spec.Ports {
				if port.NodePort == 0 {
					errMsg := "NodePort has not been assigned"
					ports[ix] = corev1.PortStatus{
						Error: &errMsg,
					}
					continue
				}
				ports[ix] = corev1.PortStatus{
					Port:     port.NodePort,
					Protocol: port.Protocol,
				}
			}
			var hostname string
			var ip string
			switch address.Type {
			case corev1.NodeHostName:
				if !c.IncludeHostnames {
					continue
				}
				hostname = address.Address
			case corev1.NodeExternalIP:
				if !c.IncludeExternalIPs {
					continue
				}
				ip = address.Address
			case corev1.NodeInternalIP:
				if !c.IncludeInternalIPs {
					continue
				}
				ip = address.Address
			default:
				// TODO: Error?
				continue
			}
			svc.Status.LoadBalancer.Ingress = append(svc.Status.LoadBalancer.Ingress, corev1.LoadBalancerIngress{
				Hostname: hostname,
				IP:       ip,
				Ports:    ports,
			})
		}
	}

	err = c.k8s.Status().Update(ctx, &svc)
	if err != nil {
		return
	}
	return
}

func parseArgs() (controllerArgs, error) {
	args := controllerArgs{}.Defaults()

	rflag.MustRegister(rflag.ForPFlag(flag.CommandLine), "", &args)
	zapFlags := goflag.NewFlagSet("", goflag.PanicOnError)
	args.zap.BindFlags(zapFlags)
	flag.CommandLine.AddGoFlagSet(zapFlags)

	// If we don't do this, the short names overlap with the host k8s flags
	kubeconfigFlags := clientcmd.RecommendedConfigOverrideFlags("")
	kubeconfigFlagPtrs := []*clientcmd.FlagInfo{
		&kubeconfigFlags.AuthOverrideFlags.ClientCertificate,
		&kubeconfigFlags.AuthOverrideFlags.ClientKey,
		&kubeconfigFlags.AuthOverrideFlags.Token,
		&kubeconfigFlags.AuthOverrideFlags.Impersonate,
		&kubeconfigFlags.AuthOverrideFlags.ImpersonateUID,
		&kubeconfigFlags.AuthOverrideFlags.ImpersonateGroups,
		&kubeconfigFlags.AuthOverrideFlags.Username,
		&kubeconfigFlags.AuthOverrideFlags.Password,
		&kubeconfigFlags.ClusterOverrideFlags.APIServer,
		&kubeconfigFlags.ClusterOverrideFlags.APIVersion,
		&kubeconfigFlags.ClusterOverrideFlags.CertificateAuthority,
		&kubeconfigFlags.ClusterOverrideFlags.InsecureSkipTLSVerify,
		&kubeconfigFlags.ClusterOverrideFlags.TLSServerName,
		&kubeconfigFlags.ClusterOverrideFlags.ProxyURL,
		//&kubeconfigFlags.ClusterOverrideFlags.DisableCompression,
		&kubeconfigFlags.ContextOverrideFlags.ClusterName,
		&kubeconfigFlags.ContextOverrideFlags.AuthInfoName,
		&kubeconfigFlags.ContextOverrideFlags.Namespace,
		&kubeconfigFlags.CurrentContext,
		&kubeconfigFlags.Timeout,
	}

	for _, ptr := range kubeconfigFlagPtrs {
		ptr.ShortName = ""
	}

	clientcmd.BindOverrideFlags(&args.Manager.kubeconfigOverrides, flag.CommandLine, kubeconfigFlags)

	flag.Parse()
	return args, nil
}

func runController(args *controllerArgs) error {
	ctx := ctrl.SetupSignalHandler()

	scheme := runtime.NewScheme()

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	kubeconfigLoader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{
			ExplicitPath: args.Manager.Kubeconfig,
		},
		&args.Manager.kubeconfigOverrides,
	)

	kubeconfig, err := kubeconfigLoader.ClientConfig()
	if err != nil {
		return errors.Wrap(err, "Failed to load kubeconfig")
	}

	mgr, err := ctrl.NewManager(kubeconfig, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: args.Manager.MetricsAddr,
		},
		HealthProbeBindAddress: args.Manager.ProbeAddr,
		LeaderElection:         args.Manager.LeaderElection.Enabled,
		LeaseDuration:          &args.Manager.LeaderElection.Lease,
		RenewDeadline:          &args.Manager.LeaderElection.Renew,
		RetryPeriod:            &args.Manager.LeaderElection.Retry,
		LeaderElectionID:       args.Manager.LeaderElection.ID,
	})
	if err != nil {
		return errors.Wrap(err, "Failed to start manager")
	}

	controller := controller{
		controllerConfig: args.Controller,
		k8s:              mgr.GetClient(),
	}

	err = builder.
		ControllerManagedBy(mgr).
		For(&corev1.Service{}).
		Complete(&controller)
	if err != nil {
		return errors.Wrap(err, "Failed to build controller")
	}

	if err = mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return errors.Wrap(err, "Failed to setup liveness probe")
	}
	if err = mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return errors.Wrap(err, "Failed to setup readiness probe")
	}

	if err = mgr.Start(ctx); err != nil {
		return errors.Wrap(err, "Failed to start manager")
	}

	return nil
}

func main() {
	args, err := parseArgs()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&args.zap)))
	setupLog := ctrl.Log.WithName("setup")

	err = runController(&args)
	if err != nil {
		setupLog.Error(err, "Failed to run controller")
		return
	}
}
