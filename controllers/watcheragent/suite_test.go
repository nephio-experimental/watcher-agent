/*
Copyright 2022-2023 The Nephio Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package watcheragent_test

import (
	"context"
	"net"
	"testing"
	"time"

	edgewatcher "github.com/nephio-project/edge-watcher"
	"github.com/nephio-project/watcher-agent/controllers/watcheragent"
	"github.com/nephio-project/watcher-agent/tests/integration/utils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/nephio-project/watcher-agent/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	k8sutils "k8s.io/utils/clock/testing"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Suite")
}

var (
	controller             *watcheragent.Reconciler
	clock                  *k8sutils.FakeClock
	serverHandler          *edgeWatcherHandler
	clusterClient          *utils.ClusterClient
	configControlNamespace *v1.Namespace
	suiteCtx               context.Context
	suitCtxCancel          context.CancelFunc
)

var _ = BeforeSuite(func() {
	var err error

	clock = k8sutils.NewFakeClock(time.Now())

	testEnv.Start()
	err = v1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	logger := zap.New(func(options *zap.Options) {
		options.Development = true
		options.DestWriter = GinkgoWriter
	})

	restMapper, err := apiutil.NewDiscoveryRESTMapper(testEnv.GetManager().GetConfig())
	Expect(err).To(BeNil())

	suiteCtx, suitCtxCancel = context.WithCancel(context.Background())

	clusterClient = utils.NewClusterClient(testEnv.GetManager().GetClient(), testEnv.ClientSet)

	configControlNamespace, err = clusterClient.CreateNamespace("config-control", nil)
	Expect(err).To(BeNil(), "unable to create config-control namespace")

	serverHandler = newEdgeWatcherHandler(suiteCtx, logger, edgewatcher.Params{
		K8sDynamicClient: testEnv.DynamicClient,
		PorchClient: &utils.FakePorchClient{
			Logger:        logger,
			ClusterClient: clusterClient,
		},
		PodIP:                "bufcon",
		Port:                 "bufcon",
		NephioNamespace:      "nephio-system",
		EdgeClusterNamespace: configControlNamespace.Name,
	})
	serverHandler.Start()

	controller = &watcheragent.Reconciler{
		Client: testEnv.GetManager().GetClient(),
		Scheme: testEnv.GetManager().GetScheme(),

		GRPCManager: watcheragent.NewGRPCManager(suiteCtx, logger,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
				return serverHandler.GetListener().Dial()
			})),
		WatcherManager: watcheragent.NewManager(suiteCtx, logger, testEnv.DynamicClient, restMapper, clock),
		WatchEvents:    make(chan event.GenericEvent),

		Log: logger.WithName("controllers").WithName("WatcherAgent"),
	}

	err = controller.SetupWithManager(testEnv.GetManager())
	Expect(err).To(BeNil())

	//+kubebuilder:scaffold:scheme

})

var _ = AfterSuite(func() {
	suitCtxCancel()
	By("tearing down the test environment")
	testEnv.TeardownCluster()
})
