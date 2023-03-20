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

package watcheragent

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-logr/logr"
	"github.com/nephio-project/watcher-agent/api/v1alpha1"
	"github.com/nephio-project/watcher-agent/pkg/watcher"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/clock"
)

// WatcherManager manages different watchers based on the received WatchRequests
// There is one-to-one mapping between watchers and WatchRequests.
type WatcherManager struct {
	ctx    context.Context
	logger logr.Logger

	mu         sync.RWMutex
	watchers   map[types.NamespacedName]map[v1alpha1.WatchRequest]*watcher.Watcher
	k8sClient  dynamic.Interface
	restMapper meta.RESTMapper
	clock      clock.Clock
}

// getResourceInterface instantiates a new dynamic.ResourceInterface set to
// watch resource in a WatchRequest
func (m *WatcherManager) getResourceInterface(logger logr.Logger,
	req v1alpha1.WatchRequest) (dynamic.ResourceInterface, error) {
	debugLogger := logger.V(1)
	debugLogger.Info("getting GVR")
	gvr, err := m.findGVR(req)
	if err != nil {
		return nil, err
	}
	debugLogger.Info("got GVR", "gvr", gvr)

	if len(req.Namespace) == 0 {
		return m.k8sClient.Resource(*gvr), nil
	}
	return m.k8sClient.Resource(*gvr).Namespace(req.Namespace), nil
}

func NewManager(ctx context.Context, logger logr.Logger, client dynamic.Interface,
	restMapper meta.RESTMapper, clock clock.Clock) *WatcherManager {
	return &WatcherManager{
		ctx:    ctx,
		logger: logger.WithName("WatcherManager"),

		watchers:   make(map[types.NamespacedName]map[v1alpha1.WatchRequest]*watcher.Watcher),
		k8sClient:  client,
		restMapper: restMapper,
		clock:      clock,
	}
}

// findGVR returns the GroupVersionResource corresponding to a given
// GroupVersionKind
func (m *WatcherManager) findGVR(req v1alpha1.WatchRequest) (
	*schema.GroupVersionResource, error) {
	logger := m.logger.WithName("findGVR").WithValues("watchReq", req)
	debugLogger := logger.V(1)

	debugLogger.Info("getting GVR")
	restMapping, err := m.restMapper.RESTMapping(schema.GroupKind{
		Group: req.Group,
		Kind:  req.Kind,
	}, req.Version)

	if err != nil {
		logger.Error(err, "error in getting GVR")
		return nil, err
	}

	debugLogger.Info("got GVR", "gvr", restMapping.Resource)
	return &restMapping.Resource, nil
}

func (m *WatcherManager) getWatcher(req v1alpha1.WatchRequest,
	key types.NamespacedName) (*watcher.Watcher, error) {
	logger := m.logger.WithName("getWatcher").WithValues("watchReq", req, "key", key)
	debugLogger := logger.V(1)

	debugLogger.Info("getting watcher")
	m.mu.RLock()
	defer m.mu.RUnlock()
	if requests, ok := m.watchers[key]; ok {
		if w, ok := requests[req]; ok {
			debugLogger.Info("retrieved the watcher")
			return w, nil
		}
		//TODO: use typed error
		err := fmt.Errorf("watchRequest not found")
		logger.Error(err, "watcher not found")
		return nil, err
	}
	err := fmt.Errorf("edgewatcher not found")
	logger.Error(err, "watcher not found")
	return nil, err
}

func (m *WatcherManager) setWatcher(req v1alpha1.WatchRequest,
	key types.NamespacedName, w *watcher.Watcher) {
	logger := m.logger.WithName("setWatcher").WithValues("watchReq", req, "key", key)
	debugLogger := logger.V(1)

	debugLogger.Info("storing watcher")
	defer debugLogger.Info("stored watcher")
	m.mu.Lock()
	defer m.mu.Unlock()
	requests, ok := m.watchers[key]
	if !ok {
		requests = make(map[v1alpha1.WatchRequest]*watcher.Watcher)
	}
	requests[req] = w
	m.watchers[key] = requests
}

