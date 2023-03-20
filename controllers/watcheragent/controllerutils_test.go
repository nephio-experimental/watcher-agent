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

	"github.com/google/uuid"
	"github.com/nephio-project/nf-deploy-controller/util"
	"github.com/nephio-project/watcher-agent/tests/integration/environment"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
	"k8s.io/apimachinery/pkg/types"

	"github.com/go-logr/logr"
	free5gctypes "github.com/nephio-project/common-lib/ausf"
	nfdeployv1alpha1 "github.com/nephio-project/common-lib/nfdeploy"
	edgewatcher "github.com/nephio-project/edge-watcher"
	"github.com/nephio-project/edge-watcher/preprocessor"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type edgeWatcherHandler struct {
	parentCtx context.Context

	logger logr.Logger
	cancel context.CancelFunc
	edgewatcher.Params
	edgewatcher.EventPublisher

	mu  sync.RWMutex
	lis *bufconn.Listener

	singleInstanceToken chan struct{}
}

func newEdgeWatcherHandler(ctx context.Context, logger logr.Logger, params edgewatcher.Params) *edgeWatcherHandler {
	handler := &edgeWatcherHandler{
		parentCtx:           ctx,
		logger:              logger,
		Params:              params,
		singleInstanceToken: make(chan struct{}, 1),
	}
	handler.singleInstanceToken <- struct{}{}
	return handler
}

func (e *edgeWatcherHandler) GetListener() *bufconn.Listener {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.lis
}

func (e *edgeWatcherHandler) SetListner(lis *bufconn.Listener) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.lis = lis
}

func (e *edgeWatcherHandler) Start() {
	<-e.singleInstanceToken

	var ctx context.Context
	ctx, e.cancel = context.WithCancel(e.parentCtx)

	e.SetListner(bufconn.Listen(bufSize))

	var err error
	e.GRPCServer = grpc.NewServer()
	e.EventPublisher, err = edgewatcher.New(ctx, e.logger, e.Params)

	go func() {
		GinkgoRecover()
		err = e.GRPCServer.Serve(e.GetListener())
		Expect(err).To(BeNil(), fmt.Sprintf("edgewatcher's GRPCServer returned error"))

		e.singleInstanceToken <- struct{}{}
	}()
}

func (e *edgeWatcherHandler) subscribe(ctx context.Context, opts edgewatcher.EventOptions, subscriberName ...string) (*edgewatcher.SubscriptionReq, error) {
	eventStream := make(chan preprocessor.Event)
	errStream := make(chan error)
	if len(subscriberName) == 0 {
		subscriberName = []string{uuid.NewString()}
	}
	subscribeReq := &edgewatcher.SubscriptionReq{
		Ctx:          ctx,
		Error:        errStream,
		EventOptions: opts,
		SubscriberInfo: edgewatcher.SubscriberInfo{
			SubscriberName: subscriberName[0],
			Channel:        eventStream,
		},
	}

	logger := e.logger.WithName("subscribe").WithValues("eventOptions", opts, "requestId", subscribeReq.SubscriberName)
	debugLogger := logger.V(1)

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case e.Subscribe() <- subscribeReq:
	}
	Eventually(errStream).Should(Receive(nil))
	debugLogger.Info("sent subscribe request")
	return subscribeReq, nil
}

func (e *edgeWatcherHandler) Stop() {
	e.cancel()
	e.GRPCServer.Stop()
}

func (e *edgeWatcherHandler) Restart() {
	e.Stop()
	e.Start()
}

const (
	upfNameFormat  = "upfdeploy%v"
	ausfNameFormat = "ausfdeploy%v"
)

type clusterTestCase struct {
	upfs           map[types.NamespacedName][]*nfdeployv1alpha1.UpfDeploy
	ausfs          map[types.NamespacedName][]*free5gctypes.AusfDeploy
	upfAckStreams  map[types.NamespacedName]chan string
	ausfAckStreams map[types.NamespacedName]chan string
}

func generateClusterCase(nfCount, eventCount int) *clusterTestCase {
	var cases *clusterTestCase
	cases = &clusterTestCase{
		upfs:           make(map[types.NamespacedName][]*nfdeployv1alpha1.UpfDeploy, nfCount),
		ausfs:          make(map[types.NamespacedName][]*free5gctypes.AusfDeploy, nfCount),
		upfAckStreams:  make(map[types.NamespacedName]chan string, nfCount),
		ausfAckStreams: make(map[types.NamespacedName]chan string, nfCount),
	}

	for j := 0; j < nfCount; j++ {
		upfName := fmt.Sprintf(upfNameFormat, j)
		ausfName := fmt.Sprintf(ausfNameFormat, j)
		upfList := make([]*nfdeployv1alpha1.UpfDeploy, eventCount)
		ausfList := make([]*free5gctypes.AusfDeploy, eventCount)

		prevUpf := &nfdeployv1alpha1.UpfDeploy{ObjectMeta: metav1.ObjectMeta{
			Name: upfName,
			Labels: map[string]string{
				util.NFDeployLabel: "nf1",
			},
		}}

		prevAusf := &free5gctypes.AusfDeploy{ObjectMeta: metav1.ObjectMeta{
			Name: ausfName,
			Labels: map[string]string{
				util.NFDeployLabel: "nf1",
			},
		}}
		for k := 0; k < eventCount; k++ {
			curUpf := prevUpf.DeepCopy()
			curUpf.Spec.N3Interfaces = append(curUpf.Spec.N3Interfaces, nfdeployv1alpha1.InterfaceConfig{
				Name:   environment.RandomAlphabaticalString(10),
				IpAddr: []string{"0.0.0.0"},
				Vlan:   []string{"vlan"},
			})
			upfList[k] = curUpf
			prevUpf = curUpf

			curAusf := prevAusf.DeepCopy()
			curAusf.Spec.NfInfo.Version += environment.RandomAlphabaticalString(5)
			ausfList[k] = curAusf
			prevAusf = curAusf
		}

		cases.upfs[types.NamespacedName{
			Name: upfName,
		}] = upfList
		cases.ausfs[types.NamespacedName{
			Name: ausfName,
		}] = ausfList
		cases.upfAckStreams[types.NamespacedName{
			Name: upfName,
		}] = make(chan string)
		cases.ausfAckStreams[types.NamespacedName{
			Name: ausfName,
		}] = make(chan string)
	}

	return cases
}
