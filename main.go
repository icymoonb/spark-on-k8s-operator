/*
Copyright 2017 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

//go:generate hack/update-codegen.sh

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/golang/glog"
	apiv1 "k8s.io/api/core/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"

	"github.com/GoogleCloudPlatform/spark-on-k8s-operator/pkg/batchscheduler"
	"github.com/GoogleCloudPlatform/spark-on-k8s-operator/pkg/batchscheduler/interface"
	crclientset "github.com/GoogleCloudPlatform/spark-on-k8s-operator/pkg/client/clientset/versioned"
	crinformers "github.com/GoogleCloudPlatform/spark-on-k8s-operator/pkg/client/informers/externalversions"
	operatorConfig "github.com/GoogleCloudPlatform/spark-on-k8s-operator/pkg/config"
	"github.com/GoogleCloudPlatform/spark-on-k8s-operator/pkg/controller/scheduledsparkapplication"
	"github.com/GoogleCloudPlatform/spark-on-k8s-operator/pkg/controller/sparkapplication"
	"github.com/GoogleCloudPlatform/spark-on-k8s-operator/pkg/crd"
	"github.com/GoogleCloudPlatform/spark-on-k8s-operator/pkg/util"
	"github.com/GoogleCloudPlatform/spark-on-k8s-operator/pkg/webhook"
)

var (
	master                      = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	kubeConfig                  = flag.String("kubeConfig", "", "Path to a kube config. Only required if out-of-cluster.")
	installCRDs                 = flag.Bool("install-crds", true, "Whether to install CRDs")
	controllerThreads           = flag.Int("controller-threads", 10, "Number of worker threads used by the SparkApplication controller.")
	resyncInterval              = flag.Int("resync-interval", 30, "Informer resync interval in seconds.")
	namespace                   = flag.String("namespace", apiv1.NamespaceAll, "The Kubernetes namespace to manage. Will manage custom resource objects of the managed CRD types for the whole cluster if unset.")
	enableWebhook               = flag.Bool("enable-webhook", false, "Whether to enable the mutating admission webhook for admitting and patching Spark pods.")
	enableMetrics               = flag.Bool("enable-metrics", false, "Whether to enable the metrics endpoint.")
	metricsPort                 = flag.String("metrics-port", "10254", "Port for the metrics endpoint.")
	metricsEndpoint             = flag.String("metrics-endpoint", "/metrics", "Metrics endpoint.")
	metricsPrefix               = flag.String("metrics-prefix", "", "Prefix for the metrics.")
	ingressURLFormat            = flag.String("ingress-url-format", "", "Ingress URL format.")
	enableLeaderElection        = flag.Bool("leader-election", false, "Enable Spark operator leader election.")
	leaderElectionLockNamespace = flag.String("leader-election-lock-namespace", "spark-operator", "Namespace in which to create the ConfigMap for leader election.")
	leaderElectionLockName      = flag.String("leader-election-lock-name", "spark-operator-lock", "Name of the ConfigMap for leader election.")
	leaderElectionLeaseDuration = flag.Duration("leader-election-lease-duration", 15*time.Second, "Leader election lease duration.")
	leaderElectionRenewDeadline = flag.Duration("leader-election-renew-deadline", 14*time.Second, "Leader election renew deadline.")
	leaderElectionRetryPeriod   = flag.Duration("leader-election-retry-period", 4*time.Second, "Leader election retry period.")
	batchSchedulerName          = flag.String("batch-scheduler-name", "", "Use specified scheduler for pods' batch scheduling.")
)

func main() {
	var metricsLabels util.ArrayFlags
	flag.Var(&metricsLabels, "metrics-labels", "Labels for the metrics")
	flag.Parse()

	// Create the client config. Use kubeConfig if given, otherwise assume in-cluster.
	config, err := buildConfig(*master, *kubeConfig)
	if err != nil {
		glog.Fatal(err)
	}
	kubeClient, err := clientset.NewForConfig(config)
	if err != nil {
		glog.Fatal(err)
	}

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)

	stopCh := make(chan struct{}, 1)
	startCh := make(chan struct{}, 1)

	if *enableLeaderElection {
		hostname, err := os.Hostname()
		if err != nil {
			glog.Fatal(err)
		}
		resourceLock, err := resourcelock.New(resourcelock.ConfigMapsResourceLock, *leaderElectionLockNamespace, *leaderElectionLockName, kubeClient.CoreV1(), resourcelock.ResourceLockConfig{
			Identity: hostname,
			// TODO: This is a workaround for a nil dereference in client-go. This line can be removed when that dependency is updated.
			EventRecorder: &record.FakeRecorder{},
		})
		if err != nil {
			glog.Fatal(err)
		}

		electionCfg := leaderelection.LeaderElectionConfig{
			Lock:          resourceLock,
			LeaseDuration: *leaderElectionLeaseDuration,
			RenewDeadline: *leaderElectionRenewDeadline,
			RetryPeriod:   *leaderElectionRetryPeriod,
			Callbacks: leaderelection.LeaderCallbacks{
				OnStartedLeading: func(c context.Context) {
					close(startCh)
				},
				OnStoppedLeading: func() {
					close(stopCh)
				},
			},
		}

		elector, err := leaderelection.NewLeaderElector(electionCfg)
		if err != nil {
			glog.Fatal(err)
		}

		go elector.Run(context.Background())
	}

	glog.Info("Starting the Spark Operator")

	crClient, err := crclientset.NewForConfig(config)
	if err != nil {
		glog.Fatal(err)
	}
	apiExtensionsClient, err := apiextensionsclient.NewForConfig(config)
	if err != nil {
		glog.Fatal(err)
	}

	var batchScheduler schedulerinterface.BatchScheduler
	if *batchSchedulerName != "" {
		if !*enableWebhook {
			glog.Fatalf(
				"failed to initialize the batch scheduler %s as it requires the webhook to be enabled", *batchSchedulerName)
		}
		batchScheduler, err = batchscheduler.GetBatchScheduler(*batchSchedulerName, config)
		if err != nil {
			glog.Fatalf("failed to initialize batch scheduler %s.", err)
		}
	}

	if *installCRDs {
		err = crd.CreateOrUpdateCRDs(apiExtensionsClient)
		if err != nil {
			glog.Fatal(err)
		}
	}

	crInformerFactory := buildCustomResourceInformerFactory(crClient)
	podInformerFactory := buildPodInformerFactory(kubeClient)

	// Start the informer factory that in turn starts the informer.
	go crInformerFactory.Start(stopCh)
	go podInformerFactory.Start(stopCh)

	var hook *webhook.WebHook
	if *enableWebhook {
		var err error
		// Don't deregister webhook on exit if leader election enabled (i.e. multiple webhooks running)
		hook, err = webhook.New(kubeClient, crInformerFactory, *namespace, !*enableLeaderElection)
		if err != nil {
			glog.Fatal(err)
		}
		if err = hook.Start(); err != nil {
			glog.Fatal(err)
		}
	}

	if *enableLeaderElection {
		glog.Info("Waiting to be elected leader before starting application controller and metrics threads")
		<-startCh
	}

	var metricConfig *util.MetricConfig
	if *enableMetrics {
		metricConfig = &util.MetricConfig{
			MetricsEndpoint: *metricsEndpoint,
			MetricsPort:     *metricsPort,
			MetricsPrefix:   *metricsPrefix,
			MetricsLabels:   metricsLabels,
		}

		glog.Info("Enabling metrics collecting and exporting to Prometheus")
		util.InitializeMetrics(metricConfig)
	}

	applicationController := sparkapplication.NewController(
		crClient, kubeClient, crInformerFactory, podInformerFactory, metricConfig, *namespace, *ingressURLFormat, batchScheduler)
	scheduledApplicationController := scheduledsparkapplication.NewController(
		crClient, kubeClient, apiExtensionsClient, crInformerFactory, clock.RealClock{})

	glog.Info("Starting application controller threads")

	if err = applicationController.Start(*controllerThreads, stopCh); err != nil {
		glog.Fatal(err)
	}
	if err = scheduledApplicationController.Start(*controllerThreads, stopCh); err != nil {
		glog.Fatal(err)
	}

	select {
	case <-signalCh:
		close(stopCh)
	case <-stopCh:
	}

	glog.Info("Shutting down the Spark Operator")
	applicationController.Stop()
	scheduledApplicationController.Stop()
	if *enableWebhook {
		if err := hook.Stop(); err != nil {
			glog.Fatal(err)
		}
	}
}

func buildConfig(masterURL string, kubeConfig string) (*rest.Config, error) {
	if kubeConfig != "" {
		return clientcmd.BuildConfigFromFlags(masterURL, kubeConfig)
	}
	return rest.InClusterConfig()
}

func buildCustomResourceInformerFactory(crClient crclientset.Interface) crinformers.SharedInformerFactory {
	var factoryOpts []crinformers.SharedInformerOption
	if *namespace != apiv1.NamespaceAll {
		factoryOpts = append(factoryOpts, crinformers.WithNamespace(*namespace))
	}
	return crinformers.NewSharedInformerFactoryWithOptions(
		crClient,
		// resyncPeriod. Every resyncPeriod, all resources in the cache will re-trigger events.
		time.Duration(*resyncInterval)*time.Second,
		factoryOpts...)
}

func buildPodInformerFactory(kubeClient clientset.Interface) informers.SharedInformerFactory {
	var podFactoryOpts []informers.SharedInformerOption
	if *namespace != apiv1.NamespaceAll {
		podFactoryOpts = append(podFactoryOpts, informers.WithNamespace(*namespace))
	}
	tweakListOptionsFunc := func(options *metav1.ListOptions) {
		options.LabelSelector = fmt.Sprintf("%s,%s", operatorConfig.SparkRoleLabel, operatorConfig.LaunchedBySparkOperatorLabel)
	}
	podFactoryOpts = append(podFactoryOpts, informers.WithTweakListOptions(tweakListOptionsFunc))
	return informers.NewSharedInformerFactoryWithOptions(kubeClient, time.Duration(*resyncInterval)*time.Second, podFactoryOpts...)
}
