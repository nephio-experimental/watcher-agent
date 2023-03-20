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
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	free5gctypes "github.com/nephio-project/common-lib/ausf"
	nfdeployv1alpha1 "github.com/nephio-project/common-lib/nfdeploy"
	edgewatcher "github.com/nephio-project/edge-watcher"
	"github.com/nephio-project/edge-watcher/preprocessor"
	"github.com/nephio-project/watcher-agent/api/v1alpha1"
	"github.com/nephio-project/watcher-agent/pkg/watcher"
	"github.com/nephio-project/watcher-agent/tests/integration/environment"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const bufSize = 1024 * 1024

var _ = Describe("Controller", func() {
	testEnv.InitOnRunningSuite()

	Describe("with manager", func() {

		var (
			ctx context.Context

			logger              logr.Logger
			clusterName         string
			nfCount, eventCount int
			eventStream         chan preprocessor.Event
			cases               *clusterTestCase
		)

		BeforeEach(func() {
			logger = zap.New(func(options *zap.Options) {
				options.Development = true
				options.DestWriter = GinkgoWriter
			})

			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(suiteCtx)

			clusterName = environment.RandomAlphabaticalString(10)
			Expect(clusterClient.CreateCluster(ctx, logger, clusterName, configControlNamespace.Name)).To(Succeed(),
				fmt.Sprintf("unable to create edgecluster CRD name: %v namespace: %v", clusterName, configControlNamespace.Name))

			nfCount = 10
			eventCount = 20

			logger.Info("sending subscription request", "clustername", clusterName)
			subscriptionReq, err := serverHandler.subscribe(ctx, edgewatcher.EventOptions{
				Type:             edgewatcher.ClusterSubscriber,
				SubscriptionName: clusterName,
			}, clusterName)
			Expect(err).To(BeNil(), fmt.Sprintf("error in subscribing to clusterEvents: %v", clusterName))
			eventStream = subscriptionReq.Channel

			cases = generateClusterCase(nfCount, eventCount)

			DeferCleanup(func() {
				logger.Info("cancelling context")
				cancel()
			})
		})

		Context("with correct watch request", func() {

			It("should create new watches and send events", func() {
				done := make(chan struct{})

				go func() {
					defer GinkgoRecover()
					defer close(done)

					var wg sync.WaitGroup
					wg.Add(2 * nfCount)

					for upfID, upfList := range cases.upfs {
						Expect(cases.upfAckStreams).To(HaveKey(upfID))
						go func(stream chan string, id types.NamespacedName, list []*nfdeployv1alpha1.UpfDeploy) {
							defer GinkgoRecover()
							defer wg.Done()
							for _, obj := range list {
								data, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
								Expect(err).To(BeNil())

								u := &unstructured.Unstructured{Object: data}
								u.SetGroupVersionKind(schema.GroupVersionKind{
									Group:   "nfdeploy.nephio.org",
									Version: "v1alpha1",
									Kind:    "UpfDeploy",
								})
								rv, err := clusterClient.Apply(ctx, logger, clusterName, u, nil)
								Expect(err).To(BeNil(), fmt.Sprintf("error in applying the object: %v, clusterName: %v", obj.GetName(), clusterName))
								Eventually(stream).Should(Receive(Equal(rv)), fmt.Sprintf("error in receiving resourceVersion on upfAckStream id: %v", id))
							}
						}(cases.upfAckStreams[upfID], upfID, upfList)
					}

					for ausfID, ausfList := range cases.ausfs {
						go func(stream chan string, id types.NamespacedName, list []*free5gctypes.AusfDeploy) {
							defer GinkgoRecover()
							defer wg.Done()
							for _, obj := range list {
								data, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
								Expect(err).To(BeNil())

								u := &unstructured.Unstructured{Object: data}
								u.SetGroupVersionKind(schema.GroupVersionKind{
									Group:   "nfdeploy.nephio.org",
									Version: "v1alpha1",
									Kind:    "AusfDeploy",
								})
								rv, err := clusterClient.Apply(ctx, logger, clusterName, u, nil)
								Expect(err).To(BeNil(), fmt.Sprintf("error in applying the object: %v, clusterName: %v", obj.GetName(), clusterName))
								Eventually(stream).Should(Receive(Equal(rv)), fmt.Sprintf("error in receiving resourceVersion on ausfAckStream id: %v", id.Name))
							}
						}(cases.ausfAckStreams[ausfID], ausfID, ausfList)
					}
					wg.Wait()
				}()

				upfItr, ausfItr := make(map[types.NamespacedName]int), make(map[types.NamespacedName]int)
			loop:
				for {
					logger.Info("waiting for events")
					select {
					case <-ctx.Done():
						break loop
					case <-done:
						break loop
					case event := <-eventStream:
						obj := event.Object.(*unstructured.Unstructured)
						if obj.GetName() == "" {
							logger.Info("empty list event", "type", event.Type, "key", event.Key, "gvr", event.Object.GetObjectKind().GroupVersionKind().String())
							continue loop
						}
						switch event.Key.Kind {
						case "UPFDeploy":
							var upfDeploy nfdeployv1alpha1.UpfDeploy
							err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &upfDeploy)
							Expect(err).To(BeNil(), "error in converting to upfdeploy")
							id := types.NamespacedName{
								Name: upfDeploy.Name,
							}
							Expect(cases.upfs).To(HaveKey(id))
							expectedUpfDeploy := cases.upfs[id][upfItr[id]]
							Expect(expectedUpfDeploy.Spec.N3Interfaces).To(Equal(upfDeploy.Spec.N3Interfaces))
							upfItr[id]++

							select {
							case <-ctx.Done():
							case cases.upfAckStreams[id] <- upfDeploy.ResourceVersion:
							}

						case "AUSFDeploy":
							var ausfDeploy free5gctypes.AusfDeploy
							err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &ausfDeploy)
							Expect(err).To(BeNil(), "error in converting to ausfdeploy")
							id := types.NamespacedName{
								Name: ausfDeploy.Name,
							}
							Expect(cases.ausfs).To(HaveKey(id))
							expectedAusfDeploy := cases.ausfs[id][ausfItr[id]]
							Expect(expectedAusfDeploy.Spec.NfInfo.Version).To(Equal(ausfDeploy.Spec.NfInfo.Version))
							ausfItr[id]++

							select {
							case <-ctx.Done():
							case cases.ausfAckStreams[id] <- ausfDeploy.ResourceVersion:
							}
						}
					}
				}
			})

			Context("changes to watcherAgent Spec", func() {
				var (
					watcherAgentCR v1alpha1.WatcherAgent
					key            types.NamespacedName
				)
				BeforeEach(func() {
					clusterNamespace, err := clusterClient.GetNamespace(clusterName)
					Expect(err).To(BeNil(),
						fmt.Sprintf("namespace not found for cluster: %v", clusterName))

					var watcherAgentCRList v1alpha1.WatcherAgentList
					err = testEnv.Client.List(ctx, &watcherAgentCRList, client.InNamespace(clusterNamespace))
					Expect(err).To(BeNil(),
						fmt.Sprintf("error in getting watcherAgent list"))

					Expect(len(watcherAgentCRList.Items)).To(Equal(1),
						fmt.Sprintf("incorrect number of watcherAgentCR: %v", len(watcherAgentCRList.Items)))

					watcherAgentCR = watcherAgentCRList.Items[0]

					key = types.NamespacedName{
						Namespace: watcherAgentCR.Namespace,
						Name:      watcherAgentCR.Name,
					}
				})
				Context("when watcherAgentCR is deleted", func() {
					It("should delete the watchers", func() {
						var wg sync.WaitGroup
						wg.Add(2)

						go func() {
							defer GinkgoRecover()
							defer wg.Done()
							Expect(testEnv.Client.Delete(ctx, &watcherAgentCR)).Should(BeNil())
						}()

						go func() {
							defer GinkgoRecover()
							defer wg.Done()
							Eventually(func() map[v1alpha1.WatchRequest]*watcher.Watcher {
								return controller.WatcherManager.ListWatches(key)
							}).
								Should(BeEmpty())
						}()

						wg.Wait()
					})
				})
				Context("when watchRequest is removed", func() {
					It("should delete the watcher", func() {
						watchRequest := watcherAgentCR.Spec.WatchRequests[0]

						var wg sync.WaitGroup
						wg.Add(2)

						go func() {
							defer GinkgoRecover()
							defer wg.Done()
							patch := client.MergeFrom(watcherAgentCR.DeepCopy())
							watcherAgentCR.Spec.WatchRequests = watcherAgentCR.Spec.WatchRequests[1:]
							Expect(testEnv.Client.Patch(ctx, &watcherAgentCR, patch)).Should(BeNil())
						}()
						go func() {
							defer GinkgoRecover()
							defer wg.Done()
							Eventually(func() map[v1alpha1.WatchRequest]*watcher.Watcher {
								return controller.WatcherManager.ListWatches(key)
							}).
								ShouldNot(HaveKey(watchRequest))
						}()

						wg.Wait()
					})
				})
				Context("when ResyncTimestamp is updated", func() {
					It("should restart the watches", func() {
						patch := client.MergeFrom(watcherAgentCR.DeepCopy())

						// Set new ResyncTimestamp
						currTime := time.Now()
						watcherAgentCR.Spec.ResyncTimestamp = metav1.NewTime(currTime)
						watcherAgentCR.Spec.EdgeWatcher = v1alpha1.EdgeWatcher{
							Addr: "newAddr",
							Port: "newPort",
						}
						Expect(testEnv.Client.Patch(ctx, &watcherAgentCR, patch)).Should(BeNil())

						// Check if controller sent the restart request to the watcher
						Eventually(func(g Gomega) time.Time {
							var object v1alpha1.WatcherAgent
							g.Expect(testEnv.Client.Get(ctx, key, &object)).To(BeNil())
							return object.Status.ResyncTimestamp.Time
						}).Should(BeTemporally("~", currTime, time.Second))

						// get around the watcher restart backoff to make the tests faster
						clock.SetTime(time.Now().Add(time.Minute))

						Eventually(func(g Gomega) {
							watchers := controller.WatcherManager.ListWatches(key)

							// check there aren't any stale watchers
							g.Expect(len(watchers)).To(Equal(len(watcherAgentCR.Spec.WatchRequests)))

							for _, req := range watcherAgentCR.Spec.WatchRequests {
								g.Expect(watchers).To(HaveKey(req))

								// check if watcher has restarted
								g.Expect(watchers[req].GetRestartCount()).To(Equal(1))

								// check if watcher's grpcClient is updated
								addr, port := watchers[req].GetGRPCClient().GetServerParams()
								g.Expect(addr).To(Equal(watcherAgentCR.Spec.EdgeWatcher.Addr))
								g.Expect(port).To(Equal(watcherAgentCR.Spec.EdgeWatcher.Port))
							}
						}, 2*time.Second)

					})
				})
			})

		})

	})
})
