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

package service

import (
	"context"

	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// ModInterface has methods to work with Service resources only for creation
type ModInterface interface {
	Create(ctx context.Context, service *core.Service, opts meta.CreateOptions) (*core.Service, error)
	Update(ctx context.Context, service *core.Service, opts meta.UpdateOptions) (*core.Service, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts meta.PatchOptions, subresources ...string) (result *core.Service, err error)
	Delete(ctx context.Context, name string, opts meta.DeleteOptions) error
}

// ReadInterface has methods to work with Secret resources with ReadOnly mode.
type ReadInterface interface {
	Get(ctx context.Context, name string, opts meta.GetOptions) (*core.Service, error)
}

// Interface has methods to work with Service resources.
type Interface interface {
	ModInterface
	ReadInterface
}
