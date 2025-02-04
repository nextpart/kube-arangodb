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
//

package features

func init() {
	registerFeature(shortPodNames)
	registerFeature(randomPodNames)
}

var shortPodNames = &feature{
	name:               "short-pod-names",
	description:        "Enable Short Pod Names",
	version:            "3.5.0",
	enterpriseRequired: false,
	enabledByDefault:   false,
}

func ShortPodNames() Feature {
	return shortPodNames
}

var randomPodNames = &feature{
	name:        "random-pod-names",
	description: "Enables generating random pod names",
}

func RandomPodNames() Feature {
	return randomPodNames
}
