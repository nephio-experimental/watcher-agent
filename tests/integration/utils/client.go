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

package utils

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-logr/logr"
	"github.com/nephio-project/watcher-agent/tests/integration/environment"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientset "k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ClusterClient struct {
	client.Client
	ClientSet          clientset.Interface
	mu                 sync.RWMutex
	clusterToNamespace map[string]string
}

func NewClusterClient(client client.Client, clientSet clientset.Interface) *ClusterClient {
	return &ClusterClient{
		Client:             client,
		ClientSet:          clientSet,
		clusterToNamespace: make(map[string]string),
	}
}

func (c *ClusterClient) CreateNamespace(baseName string, labels map[string]string) (*corev1.Namespace, error) {
	name := fmt.Sprintf("%s-%s", baseName, environment.RandomAlphabaticalString(8))
	namespaceObj := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "",
			Labels:    labels,
		},
	}
	var got *corev1.Namespace
	var err error
	maxAttempts := 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		got, err = c.ClientSet.CoreV1().Namespaces().Create(context.TODO(), namespaceObj, metav1.CreateOptions{})
		if err != nil {
			if apierrors.IsAlreadyExists(err) {
				// regenerate on conflict
				namespaceObj.Name = fmt.Sprintf("%v-%v", baseName, environment.RandomAlphabaticalString(8))
			}
		} else {
			break
		}
	}
	return got, err
}

func (c *ClusterClient) CreateCluster(ctx context.Context, logger logr.Logger, clusterName string, namespace string) error {
	clusterNamespace, err := c.CreateNamespace(clusterName, nil)
	if err != nil {
		return fmt.Errorf("error in creating a namespace to emulate cluster: %v err: %v", clusterName, err)
	}
	c.SetNamespace(clusterName, clusterNamespace.Name)
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cloud.nephio.org",
		Version: "v1alpha1",
		Kind:    "EdgeCluster",
	})
	u.SetName(clusterName)
	u.SetNamespace(namespace)
	_, err = c.Apply(ctx, logger, clusterName, u, nil, namespace)
	if err != nil {
		return fmt.Errorf("error in creating EdgeCluster: %v, err: %v", clusterName, err)
	}
	return nil
}

func (c *ClusterClient) SetNamespace(clusterName, namespace string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.clusterToNamespace[clusterName] = namespace
}

func (c *ClusterClient) GetNamespace(clusterName string) (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	namespace, ok := c.clusterToNamespace[clusterName]
	if !ok {
		return "", fmt.Errorf("cluster to namespace binding not found for cluster: %v", clusterName)
	}

	return namespace, nil
}

func (c *ClusterClient) Apply(ctx context.Context, logger logr.Logger, clusterName string, object client.Object, oldObject client.Object, namespaces ...string) (resourceVersion string, err error) {
	var namespace string
	if len(namespaces) == 0 {
		namespace, err = c.GetNamespace(clusterName)
		if err != nil {
			return "", err
		}
	} else {
		namespace = namespaces[0]
	}
	object.SetNamespace(namespace)
	if oldObject == nil {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(object.GetObjectKind().GroupVersionKind())
		oldObject = u
	}
	if err := c.Get(ctx, client.ObjectKey{
		Name:      object.GetName(),
		Namespace: object.GetNamespace(),
	}, oldObject); err != nil {
		if !apierrors.IsNotFound(err) {
			return "", fmt.Errorf("unknown error occured in getting object name: %v, namespace: %v, gvk: %v, err: %v",
				object.GetName(), object.GetNamespace(), object.GetObjectKind().GroupVersionKind().String(), err)
		}
		if err := c.Create(ctx, object); err != nil {
			return "", fmt.Errorf("error while creating object name: %v, namespace: %v, gvk: %v, err: %v",
				object.GetName(), object.GetNamespace(), object.GetObjectKind().GroupVersionKind().String(), err)
		}
		logger.Info("create successful", "new resourceversion", object.GetResourceVersion(), "gvr", object.GetObjectKind().GroupVersionKind().String())
		return object.GetResourceVersion(), nil
	}

	object.SetResourceVersion(oldObject.GetResourceVersion())
	if err := c.Update(ctx, object); err != nil {
		return "", fmt.Errorf("error while updating object name: %v, namespace: %v, gvk: %v, err: %v",
			object.GetName(), object.GetNamespace(), object.GetObjectKind().GroupVersionKind().String(), err)
	}

	logger.Info("update successfull", "new resourceversion", object.GetResourceVersion(), "old resourceVersion", oldObject.GetResourceVersion(), "gvr", object.GetObjectKind().GroupVersionKind().String())
	return object.GetResourceVersion(), nil
}
