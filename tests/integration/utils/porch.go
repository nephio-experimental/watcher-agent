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
	"strings"

	"github.com/go-logr/logr"
	"github.com/nephio-project/common-lib/edge/porch"
	"github.com/nephio-project/watcher-agent/api/v1alpha1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

type FakePorchClient struct {
	logr.Logger
	*ClusterClient
}

func (f *FakePorchClient) ApplyPackage(ctx context.Context, contents map[string]string, packageName, clusterName string) error {
	watcherAgentCRFile, ok := contents["watcheragent.yaml"]
	if !ok {
		return fmt.Errorf("watcheragent.yaml not found")
	}
	var watcherAgentCR v1alpha1.WatcherAgent
	err := yaml.NewYAMLOrJSONDecoder(strings.NewReader(watcherAgentCRFile), 100).Decode(&watcherAgentCR)
	if err != nil {
		return fmt.Errorf("error while decoding watcherAgentCR yaml: %v", err)
	}

	namespace, err := f.GetNamespace(clusterName)
	if err != nil {
		return err
	}

	for i := 0; i < len(watcherAgentCR.Spec.WatchRequests); i++ {
		watcherAgentCR.Spec.WatchRequests[i].Namespace = namespace
	}

	if _, err := f.Apply(ctx, f.Logger, clusterName, &watcherAgentCR, &v1alpha1.WatcherAgent{}); err != nil {
		return fmt.Errorf("error in applying the watcherAgentCR: %v", err)
	}
	return nil
}

var _ porch.Client = &FakePorchClient{}
