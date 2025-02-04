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

package serviceaccount

import core "k8s.io/api/core/v1"

type Inspector interface {
	ServiceAccount(name string) (*core.ServiceAccount, bool)
	IterateServiceAccounts(action Action, filters ...Filter) error
	ServiceAccountReadInterface() ReadInterface
}

type Filter func(pod *core.ServiceAccount) bool
type Action func(pod *core.ServiceAccount) error
