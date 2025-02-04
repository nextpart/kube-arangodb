//
// DISCLAIMER
//
// Copyright 2016-2021 ArangoDB GmbH, Cologne, Germany
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Copyright holder is ArangoDB GmbH, Cologne, Germany
//
// Author Ewout Prangsma
//

package k8sutil

const (
	// Arango constants
	ArangoPort           = 8529
	ArangoSyncMasterPort = 8629
	ArangoSyncWorkerPort = 8729
	ArangoExporterPort   = 9101

	ArangoExporterStatusEndpoint        = "/_api/version"
	ArangoExporterClusterHealthEndpoint = "/_admin/cluster/health"
	ArangoExporterInternalEndpoint      = "/_admin/metrics"
	ArangoExporterInternalEndpointV2    = "/_admin/metrics/v2"
	ArangoExporterDefaultEndpoint       = "/metrics"

	// K8s constants
	ClusterIPNone       = "None"
	TopologyKeyHostname = "kubernetes.io/hostname"

	// Internal constants
	ImageIDAndVersionRole = "id" // Role use by identification pods
)
