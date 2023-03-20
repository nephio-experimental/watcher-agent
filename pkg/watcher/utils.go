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

package watcher

import (
	"context"
	"fmt"

	pb "github.com/nephio-project/edge-watcher/protos"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/errors"
)

// sendEvent sends the event to grpc client from watcherStore and restarts
// reflector in case of errors
func (w *Watcher) sendEvent(ctx context.Context, obj interface{},
	eventType pb.EventType, resourceVersion string) error {
	logger := w.logger.WithName("watcher.sendEvent").WithValues("watchReq", w.WatchRequest, "eventType", eventType)
	debugLogger := logger.V(1)

	debugLogger.Info("converting received object to unstructured.Unstructured")
	data, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		logger.Error(err, "error converting to unstructured.Unstructured")
		if resyncErr := w.ReSync(ctx, "sendEvent"); resyncErr != nil {
			return errors.NewAggregate([]error{err, resyncErr})
		}
		return err
	}
	debugLogger.Info("converted to unstructured.Unstructured")
	u := &unstructured.Unstructured{Object: data}

	reqParams := EventParameters{
		WatchRequest: w.WatchRequest,
		ClusterName:  w.ClusterName,
		Type:         eventType,
		Object:       u,
		TimeStamp:    w.Clock.Now(),
	}

	if eventType != pb.EventType_List {
		resourceVersion = u.GetResourceVersion()
	}

	debugLogger.Info("sending event to grpcClient", "resourceVersion", resourceVersion)

	// TODO: add backoff retries
	resp, err := w.GetGRPCClient().SendEvent(ctx, reqParams)

	if err == nil && resp == pb.ResponseType_OK {
		debugLogger.Info("received OK response")
		debugLogger.Info("sending the new resourceVersion")
		select {
		case <-ctx.Done():
			return ctx.Err()
		case w.ResourceVersionCh <- resourceVersion:
			debugLogger.Info("ResourceVersionCh was empty")
		default:
			<-w.ResourceVersionCh
			w.ResourceVersionCh <- resourceVersion
			debugLogger.Info("ResourceVersionCh was full")
		}
		debugLogger.Info("sent new resourceVersion", "resourceVersion", resourceVersion)
		go w.InvokeWatcherAgentController()
		return nil
	}

	logger.Error(err, "restarting reflector", "response", resp)

	if curErr := w.ReSync(ctx, "sendEvent"); curErr != nil {
		logger.Error(errors.NewAggregate([]error{err, curErr}),
			"error in restarting reflector")
		return err
	}
	debugLogger.Info("restarted reflector")

	if err != nil {
		logger.Error(err, "error received from GRPC client")
		return err
	}

	if resp == pb.ResponseType_RESET {
		debugLogger.Info("reset response received from GRPC client")
		//TODO: use typed error
		return fmt.Errorf("reset response received from edgewatcher")
	}

	debugLogger.Info("unknown response received from GRPC client", "response", resp)
	//TODO: use typed error
	return fmt.Errorf("unknown response received from edgewatcher")
}
