//
// DISCLAIMER
//
// Copyright 2020-2021 ArangoDB GmbH, Cologne, Germany
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
// Author Tomasz Mielech
//

package deployment

import (
	"context"
	"sync"
	"time"

	"github.com/arangodb/kube-arangodb/pkg/util/globals"

	"github.com/arangodb/kube-arangodb/pkg/util/errors"

	"github.com/rs/zerolog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	api "github.com/arangodb/kube-arangodb/pkg/apis/deployment/v1"
	"github.com/arangodb/kube-arangodb/pkg/util"
	"github.com/arangodb/kube-arangodb/pkg/util/arangod"
	"github.com/arangodb/kube-arangodb/pkg/util/k8sutil"
)

// clusterScalingIntegration is a helper to communicate with the clusters
// scaling UI.
type clusterScalingIntegration struct {
	log           zerolog.Logger
	depl          *Deployment
	pendingUpdate struct {
		mutex sync.Mutex
		spec  *api.DeploymentSpec
	}
	lastNumberOfServers struct {
		arangod.NumberOfServers
		mutex sync.Mutex
	}
	scaleEnabled struct {
		mutex   sync.Mutex
		enabled bool
	}
}

const (
	maxClusterBootstrapTime = time.Minute * 2 // Time we allow a cluster bootstrap to take, before we can do cluster inspections.
)

// newClusterScalingIntegration creates a new clusterScalingIntegration.
func newClusterScalingIntegration(depl *Deployment) *clusterScalingIntegration {
	ci := &clusterScalingIntegration{
		log:  depl.deps.Log,
		depl: depl,
	}
	ci.scaleEnabled.enabled = true
	return ci
}

// SendUpdateToCluster records the given spec to be sended to the cluster.
func (ci *clusterScalingIntegration) SendUpdateToCluster(spec api.DeploymentSpec) {
	ci.pendingUpdate.mutex.Lock()
	defer ci.pendingUpdate.mutex.Unlock()
	ci.pendingUpdate.spec = &spec
}

// checkScalingCluster checks if inspection
// returns true if inspection occurred
func (ci *clusterScalingIntegration) checkScalingCluster(ctx context.Context, expectSuccess bool) bool {
	ci.scaleEnabled.mutex.Lock()
	defer ci.scaleEnabled.mutex.Unlock()

	if !ci.depl.config.ScalingIntegrationEnabled {
		return false
	}

	status, _ := ci.depl.GetStatus()

	if !ci.scaleEnabled.enabled {
		// Check if it is possible to turn on scaling without any issue
		if status.Plan.IsEmpty() && ci.setNumberOfServers(ctx) == nil {
			// Scaling should be enabled because there is no Plan.
			// It can happen when the enabling action fails
			ci.scaleEnabled.enabled = true
		}
	}

	if ci.depl.GetPhase() != api.DeploymentPhaseRunning || !ci.scaleEnabled.enabled {
		// Deployment must be in running state and scaling must be enabled
		return false
	}

	// Update cluster with our state
	safeToAskCluster, err := ci.updateClusterServerCount(ctx, expectSuccess)
	if err != nil {
		if expectSuccess {
			ci.log.Debug().Err(err).Msg("Cluster update failed")
		}
	} else if safeToAskCluster {
		// Inspect once
		if err := ci.inspectCluster(ctx, expectSuccess); err != nil {
			if expectSuccess {
				ci.log.Debug().Err(err).Msg("Cluster inspection failed")
			}
		} else {
			return true
		}
	}
	return false
}

// listenForClusterEvents keep listening for changes entered in the UI of the cluster.
func (ci *clusterScalingIntegration) ListenForClusterEvents(stopCh <-chan struct{}) {
	start := time.Now()
	goodInspections := 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for {
		expectSuccess := goodInspections > 0 || time.Since(start) > maxClusterBootstrapTime

		if ci.checkScalingCluster(ctx, expectSuccess) {
			goodInspections++
		}

		select {
		case <-time.After(time.Second * 2):
			// Continue
		case <-stopCh:
			// We're done
			return
		}
	}
}

