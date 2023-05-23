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

package watcherservice

import (
	"fmt"

	pb "github.com/nephio-project/edge-watcher/protos"
	"github.com/nephio-project/watcher-agent/api/v1alpha1"
)

func getPtr[T pb.CRDKind | pb.APIGroup | pb.Version | pb.EventType](x T) *T {
	return &x
}

func generateRequestMetadata(watchReq v1alpha1.WatchRequest) (
	*pb.RequestMetadata, error) {

	requestMetadata := &pb.RequestMetadata{
		Namespace: &watchReq.Namespace,
	}

	switch watchReq.Kind {
	case "UPFDeployment":
		requestMetadata.Kind = getPtr(pb.CRDKind_UPFDeployment)
	case "SMFDeployment":
		requestMetadata.Kind = getPtr(pb.CRDKind_SMFDeployment)
	case "AMFDeployment":
		requestMetadata.Kind = getPtr(pb.CRDKind_AMFDeployment)
	default:
		err := fmt.Errorf("invalid object kind: %v", watchReq.Kind)
		return nil, err
	}

	switch watchReq.Group {
	case "workload.nephio.org":
		requestMetadata.Group = getPtr(pb.APIGroup_NFDeployNephioOrg)
	default:
		err := fmt.Errorf("invalid object group: %v", watchReq.Group)
		return nil, err
	}

	switch watchReq.Version {
	case "v1alpha1":
		requestMetadata.Version = getPtr(pb.Version_v1alpha1)
	default:
		err := fmt.Errorf("invalid object version: %v",
			watchReq.Version)
		return nil, err
	}

	return requestMetadata, nil
}
