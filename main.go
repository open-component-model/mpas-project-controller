// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Open Component Model contributors.
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"os"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	gcv1alpha1 "github.com/open-component-model/git-controller/apis/mpas/v1alpha1"
	mpasv1alpha1 "github.com/open-component-model/mpas-project-controller/api/v1alpha1"
	"github.com/open-component-model/mpas-project-controller/controllers"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(mpasv1alpha1.AddToScheme(scheme))
	utilruntime.Must(sourcev1.AddToScheme(scheme))
	utilruntime.Must(kustomizev1.AddToScheme(scheme))
	utilruntime.Must(gcv1alpha1.AddToScheme(scheme))
	utilruntime.Must(certmanagerv1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var (
		metricsAddr           string
		enableLeaderElection  bool
		probeAddr             string
		clusterRoleName       string
		prefix                string
		defaultCommitName     string
		defaultCommitEmail    string
		defaultCommitMessage  string
		defaultNamespace      string
		registryAddress       string
		certificateIssuerName string
	)

	flag.StringVar(
		&certificateIssuerName,
		"certificate-issuer-name",
		"mpas-certificate-issuer",
		"The name of the ClusterIssuer to request certificates from. By default this is created by the MPAS Bootstrap command.",
	)
	flag.StringVar(
		&registryAddress,
		"registry-address",
		"registry.ocm-system.svc.cluster.local",
		"The address of the internal registry. This is used for the certificate DNS names.",
	)
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(
		&clusterRoleName,
		"cluster-role-name",
		"mpas-projects-clusterrole",
		"The name of the cluster role to use for project ServiceAccounts.",
	)
	flag.StringVar(
		&prefix,
		"prefix",
		"mpas",
		"The prefix to use for all resources and Git repositories created by the controller.",
	)
	flag.StringVar(
		&defaultCommitName,
		"default-commit-name",
		"MPAS System",
		"The name to use for automated commits if one is not provided in the Project spec.",
	)
	flag.StringVar(
		&defaultCommitEmail,
		"default-commit-email",
		"automated@ocm.software",
		"The email address to use for automated commits if one is not provided in the Project spec.",
	)
	flag.StringVar(
		&defaultCommitMessage,
		"default-commit-message",
		"Automated commit by MPAS Project Controller",
		"The commit message to use for automated commits if one is not provided in the Project spec.",
	)
	flag.StringVar(
		&defaultNamespace,
		"default-namespace",
		"mpas-system",
		"The namespace in which this controller is running in. This namespace is used to locate Project objects.",
	)

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	const metricsServerPort = 9443
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   metricsServerPort,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "bccfd20b.ocm.software",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controllers.ProjectReconciler{
		Client:          mgr.GetClient(),
		Scheme:          mgr.GetScheme(),
		ClusterRoleName: clusterRoleName,
		Prefix:          prefix,
		IssuerName:      certificateIssuerName,
		RegistryAddr:    registryAddress,
		DefaultCommitTemplate: mpasv1alpha1.CommitTemplate{
			Name:    defaultCommitName,
			Email:   defaultCommitEmail,
			Message: defaultCommitMessage,
		},
		DefaultNamespace: defaultNamespace,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Project")
		os.Exit(1)
	}

	if err = (&controllers.SecretsReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		DefaultNamespace: defaultNamespace,
		EventRecorder:    mgr.GetEventRecorderFor("secret-controller"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Secret")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
