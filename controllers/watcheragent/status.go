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

	"github.com/nephio-project/watcher-agent/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// findWatchRequestID finds the id of a v1alpha1.WatchRequestStatus entry
// matching a v1alpha1.WatchRequest
func findWatchRequestID(watchRequest v1alpha1.WatchRequest,
	statuses []v1alpha1.WatchRequestStatus) (id int, arr []v1alpha1.WatchRequestStatus) {
	for i, status := range statuses {
		if status.WatchRequest == watchRequest {
			return i, statuses
		}
	}

	statuses = append(statuses, v1alpha1.WatchRequestStatus{
		WatchRequest: watchRequest,
	})
	return len(statuses) - 1, statuses
}

// updateStatus updates the most recently received (if available)
// resourceVersion of all the active watches
func (r *Reconciler) updateStatus(ctx context.Context, req ctrl.Request, syncedTimestamp metav1.Time) (
	*v1alpha1.WatcherAgent, error) {
	logger := r.Log.WithName("updateStatus").
		WithValues("req", req)
	debugLogger := logger.V(1)

	instance := &v1alpha1.WatcherAgent{}
	if err := r.Client.Get(ctx, types.NamespacedName{
		Name:      req.Name,
		Namespace: req.Namespace,
	}, instance); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Error(err, "watcherAgent object not found")
			return nil, err
		}
		logger.Error(err, "unknown error occurred while retrieving the watcherAgent object")
		return nil, err
	}
	debugLogger.Info("got watcherAgent object", "obj", instance)

	debugLogger.Info("getting list of current watches")

	// Anytime any watcher sends a new resourceVersion we retrieve and check every
	// watcher's resourceVersionCh. This is necessary because the Reconcile func
	// is meant to be level triggered. We don't send the Reconcile func reason for
	// the reconcile request.
	// We don't see any scalability concerns with this as of now, since number of
	// resources watched by a single watcheragent is O(100).
	currentWatches := r.WatcherManager.ListWatches(req.NamespacedName)
	var list []v1alpha1.WatchRequest
	for req, _ := range currentWatches {
		list = append(list, req)
	}
	debugLogger.Info("got list of current watches", "currentWatches", list)

	for watchReq, watcher := range currentWatches {
		select {
		case resourceVersion, ok := <-watcher.ResourceVersionCh:
			if !ok {
				err := fmt.Errorf("watcher.resourceversionCh closed req: %v", watchReq)
				logger.Error(err, "unable to set resourceVersion")
			} else {
				debugLogger.Info("set watchReq resourceVersion", "watchReq", watchReq, "resourceVersion", resourceVersion)

				var id int
				id, instance.Status.WatchRequests = findWatchRequestID(watchReq, instance.Status.WatchRequests)
				instance.Status.WatchRequests[id].ResourceVersion = resourceVersion
			}
		default:
		}
	}

	// updateStatus runs after syncResources, hence resync must have been
	// successful. But a new resync request could have occured while we are still
	// processing previous request, so we need to update with the older timestamp.
	instance.Status.ResyncTimestamp = syncedTimestamp

	return instance, nil
}
