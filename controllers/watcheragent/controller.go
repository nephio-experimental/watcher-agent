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

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/nephio-project/watcher-agent/api/v1alpha1"
)

// Reconciler reconciles a WatcherAgent object
type Reconciler struct {
	client.Client
	Scheme *runtime.Scheme

	GRPCManager    *gRPCManager
	WatcherManager *WatcherManager
	WatchEvents    chan event.GenericEvent

	Log logr.Logger
}

const watcherAgentFinalzer = "monitor.nephio.org/watcherAgent"

//+kubebuilder:rbac:groups=monitor.nephio.org,resources=watcheragents,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=monitor.nephio.org,resources=watcheragents/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=monitor.nephio.org,resources=watcheragents/finalizers,verbs=update
//+kubebuilder:rbac:groups=workload.nephio.org,namespace=nephio-system,resources=upfdeployments;smfdeployments,verbs=get;list;watch

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Log.WithValues("request", req)
	debugLogger := logger.V(1)
	obj := &v1alpha1.WatcherAgent{}

	defer debugLogger.Info("Exit")

	debugLogger.Info("getting the watcherAgent object")
	if err := r.Client.Get(ctx, types.NamespacedName{
		Name:      req.Name,
		Namespace: req.Namespace,
	}, obj); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Error(err, "watcherAgent object not found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "unknown error occurred while retrieving the watcherAgent object")
		return ctrl.Result{}, err
	}
	debugLogger.Info("got watcherAgent object", "obj", obj)

	debugLogger.Info("check if the object is being deleted")
	if obj.ObjectMeta.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(obj, watcherAgentFinalzer) {
			controllerutil.AddFinalizer(obj, watcherAgentFinalzer)
			debugLogger.Info("adding finalizer", "finalizer", watcherAgentFinalzer)
			if err := r.Client.Update(ctx, obj); err != nil {
				logger.Error(err, "error in updating the object after adding finalizer")
				return ctrl.Result{}, err
			}
		}
	} else {
		if controllerutil.ContainsFinalizer(obj, watcherAgentFinalzer) {
			debugLogger.Info("object is being deleted")
			debugLogger.Info("performing cleanup")
			if err := r.abort(obj); err != nil {
				logger.Error(err, "error in cleaning up the watches")
				return ctrl.Result{}, err
			}

			controllerutil.RemoveFinalizer(obj, watcherAgentFinalzer)
			debugLogger.Info("removing finalizer", "finalizer", watcherAgentFinalzer)
			if err := r.Client.Update(ctx, obj); err != nil {
				logger.Error(err, "error in updating the object after removing finalizer")
				return ctrl.Result{}, err
			}
		}

		return ctrl.Result{}, nil
	}

	debugLogger.Info("syncing resources")
	if err := r.syncResources(ctx, obj); err != nil {
		logger.Error(err, "error in syncing resources")
		return ctrl.Result{}, err
	}

	debugLogger.Info("updating status")
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var currObj *v1alpha1.WatcherAgent
		var err error
		debugLogger.Info("generating status")
		if currObj, err = r.updateStatus(ctx, req, obj.Spec.ResyncTimestamp); err != nil {
			logger.Error(err, "error in updating status")
			return err
		}
		debugLogger.Info("updating status", "key", req, "status", currObj.Status.WatchRequests)

		if err := r.Client.Status().Update(ctx, currObj); err != nil {
			logger.Error(err, "error in setting status")
			return err
		}

		return nil
	},
	)

	if err != nil {
		logger.Error(err, "failed to update status")
	} else {
		logger.Info("successfully updated status")
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.WatcherAgent{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Watches(&source.Channel{Source: r.WatchEvents},
			&handler.EnqueueRequestForObject{}).
		Complete(r)
}
