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

package reconcile

import (
	"context"

	"github.com/arangodb/kube-arangodb/pkg/backup/utils"
	"github.com/arangodb/kube-arangodb/pkg/deployment/features"
	"github.com/arangodb/kube-arangodb/pkg/util/constants"

	api "github.com/arangodb/kube-arangodb/pkg/apis/deployment/v1"
	"github.com/rs/zerolog"
)

func init() {
	registerAction(api.ActionTypeKillMemberPod, newKillMemberPodAction)
}

// newKillMemberPodAction creates a new Action that implements the given
// planned KillMemberPod action.
func newKillMemberPodAction(log zerolog.Logger, action api.Action, actionCtx ActionContext) Action {
	a := &actionKillMemberPod{}

	a.actionImpl = newActionImplDefRef(log, action, actionCtx, defaultTimeout)

	return a
}

// actionKillMemberPod implements an KillMemberPod.
type actionKillMemberPod struct {
	// actionImpl implement timeout and member id functions
	actionImpl
}

// Start performs the start of the action.
// Returns true if the action is completely finished, false in case
// the start time needs to be recorded and a ready condition needs to be checked.
func (a *actionKillMemberPod) Start(ctx context.Context) (bool, error) {
	if !features.GracefulShutdown().Enabled() {
		return true, nil
	}

	log := a.log
	m, ok := a.actionCtx.GetMemberStatusByID(a.action.MemberID)
	if !ok {
		log.Error().Msg("No such member")
		return true, nil
	}

	if err := a.actionCtx.DeletePod(ctx, m.PodName); err != nil {
		log.Error().Err(err).Msg("Unable to kill pod")
		return true, nil
	}

	return false, nil
}

// CheckProgress checks the progress of the action.
// Returns: ready, abort, error.
func (a *actionKillMemberPod) CheckProgress(ctx context.Context) (bool, bool, error) {
	if !features.GracefulShutdown().Enabled() {
		return true, false, nil
	}

	log := a.log
	m, ok := a.actionCtx.GetMemberStatusByID(a.action.MemberID)
	if !ok {
		log.Error().Msg("No such member")
		return true, false, nil
	}

	p, ok := a.actionCtx.GetCachedStatus().Pod(m.PodName)
	if !ok {
		log.Error().Msg("No such member")
		return true, false, nil
	}

	l := utils.StringList(p.Finalizers)

	if !l.Has(constants.FinalizerPodGracefulShutdown) {
		return true, false, nil
	}

	if l.Has(constants.FinalizerDelayPodTermination) {
		return false, false, nil
	}

	return true, false, nil
}
