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

package v2alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStorageEngineValidate(t *testing.T) {
	// Valid
	assert.Nil(t, StorageEngine("MMFiles").Validate())
	assert.Nil(t, StorageEngine("RocksDB").Validate())

	// Not valid
	assert.Error(t, StorageEngine("").Validate())
	assert.Error(t, StorageEngine(" mmfiles").Validate())
	assert.Error(t, StorageEngine("mmfiles").Validate())
	assert.Error(t, StorageEngine("rocksdb").Validate())
}
