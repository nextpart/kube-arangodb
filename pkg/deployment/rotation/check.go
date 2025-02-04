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
// Author Adam Janikowski
//

package rotation

import (
	"github.com/arangodb/kube-arangodb/pkg/apis/deployment"
	api "github.com/arangodb/kube-arangodb/pkg/apis/deployment/v1"
	"github.com/arangodb/kube-arangodb/pkg/backup/utils"
	"github.com/arangodb/kube-arangodb/pkg/util/constants"
	"github.com/arangodb/kube-arangodb/pkg/util/k8sutil"
	inspectorInterface "github.com/arangodb/kube-arangodb/pkg/util/k8sutil/inspector"
	"github.com/rs/zerolog"
	core "k8s.io/api/core/v1"
)

type Mode int

const (
	SkippedRotation Mode = iota
	SilentRotation
	InPlaceRotation
	GracefulRotation
	EnforcedRotation
)

// And returns the higher value of the rotation mode.
func (m Mode) And(b Mode) Mode {
	if m > b {
		return m
	}

	return b
}

// CheckPossible returns true if rotation is possible
func CheckPossible(member api.MemberStatus) bool {
	return !member.Conditions.IsTrue(api.ConditionTypeTerminated)
}

func IsRotationRequired(log zerolog.Logger, cachedStatus inspectorInterface.Inspector, spec api.DeploymentSpec, member api.MemberStatus, group api.ServerGroup, pod *core.Pod, specTemplate, statusTemplate *api.ArangoMemberPodTemplate) (mode Mode, plan api.Plan, reason string, err error) {
	// Determine if rotation is required based on plan and actions

	// Set default mode for return value
	mode = SkippedRotation

	// We are under termination
	if pod != nil {
		if member.Conditions.IsTrue(api.ConditionTypeTerminating) || pod.DeletionTimestamp != nil {
			if l := utils.StringList(pod.Finalizers); l.Has(constants.FinalizerPodGracefulShutdown) && !l.Has(constants.FinalizerDelayPodTermination) {
				reason = "Recreation enforced by deleted state"
				mode = EnforcedRotation
			}

			return
		}
	}

	if !CheckPossible(member) {
		// Check is not possible due to improper state of member
		return
	}

	if spec.MemberPropagationMode.Get() == api.DeploymentMemberPropagationModeAlways && member.Conditions.IsTrue(api.ConditionTypePendingRestart) {
		reason = "Restart is pending"
		mode = EnforcedRotation
		return
	}

	// Check if pod details are propagated
	if pod != nil {
		if member.PodUID != pod.UID {
			reason = "Pod UID does not match, this pod is not managed by Operator. Recreating"
			mode = EnforcedRotation
			return
		}

		if _, ok := pod.Annotations[deployment.ArangoDeploymentPodRotateAnnotation]; ok {
			reason = "Recreation enforced by annotation"
			mode = EnforcedRotation
			return
		}
	}

	if member.PodSpecVersion == "" {
		reason = "Pod Spec Version is nil - recreating pod"
		mode = EnforcedRotation
		return
	}

	if specTemplate == nil || statusTemplate == nil {
		// If spec or status is nil rotation is not needed
		return
	}

	// Check if any of resize events are in place
	if member.Conditions.IsTrue(api.ConditionTypePendingTLSRotation) {
		reason = "TLS Rotation pending"
		mode = EnforcedRotation
		return
	}

	pvc, exists := cachedStatus.PersistentVolumeClaim(member.PersistentVolumeClaimName)
	if exists {
		if k8sutil.IsPersistentVolumeClaimFileSystemResizePending(pvc) {
			reason = "PVC Resize pending"
			mode = EnforcedRotation
			return
		}
	}

	if mode, plan, err := compare(log, spec, member, group, specTemplate, statusTemplate); err != nil {
		return SkippedRotation, nil, "", err
	} else {
		return mode, plan, "Pod needs rotation", nil
	}
}