// Perform a single inspection of the cluster
func (ci *clusterScalingIntegration) inspectCluster(ctx context.Context, expectSuccess bool) error {
	log := ci.log

	ctxChild, cancel := globals.GetGlobalTimeouts().ArangoD().WithTimeout(ctx)
	defer cancel()
	c, err := ci.depl.clientCache.GetDatabase(ctxChild)
	if err != nil {
		return errors.WithStack(err)
	}

	ctxChild, cancel = globals.GetGlobalTimeouts().ArangoD().WithTimeout(ctx)
	defer cancel()
	req, err := arangod.GetNumberOfServers(ctxChild, c.Connection())
	if err != nil {
		if expectSuccess {
			log.Debug().Err(err).Msg("Failed to get number of servers")
		}
		return errors.WithStack(err)
	}
	if req.Coordinators == nil && req.DBServers == nil {
		// Nothing to check
		return nil
	}
	coordinatorsChanged := false
	dbserversChanged := false
	ci.lastNumberOfServers.mutex.Lock()
	defer ci.lastNumberOfServers.mutex.Unlock()
	desired := ci.lastNumberOfServers.NumberOfServers
	if req.Coordinators != nil && desired.Coordinators != nil && req.GetCoordinators() != desired.GetCoordinators() {
		// #Coordinator has changed
		coordinatorsChanged = true
	}
	if req.DBServers != nil && desired.DBServers != nil && req.GetDBServers() != desired.GetDBServers() {
		// #DBServers has changed
		dbserversChanged = true
	}
	if !coordinatorsChanged && !dbserversChanged {
		// if there is nothing to change, check if we never have asked the cluster before
		// if so, fill in the values for the first time.
		// This happens, when the operator is redeployed and there has not been any
		// update events yet.
		if desired.Coordinators == nil || desired.DBServers == nil {
			if req.Coordinators != nil {
				ci.lastNumberOfServers.NumberOfServers.Coordinators = req.Coordinators
			}
			if req.DBServers != nil {
				ci.lastNumberOfServers.NumberOfServers.DBServers = req.DBServers
			}
		}

		// Nothing has changed
		return nil
	}
	// Let's update the spec
	apiObject := ci.depl.apiObject
	ctxChild, cancel = globals.GetGlobalTimeouts().Kubernetes().WithTimeout(ctx)
	defer cancel()
	current, err := ci.depl.deps.DatabaseCRCli.DatabaseV1().ArangoDeployments(apiObject.Namespace).Get(ctxChild, apiObject.Name, metav1.GetOptions{})
	if err != nil {
		return errors.WithStack(err)
	}
	newSpec := current.Spec.DeepCopy()
	if coordinatorsChanged {
		newSpec.Coordinators.Count = util.NewInt(req.GetCoordinators())
	}
	if dbserversChanged {
		newSpec.DBServers.Count = util.NewInt(req.GetDBServers())
	}
	// Validate will additionally check if
	// 		min <= count <= max holds for the given server groups
	if err := newSpec.Validate(); err != nil {
		// Log failure & create event
		log.Warn().Err(err).Msg("Validation of updated spec has failed")
		ci.depl.CreateEvent(k8sutil.NewErrorEvent("Validation failed", err, apiObject))
		// Restore original spec in cluster
		ci.SendUpdateToCluster(current.Spec)
	} else {
		if err := ci.depl.updateCRSpec(ctx, *newSpec); err != nil {
			log.Warn().Err(err).Msg("Failed to update current deployment")
			return errors.WithStack(err)
		}
	}
	return nil
}