func (m *WatcherManager) removeWatcher(req v1alpha1.WatchRequest,
	key types.NamespacedName) (*watcher.Watcher, error) {
	logger := m.logger.WithName("removeWatcher").WithValues("watchReq", req, "key", key)
	debugLogger := logger.V(1)

	debugLogger.Info("removing watcher")
	m.mu.Lock()
	defer m.mu.Unlock()
	requests, ok := m.watchers[key]
	if !ok {
		err := fmt.Errorf("edgewatcher not found")
		logger.Error(err, "error in removing watcher")
		return nil, err
	}
	w, ok := requests[req]
	if !ok {
		err := fmt.Errorf("watchRequest not found")
		logger.Error(err, "error in removing watcher")
		return nil, err
	}
	delete(requests, req)

	if len(requests) != 0 {
		m.watchers[key] = requests
	} else {
		delete(m.watchers, key)
	}

	debugLogger.Info("removed watcher")
	return w, nil
}

// ListWatches returns currently active watchers mapped to corresponding watchReq
func (m *WatcherManager) ListWatches(key types.NamespacedName) map[v1alpha1.
	WatchRequest]*watcher.Watcher {
	logger := m.logger.WithName("ListWatches").WithValues("key", key)
	debugLogger := logger.V(1)

	debugLogger.Info("listing watches")
	m.mu.RLock()
	defer m.mu.RUnlock()
	requests, ok := m.watchers[key]
	if !ok {
		return nil
	}
	copyRequests := make(map[v1alpha1.WatchRequest]*watcher.Watcher, len(m.watchers))
	for req, w := range requests {
		copyRequests[req] = w
	}
	return copyRequests
}

// startWatch creates, starts, stores a new Watcher in case one with same
// v1alpha1.WatchRequest and v1alpha1.Edgewatcher is not already running
func (m *WatcherManager) startWatch(req v1alpha1.WatchRequest, key types.NamespacedName,
	grpcClient watcher.GRPCClient, invokeController func(), clusterName string) error {
	logger := m.logger.WithName("startWatch").WithValues("key", key, "watchReq", req)
	debugLogger := logger.V(1)

	debugLogger.Info("starting watch")

	debugLogger.Info("checking existing watch")
	if _, err := m.getWatcher(req, key); err == nil {
		err := fmt.Errorf("duplicate watch request")
		logger.Error(err, "error in starting watch")
		// TODO: use typed error
		return err
	}
	debugLogger.Info("new watch request")

	resourceInterface, err := m.getResourceInterface(logger, req)
	if err != nil {
		logger.Error(err, "error in generating resource Interface")
		return err
	}

	w := watcher.New(m.ctx, m.logger, &watcher.Params{
		WatchRequest:                 req,
		InvokeWatcherAgentController: invokeController,
		Clock:                        m.clock,
		ResourceInterface:            resourceInterface,
		ClusterName:                  clusterName,
	}, grpcClient)

	debugLogger.Info("created new watcher")

	m.setWatcher(req, key, w)

	go w.Run()
	debugLogger.Info("stored and started the watcher")

	return nil
}

func (m *WatcherManager) resyncWatch(ctx context.Context,
	req v1alpha1.WatchRequest, key types.NamespacedName, grpcClient watcher.GRPCClient) error {
	logger := m.logger.WithName("resyncWatch").WithValues("watchReq", req, "key", key)
	debugLogger := logger.V(1)

	debugLogger.Info("getting watcher")
	m.mu.RLock()
	defer m.mu.RUnlock()
	if requests, ok := m.watchers[key]; ok {
		if w, ok := requests[req]; ok {
			w.SetGRPCClient(grpcClient)
			debugLogger.Info("successfully re-synced watcher")
			if err := w.ReSync(ctx, "controller"); err != nil {
				logger.Error(err, "error in restarting watcher")
				return err
			}
			return nil
		}
		//TODO: use typed error
		err := fmt.Errorf("watchRequest not found")
		logger.Error(err, "couldn't resync watcher")
		return err
	}
	err := fmt.Errorf("edgewatcher not found")
	logger.Error(err, "couldn't resync watcher")
	return err
}

// abortWatch stops and deletes a Watcher by cancelling the context
func (m *WatcherManager) abortWatch(req v1alpha1.WatchRequest,
	key types.NamespacedName) error {
	logger := m.logger.WithName("abortWatch").WithValues("key", key, "watchReq", req)
	debugLogger := logger.V(1)

	debugLogger.Info("removing watcher from map")
	w, err := m.removeWatcher(req, key)
	if err != nil {
		logger.Error(err, "error removing watcher from map")
		return err
	}
	w.Cancel()
	debugLogger.Info("stopped watcher")
	return nil
}
