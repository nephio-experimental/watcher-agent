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
	"strconv"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	pb "github.com/nephio-project/edge-watcher/protos"
	"github.com/nephio-project/watcher-agent/pkg/watcher"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func generateConfigMaps(startIndex int, count int) (list []*corev1.ConfigMap) {
	for i := startIndex; i < startIndex+count; i++ {
		list = append(list, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: "configmap" + strconv.Itoa(i),
			},
		})
	}
	return
}

// alterConfigMap modifies the given configMaps to force an update
func alterConfigMap(list []*corev1.ConfigMap) {
	for _, cfg := range list {
		if cfg.Data == nil {
			cfg.Data = make(map[string]string)
		}
		cfg.Data[cfg.Name] = uuid.New().String()
	}
}

type watchTest struct {
	ctx    context.Context
	logger logr.Logger

	gvr    schema.GroupVersionResource
	params *watcher.Params

	grpcClient *fakeGRPCClient
	controller *fakeController
	watcher    *watcher.Watcher
}

// createConfigMaps creates the given configMaps using testEnv.ClientSet and
// sends the resourceVersion of the created object on resourceVersionStream
func (test *watchTest) createConfigMaps(objects []*corev1.ConfigMap) (
	resourceVersionStream <-chan string) {
	logger := test.logger.WithName("createConfigMaps")

	stream := make(chan string, len(objects))
	go func() {
		defer GinkgoRecover()
		defer close(stream)

		for _, object := range objects {
			curObject, err := testEnv.ClientSet.CoreV1().ConfigMaps(test.params.WatchRequest.Namespace).
				Create(test.ctx, object, metav1.CreateOptions{})
			Expect(err).To(BeNil())
			//logger.Info("configmap creation successful", "id", i, "name", object.Name, "namespace", object.Namespace)
			select {
			case <-test.ctx.Done():
				//logger.Error(gctx.Err(), "create group context cancelled")
				Fail("group context cancelled")
			case stream <- curObject.GetResourceVersion():
				logger.Info("created configMap", "name", curObject.Name, "resourceVersion", curObject.GetResourceVersion())
			}
		}
		logger.Info("created configMaps", "list", objects)
	}()

	return stream
}

// createConfigMaps updates the given configMaps using testEnv.ClientSet and
// sends the resourceVersion of the updated object on resourceVersionStream
func (test *watchTest) updateConfigMaps(objects []*corev1.ConfigMap) (
	resourceVersionStream <-chan string) {
	logger := test.logger.WithName("updateConfigMaps")

	stream := make(chan string, len(objects))
	go func() {
		defer GinkgoRecover()
		defer close(stream)

		for _, object := range objects {
			curObject, err := testEnv.ClientSet.CoreV1().ConfigMaps(test.params.WatchRequest.Namespace).
				Update(test.ctx, object, metav1.UpdateOptions{})
			Expect(err).To(BeNil())
			//logger.Info("configmap updation successful", "id", i, "name", object.Name, "namespace", object.Namespace)
			select {
			case <-test.ctx.Done():
				//logger.Error(gctx.Err(), "create group context cancelled")
				Fail("group context cancelled")
			case stream <- curObject.GetResourceVersion():
				logger.Info("updated configMap", "name", curObject.Name, "resourceVersion", curObject.GetResourceVersion())
			}
		}
		logger.Info("updated configMaps", "list", objects)
	}()

	return stream
}

func (test *watchTest) deleteConfigMaps(objects []*corev1.ConfigMap) {
	logger := test.logger.WithName("deleteConfigMaps")

	for _, object := range objects {
		err := testEnv.ClientSet.CoreV1().ConfigMaps(test.params.WatchRequest.Namespace).
			Delete(test.ctx, object.Name, metav1.DeleteOptions{})
		Expect(err).To(BeNil())
		//logger.Info("configmap deletion successful", "id", i, "name", object.Name, "namespace", object.Namespace)
	}
	logger.Info("updated configMaps", "list", objects)
}

type fakeGRPCClient struct {
	ctx    context.Context
	logger logr.Logger

	events chan watcher.EventParameters

	mu       sync.RWMutex
	response pb.ResponseType
	grpcErr  error
}

func (client *fakeGRPCClient) GetServerParams() (addr, port string) {
	return
}

func (client *fakeGRPCClient) setResponse(response pb.ResponseType) {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.response = response
}

func (client *fakeGRPCClient) setError(err error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.grpcErr = err
}

