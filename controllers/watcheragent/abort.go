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
	"github.com/nephio-project/watcher-agent/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
)

// abort halts any active watches and GRPCClient
func (r *Reconciler) abort(instance *v1alpha1.WatcherAgent) error {
	logger := r.Log.WithName("abort").
		WithValues("name", instance.Name, "namespace", instance.Namespace)
	debugLogger := logger.V(1)

	debugLogger.Info("getting list of current watches")
	current := r.WatcherManager.ListWatches(types.NamespacedName{
		Name:      instance.Name,
		Namespace: instance.Namespace,
	})
	debugLogger.Info("got list of current watches", "currentWatchesCount", len(current))

	for req, _ := range current {
		debugLogger.Info("stopping watch", "watchReq", req, "edgewatcher", instance.Spec.EdgeWatcher)
		if err := r.WatcherManager.abortWatch(req, types.NamespacedName{
			Namespace: instance.Namespace,
			Name:      instance.Name,
		}); err != nil {
			logger.Error(err, "error in stopping the watch", "watchReq", req, "edgewatcher", instance.Spec.EdgeWatcher)
			return err
		}
	}

	debugLogger.Info("deleting the grpcClient", "edgewatcher", instance.Spec.EdgeWatcher)
	r.GRPCManager.deleteClient(instance.Spec.EdgeWatcher)
	return nil
}
