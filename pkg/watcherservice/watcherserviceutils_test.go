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
	"fmt"
	"math/rand"
	"strconv"
	"sync"
	"time"

	"github.com/go-logr/logr"
	pb "github.com/nephio-project/edge-watcher/protos"
	"github.com/nephio-project/nf-deploy-controller/util"
	"github.com/nephio-project/watcher-agent/api/v1alpha1"
	"github.com/nephio-project/watcher-agent/pkg/watcher"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type eventRequestKey struct {
	ClusterName  string
	NFDeployName string
	Namespace    string
	Kind         pb.CRDKind
	Group        pb.APIGroup
	Version      pb.Version
}

type fakeWatcherServiceServer struct {
	pb.UnimplementedWatcherServiceServer

	ctx    context.Context
	logger logr.Logger

	requestStream chan *pb.EventRequest

	mu       sync.Mutex
	errCount int
	errCode  codes.Code
}

func getEventRequestKey(req *pb.EventRequest) eventRequestKey {
	return eventRequestKey{
		ClusterName:  req.Metadata.GetClusterName(),
		NFDeployName: req.Metadata.GetNfdeployName(),
		Namespace:    req.Metadata.Request.GetNamespace(),
		Kind:         req.Metadata.Request.GetKind(),
		Group:        req.Metadata.Request.GetGroup(),
		Version:      req.Metadata.Request.GetVersion(),
	}
}

func getPtr[T pb.CRDKind | pb.APIGroup | pb.Version | pb.EventType | pb.ResponseType | string](val T) *T {
	return &val
}

// ReportEvent fakes the edgewatcher server behavior by sending the incoming
// request to fakeWatcherServiceServer.requestStream
func (server *fakeWatcherServiceServer) ReportEvent(ctx context.Context,
	request *pb.EventRequest) (*pb.EventResponse, error) {
	logger := server.logger.WithName("ReportEvent").WithValues("requestKey", getEventRequestKey(request))
	debugLogger := logger.V(1)

	debugLogger.Info("received EventRequest")
	select {
	case <-ctx.Done():
		logger.Error(ctx.Err(), "request context cancelled")
		return nil, ctx.Err()
	case <-server.ctx.Done():
		logger.Error(server.ctx.Err(), "server context cancelled")
		return nil, server.ctx.Err()
	case server.requestStream <- request:
		debugLogger.Info("sent request to on server.requestStream")
	}

	server.mu.Lock()
	defer server.mu.Unlock()

	if server.errCount == 0 {
		return &pb.EventResponse{
			Response: getPtr(pb.ResponseType_OK),
		}, nil
	}

	server.errCount -= 1
	return &pb.EventResponse{
		Response: getPtr(pb.ResponseType_RESET),
	}, status.Error(server.errCode, "fakeError")
}

// CheckEvents matches incoming pb.EventRequest with given events
func (server *fakeWatcherServiceServer) CheckEvents(wg *sync.WaitGroup, cases []*clientCase) {
	defer GinkgoRecover()
	defer wg.Done()

	logger := server.logger.WithName("CheckEvents").WithValues("count", len(cases))
	debugLogger := logger.V(1)

	requests := make(map[eventRequestKey]*clientCase, len(cases))
	for _, curr := range cases {
		requests[getEventRequestKey(curr.EventRequest)] = curr
	}
	debugLogger.Info("created requests map")

	var currWg sync.WaitGroup
	currWg.Add(len(cases))
	for i := 0; i < len(cases); i++ {
		go func() {
			defer GinkgoRecover()
			defer currWg.Done()

			var key eventRequestKey

			select {
			case <-server.ctx.Done():
				Fail(fmt.Sprintf("server context cancelled: %v", server.ctx.Err()))
			case req := <-server.requestStream:
				key = getEventRequestKey(req)
				Expect(requests).To(HaveKey(key))
				Expect(requests[key].EventRequest.EventTimestamp.AsTime()).
					To(BeTemporally("==", req.EventTimestamp.AsTime()))
				Expect(requests[key].EventRequest.Object).To(Equal(req.Object))
			}
		}()
	}
	debugLogger.Info("waiting on CheckEvents goroutines")
	currWg.Wait()
}

type clientCase struct {
	*watcher.EventParameters
	*pb.EventRequest
}

func generateCases(n int) (cases []*clientCase, err error) {
	for i := 0; i < n; i++ {

		object := &unstructured.Unstructured{}
		object.SetName(strconv.Itoa(i))
		object.SetLabels(map[string]string{
			"cloud.nephio.org/cluster": "cluster1",
			util.NFDeployLabel:         fmt.Sprintf("nfdeploy-%v", i),
		})

		params := &watcher.EventParameters{
			WatchRequest: v1alpha1.WatchRequest{
				Group:     "nfdeploy.nephio.org",
				Version:   "v1alpha1",
				Kind:      "UpfDeploy",
				Namespace: fmt.Sprintf("upf-%d", i),
			},
			ClusterName: "cluster1",
			Type:        pb.EventType_Added,
			Object:      object,
			TimeStamp:   time.Now(),
		}

		params.Type = pb.EventType(rand.Intn(4))

		objectJSON, err := object.MarshalJSON()
		if err != nil {
			return nil, err
		}
		request := &pb.EventRequest{
			Metadata: &pb.Metadata{
				Type: &params.Type,
				Request: &pb.RequestMetadata{
					Namespace: &params.Namespace,
					Kind:      getPtr(pb.CRDKind_UPFDeploy),
					Group:     getPtr(pb.APIGroup_NFDeployNephioOrg),
					Version:   getPtr(pb.Version_v1alpha1),
				},
				ClusterName:  getPtr("cluster1"),
				NfdeployName: getPtr(fmt.Sprintf("nfdeploy-%v", i)),
			},
			EventTimestamp: timestamppb.New(params.TimeStamp),
			Object:         objectJSON,
		}

		if rand.Intn(2) == 0 {
			params.WatchRequest.Kind = "SmfDeploy"
			request.Metadata.Request.Kind = getPtr(pb.CRDKind_SMFDeploy)
		}

		cases = append(cases, &clientCase{
			EventParameters: params,
			EventRequest:    request,
		})
	}
	return
}