// updateClusterServerCount updates the intended number of servers of the cluster.
// Returns true when it is safe to ask the cluster for updates.
func (ci *clusterScalingIntegration) updateClusterServerCount(ctx context.Context, expectSuccess bool) (bool, error) {
	// Any update needed?
	ci.pendingUpdate.mutex.Lock()
	spec := ci.pendingUpdate.spec
	ci.pendingUpdate.mutex.Unlock()
	if spec == nil {
		// Nothing pending
		return true, nil
	}

	log := ci.log
	var coordinatorCountPtr *int
	var dbserverCountPtr *int

	coordinatorCount, dbserverCount := ci.getNumbersOfServers()

	if spec.Coordinators.GetMaxCount() == spec.Coordinators.GetMinCount() {
		coordinatorCountPtr = nil
	} else {
		coordinatorCountPtr = &coordinatorCount
	}

	if spec.DBServers.GetMaxCount() == spec.DBServers.GetMinCount() {
		dbserverCountPtr = nil
	} else {
		dbserverCountPtr = &dbserverCount
	}

	lastNumberOfServers := ci.GetLastNumberOfServers()

	// This is to prevent unneseccary updates that may override some values written by the WebUI (in the case of a update loop)
	if coordinatorCount != lastNumberOfServers.GetCoordinators() || dbserverCount != lastNumberOfServers.GetDBServers() {
		if err := ci.depl.SetNumberOfServers(ctx, coordinatorCountPtr, dbserverCountPtr); err != nil {
			if expectSuccess {
				log.Debug().Err(err).Msg("Failed to set number of servers")
			}
			return false, errors.WithStack(err)
		}
	}

	// Success, now update internal state
	safeToAskCluster := false
	ci.pendingUpdate.mutex.Lock()
	if spec == ci.pendingUpdate.spec {
		ci.pendingUpdate.spec = nil
		safeToAskCluster = true
	}
	ci.pendingUpdate.mutex.Unlock()

	ci.lastNumberOfServers.mutex.Lock()
	defer ci.lastNumberOfServers.mutex.Unlock()

	ci.lastNumberOfServers.Coordinators = &coordinatorCount
	ci.lastNumberOfServers.DBServers = &dbserverCount
	return safeToAskCluster, nil
}

// GetLastNumberOfServers returns the last number of servers
func (ci *clusterScalingIntegration) GetLastNumberOfServers() arangod.NumberOfServers {
	ci.lastNumberOfServers.mutex.Lock()
	defer ci.lastNumberOfServers.mutex.Unlock()

	return ci.lastNumberOfServers.NumberOfServers
}

// DisableScalingCluster disables scaling DBservers and coordinators
func (ci *clusterScalingIntegration) DisableScalingCluster(ctx context.Context) error {
	ci.scaleEnabled.mutex.Lock()
	defer ci.scaleEnabled.mutex.Unlock()

	// Turn off scaling DBservers and coordinators in arangoDB for the UI
	if err := ci.depl.SetNumberOfServers(ctx, nil, nil); err != nil {
		return errors.WithStack(err)
	}

	ci.scaleEnabled.enabled = false
	return nil
}

// EnableScalingCluster enables scaling DBservers and coordinators
func (ci *clusterScalingIntegration) EnableScalingCluster(ctx context.Context) error {
	ci.scaleEnabled.mutex.Lock()
	defer ci.scaleEnabled.mutex.Unlock()

	if ci.scaleEnabled.enabled {
		return nil
	}

	if err := ci.setNumberOfServers(ctx); err != nil {
		return errors.WithStack(err)
	}
	ci.scaleEnabled.enabled = true
	return nil
}

func (ci *clusterScalingIntegration) setNumberOfServers(ctx context.Context) error {
	numOfCoordinators, numOfDBServers := ci.getNumbersOfServers()
	return ci.depl.SetNumberOfServers(ctx, &numOfCoordinators, &numOfDBServers)
}

func (ci *clusterScalingIntegration) getNumbersOfServers() (int, int) {
	status, _ := ci.depl.getStatus()
	return len(status.Members.Coordinators), len(status.Members.DBServers)
}
