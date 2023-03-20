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
	"sync"

	"github.com/go-logr/logr"
	"github.com/nephio-project/watcher-agent/api/v1alpha1"
	"github.com/nephio-project/watcher-agent/pkg/watcher"
	"github.com/nephio-project/watcher-agent/pkg/watcherservice"
	"google.golang.org/grpc"
)

type gRPCManager struct {
	ctx    context.Context
	logger logr.Logger

	mu          sync.RWMutex
	clients     map[v1alpha1.EdgeWatcher]*watcherservice.Client
	dialOptions []grpc.DialOption
}

func NewGRPCManager(ctx context.Context, logger logr.Logger,
	dialOptions ...grpc.DialOption) *gRPCManager {
	return &gRPCManager{
		ctx:         ctx,
		logger:      logger.WithName("ClientManager"),
		clients:     make(map[v1alpha1.EdgeWatcher]*watcherservice.Client),
		dialOptions: dialOptions,
	}
}

// getClient retrieves ( or creates ) a watcher.GRPCClient for a
// v1alpha1.EdgeWatcher
func (m *gRPCManager) getClient(ew v1alpha1.EdgeWatcher) (
	watcher.GRPCClient, error) {
	logger := m.logger.WithName("getClient").
		WithValues("addr", ew.Addr, "port", ew.Port)
	debugLogger := logger.V(1)

	m.mu.RLock()
	client, ok := m.clients[ew]
	m.mu.RUnlock()

	if ok {
		return client, nil
	}

	debugLogger.Info("creating new grpc client")
	client, err := watcherservice.NewClient(m.ctx, m.logger, ew.Addr, ew.Port,
		m.dialOptions...)
	if err != nil {
		return nil, err
	}

	debugLogger.Info("created grpc client successfully")
	m.mu.Lock()
	m.clients[ew] = client
	m.mu.Unlock()

	return client, nil
}

// deleteClient stops and deletes a client
func (m *gRPCManager) deleteClient(ew v1alpha1.EdgeWatcher) {
	logger := m.logger.WithName("deleteClient").
		WithValues("addr", ew.Addr, "port", ew.Port)
	defer logger.V(1).Info("deleting client")
	m.mu.Lock()
	defer m.mu.Unlock()

	if client, ok := m.clients[ew]; ok {
		client.Cancel()
	}
	delete(m.clients, ew)
}
