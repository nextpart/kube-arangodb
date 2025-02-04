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

import "testing"

func TestValidateResourceName(t *testing.T) {
	validNames := []string{
		"foo",
		"name-something.foo",
		"129.abc",
	}
	for _, name := range validNames {
		if err := ValidateResourceName(name); err != nil {
			t.Errorf("Name '%s' is valid, but ValidateResourceName reports '%s'", name, err)
		}
		if err := ValidateOptionalResourceName(name); err != nil {
			t.Errorf("Name '%s' is valid, but ValidateResourceName reports '%s'", name, err)
		}
	}

	invalidNames := []string{
		"",
		"Upper",
		"name_underscore",
	}
	for _, name := range invalidNames {
		if err := ValidateResourceName(name); err == nil {
			t.Errorf("Name '%s' is invalid, but ValidateResourceName reports no error", name)
		}
	}
}
