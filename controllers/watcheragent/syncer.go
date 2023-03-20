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

	"github.com/nephio-project/watcher-agent/api/v1alpha1"
	"github.com/nephio-project/watcher-agent/pkg/watcherservice"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

const ClusterNameLabel = "cloud.nephio.org/cluster"

// syncResources creates a diff of the current and requested watches and
// reconciles the differences
func (r *Reconciler) syncResources(ctx context.Context,
	instance *v1alpha1.WatcherAgent) error {
	key := types.NamespacedName{
		Name:      instance.Name,
		Namespace: instance.Namespace,
	}
	logger := r.Log.WithName("syncResources").
		WithValues("name", instance.Name, "namespace", instance.Namespace)
	debugLogger := logger.V(1)

	clusterName, err := watcherservice.GetAnnotationVal(ClusterNameLabel, instance.Annotations)
	if err != nil {
		logger.Error(err, "label not found")
		return err
	}

	requiredMap := make(map[v1alpha1.WatchRequest]bool, len(instance.Spec.WatchRequests))

	debugLogger.Info("getting list of current watches")
	current := r.WatcherManager.ListWatches(key)
	debugLogger.Info("got list of current watches", "currentWatches", current)

	grpcClient, err := r.GRPCManager.getClient(instance.Spec.EdgeWatcher)
	if err != nil {
		return err
	}

	var added, unchanged, removed []v1alpha1.WatchRequest

	debugLogger.Info("updating added and unchanged array")
	for _, watchReq := range instance.Spec.WatchRequests {
		if _, ok := current[watchReq]; !ok {
			debugLogger.Info("adding watchRequest to added list", "watchReq", watchReq)
			added = append(added, watchReq)
		} else {
			debugLogger.Info("adding to watchRequest to unchanged list", "watchReq", watchReq)
			unchanged = append(unchanged, watchReq)
		}
		requiredMap[watchReq] = true
	}

	debugLogger.Info("updating removed array")
	for watchReq, _ := range current {
		if _, ok := requiredMap[watchReq]; !ok {
			debugLogger.Info("adding watchRequest to removed list", "watchReq", watchReq)
			removed = append(removed, watchReq)
		}
	}

	debugLogger.Info("adding new watches")
	for _, watchReq := range added {
		invokeController := func() {
			r.WatchEvents <- event.GenericEvent{
				Object: instance,
			}
		}

		debugLogger.Info("adding watch request", "watchReq", watchReq)
		err := r.WatcherManager.startWatch(watchReq, types.NamespacedName{
			Name: instance.Name, Namespace: instance.Namespace,
		}, grpcClient,
			invokeController, clusterName)

		if err != nil {
			logger.Error(err, "error in starting the watch")
			return err
		}
	}

	debugLogger.Info("removing watches")
	for _, watchReq := range removed {
		debugLogger.Info("removing watchReq", "watchReq", watchReq)
		err := r.WatcherManager.abortWatch(watchReq, key)

		if err != nil {
			logger.Error(err, "error in removing the watch")
			return err
		}
	}

	debugLogger.Info("timstamps", "newTimestamp",
		instance.Spec.ResyncTimestamp.String(), "oldTimestamp", instance.Status.ResyncTimestamp.String())
	// if there is a more recent resync request from edgewatcher than resync all
	// the unchanged watchReqs
	if !instance.Spec.ResyncTimestamp.Equal(&instance.Status.ResyncTimestamp) {
		debugLogger.Info("timestamp updated", "newTimestamp",
			instance.Spec.ResyncTimestamp.String(), "oldTimestamp", instance.Status.ResyncTimestamp.String())
		for _, watchReq := range unchanged {
			debugLogger.Info("restarting watcher",
				"watchReq", watchReq)
			if err := r.WatcherManager.resyncWatch(ctx, watchReq,
				key, grpcClient); err != nil {
				logger.Error(err, "error in restarting watcher", "watchReq", watchReq)
				return err
			}
		}
	}
	return nil
}
