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
// Author Adam Janikowski
// Author Tomasz Mielech
//

package reconcile

import (
	"context"

	"github.com/arangodb/kube-arangodb/pkg/util/globals"

	"github.com/arangodb/kube-arangodb/pkg/util"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"

	api "github.com/arangodb/kube-arangodb/pkg/apis/deployment/v1"
	"github.com/arangodb/kube-arangodb/pkg/deployment/pod"
	"github.com/rs/zerolog"
)

func init() {
	registerAction(api.ActionTypeEncryptionKeyStatusUpdate, newEncryptionKeyStatusUpdate)
}

func newEncryptionKeyStatusUpdate(log zerolog.Logger, action api.Action, actionCtx ActionContext) Action {
	a := &encryptionKeyStatusUpdateAction{}

	a.actionImpl = newActionImplDefRef(log, action, actionCtx, defaultTimeout)

	return a
}

type encryptionKeyStatusUpdateAction struct {
	actionImpl

	actionEmptyCheckProgress
}

func (a *encryptionKeyStatusUpdateAction) Start(ctx context.Context) (bool, error) {
	if err := ensureEncryptionSupport(a.actionCtx); err != nil {
		a.log.Error().Err(err).Msgf("Action not supported")
		return true, nil
	}

	ctxChild, cancel := globals.GetGlobalTimeouts().Kubernetes().WithTimeout(ctx)
	defer cancel()

	f, err := a.actionCtx.GetCachedStatus().SecretReadInterface().Get(ctxChild, pod.GetEncryptionFolderSecretName(a.actionCtx.GetAPIObject().GetName()), meta.GetOptions{})
	if err != nil {
		a.log.Error().Err(err).Msgf("Unable to get folder info")
		return true, nil
	}

	keyHashes := secretKeysToListWithPrefix(f)

	if err = a.actionCtx.WithStatusUpdate(ctx, func(s *api.DeploymentStatus) bool {
		if len(keyHashes) == 0 {
			if s.Hashes.Encryption.Keys != nil {
				s.Hashes.Encryption.Keys = nil
				return true
			}

			return false
		}

		if !util.CompareStringArray(keyHashes, s.Hashes.Encryption.Keys) {
			s.Hashes.Encryption.Keys = keyHashes
			return true
		}
		return false
	}); err != nil {
		return false, err
	}

	return true, nil
}
