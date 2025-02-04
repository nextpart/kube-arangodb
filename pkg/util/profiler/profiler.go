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

package profiler

import (
	"time"

	"github.com/rs/zerolog"
)

// Session is a single timed action
type Session time.Time

// Start a profiling session
func Start() Session {
	return Session(time.Now())
}

// Done with a profiling session, log when time is "long"
func (t Session) Done(log zerolog.Logger, msg string) {
	t.LogIf(log, time.Second/4, msg)
}

// LogIf logs the time taken since the start of the session, if that is longer
// than the given minimum duration.
func (t Session) LogIf(log zerolog.Logger, minLen time.Duration, msg string) {
	interval := time.Since(time.Time(t))
	if interval > minLen {
		log.Debug().Str("time-taken", interval.String()).Msg("profiler: " + msg)
	}
}
