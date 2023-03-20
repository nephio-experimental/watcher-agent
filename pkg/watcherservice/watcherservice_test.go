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

package watcherservice_test

import (
	"context"
	"math/rand"
	"net"
	"sync"

	pb "github.com/nephio-project/edge-watcher/protos"
	"github.com/nephio-project/watcher-agent/pkg/watcher"
	"github.com/nephio-project/watcher-agent/pkg/watcherservice"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const bufSize = 1024 * 1024

var _ = Describe("WatcherService Client", func() {
	var ctx context.Context
	var cancel context.CancelFunc

	var fakeServer *fakeWatcherServiceServer
	var client watcher.GRPCClient
	var cases []*clientCase
	BeforeEach(func() {
		rand.Seed(GinkgoRandomSeed())
		logger := zap.New(func(options *zap.Options) {
			options.Development = true
			options.DestWriter = GinkgoWriter
		})

		ctx, cancel = context.WithCancel(context.Background())
		listener := bufconn.Listen(bufSize)
		fakeServer = &fakeWatcherServiceServer{
			ctx:    ctx,
			logger: logger.WithName("fakeServer"),

			requestStream: make(chan *pb.EventRequest),
		}

		s := grpc.NewServer()
		pb.RegisterWatcherServiceServer(s, fakeServer)
		go func() {
			<-ctx.Done()
			s.GracefulStop()
		}()

		go func() {
			defer GinkgoRecover()
			err := s.Serve(listener)
			Expect(err).To(BeNil())
		}()

		var err error
		client, err = watcherservice.NewClient(ctx, logger, "bufnet", "port",
			grpc.WithContextDialer(
				func(ctx context.Context, addr string) (net.Conn, error) {
					return listener.Dial()
				}),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		Expect(err).To(BeNil())

		cases, err = generateCases(1000 + 1)
	})

	AfterEach(func() {
		cancel()
	})

	It("should send events", func() {
		var wg sync.WaitGroup
		wg.Add(1)
		go fakeServer.CheckEvents(&wg, cases)

		wg.Add(len(cases))
		for _, curr := range cases {
			go func(curr *clientCase) {
				defer GinkgoRecover()
				defer wg.Done()
				Expect(client.SendEvent(ctx, *curr.EventParameters)).Error().
					ShouldNot(HaveOccurred())
			}(curr)
		}
		wg.Wait()
	})

	Context("edgewatcher returns UNAVAILABLE error", func() {
		It("should retry 5 times", func() {
			fakeServer.errCode = codes.Unavailable
			fakeServer.errCount = 4
			var wg sync.WaitGroup

			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 5; i++ {
					select {
					case <-ctx.Done():
						return
					case <-fakeServer.requestStream:
					}
				}
			}()

			Expect(client.SendEvent(ctx, *cases[0].EventParameters)).Error().
				ShouldNot(HaveOccurred())

			wg.Wait()
		})
		It("should return error after 5 retries", func() {
			fakeServer.errCode = codes.Unavailable
			fakeServer.errCount = 5
			var wg sync.WaitGroup

			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 5; i++ {
					select {
					case <-ctx.Done():
						return
					case <-fakeServer.requestStream:
					}
				}
			}()

			_, err := client.SendEvent(ctx, *cases[0].EventParameters)
			Expect(err).Should(HaveOccurred())

			wg.Wait()
		})
	})
})
