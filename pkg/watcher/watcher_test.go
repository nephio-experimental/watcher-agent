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

package watcher_test

import (
	"context"
	"fmt"
	"sync"
	"time"

	pb "github.com/nephio-project/edge-watcher/protos"
	"github.com/nephio-project/watcher-agent/api/v1alpha1"
	"github.com/nephio-project/watcher-agent/pkg/watcher"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clock "k8s.io/utils/clock/testing"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var _ = Describe("Watcher", func() {
	var cancel context.CancelFunc
	var ctx context.Context
	var test *watchTest

	var objects []*corev1.ConfigMap
	var resourceVersionStream <-chan string
	BeforeEach(func() {
		// create new logger
		logger := zap.New(func(options *zap.Options) {
			options.Development = true
			options.DestWriter = GinkgoWriter
		})

		// start new context
		ctx, cancel = context.WithCancel(context.Background())
		controller := &fakeController{
			ctx:    ctx,
			logger: logger.WithName("fakeController"),

			events: make(chan struct{}),
		}

		grpcClient := &fakeGRPCClient{
			ctx:    ctx,
			logger: logger.WithName("fakeGRPCClient"),

			events:   make(chan watcher.EventParameters),
			response: pb.ResponseType_OK,
		}

		// gvr for configMap is used to instantiate new dynamic resourceInterface
		gvr := schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "configmaps",
		}

		params := &watcher.Params{
			WatchRequest: v1alpha1.WatchRequest{
				Group:     "",
				Version:   "v1",
				Kind:      "ConfigMap",
				Namespace: testEnv.GetNamespace(),
			},
			InvokeWatcherAgentController: controller.invokeController,
			Clock:                        clock.NewFakeClock(time.Now()),
			ResourceInterface:            testEnv.DynamicClient.Resource(gvr).Namespace(testEnv.GetNamespace()),
		}

		w := watcher.New(ctx, logger, params, grpcClient)

		test = &watchTest{
			ctx:    ctx,
			logger: logger.WithName("watchTest"),

			gvr:    gvr,
			params: params,

			grpcClient: grpcClient,
			controller: controller,
			watcher:    w,
		}

		objects = generateConfigMaps(0, 10)

	})

	AfterEach(func() {
		cancel()
	})

	Describe("without RESET response", func() {
		Context("for non-list events", func() {
			BeforeEach(func() {
				go test.watcher.Run()
				select {
				case <-ctx.Done():
				case <-test.watcher.ResourceVersionCh:
					select {
					case <-ctx.Done():
					case <-test.grpcClient.events:
					}
				case <-test.grpcClient.events:
					select {
					case <-ctx.Done():
					case <-test.watcher.ResourceVersionCh:
					}
				}
				resourceVersionStream = test.createConfigMaps(objects)
			})

			It("should send add event", func() {

				var wg sync.WaitGroup
				wg.Add(2)

				go test.controller.verify(&wg, test.watcher.ResourceVersionCh, resourceVersionStream)
				go test.grpcClient.verify(&wg, pb.EventType_Added, objects...)

				wg.Wait()

				test.deleteConfigMaps(objects)
			})

			It("should send update event", func() {
				// receive create events

				var wg sync.WaitGroup
				wg.Add(2)

				go test.controller.verify(&wg, test.watcher.ResourceVersionCh, resourceVersionStream)
				go test.grpcClient.verify(&wg, pb.EventType_Added, objects...)

				wg.Wait()

				alterConfigMap(objects)

				resourceVersionStream = test.updateConfigMaps(objects)
				wg.Add(2)
				go test.controller.verify(&wg, test.watcher.ResourceVersionCh, resourceVersionStream)
				go test.grpcClient.verify(&wg, pb.EventType_Modified, objects...)
				wg.Wait()

				test.deleteConfigMaps(objects)
			})
			It("should send delete event", func() {
				// receive create events

				var wg sync.WaitGroup
				wg.Add(2)

				go test.controller.verify(&wg, test.watcher.ResourceVersionCh, resourceVersionStream)
				go test.grpcClient.verify(&wg, pb.EventType_Added, objects...)

				wg.Wait()

				test.deleteConfigMaps(objects)
				wg.Add(1)
				go test.grpcClient.verify(&wg, pb.EventType_Deleted, objects...)

				wg.Wait()

			})
		})

		It("should send list event", func() {
			resourceVersionStream = test.createConfigMaps(objects)
			Eventually(resourceVersionStream).Should(BeClosed())
			go test.watcher.Run()

			var wg sync.WaitGroup

			wg.Add(1)
			go test.grpcClient.verify(&wg, pb.EventType_List, objects...)
			wg.Wait()

			test.deleteConfigMaps(objects)
		})
	})
	Context("for RESET response", func() {
		It("should resend list", func() {
			resourceVersionStream = test.createConfigMaps(objects)
			Eventually(resourceVersionStream).Should(BeClosed())

			go test.watcher.Run()

			alterConfigMap(objects)

			// set RESET response
			test.grpcClient.setResponse(pb.ResponseType_RESET)
			// update configmaps
			resourceVersionStream = test.updateConfigMaps(objects)
			Eventually(resourceVersionStream).Should(BeClosed())

			test.grpcClient.setResponse(pb.ResponseType_OK)
			var wg sync.WaitGroup

			// receive list events
			wg.Add(1)
			go test.grpcClient.verify(&wg, pb.EventType_List, objects...)
			wg.Wait()

			test.deleteConfigMaps(objects)
		})
	})

	Context("for err received from grpc send", func() {
		It("should resend list", func() {
			resourceVersionStream = test.createConfigMaps(objects)
			Eventually(resourceVersionStream).Should(BeClosed())

			go test.watcher.Run()

			alterConfigMap(objects)

			// set grpc Err
			test.grpcClient.setError(fmt.Errorf("fake error"))
			// update configmaps
			resourceVersionStream = test.updateConfigMaps(objects)
			Eventually(resourceVersionStream).Should(BeClosed())

			test.grpcClient.setError(nil)
			var wg sync.WaitGroup

			// receive list events
			wg.Add(1)
			go test.grpcClient.verify(&wg, pb.EventType_List, objects...)
			wg.Wait()

			test.deleteConfigMaps(objects)
		})
	})

})
