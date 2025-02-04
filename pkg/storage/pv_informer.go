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

package storage

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/arangodb/kube-arangodb/pkg/util/k8sutil"
)

// listenForPvEvents keep listening for changes in PV's until the given channel is closed.
func (ls *LocalStorage) listenForPvEvents() {
	getPv := func(obj interface{}) (*v1.PersistentVolume, bool) {
		pv, ok := obj.(*v1.PersistentVolume)
		if !ok {
			tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
			if !ok {
				return nil, false
			}
			pv, ok = tombstone.Obj.(*v1.PersistentVolume)
			return pv, ok
		}
		return pv, true
	}

	rw := k8sutil.NewResourceWatcher(
		ls.deps.Log,
		ls.deps.KubeCli.CoreV1().RESTClient(),
		"persistentvolumes",
		"", //ls.apiObject.GetNamespace(),
		&v1.PersistentVolume{},
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				// Ignore
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				if pv, ok := getPv(newObj); ok {
					ls.send(&localStorageEvent{
						Type:             eventPVUpdated,
						PersistentVolume: pv,
					})
				}
			},
			DeleteFunc: func(obj interface{}) {
				// Ignore
			},
		})

	rw.Run(ls.stopCh)
}
