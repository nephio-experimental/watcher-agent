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

	"github.com/go-logr/logr"
	pb "github.com/nephio-project/edge-watcher/protos"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
)

// watcherStore receives events from cache.Reflector. It implements cache.Store
// (https://pkg.go.dev/k8s.io/client-go/tools/cache#Store) but only Add, Update
// Delete, Replace are used by the
// cache.Reflector(https://pkg.go.dev/k8s.io/client-go/tools/cache#Reflector)
type watcherStore struct {
	ctx    context.Context
	logger logr.Logger

	watcher *Watcher
}

// Add is invoked by cache.Reflector on receiving create events
func (w *watcherStore) Add(obj interface{}) error {
	logger := w.logger.WithName("watcherStore.Add").WithValues("watchReq", w.watcher.WatchRequest)
	debugLogger := logger.V(1)

	debugLogger.Info("got add event")
	err := w.watcher.sendEvent(w.ctx, obj, pb.EventType_Added, "")
	if err != nil {
		logger.Error(err, "error sending add event")
	}
	return err
}

// Update is invoked by cache.Reflector on receiving update events
func (w *watcherStore) Update(obj interface{}) error {
	logger := w.logger.WithName("watcherStore.Update").WithValues("watchReq", w.watcher.WatchRequest)
	debugLogger := logger.V(1)

	debugLogger.Info("got update event")
	err := w.watcher.sendEvent(w.ctx, obj, pb.EventType_Modified, "")
	if err != nil {
		logger.Error(err, "error sending update event")
	}
	return err
}

// Delete is invoked by cache.Reflector on receiving delete events
func (w *watcherStore) Delete(obj interface{}) error {
	logger := w.logger.WithName("watcherStore.Delete").WithValues("watchReq", w.watcher.WatchRequest)
	debugLogger := logger.V(1)

	debugLogger.Info("got delete event")
	err := w.watcher.sendEvent(w.ctx, obj, pb.EventType_Deleted, "")
	if err != nil {
		logger.Error(err, "error delete add event")
	}
	return err
}

// Replace is invoked by cache.Reflector on receiving a new list
func (w *watcherStore) Replace(items []interface{}, resourceVersion string) error {
	logger := w.logger.WithName("watcherStore.Replace").WithValues("watchReq", w.watcher.WatchRequest, "resourceVersion", resourceVersion)
	debugLogger := logger.V(1)

	debugLogger.Info("got list event", "count", len(items))

	for id, obj := range items {
		debugLogger.Info("sending event", "id", id)

		if err := w.watcher.sendEvent(w.ctx, obj, pb.EventType_List, resourceVersion); err != nil {
			logger.Error(err, "error sending list event")
			return err
		}
	}

	if len(items) == 0 {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   w.watcher.Group,
			Version: w.watcher.Version,
			Kind:    w.watcher.Kind,
		})
		u.SetNamespace(w.watcher.Namespace)
		debugLogger.Info("sending empty list event")
		if err := w.watcher.sendEvent(w.ctx, u, pb.EventType_List, resourceVersion); err != nil {
			logger.Error(err, "error sending list event")
			return err
		}
	}
	return nil
}

var _ cache.Store = &watcherStore{}