// SendEvent receives the event from watcherStore and sends them on
// fakeGRPCClient.events for fakeGRPCClient.verify to check
func (client *fakeGRPCClient) SendEvent(ctx context.Context,
	params watcher.EventParameters) (pb.ResponseType, error) {
	defer GinkgoRecover()
	logger := client.logger.WithName("SendEvent").WithValues("eventType", params.Type, "watchReq", params.WatchRequest, "timeStamp", params.TimeStamp)
	debugLogger := logger.V(1)

	debugLogger.Info("event received")

	if resp, err := func() (pb.ResponseType, error) {
		client.mu.RLock()
		defer client.mu.RUnlock()

		if client.response == pb.ResponseType_RESET {
			debugLogger.Info("sent RESET response")
			return pb.ResponseType_RESET, client.grpcErr
		}
		if client.grpcErr != nil {
			debugLogger.Info("sent err in response", "err", client.grpcErr)
			return pb.ResponseType_OK, client.grpcErr
		}
		return pb.ResponseType_OK, nil
	}(); resp == pb.ResponseType_RESET || err != nil {
		return resp, err
	}

	select {
	case <-client.ctx.Done():
		logger.Error(client.ctx.Err(), "grpcClient context cancelled")
		return pb.ResponseType_RESET, client.ctx.Err()
	case <-ctx.Done():
		logger.Error(ctx.Err(), "request context cancelled")
		return pb.ResponseType_RESET, ctx.Err()
	case client.events <- params:
		debugLogger.Info("event sent")
	}

	return pb.ResponseType_OK, nil
}

// verify listens on fakeGRPCClient.events for new events and matches them with
// the given configMaps
func (client *fakeGRPCClient) verify(wg *sync.WaitGroup, eventType pb.EventType, objects ...*corev1.ConfigMap) {
	defer GinkgoRecover()
	defer wg.Done()

	logger := client.logger.WithName("verify")
	logger.Info("start receiving events", "count", len(objects))
	defer logger.Info("exit")
	keys := make(map[types.NamespacedName]map[string]string)
	for _, object := range objects {
		keys[types.NamespacedName{
			Name:      object.Name,
			Namespace: object.Namespace,
		}] = object.Data
	}
	for i := 0; i < len(objects); i++ {
		select {
		case <-client.ctx.Done():
			logger.Info("client context cancelled")
			Fail("client context cancelled")
		case event, ok := <-client.events:
			if !ok {
				logger.Info("event stream closed")
				Fail("client event stream closed")
			}
			logger.Info("event received", "id", i, "eventType", event.Type, "objectType", event.Object.GetKind(), "watchReq", event.WatchRequest, "name", event.Object.GetName(), "namespace", event.Object.GetNamespace())
			Expect(event.Type).To(Equal(eventType))
			Expect(event.Object.GetKind()).To(Equal("ConfigMap"))

			configMap := &corev1.ConfigMap{}
			err := runtime.DefaultUnstructuredConverter.FromUnstructured(event.Object.Object, configMap)
			if err != nil {
				panic(err)
			}

			currentKey := types.NamespacedName{
				Name: configMap.Name,
			}
			if configMap.Namespace != "default" {
				currentKey.Namespace = configMap.Namespace
			}
			Expect(keys).Should(HaveKeyWithValue(currentKey, configMap.Data))

			logger.Info("event verified")
		}
	}
}

// fakeController simulates watcherAgent controller's behavior for Watcher
type fakeController struct {
	ctx    context.Context
	logger logr.Logger

	events chan struct{}
}

func (controller *fakeController) invokeController() {
	select {
	case <-controller.ctx.Done():
		return
	case controller.events <- struct{}{}:
	}
}

// verify listens on receivedResourceVersions and correctResourceVersions and
// checks if receivedRV >= expectedRV. This simulates the correct controller
// behavior since Watcher can send new resourceVersion before controller has the
// chance to read the previous one
func (controller *fakeController) verify(wg *sync.WaitGroup,
	receivedResourceVersions, correctResourceVersions <-chan string) {
	defer GinkgoRecover()
	defer wg.Done()

	logger := controller.logger.WithName("verify")
	defer logger.Info("exit")

	logger.Info("start receiving resourceVersions")
	var previousRV string
loop:
	for {
		select {
		case <-controller.ctx.Done():
			Fail("controller context cancelled")
			return
		case rv, ok := <-correctResourceVersions:
			if !ok {
				logger.Info("correctResourceVersion chan closed")
				return
			}

			if previousRV >= rv {
				continue loop
			}

			correctRV, err := strconv.Atoi(rv)
			if err != nil {
				panic(err)
			}

			logger.Info("verify resourceVersion", "version", correctRV)

			select {
			case <-controller.ctx.Done():
				Fail("controller context cancelled")
				return
			case <-controller.events:
				logger.Info("controller invoked")
			case <-time.After(time.Second):
				logger.Info("timed out waiting for events")
				Fail("timed out waiting for events")
			}

			logger.Info("check resourceVersion channel")

			select {
			case receivedRV := <-receivedResourceVersions:
				Expect(strconv.Atoi(receivedRV)).To(BeNumerically(">=", correctRV))
				previousRV = receivedRV
			}

			logger.Info("verified resourceVersion", "version", correctRV)

		}
	}
}
